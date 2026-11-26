package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/adapters/httpapi"
	"github.com/shakcho/tacit/internal/platform/memory"
)

const sampleID = "smp_0123456789abcdef0123456789abcdef"

func newHandler() http.Handler {
	return httpapi.NewHandler(memory.NewShellSamples(memory.Limits{Samples: 8, IdempotencyEntries: 16}))
}

type fakeAuthenticator struct {
	error     error
	principal access.LocalPrincipal
	target    access.OrganizationTarget
}

type verifierStub struct{ identity access.VerifiedIdentity }

func (stub verifierStub) Verify(context.Context, access.BearerToken) (access.VerifiedIdentity, error) {
	return stub.identity, nil
}

type resolverStub struct{ admission access.LocalAdmission }

func (stub resolverStub) ResolveIdentity(
	context.Context,
	access.VerifiedIdentity,
	access.OrganizationTarget,
) (access.LocalAdmission, bool, error) {
	return stub.admission, true, nil
}

func (authenticator *fakeAuthenticator) Authenticate(
	_ context.Context,
	_ access.BearerToken,
	target access.OrganizationTarget,
) (access.LocalPrincipal, error) {
	authenticator.target = target
	return authenticator.principal, authenticator.error
}

type fakeSessionDenier struct{ denied bool }

func (denier *fakeSessionDenier) Deny(context.Context, access.BearerToken) error {
	denier.denied = true
	return nil
}

type fakeWebhookVerifier struct{ event access.LifecycleEvent }

func (verifier fakeWebhookVerifier) Verify([]byte, string) (access.LifecycleEvent, error) {
	return verifier.event, nil
}

type fakeLifecycle struct{ applied bool }

func (lifecycle *fakeLifecycle) ApplyLifecycle(context.Context, access.LifecycleEvent) (access.LifecycleOutcome, error) {
	lifecycle.applied = true
	return access.LifecycleApplied, nil
}

func identityHandler(t *testing.T, authenticator *fakeAuthenticator, denier *fakeSessionDenier, lifecycle *fakeLifecycle) http.Handler {
	t.Helper()
	return httpapi.NewHandler(memory.NewShellSamples(memory.Limits{Samples: 8, IdempotencyEntries: 16}),
		httpapi.WithIdentity(httpapi.IdentityDependencies{
			Authenticator:   authenticator,
			Lifecycle:       lifecycle,
			SessionDenier:   denier,
			WebhookVerifier: fakeWebhookVerifier{event: lifecycleEvent(t)},
		}),
		httpapi.WithRealtime(newEventService(t, &eventFeed{}, 4).service),
	)
}

func lifecycleEvent(t *testing.T) access.LifecycleEvent {
	t.Helper()
	namespace, _ := access.ParseIdentityNamespace("workos-test")
	eventID, _ := access.ParseExternalEventID("event_1")
	sessionID, _ := access.ParseExternalSessionID("session_1")
	return access.LifecycleEvent{
		Namespace: namespace, ID: eventID, Kind: access.LifecycleSessionRevoked,
		PayloadHash: sha256.Sum256([]byte("event")), OccurredAt: time.Unix(1, 0), Session: sessionID,
	}
}

func principal(t *testing.T) access.LocalPrincipal {
	t.Helper()
	user, _ := access.ParseUserID("usr_0123456789abcdef0123456789abcdef")
	organization, _ := access.ParseOrganizationID("org_0123456789abcdef0123456789abcdef")
	roles, err := access.NewRoles(access.RoleAdmin, access.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	namespace, _ := access.ParseIdentityNamespace("workos-test")
	subject, _ := access.ParseExternalSubject("user_test")
	externalOrganization, _ := access.ParseExternalOrganizationID("organization_test")
	session, _ := access.ParseExternalSessionID("session_test")
	service := access.NewAuthenticationService(
		verifierStub{identity: access.VerifiedIdentity{
			Namespace: namespace, Subject: subject, Organization: externalOrganization, Session: session,
			IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
		}},
		resolverStub{admission: access.LocalAdmission{User: user, Organization: organization, Roles: roles}},
	)
	token, _ := access.ParseBearerToken("test-token")
	principal, err := service.Authenticate(t.Context(), token, access.TokenOrganization())
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func createRequest(body, key string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/shell-samples", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	return request
}

func TestShellSamplesAreAbsentWithoutAStore(t *testing.T) {
	handler := httpapi.NewHandler(nil)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/shell-samples", nil),
		httptest.NewRequest(http.MethodGet, "/v1/shell-samples/"+sampleID, nil),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s returned %d", request.Method, request.URL.Path, response.Code)
		}
	}
}

func TestCreateReplayAndFindShellSample(t *testing.T) {
	handler := newHandler()
	body := `{"id":"` + sampleID + `","offsetMicroseconds":"1234567"}`

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest(body, "command-1"))
	if created.Code != http.StatusCreated || created.Header().Get("Location") != "/v1/shell-samples/"+sampleID {
		t.Fatalf("create = %d %v %s", created.Code, created.Header(), created.Body.String())
	}
	var result struct {
		Replayed bool `json:"replayed"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.Replayed {
		t.Fatalf("create response = %v, %v", result, err)
	}

	replayed := httptest.NewRecorder()
	handler.ServeHTTP(replayed, createRequest(body, "command-1"))
	if replayed.Code != http.StatusOK || !bytes.Contains(replayed.Body.Bytes(), []byte(`"replayed":true`)) {
		t.Fatalf("replay = %d %s", replayed.Code, replayed.Body.String())
	}

	found := httptest.NewRecorder()
	handler.ServeHTTP(found, httptest.NewRequest(http.MethodGet, "/v1/shell-samples/"+sampleID, nil))
	if found.Code != http.StatusOK || found.Header().Get("Content-Type") != "application/json" || !bytes.Contains(found.Body.Bytes(), []byte(`"offsetMicroseconds":"1234567"`)) {
		t.Fatalf("find = %d %v %s", found.Code, found.Header(), found.Body.String())
	}
}

func TestBoundaryProblems(t *testing.T) {
	tests := []struct {
		name        string
		request     *http.Request
		wantStatus  int
		wantCode    string
		contentType string
	}{
		{"content type", httptest.NewRequest(http.MethodPost, "/v1/shell-samples", strings.NewReader(`{}`)), http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "application/problem+json"},
		{"malformed JSON", createRequest(`{"id":`, "command"), http.StatusBadRequest, "BAD_REQUEST", "application/problem+json"},
		{"unknown field", createRequest(`{"id":"`+sampleID+`","offsetMicroseconds":"0","extra":true}`, "command"), http.StatusBadRequest, "BAD_REQUEST", "application/problem+json"},
		{"invalid ID", createRequest(`{"id":"wrong","offsetMicroseconds":"0"}`, "command"), http.StatusUnprocessableEntity, "INVALID_SAMPLE_ID", "application/problem+json"},
		{"invalid time", createRequest(`{"id":"`+sampleID+`","offsetMicroseconds":"01"}`, "command"), http.StatusUnprocessableEntity, "INVALID_MEDIA_TIME", "application/problem+json"},
		{"missing key", createRequest(`{"id":"`+sampleID+`","offsetMicroseconds":"0"}`, ""), http.StatusUnprocessableEntity, "INVALID_IDEMPOTENCY_KEY", "application/problem+json"},
		{"too large", createRequest(`{"padding":"`+strings.Repeat("x", httpapi.MaxInputBytes)+`"}`, "command"), http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "application/problem+json"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			newHandler().ServeHTTP(response, test.request)
			if response.Code != test.wantStatus || response.Header().Get("Content-Type") != test.contentType || !bytes.Contains(response.Body.Bytes(), []byte(`"code":"`+test.wantCode+`"`)) {
				t.Fatalf("response = %d %v %s", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestConflictsHaveStableCodes(t *testing.T) {
	handler := newHandler()
	first := `{"id":"` + sampleID + `","offsetMicroseconds":"0"}`
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest(first, "same-key"))

	for _, test := range []struct {
		body string
		key  string
		code string
	}{
		{`{"id":"smp_1123456789abcdef0123456789abcdef","offsetMicroseconds":"0"}`, "same-key", "IDEMPOTENCY_CONFLICT"},
		{`{"id":"` + sampleID + `","offsetMicroseconds":"1"}`, "new-key", "SAMPLE_CONFLICT"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, createRequest(test.body, test.key))
		if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte(`"code":"`+test.code+`"`)) {
			t.Fatalf("conflict = %d %s", response.Code, response.Body.String())
		}
	}
}

func TestServesCanonicalOpenAPIDocument(t *testing.T) {
	response := httptest.NewRecorder()
	newHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/yaml" {
		t.Fatalf("specification = %d %v", response.Code, response.Header())
	}
	for _, fragment := range []string{"openapi: 3.1.0", "/v1/auth/session:", "bearerAuth:", "/webhooks/workos:", "/v1/shell-samples:", "operationId: createShellSample", "application/problem+json"} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Errorf("specification does not contain %q", fragment)
		}
	}
	for _, capability := range access.CapabilityCatalog() {
		for _, value := range []string{capability.Action.String(), capability.Resource.String(), capability.Scope.String()} {
			if !strings.Contains(response.Body.String(), value) {
				t.Errorf("OpenAPI does not contain authorization value %q", value)
			}
		}
	}
}

func TestAuthenticationSessionMapsFailuresAndReturnsOnlyLocalIdentity(t *testing.T) {
	tests := []struct {
		name       string
		error      error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid credential", error: access.ErrUnauthenticated, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHENTICATED"},
		{name: "no local membership", error: access.ErrNotAdmitted, wantStatus: http.StatusForbidden, wantCode: "NOT_ADMITTED"},
		{name: "dependency failure", error: access.ErrIdentityUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authenticator := &fakeAuthenticator{error: test.error}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/auth/session", nil)
			request.Header.Set("Authorization", "Bearer token")
			identityHandler(t, authenticator, &fakeSessionDenier{}, &fakeLifecycle{}).ServeHTTP(response, request)
			if response.Code != test.wantStatus || !bytes.Contains(response.Body.Bytes(), []byte(`"code":"`+test.wantCode+`"`)) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}

	authenticator := &fakeAuthenticator{principal: principal(t)}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/auth/session", nil)
	request.Header.Set("Authorization", "Bearer token")
	identityHandler(t, authenticator, &fakeSessionDenier{}, &fakeLifecycle{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"organizationId":"org_0123456789abcdef0123456789abcdef"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"policyRevision":"tacit-authorization-v1"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"action":"manage_members","allowed":true`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"cursor":"v1.`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"revision":"0"`)) ||
		!authenticator.target.UsesTokenOrganization() {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestSessionDenialAndWebhookBoundaries(t *testing.T) {
	authenticator := &fakeAuthenticator{principal: principal(t)}
	denier := &fakeSessionDenier{}
	lifecycle := &fakeLifecycle{}
	handler := identityHandler(t, authenticator, denier, lifecycle)

	denialResponse := httptest.NewRecorder()
	denialRequest := httptest.NewRequest(http.MethodPost, "/internal/auth/session-denials", nil)
	denialRequest.Header.Set("Authorization", "Bearer token")
	handler.ServeHTTP(denialResponse, denialRequest)
	if denialResponse.Code != http.StatusNoContent || !denier.denied {
		t.Fatalf("denial = %d, applied = %v", denialResponse.Code, denier.denied)
	}

	webhookResponse := httptest.NewRecorder()
	webhookRequest := httptest.NewRequest(http.MethodPost, "/webhooks/workos", strings.NewReader(`{"event":"session.revoked"}`))
	webhookRequest.Header.Set("Content-Type", "application/json")
	webhookRequest.Header.Set("WorkOS-Signature", "t=1, v1=fixture")
	handler.ServeHTTP(webhookResponse, webhookRequest)
	if webhookResponse.Code != http.StatusNoContent || !lifecycle.applied {
		t.Fatalf("webhook = %d %s, applied = %v", webhookResponse.Code, webhookResponse.Body.String(), lifecycle.applied)
	}
}

func TestIdentityBoundaryRequiresExactlyOneBearer(t *testing.T) {
	handler := identityHandler(t, &fakeAuthenticator{principal: principal(t)}, &fakeSessionDenier{}, &fakeLifecycle{})
	for _, authorization := range []string{"", "Basic legacy", "Bearer ", "Bearer first second"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/auth/session", nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("authorization %q = %d", authorization, response.Code)
		}
	}
	duplicateResponse := httptest.NewRecorder()
	duplicateRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/session", nil)
	duplicateRequest.Header.Add("Authorization", "Bearer first")
	duplicateRequest.Header.Add("Authorization", "Bearer second")
	handler.ServeHTTP(duplicateResponse, duplicateRequest)
	if duplicateResponse.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate authorization = %d", duplicateResponse.Code)
	}
}

func TestTenantBoundaryRequiresExactLocalOrganization(t *testing.T) {
	authenticator := &fakeAuthenticator{principal: principal(t)}
	handler := identityHandler(t, authenticator, &fakeSessionDenier{}, &fakeLifecycle{})
	body := `{"id":"` + sampleID + `","offsetMicroseconds":"0"}`

	missing := createRequest(body, "tenant-command-missing")
	missing.Header.Set("Authorization", "Bearer token")
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing organization = %d", missingResponse.Code)
	}

	valid := createRequest(body, "tenant-command-valid")
	valid.Header.Set("Authorization", "Bearer token")
	valid.Header.Set("X-Tacit-Organization", principal(t).OrganizationID().String())
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	organization, exact := authenticator.target.Organization()
	if validResponse.Code != http.StatusCreated || !exact || organization != principal(t).OrganizationID() {
		t.Fatalf("tenant request = %d, target = %v, exact = %v", validResponse.Code, organization, exact)
	}
}
