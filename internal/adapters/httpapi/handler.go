package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/platform/process"
	"github.com/shakcho/tacit/internal/runtime/realtime"
	"github.com/shakcho/tacit/internal/runtime/shellsample"
)

const MaxInputBytes = 1 << 20
const MaxWebhookBytes = 256 << 10

//go:embed openapi.yaml
var specification embed.FS

type Handler struct {
	authenticator   access.Authenticator
	lifecycle       access.IdentityLifecycle
	samples         shellsample.Samples
	sessionDenier   access.SessionDenier
	webhookVerifier identityWebhookVerifier
	realtime        *realtime.Service
	mux             *http.ServeMux
}

type identityWebhookVerifier interface {
	Verify([]byte, string) (access.LifecycleEvent, error)
}

type IdentityDependencies struct {
	Authenticator   access.Authenticator
	Lifecycle       access.IdentityLifecycle
	SessionDenier   access.SessionDenier
	WebhookVerifier identityWebhookVerifier
}

type Option func(*Handler)

func WithIdentity(dependencies IdentityDependencies) Option {
	return func(handler *Handler) {
		handler.authenticator = dependencies.Authenticator
		handler.lifecycle = dependencies.Lifecycle
		handler.sessionDenier = dependencies.SessionDenier
		handler.webhookVerifier = dependencies.WebhookVerifier
	}
}

func WithRealtime(service *realtime.Service) Option {
	return func(handler *Handler) { handler.realtime = service }
}

func NewHandler(samples shellsample.Samples, options ...Option) *Handler {
	handler := &Handler{samples: samples, mux: http.NewServeMux()}
	for _, option := range options {
		option(handler)
	}
	handler.mux.HandleFunc("GET /openapi.yaml", handler.serveSpecification)
	handler.mux.HandleFunc("GET /v1/auth/session", handler.getAuthenticationSession)
	handler.mux.HandleFunc("GET /v1/events", handler.getEvents)
	handler.mux.HandleFunc("POST /internal/auth/session-denials", handler.denyAuthenticationSession)
	handler.mux.HandleFunc("POST /webhooks/workos", handler.receiveWorkOSWebhook)
	handler.mux.HandleFunc("POST /v1/shell-samples", handler.createShellSample)
	handler.mux.HandleFunc("GET /v1/shell-samples/{sampleId}", handler.getShellSample)
	handler.mux.HandleFunc("/", handler.notFound)
	return handler
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mux.ServeHTTP(response, request)
}

func (handler *Handler) Drain() {
	if handler.realtime != nil {
		handler.realtime.Drain()
	}
}

type createShellSampleInput struct {
	ID                 string `json:"id"`
	OffsetMicroseconds string `json:"offsetMicroseconds"`
}

type shellSampleResponse struct {
	ID                 string `json:"id"`
	OffsetMicroseconds string `json:"offsetMicroseconds"`
}

type createShellSampleResponse struct {
	Sample   shellSampleResponse `json:"sample"`
	Replayed bool                `json:"replayed"`
}

func (handler *Handler) createShellSample(response http.ResponseWriter, request *http.Request) {
	if handler.authenticator != nil {
		if problem := handler.requireTenantPrincipal(request); problem != nil {
			writeProblem(response, request, *problem)
			return
		}
	}
	if problem := requireJSON(request); problem != nil {
		writeProblem(response, request, *problem)
		return
	}

	var input createShellSampleInput
	if problem := decodeJSON(response, request, &input); problem != nil {
		writeProblem(response, request, *problem)
		return
	}
	id, err := shellsample.ParseID(input.ID)
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusUnprocessableEntity, "INVALID_SAMPLE_ID"))
		return
	}
	offset, err := shellsample.ParseMediaTime(input.OffsetMicroseconds)
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusUnprocessableEntity, "INVALID_MEDIA_TIME"))
		return
	}
	keyValues := request.Header.Values("Idempotency-Key")
	if len(keyValues) != 1 {
		writeProblem(response, request, newProblem(http.StatusUnprocessableEntity, "INVALID_IDEMPOTENCY_KEY"))
		return
	}
	key, err := shellsample.ParseIdempotencyKey(keyValues[0])
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusUnprocessableEntity, "INVALID_IDEMPOTENCY_KEY"))
		return
	}

	result, err := handler.samples.Create(request.Context(), shellsample.CreateCommand{ID: id, Offset: offset, Key: key})
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "UNAVAILABLE"))
		return
	}
	sample, ok := result.Sample()
	switch result.Outcome() {
	case shellsample.Created:
		response.Header().Set("Location", "/v1/shell-samples/"+sample.ID.String())
		writeJSON(response, http.StatusCreated, createResponse(sample, false))
	case shellsample.Replayed:
		writeJSON(response, http.StatusOK, createResponse(sample, true))
	case shellsample.IdempotencyConflict:
		writeProblem(response, request, newProblem(http.StatusConflict, "IDEMPOTENCY_CONFLICT"))
	case shellsample.SampleConflict:
		writeProblem(response, request, newProblem(http.StatusConflict, "SAMPLE_CONFLICT"))
	case shellsample.Unavailable:
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "UNAVAILABLE"))
	default:
		if ok {
			writeProblem(response, request, newProblem(http.StatusInternalServerError, "INTERNAL"))
			return
		}
		writeProblem(response, request, newProblem(http.StatusInternalServerError, "INTERNAL"))
	}
}

func (handler *Handler) getShellSample(response http.ResponseWriter, request *http.Request) {
	if handler.authenticator != nil {
		if problem := handler.requireTenantPrincipal(request); problem != nil {
			writeProblem(response, request, *problem)
			return
		}
	}
	id, err := shellsample.ParseID(request.PathValue("sampleId"))
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusUnprocessableEntity, "INVALID_SAMPLE_ID"))
		return
	}
	sample, found, err := handler.samples.Find(request.Context(), id)
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "UNAVAILABLE"))
		return
	}
	if !found {
		writeProblem(response, request, newProblem(http.StatusNotFound, "NOT_FOUND"))
		return
	}
	writeJSON(response, http.StatusOK, sampleResponse(sample))
}

type authenticationSessionResponse struct {
	Authorization  authorizationSnapshotResponse `json:"authorization"`
	OrganizationID string                        `json:"organizationId"`
	Roles          []string                      `json:"roles"`
	UserID         string                        `json:"userId"`
	Cursor         string                        `json:"cursor"`
	Revision       string                        `json:"revision"`
}

type authorizationSnapshotResponse struct {
	PolicyRevision string                          `json:"policyRevision"`
	Decisions      []authorizationDecisionResponse `json:"decisions"`
}

type authorizationDecisionResponse struct {
	Action   string `json:"action"`
	Allowed  bool   `json:"allowed"`
	Resource string `json:"resource"`
	Scope    string `json:"scope"`
	Subject  string `json:"subject"`
}

func (handler *Handler) getAuthenticationSession(response http.ResponseWriter, request *http.Request) {
	principal, problem := handler.authenticate(request, access.TokenOrganization())
	if problem != nil {
		writeProblem(response, request, *problem)
		return
	}
	if handler.realtime == nil {
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"))
		return
	}
	snapshot, cursor, err := handler.realtime.SessionSnapshot(request.Context(), principal)
	if errors.Is(err, access.ErrUnauthenticated) {
		writeProblem(response, request, newProblem(http.StatusUnauthorized, "UNAUTHENTICATED"))
		return
	}
	if errors.Is(err, access.ErrNotAdmitted) {
		writeProblem(response, request, newProblem(http.StatusForbidden, "NOT_ADMITTED"))
		return
	}
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"))
		return
	}
	principal = snapshot.Principal
	roleValues := principal.Roles().Values()
	roles := make([]string, 0, len(roleValues))
	for _, role := range roleValues {
		roles = append(roles, role.String())
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, authenticationSessionResponse{
		Authorization:  authorizationSnapshot(access.ContextDecisions(principal)),
		OrganizationID: principal.OrganizationID().String(),
		Roles:          roles,
		UserID:         principal.UserID().String(),
		Cursor:         cursor,
		Revision:       strconv.FormatInt(snapshot.Point.Position, 10),
	})
}

func authorizationSnapshot(decisions access.DecisionSet) authorizationSnapshotResponse {
	values := decisions.Decisions()
	response := authorizationSnapshotResponse{
		PolicyRevision: access.PolicyRevision,
		Decisions:      make([]authorizationDecisionResponse, 0, len(values)),
	}
	for _, decision := range values {
		response.Decisions = append(response.Decisions, authorizationDecisionResponse{
			Action: decision.Action().String(), Allowed: decision.Allowed(),
			Resource: decision.Resource().String(), Scope: decision.Scope().String(),
			Subject: decision.Subject().String(),
		})
	}
	return response
}

func (handler *Handler) denyAuthenticationSession(response http.ResponseWriter, request *http.Request) {
	if handler.sessionDenier == nil {
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"))
		return
	}
	token, problem := bearerToken(request)
	if problem != nil {
		writeProblem(response, request, *problem)
		return
	}
	if err := handler.sessionDenier.Deny(request.Context(), token); err != nil {
		if errors.Is(err, access.ErrUnauthenticated) {
			writeProblem(response, request, newProblem(http.StatusUnauthorized, "UNAUTHENTICATED"))
			return
		}
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"))
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) receiveWorkOSWebhook(response http.ResponseWriter, request *http.Request) {
	if handler.webhookVerifier == nil || handler.lifecycle == nil {
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"))
		return
	}
	if problem := requireJSON(request); problem != nil {
		writeProblem(response, request, *problem)
		return
	}
	signatures := request.Header.Values("WorkOS-Signature")
	if len(signatures) != 1 {
		writeProblem(response, request, newProblem(http.StatusBadRequest, "INVALID_WEBHOOK"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, MaxWebhookBytes))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeProblem(response, request, newProblem(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE"))
			return
		}
		writeProblem(response, request, newProblem(http.StatusBadRequest, "INVALID_WEBHOOK"))
		return
	}
	event, err := handler.webhookVerifier.Verify(body, signatures[0])
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusBadRequest, "INVALID_WEBHOOK"))
		return
	}
	_, err = handler.lifecycle.ApplyLifecycle(request.Context(), event)
	if errors.Is(err, access.ErrLifecycleConflict) {
		writeProblem(response, request, newProblem(http.StatusConflict, "WEBHOOK_CONFLICT"))
		return
	}
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"))
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) requireTenantPrincipal(request *http.Request) *problem {
	organizations := request.Header.Values("X-Tacit-Organization")
	if len(organizations) != 1 {
		value := newProblem(http.StatusUnprocessableEntity, "INVALID_ORGANIZATION")
		return &value
	}
	organization, err := access.ParseOrganizationID(organizations[0])
	if err != nil {
		value := newProblem(http.StatusUnprocessableEntity, "INVALID_ORGANIZATION")
		return &value
	}
	_, value := handler.authenticate(request, access.ExactOrganization(organization))
	return value
}

func (handler *Handler) authenticate(request *http.Request, target access.OrganizationTarget) (access.LocalPrincipal, *problem) {
	if handler.authenticator == nil {
		value := newProblem(http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE")
		return access.LocalPrincipal{}, &value
	}
	token, value := bearerToken(request)
	if value != nil {
		return access.LocalPrincipal{}, value
	}
	principal, err := handler.authenticator.Authenticate(request.Context(), token, target)
	if err == nil {
		return principal, nil
	}
	status, code := http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"
	if errors.Is(err, access.ErrUnauthenticated) {
		status, code = http.StatusUnauthorized, "UNAUTHENTICATED"
	} else if errors.Is(err, access.ErrNotAdmitted) {
		status, code = http.StatusForbidden, "NOT_ADMITTED"
	}
	problem := newProblem(status, code)
	return access.LocalPrincipal{}, &problem
}

func bearerToken(request *http.Request) (access.BearerToken, *problem) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		value := newProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
		return access.BearerToken{}, &value
	}
	token, err := access.ParseBearerToken(strings.TrimPrefix(values[0], "Bearer "))
	if err != nil {
		value := newProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
		return access.BearerToken{}, &value
	}
	return token, nil
}

func (handler *Handler) serveSpecification(response http.ResponseWriter, request *http.Request) {
	document, err := specification.ReadFile("openapi.yaml")
	if err != nil {
		writeProblem(response, request, newProblem(http.StatusInternalServerError, "INTERNAL"))
		return
	}
	response.Header().Set("Content-Type", "application/yaml")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(document)
}

func (handler *Handler) notFound(response http.ResponseWriter, request *http.Request) {
	writeProblem(response, request, newProblem(http.StatusNotFound, "NOT_FOUND"))
}

func requireJSON(request *http.Request) *problem {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		value := newProblem(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
		return &value
	}
	return nil
}

func decodeJSON(response http.ResponseWriter, request *http.Request, output any) *problem {
	reader := http.MaxBytesReader(response, request.Body, MaxInputBytes)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		value := decodeProblem(err)
		return &value
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		value := decodeProblem(err)
		return &value
	}
	return nil
}

func decodeProblem(err error) problem {
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		return newProblem(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE")
	}
	return newProblem(http.StatusBadRequest, "BAD_REQUEST")
}

func createResponse(sample shellsample.Sample, replayed bool) createShellSampleResponse {
	return createShellSampleResponse{Sample: sampleResponse(sample), Replayed: replayed}
}

func sampleResponse(sample shellsample.Sample) shellSampleResponse {
	return shellSampleResponse{ID: sample.ID.String(), OffsetMicroseconds: sample.Offset.String()}
}

type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Code      string `json:"code"`
	RequestID string `json:"requestId,omitempty"`
}

func newProblem(status int, code string) problem {
	title, detail := problemText(code)
	return problem{
		Type:   "https://api.tacit.dev/problems/" + strings.ToLower(strings.ReplaceAll(code, "_", "-")),
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code,
	}
}

func problemText(code string) (string, string) {
	switch code {
	case "BAD_REQUEST":
		return "Invalid request", "The JSON request body is malformed."
	case "UNSUPPORTED_MEDIA_TYPE":
		return "Unsupported media type", "The request body must use application/json."
	case "REQUEST_TOO_LARGE":
		return "Request too large", "The request body exceeds the one MiB input limit."
	case "INVALID_SAMPLE_ID":
		return "Invalid sample ID", "The sample ID is not canonical."
	case "INVALID_MEDIA_TIME":
		return "Invalid media time", "The recording offset must be canonical nonnegative microseconds."
	case "INVALID_IDEMPOTENCY_KEY":
		return "Invalid idempotency key", "Provide one valid Idempotency-Key header."
	case "IDEMPOTENCY_CONFLICT":
		return "Idempotency conflict", "The idempotency key was already used for a different command."
	case "SAMPLE_CONFLICT":
		return "Sample conflict", "The sample ID already identifies a different sample."
	case "NOT_FOUND":
		return "Not found", "The requested resource does not exist."
	case "UNAVAILABLE":
		return "Service unavailable", "The operation cannot be completed by this process."
	case "UNAUTHENTICATED":
		return "Authentication required", "Provide one valid managed-identity bearer credential."
	case "NOT_ADMITTED":
		return "Workspace access denied", "The verified identity has no active local workspace membership."
	case "IDENTITY_UNAVAILABLE":
		return "Identity service unavailable", "Identity checks cannot be completed safely."
	case "INVALID_ORGANIZATION":
		return "Invalid organization", "Provide one canonical local organization ID."
	case "INVALID_EVENT_REQUEST":
		return "Invalid event request", "The stream scope comes from the authenticated session."
	case "RESYNC_REQUIRED":
		return "Session snapshot required", "Fetch the current authentication session and reconnect with its cursor."
	case "INVALID_WEBHOOK":
		return "Invalid webhook", "The WorkOS webhook could not be verified."
	case "WEBHOOK_CONFLICT":
		return "Webhook conflict", "The event ID was previously used with a different payload."
	default:
		return "Internal error", "The operation failed."
	}
}

func writeProblem(response http.ResponseWriter, request *http.Request, value problem) {
	value.RequestID = process.RequestID(request.Context())
	response.Header().Set("Cache-Control", "no-store")
	if value.Status == http.StatusUnauthorized {
		response.Header().Set("WWW-Authenticate", `Bearer realm="tacit"`)
	}
	response.Header().Set("Content-Type", "application/problem+json")
	writeJSON(response, value.Status, value)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	if response.Header().Get("Content-Type") == "" {
		response.Header().Set("Content-Type", "application/json")
	}
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
