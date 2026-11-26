package process

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

type Config struct {
	Role         string
	Listen       string
	DrainTimeout time.Duration
	Logger       *slog.Logger
}

type HandlerFactory func() http.Handler

type phase uint8

const (
	serving phase = iota
	draining
	stopped
)

type lifecycle struct {
	mu    sync.Mutex
	phase phase
}

func newLifecycle() *lifecycle {
	return &lifecycle{}
}

func (life *lifecycle) ready() bool {
	life.mu.Lock()
	defer life.mu.Unlock()
	return life.phase == serving
}

func (life *lifecycle) drain() {
	life.mu.Lock()
	life.phase = draining
	life.mu.Unlock()
}

func (life *lifecycle) stop() {
	life.mu.Lock()
	life.phase = stopped
	life.mu.Unlock()
}

func (life *lifecycle) handler(application http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			switch request.URL.Path {
			case "/healthz":
				writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
				return
			case "/readyz":
				readiness, checksDependencies := application.(interface{ Ready(context.Context) bool })
				if life.ready() && (!checksDependencies || readiness.Ready(request.Context())) && life.ready() {
					writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
				} else {
					writeUnavailable(response, request)
				}
				return
			}
		}
		if !life.ready() {
			writeUnavailable(response, request)
			return
		}
		application.ServeHTTP(response, request)
	})
}

func Run(ctx context.Context, cfg Config, factory HandlerFactory) error {
	if cfg.DrainTimeout <= 0 {
		return errors.New("drain timeout must be positive")
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	return serve(ctx, cfg, listener, factory)
}

func serve(ctx context.Context, cfg Config, listener net.Listener, factory HandlerFactory) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	life := newLifecycle()
	application := factory()
	requestContext, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server := &http.Server{
		Handler:           requestMiddleware(cfg.Role, cfg.Logger, life.handler(application)),
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return requestContext },
		ErrorLog:          slog.NewLogLogger(slog.NewJSONHandler(io.Discard, nil), slog.LevelError),
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()
	cfg.Logger.Info("process listening", "role", cfg.Role)
	var serveError error
	select {
	case serveError = <-serverErrors:
	case <-ctx.Done():
	}

	drainContext, cancel := context.WithTimeout(context.Background(), cfg.DrainTimeout)
	defer cancel()
	life.drain()
	if application, ok := application.(interface{ Drain() }); ok {
		application.Drain()
	}
	httpDrained := make(chan error, 1)
	go func() {
		httpDrained <- server.Shutdown(drainContext)
	}()
	select {
	case shutdownError := <-httpDrained:
		life.stop()
		cfg.Logger.Info("process stopped", "role", cfg.Role, "drained", true)
		if errors.Is(serveError, http.ErrServerClosed) {
			serveError = nil
		}
		return errors.Join(serveError, shutdownError)
	case <-drainContext.Done():
		cancelRequests()
		_ = server.Close()
		life.stop()
		cfg.Logger.Info("process stopped", "role", cfg.Role, "drained", false)
		return errors.Join(serveError, drainContext.Err())
	}
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeUnavailable(response http.ResponseWriter, request *http.Request) {
	value := struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		Status    int    `json:"status"`
		Detail    string `json:"detail"`
		Code      string `json:"code"`
		RequestID string `json:"requestId,omitempty"`
	}{
		Type:      "https://api.tacit.dev/problems/unavailable",
		Title:     "Service unavailable",
		Status:    http.StatusServiceUnavailable,
		Detail:    "The operation cannot be completed by this process.",
		Code:      "UNAVAILABLE",
		RequestID: RequestID(request.Context()),
	}
	response.Header().Set("Content-Type", "application/problem+json")
	response.WriteHeader(value.Status)
	_ = json.NewEncoder(response).Encode(value)
}
