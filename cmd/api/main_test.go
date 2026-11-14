package main

import (
	"net/http"
	"net/http/httptest"
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

func TestGraphQLShellReportsNotImplemented(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	response := httptest.NewRecorder()
	newHandler().ServeHTTP(response, request)

	if response.Code != http.StatusNotImplemented {
		t.Fatalf("expected status %d, got %d", http.StatusNotImplemented, response.Code)
	}
}
