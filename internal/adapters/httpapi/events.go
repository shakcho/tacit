package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/realtime"
)

const eventWriteTimeout = 250 * time.Millisecond

func (handler *Handler) getEvents(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeProblem(response, request, newProblem(http.StatusMethodNotAllowed, "INVALID_EVENT_REQUEST"))
		return
	}
	if request.URL.ForceQuery || request.URL.RawQuery != "" || request.Header.Values("X-Tacit-Organization") != nil ||
		request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
		writeProblem(response, request, newProblem(http.StatusBadRequest, "INVALID_EVENT_REQUEST"))
		return
	}
	authContext, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	principal, failure := handler.authenticate(request.WithContext(authContext), access.TokenOrganization())
	cancel()
	if failure != nil {
		writeProblem(response, request, *failure)
		return
	}
	if handler.realtime == nil {
		writeEventFailure(response, request, realtime.ErrUnavailable)
		return
	}
	var cursor string
	if values := request.Header.Values("Last-Event-ID"); len(values) == 1 {
		cursor = values[0]
	}
	controller := http.NewResponseController(response)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		writeEventFailure(response, request, err)
		return
	}
	stream, err := handler.realtime.Attach(request.Context(), principal, cursor)
	if err != nil {
		writeEventFailure(response, request, err)
		return
	}
	started := false
	err = stream.Run(func(frame realtime.Frame) error {
		if frame.Kind == realtime.Open {
			response.Header().Set("Content-Type", "text/event-stream")
			response.Header().Set("Cache-Control", "no-store, no-transform")
			response.Header().Set("X-Accel-Buffering", "no")
			started = true
		}
		return flushFrame(response, controller, frame)
	})
	if err != nil && !started && request.Context().Err() == nil {
		writeEventFailure(response, request, err)
	}
}

func writeEventFailure(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, realtime.ErrResync):
		writeProblem(response, request, newProblem(http.StatusConflict, "RESYNC_REQUIRED"))
	case errors.Is(err, access.ErrNotAdmitted):
		writeProblem(response, request, newProblem(http.StatusForbidden, "NOT_ADMITTED"))
	default:
		response.Header().Set("Retry-After", "2")
		writeProblem(response, request, newProblem(http.StatusServiceUnavailable, "UNAVAILABLE"))
	}
}

func flushFrame(response http.ResponseWriter, controller *http.ResponseController, frame realtime.Frame) error {
	data, err := encodeFrame(frame)
	if err != nil {
		return err
	}
	if err := controller.SetWriteDeadline(time.Now().Add(eventWriteTimeout)); err != nil {
		return err
	}
	defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	if _, err := response.Write(data); err != nil {
		return err
	}
	return controller.Flush()
}

type eventResourceResponse struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func encodeFrame(frame realtime.Frame) ([]byte, error) {
	var body any
	switch frame.Kind {
	case realtime.Open:
		return []byte(": connected\n\n"), nil
	case realtime.Heartbeat:
		return []byte(": heartbeat\n\n"), nil
	case realtime.Checkpoint:
		body = struct{}{}
	case realtime.Invalidation:
		body = struct {
			Resource eventResourceResponse `json:"resource"`
			Revision string                `json:"revision"`
		}{
			Resource: eventResourceResponse{Type: realtime.SessionResource, ID: frame.Resource.String()},
			Revision: strconv.FormatInt(frame.Revision, 10),
		}
	case realtime.Resync:
		body = map[string]string{"code": "RESYNC_REQUIRED"}
	case realtime.AccessRevoked:
		body = map[string]string{"code": "ACCESS_REVOKED"}
	default:
		return nil, errors.New("unknown realtime frame")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var id string
	if frame.Cursor != "" {
		id = "id: " + frame.Cursor + "\n"
	}
	return fmt.Appendf(nil, "%sevent: %s\ndata: %s\n\n", id, frame.Kind, payload), nil
}
