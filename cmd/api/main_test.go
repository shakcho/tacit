package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPartialIdentityConfigurationFailsClosed(t *testing.T) {
	t.Setenv("TACIT_DATABASE_URL", "postgres://configured")
	t.Setenv("TACIT_IDENTITY_NAMESPACE", "")
	t.Setenv("TACIT_WORKOS_ISSUER", "")
	t.Setenv("TACIT_WORKOS_CLIENT_ID", "")
	t.Setenv("TACIT_WORKOS_JWKS_URL", "")
	t.Setenv("TACIT_WORKOS_WEBHOOK_SECRET", "")

	handler, closeDependencies, err := newProductionHandler(context.Background())
	if err == nil || handler != nil || closeDependencies != nil {
		t.Fatalf("newProductionHandler() handler_nil=%v close_nil=%v error=%v, want configuration error",
			handler == nil, closeDependencies == nil, err)
	}
}

func TestProductionRequiresIdentityConfiguration(t *testing.T) {
	t.Setenv("TACIT_ENV", "production")
	t.Setenv("TACIT_DATABASE_URL", "")
	t.Setenv("TACIT_IDENTITY_NAMESPACE", "")
	t.Setenv("TACIT_WORKOS_ISSUER", "")
	t.Setenv("TACIT_WORKOS_CLIENT_ID", "")
	t.Setenv("TACIT_WORKOS_JWKS_URL", "")
	t.Setenv("TACIT_WORKOS_WEBHOOK_SECRET", "")

	handler, closeDependencies, err := newProductionHandler(context.Background())
	if err == nil || handler != nil || closeDependencies != nil {
		t.Fatalf("newProductionHandler() handler_nil=%v close_nil=%v error=%v, want production identity error",
			handler == nil, closeDependencies == nil, err)
	}
}

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

func TestAPIPoolMaximum(t *testing.T) {
	for _, raw := range []string{"", "-1", "0", "5", "invalid"} {
		if _, err := apiPoolMaximum(raw, true); err == nil {
			t.Errorf("production accepted pool configuration %q", raw)
		}
	}
	if size, err := apiPoolMaximum("3", true); err != nil || size != 3 {
		t.Fatalf("pool size = %d, %v", size, err)
	}
	if size, err := apiPoolMaximum("", false); err != nil || size != 4 {
		t.Fatalf("development pool size = %d, %v", size, err)
	}
}

func TestProductionDatabaseEndpoints(t *testing.T) {
	t.Setenv("TACIT_DATABASE_POOLER_ENDPOINT", "pooler.example.test:6543")
	t.Setenv("TACIT_DATABASE_POOLER_DATABASE", "tacit")
	t.Setenv("TACIT_DATABASE_POOLER_LOGIN", "tacit_api")
	pool := "postgres://tacit_api:example@pooler.example.test:6543/tacit"
	listener := "postgres://tacit_listener:example@db.example.test:5432/tacit"
	if err := validateDatabaseEndpoints(pool, listener); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", pool, strings.TrimSuffix(listener, "/tacit") + "/other"} {
		if err := validateDatabaseEndpoints(pool, invalid); err == nil {
			t.Error("accepted a missing, pooled, or cross-database listener")
		}
	}
	t.Setenv("TACIT_DATABASE_POOLER_LOGIN", "another_login")
	if err := validateDatabaseEndpoints(pool, listener); err == nil {
		t.Error("accepted a connection to an unbudgeted pool tuple")
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
