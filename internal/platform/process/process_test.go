package process

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func start(t *testing.T, bound time.Duration, factory HandlerFactory) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, Config{Role: "test", DrainTimeout: bound, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, listener, factory)
	}()
	t.Cleanup(cancel)
	return listener.Addr().String(), cancel, result
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
	ready := make(chan *lifecycle, 1)
	address, cancel, result := start(t, time.Second, func(tracker SocketTracker) http.Handler {
		ready <- tracker.(*lifecycle)
		return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			_, _ = response.Write([]byte("completed"))
		})
	})
	life := <-ready
	responseBody := make(chan string, 1)
	go func() {
		response, err := http.Get("http://" + address + "/graphql")
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
	deadline := time.Now().Add(time.Second)
	for life.ready() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{"/healthz", http.StatusOK, "{\"status\":\"ok\"}\n"},
		{"/readyz", http.StatusServiceUnavailable, "{\"status\":\"unavailable\"}\n"},
		{"/graphql", http.StatusServiceUnavailable, "{\"code\":\"UNAVAILABLE\",\"message\":\"Service unavailable.\"}\n"},
	} {
		response := httptest.NewRecorder()
		life.handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("drain admitted application work") })).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || response.Body.String() != test.body {
			t.Errorf("%s during drain = %d %s", test.path, response.Code, response.Body.String())
		}
	}
	close(release)
	if body := <-responseBody; body != "completed" {
		t.Fatalf("active HTTP response = %q", body)
	}
	awaitResult(t, result, nil)
}

func TestHTTPDrainForceCancelsAtDeadline(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	address, cancel, result := start(t, 50*time.Millisecond, func(SocketTracker) http.Handler {
		return http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			close(started)
			<-request.Context().Done()
			close(canceled)
		})
	})
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := http.Get("http://" + address + "/graphql")
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

func websocket(t *testing.T, address string) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprintf(connection, "GET /graphql HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", address)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols || response.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("WebSocket handshake = %d %v", response.StatusCode, response.Header)
	}
	return connection
}

func TestWebSocketDrain(t *testing.T) {
	for _, graceful := range []bool{true, false} {
		t.Run(fmt.Sprint(graceful), func(t *testing.T) {
			registered := make(chan struct{})
			operationCanceled := make(chan struct{})
			address, cancel, result := start(t, 50*time.Millisecond, func(tracker SocketTracker) http.Handler {
				return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
					connection, buffer, err := http.NewResponseController(response).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					operation, cancelOperation := context.WithCancel(request.Context())
					defer cancelOperation()
					release, accepted := tracker.Register(connection, cancelOperation)
					if !accepted {
						_ = connection.Close()
						return
					}
					defer release()
					defer connection.Close()
					hash := sha1.Sum([]byte(request.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
					_, _ = fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(hash[:]))
					_ = buffer.Flush()
					close(registered)
					<-operation.Done()
					close(operationCanceled)
					if !graceful {
						_, _ = connection.Read(make([]byte, 1))
					}
				})
			})
			connection := websocket(t, address)
			<-registered
			cancel()
			if graceful {
				awaitResult(t, result, nil)
			} else {
				awaitResult(t, result, context.DeadlineExceeded)
			}
			select {
			case <-operationCanceled:
			default:
				t.Fatal("socket operation was not canceled")
			}
			if _, err := connection.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("socket was not closed: %v", err)
			}
		})
	}
}

func TestDrainingRejectsSocketRegistration(t *testing.T) {
	life := newLifecycle()
	life.drain()
	connection, other := net.Pipe()
	defer connection.Close()
	defer other.Close()
	if _, accepted := life.Register(connection, func() {}); accepted {
		t.Fatal("drain accepted socket")
	}
}

func TestRunRejectsInvalidDrainTimeoutBeforeListening(t *testing.T) {
	err := Run(context.Background(), Config{Listen: "127.0.0.1:0"}, func(SocketTracker) http.Handler {
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
