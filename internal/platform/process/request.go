package process

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strconv"
)

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func validRequestID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}

func requestMiddleware(role string, logger *slog.Logger, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		values := request.Header.Values("X-Request-ID")
		var id string
		if len(values) == 1 && validRequestID(values[0]) {
			id = values[0]
		} else {
			var bytes [16]byte
			_, _ = rand.Read(bytes[:])
			id = hex.EncodeToString(bytes[:])
		}
		response.Header().Set("X-Request-ID", id)
		writer := &statusWriter{ResponseWriter: response, status: http.StatusOK}
		handler.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), requestIDKey{}, id)))
		logger.Info("request completed", "role", role, "method", safeMethod(request.Method), "route", route(request.URL.Path), "status_class", strconv.Itoa(writer.status/100)+"xx", "request_id", id)
	})
}

func safeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func route(path string) string {
	switch path {
	case "/healthz", "/readyz", "/graphql":
		return path
	default:
		return "unmatched"
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.wrote {
		return
	}
	writer.wrote = true
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Write(data []byte) (int, error) {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, buffer, err := http.NewResponseController(writer.ResponseWriter).Hijack()
	if err == nil {
		writer.status = http.StatusSwitchingProtocols
		writer.wrote = true
	}
	return connection, buffer, err
}
