package graphql

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const MaxInputBytes = 1 << 20

// Boundary reads through the limit before the handler can perform any command.
func Boundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, MaxInputBytes)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			var sizeError *http.MaxBytesError
			if errors.As(err, &sizeError) {
				writeError(response, http.StatusRequestEntityTooLarge, RequestTooLarge)
			} else {
				writeError(response, http.StatusBadRequest, BadRequest)
			}
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(response, request)
	})
}

type Code string

const (
	BadRequest            Code = "BAD_REQUEST"
	RequestTooLarge       Code = "REQUEST_TOO_LARGE"
	InvalidID             Code = "INVALID_ID"
	InvalidMediaTime      Code = "INVALID_MEDIA_TIME"
	InvalidCursor         Code = "INVALID_CURSOR"
	InvalidIdempotencyKey Code = "INVALID_IDEMPOTENCY_KEY"
	Internal              Code = "INTERNAL"
)

func safeMessage(code Code) string {
	switch code {
	case BadRequest:
		return "Invalid request."
	case RequestTooLarge:
		return "Request exceeds the input limit."
	case InvalidID:
		return "Invalid sample ID."
	case InvalidMediaTime:
		return "Invalid recording offset."
	case InvalidCursor:
		return "Invalid cursor."
	case InvalidIdempotencyKey:
		return "Invalid idempotency key."
	default:
		return "Internal error."
	}
}

func writeError(response http.ResponseWriter, status int, code Code) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]any{
		"errors": []any{map[string]any{"message": safeMessage(code), "extensions": map[string]Code{"code": code}}},
	})
}
