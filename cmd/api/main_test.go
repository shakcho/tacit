package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/healthz", "/readyz"} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			newHandler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
				t.Fatalf("expected application/json, got %q", contentType)
			}
			if body := response.Body.String(); body != "{\"status\":\"ok\"}\n" {
				t.Fatalf("unexpected response body %q", body)
			}
		})
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
