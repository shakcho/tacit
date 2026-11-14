package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultListenAddress(t *testing.T) {
	t.Setenv("TACIT_API_ADDRESS", "")
	if got := defaultListenAddress(); got != "127.0.0.1:8080" {
		t.Fatalf("default address = %q", got)
	}
	t.Setenv("TACIT_API_ADDRESS", "127.0.0.1:9876")
	if got := defaultListenAddress(); got != "127.0.0.1:9876" {
		t.Fatalf("environment address = %q", got)
	}
}

func TestOpenAPIControlPlaneShell(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/v1/shell-samples", strings.NewReader(`{"id":"smp_0123456789abcdef0123456789abcdef","offsetMicroseconds":"0"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "main-test")
	response := httptest.NewRecorder()
	newHandler().ServeHTTP(response, request)

	if response.Code != http.StatusCreated || !bytes.Contains(response.Body.Bytes(), []byte(`"replayed":false`)) {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}

	legacy := httptest.NewRecorder()
	newHandler().ServeHTTP(legacy, httptest.NewRequest(http.MethodPost, "/graphql", nil))
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("legacy GraphQL route = %d", legacy.Code)
	}
}
