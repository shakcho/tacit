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

type SocketTracker interface {
	Register(io.Closer, context.CancelFunc) (release func(), accepted bool)
}

type HandlerFactory func(SocketTracker) http.Handler

type phase uint8

const (
	serving phase = iota
	draining
	stopped
)

type socket struct {
	connection io.Closer
	cancel     context.CancelFunc
}

type lifecycle struct {
	mu      sync.Mutex
	phase   phase
	nextID  uint64
	sockets map[uint64]socket
	empty   chan struct{}
}

func newLifecycle() *lifecycle {
	return &lifecycle{sockets: make(map[uint64]socket), empty: make(chan struct{})}
}

func (life *lifecycle) Register(connection io.Closer, cancel context.CancelFunc) (func(), bool) {
	life.mu.Lock()
	defer life.mu.Unlock()
	if life.phase != serving {
		return nil, false
	}
	life.nextID++
	id := life.nextID
	life.sockets[id] = socket{connection: connection, cancel: cancel}
	return sync.OnceFunc(func() {
		life.mu.Lock()
		defer life.mu.Unlock()
		delete(life.sockets, id)
		if life.phase == draining && len(life.sockets) == 0 {
			close(life.empty)
		}
	}), true
}

func (life *lifecycle) ready() bool {
	life.mu.Lock()
	defer life.mu.Unlock()
	return life.phase == serving
}

func (life *lifecycle) drain() {
	life.mu.Lock()
	life.phase = draining
	sockets := make([]socket, 0, len(life.sockets))
	for _, socket := range life.sockets {
		sockets = append(sockets, socket)
	}
	if len(sockets) == 0 {
		close(life.empty)
	}
	life.mu.Unlock()
	for _, socket := range sockets {
		socket.cancel()
	}
}

func (life *lifecycle) closeSockets() {
	life.mu.Lock()
	life.phase = stopped
	sockets := make([]socket, 0, len(life.sockets))
	for _, socket := range life.sockets {
		sockets = append(sockets, socket)
	}
	life.mu.Unlock()
	for _, socket := range sockets {
		_ = socket.connection.Close()
	}
}

func (life *lifecycle) handler(application http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			switch request.URL.Path {
			case "/healthz":
				writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
				return
			case "/readyz":
				if life.ready() {
					writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
				} else {
					writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				}
				return
			}
		}
		if !life.ready() {
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "UNAVAILABLE", "message": "Service unavailable."})
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
	application := factory(life)
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
	httpDrained := make(chan struct{})
	go func() {
		_ = server.Shutdown(drainContext)
		close(httpDrained)
	}()
	socketsDrained := life.empty
	for httpDrained != nil || socketsDrained != nil {
		select {
		case <-httpDrained:
			httpDrained = nil
		case <-socketsDrained:
			socketsDrained = nil
		case <-drainContext.Done():
			cancelRequests()
			_ = server.Close()
			life.closeSockets()
			cfg.Logger.Info("process stopped", "role", cfg.Role, "drained", false)
			return errors.Join(serveError, drainContext.Err())
		}
	}
	life.closeSockets()
	cfg.Logger.Info("process stopped", "role", cfg.Role, "drained", true)
	if errors.Is(serveError, http.ErrServerClosed) {
		return nil
	}
	return serveError
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
