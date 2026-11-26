package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/adapters/httpapi"
	"github.com/shakcho/tacit/internal/adapters/workos"
	"github.com/shakcho/tacit/internal/platform/memory"
	"github.com/shakcho/tacit/internal/platform/postgres"
	"github.com/shakcho/tacit/internal/platform/process"
	"github.com/shakcho/tacit/internal/runtime/realtime"
	"github.com/shakcho/tacit/internal/runtime/shellsample"
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
	err = process.Run(shutdownContext, process.Config{
		Role: "api", Listen: *listenAddress, DrainTimeout: *drainTimeout, Logger: logger,
	}, func() http.Handler { return handler })
	closeDependencies()
	if err != nil {
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
	production := os.Getenv("TACIT_ENV") == "production"
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
		if production {
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
	maxConnections, err := apiPoolMaximum(os.Getenv("TACIT_DATABASE_MAX_CONNECTIONS"), production)
	if err != nil {
		return nil, nil, err
	}
	listenerURL := os.Getenv("TACIT_REALTIME_LISTEN_DATABASE_URL")
	if production {
		if err := validateDatabaseEndpoints(values["database"], listenerURL); err != nil {
			return nil, nil, err
		}
	}
	secret := []byte(os.Getenv("TACIT_REALTIME_CURSOR_KEY"))
	if len(secret) == 0 && !production {
		localKey := sha256.Sum256([]byte("tacit.dev.realtime.key\x00" + values["webhook"]))
		secret = localKey[:]
	}
	codec, err := realtime.NewCursorCodec(secret)
	if err != nil {
		return nil, nil, err
	}
	store, err := postgres.Open(ctx, postgres.Config{
		URL: values["database"], ApplicationName: "tacit-api", MaxConnections: maxConnections, MinConnections: 1,
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
	stopListening, err := store.ListenForRealtime(context.WithoutCancel(ctx), listenerURL, registry)
	if err != nil {
		closeStore()
		return nil, nil, err
	}
	var samples shellsample.Samples
	if !production {
		samples = memory.NewShellSamples(memory.Limits{
			Samples: 256, IdempotencyEntries: 512,
		})
	}
	handler := httpapi.NewHandler(samples, httpapi.WithIdentity(httpapi.IdentityDependencies{
		Authenticator: authenticator, Lifecycle: store, SessionDenier: sessionDenier, WebhookVerifier: webhookVerifier,
	}), httpapi.WithRealtime(realtime.NewService(postgres.NewRealtimeFeed(store), codec, registry)), httpapi.WithReadiness(store.Ready))
	return handler, func() {
		stopListening()
		closeStore()
	}, nil
}

func apiPoolMaximum(raw string, production bool) (int32, error) {
	const maximum = 4
	if raw == "" && !production {
		return maximum, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maximum {
		return 0, fmt.Errorf("TACIT_DATABASE_MAX_CONNECTIONS must be between 1 and %d", maximum)
	}
	return int32(value), nil
}

func validateDatabaseEndpoints(poolURL, listenerURL string) error {
	pool, poolErr := pgx.ParseConfig(poolURL)
	listener, listenerErr := pgx.ParseConfig(listenerURL)
	if poolURL == "" || listenerURL == "" || poolErr != nil || listenerErr != nil {
		return fmt.Errorf("production requires a transaction pool URL and an explicit direct LISTEN URL")
	}
	endpoint := func(config *pgx.ConnConfig) string {
		return net.JoinHostPort(strings.ToLower(config.Host), strconv.Itoa(int(config.Port)))
	}
	if endpoint(pool) != os.Getenv("TACIT_DATABASE_POOLER_ENDPOINT") ||
		pool.Database != os.Getenv("TACIT_DATABASE_POOLER_DATABASE") ||
		pool.User != os.Getenv("TACIT_DATABASE_POOLER_LOGIN") {
		return fmt.Errorf("production transaction pool URL must match the configured pool tuple")
	}
	if endpoint(pool) == endpoint(listener) || pool.Database != listener.Database {
		return fmt.Errorf("production LISTEN requires a distinct direct endpoint for the application database")
	}
	for _, config := range []*pgx.ConnConfig{pool, listener} {
		for _, fallback := range config.Fallbacks {
			if !strings.EqualFold(config.Host, fallback.Host) || config.Port != fallback.Port {
				return fmt.Errorf("production database URLs must each identify one endpoint")
			}
		}
	}
	return nil
}
