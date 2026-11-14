package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shakcho/tacit/internal/adapters/httpapi"
	"github.com/shakcho/tacit/internal/platform/memory"
)

const sampleID = "smp_0123456789abcdef0123456789abcdef"

func newHandler() http.Handler {
	return httpapi.NewHandler(memory.NewShellSamples(memory.Limits{Samples: 8, IdempotencyEntries: 16}))
}

func createRequest(body, key string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/shell-samples", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	return request
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
	for _, fragment := range []string{"openapi: 3.1.0", "/v1/shell-samples:", "operationId: createShellSample", "application/problem+json"} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Errorf("specification does not contain %q", fragment)
		}
	}
}
