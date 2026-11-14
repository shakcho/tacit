package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/shakcho/tacit/internal/platform/process"
	"github.com/shakcho/tacit/internal/runtime/shellsample"
)

const MaxInputBytes = 1 << 20

//go:embed openapi.yaml
var specification embed.FS

type Handler struct {
	samples shellsample.Samples
	mux     *http.ServeMux
}

func NewHandler(samples shellsample.Samples) *Handler {
	handler := &Handler{samples: samples, mux: http.NewServeMux()}
	handler.mux.HandleFunc("GET /openapi.yaml", handler.serveSpecification)
	handler.mux.HandleFunc("POST /v1/shell-samples", handler.createShellSample)
	handler.mux.HandleFunc("GET /v1/shell-samples/{sampleId}", handler.getShellSample)
	handler.mux.HandleFunc("/", handler.notFound)
	return handler
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mux.ServeHTTP(response, request)
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
	default:
		return "Internal error", "The operation failed."
	}
}

func writeProblem(response http.ResponseWriter, request *http.Request, value problem) {
	value.RequestID = process.RequestID(request.Context())
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
