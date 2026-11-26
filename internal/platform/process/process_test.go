package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryAddress string

func (address memoryAddress) Network() string { return "memory" }
func (address memoryAddress) String() string  { return string(address) }

type memoryListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newMemoryListener() *memoryListener {
	return &memoryListener{connections: make(chan net.Conn), closed: make(chan struct{})}
}

func (listener *memoryListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

func (listener *memoryListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return nil
}

func (listener *memoryListener) Addr() net.Addr { return memoryAddress("tacit.test") }

func (listener *memoryListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case listener.connections <- server:
		return client, nil
	case <-listener.closed:
		_ = server.Close()
		_ = client.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		_ = server.Close()
		_ = client.Close()
		return nil, ctx.Err()
	}
}

func start(t *testing.T, bound time.Duration, factory HandlerFactory) (*http.Client, context.CancelFunc, <-chan error) {
	t.Helper()
	listener := newMemoryListener()
	transport := &http.Transport{DialContext: listener.dial}
	client := &http.Client{Transport: transport}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, Config{Role: "test", DrainTimeout: bound, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, listener, factory)
	}()
	t.Cleanup(func() {
		cancel()
		client.CloseIdleConnections()
	})
	return client, cancel, result
}

func awaitResult(t *testing.T, result <-chan error, want error) {
	t.Helper()
	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Fatalf("process error = %v; want %v", err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process exceeded shutdown bound")
	}
}

func TestHTTPDrainCompletesActiveRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client, cancel, result := start(t, time.Second, func() http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			_, _ = response.Write([]byte("completed"))
		})
	})
	responseBody := make(chan string, 1)
	go func() {
		response, err := client.Get("http://tacit.test/v1/shell-samples/sample")
		if err != nil {
			responseBody <- err.Error()
			return
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		responseBody <- string(body)
	}()
	<-started
	cancel()
	close(release)
	if body := <-responseBody; body != "completed" {
		t.Fatalf("active HTTP response = %q", body)
	}
	awaitResult(t, result, nil)
}

type unavailableHandler struct{}

func (unavailableHandler) Ready(context.Context) bool                   { return false }
func (unavailableHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestDependencyReadinessDoesNotChangeLiveness(t *testing.T) {
	handler := newLifecycle().handler(unavailableHandler{})
	for path, want := range map[string]int{"/healthz": http.StatusOK, "/readyz": http.StatusServiceUnavailable} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Errorf("%s returned %d, want %d", path, response.Code, want)
		}
	}
}

func TestLifecycleRejectsTrafficDuringDrain(t *testing.T) {
	life := newLifecycle()
	life.drain()
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/healthz", http.StatusOK},
		{"/readyz", http.StatusServiceUnavailable},
		{"/v1/shell-samples/sample", http.StatusServiceUnavailable},
	} {
		response := httptest.NewRecorder()
		life.handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("drain admitted application work") })).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status {
			t.Errorf("%s during drain = %d %s", test.path, response.Code, response.Body.String())
		}
		if test.path == "/healthz" && response.Body.String() != "{\"status\":\"ok\"}\n" {
			t.Errorf("health during drain = %s", response.Body.String())
		}
		if test.path != "/healthz" && (response.Header().Get("Content-Type") != "application/problem+json" || !strings.Contains(response.Body.String(), `"code":"UNAVAILABLE"`)) {
			t.Errorf("%s problem = %v %s", test.path, response.Header(), response.Body.String())
		}
	}
}

func TestHTTPDrainForceCancelsAtDeadline(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	client, cancel, result := start(t, 50*time.Millisecond, func() http.Handler {
		return http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			close(started)
			<-request.Context().Done()
			close(canceled)
		})
	})
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := client.Get("http://tacit.test/v1/shell-samples/sample")
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	<-started
	cancel()
	awaitResult(t, result, context.DeadlineExceeded)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP operation was not canceled")
	}
	<-requestDone
}

func TestRunRejectsInvalidDrainTimeoutBeforeListening(t *testing.T) {
	err := Run(context.Background(), Config{Listen: "127.0.0.1:0"}, func() http.Handler {
		t.Fatal("invalid configuration built a handler")
		return http.NotFoundHandler()
	})
	if err == nil || err.Error() != "drain timeout must be positive" {
		t.Fatalf("Run error = %v", err)
	}
}

func TestProbeResponseContract(t *testing.T) {
	handler := newLifecycle().handler(http.NotFoundHandler())
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != "{\"status\":\"ok\"}\n" {
			t.Fatalf("%s = %d %v %s", path, response.Code, response.Header(), response.Body.String())
		}
	}
}

func TestRequestCorrelationAndRedaction(t *testing.T) {
	for _, headers := range [][]string{{"request-42"}, {"bad id"}, {"one", "two"}, nil} {
		var logs bytes.Buffer
		var contextID string
		handler := requestMiddleware("api", slog.New(slog.NewJSONHandler(&logs, nil)), http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			contextID = RequestID(request.Context())
			response.WriteHeader(http.StatusAccepted)
		}))
		request := httptest.NewRequest("SECRET-METHOD", "/transcript-secret?token=token-secret", strings.NewReader("credential-secret"))
		request.Header.Set("Authorization", "Bearer credential-secret")
		request.Header["X-Request-Id"] = headers
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		id := response.Header().Get("X-Request-ID")
		if !validRequestID(id) || contextID != id {
			t.Fatalf("correlation = %q, %q", id, contextID)
		}
		if len(headers) == 1 && headers[0] == "request-42" && id != "request-42" {
			t.Fatal("valid request ID was replaced")
		}
		var event map[string]any
		if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event["request_id"] != id || event["role"] != "api" || event["method"] != "OTHER" || event["route"] != "unmatched" || event["status_class"] != "2xx" {
			t.Fatalf("log metadata = %v", event)
		}
		for _, secret := range []string{"transcript-secret", "token-secret", "credential-secret", "SECRET-METHOD", "bad id"} {
			if strings.Contains(logs.String(), secret) {
				t.Fatalf("logs contained %q", secret)
			}
		}
	}
}
