package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/adapters/httpapi"
	"github.com/shakcho/tacit/internal/adapters/workos"
	"github.com/shakcho/tacit/internal/platform/memory"
	"github.com/shakcho/tacit/internal/platform/postgres"
	"github.com/shakcho/tacit/internal/platform/process"
	"github.com/shakcho/tacit/internal/runtime/realtime"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print build version")
	listenAddress := flag.String("listen", defaultListenAddress(), "HTTP listen address")
	drainTimeout := flag.Duration("drain-timeout", 10*time.Second, "shutdown drain bound")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	handler, closeDependencies, err := newProductionHandler(shutdownContext)
	if err != nil {
		logger.Error("API initialization failed", "role", "api")
		os.Exit(1)
	}
	defer closeDependencies()
	if err := process.Run(shutdownContext, process.Config{
		Role: "api", Listen: *listenAddress, DrainTimeout: *drainTimeout, Logger: logger,
	}, func() http.Handler { return handler }); err != nil {
		logger.Error("process failed", "role", "api")
		os.Exit(1)
	}
}

func defaultListenAddress() string {
	if address := os.Getenv("TACIT_API_ADDRESS"); address != "" {
		return address
	}
	return "127.0.0.1:8080"
}

func newHandler() http.Handler {
	return httpapi.NewHandler(memory.NewShellSamples(memory.Limits{
		Samples:            256,
		IdempotencyEntries: 512,
	}))
}

func newProductionHandler(ctx context.Context) (http.Handler, func(), error) {
	values := map[string]string{
		"database":  os.Getenv("TACIT_DATABASE_URL"),
		"namespace": os.Getenv("TACIT_IDENTITY_NAMESPACE"),
		"issuer":    os.Getenv("TACIT_WORKOS_ISSUER"),
		"client":    os.Getenv("TACIT_WORKOS_CLIENT_ID"),
		"jwks":      os.Getenv("TACIT_WORKOS_JWKS_URL"),
		"webhook":   os.Getenv("TACIT_WORKOS_WEBHOOK_SECRET"),
	}
	configured := false
	for _, value := range values {
		configured = configured || value != ""
	}
	if !configured {
		if os.Getenv("TACIT_ENV") == "production" {
			return nil, nil, fmt.Errorf("identity configuration is required in production")
		}
		return newHandler(), func() {}, nil
	}
	for _, value := range values {
		if value == "" {
			return nil, nil, fmt.Errorf("identity configuration is incomplete")
		}
	}
	namespace, err := access.ParseIdentityNamespace(values["namespace"])
	if err != nil {
		return nil, nil, fmt.Errorf("identity namespace is invalid")
	}
	secret := []byte(os.Getenv("TACIT_REALTIME_CURSOR_KEY"))
	if len(secret) == 0 && os.Getenv("TACIT_ENV") != "production" {
		localKey := sha256.Sum256([]byte("tacit.dev.realtime.key\x00" + values["webhook"]))
		secret = localKey[:]
	}
	codec, err := realtime.NewCursorCodec(secret)
	if err != nil {
		return nil, nil, err
	}
	store, err := postgres.Open(ctx, postgres.Config{
		URL: values["database"], ApplicationName: "tacit-api", MaxConnections: 4, MinConnections: 1,
		ConnectTimeout: 5 * time.Second, HealthCheckPeriod: 30 * time.Second, PingTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open identity store: %w", err)
	}
	closeStore := func() { store.Close() }
	verifier, err := workos.NewVerifier(workos.VerifierConfig{
		Namespace: namespace, Issuer: values["issuer"], ClientID: values["client"], JWKSURL: values["jwks"],
	})
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("configure identity verifier: %w", err)
	}
	webhookVerifier, err := workos.NewWebhookVerifier(workos.WebhookConfig{
		Namespace: namespace, Secrets: []string{values["webhook"]},
	})
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("configure identity webhook: %w", err)
	}
	authenticator := access.NewAuthenticationService(verifier, store)
	sessionDenier := access.NewSessionDenialService(verifier, store)
	registry := realtime.NewRegistry(64)
	stopListening, err := store.ListenForRealtime(ctx, os.Getenv("TACIT_REALTIME_LISTEN_DATABASE_URL"), registry)
	if err != nil {
		closeStore()
		return nil, nil, err
	}
	handler := httpapi.NewHandler(memory.NewShellSamples(memory.Limits{
		Samples: 256, IdempotencyEntries: 512,
	}), httpapi.WithIdentity(httpapi.IdentityDependencies{
		Authenticator: authenticator, Lifecycle: store, SessionDenier: sessionDenier, WebhookVerifier: webhookVerifier,
	}), httpapi.WithRealtime(realtime.NewService(postgres.NewRealtimeFeed(store), codec, registry)))
	return handler, func() {
		stopListening()
		closeStore()
	}, nil
}
