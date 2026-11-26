package httpapi_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/adapters/httpapi"
	"github.com/shakcho/tacit/internal/runtime/realtime"
)

type eventFeed struct {
	mu      sync.Mutex
	changes []realtime.Change
	revoked bool
}

var initialPoint = realtime.Point{Event: realtime.EventID{1}}

func (feed *eventFeed) SessionSnapshot(_ context.Context, principal access.LocalPrincipal) (realtime.Snapshot, error) {
	return realtime.Snapshot{Principal: principal, Point: initialPoint}, nil
}

func (feed *eventFeed) ResolveCursor(_ context.Context, _ access.OrganizationID, id realtime.EventID) (realtime.Point, error) {
	if id == initialPoint.Event {
		return initialPoint, nil
	}
	return realtime.Point{}, realtime.ErrResync
}

func (feed *eventFeed) Replay(_ context.Context, _ access.OrganizationID, point realtime.Point) (realtime.Page, error) {
	feed.mu.Lock()
	defer feed.mu.Unlock()
	head := int64(len(feed.changes))
	if point.Position > head || head-point.Position > realtime.MaxReplay {
		return realtime.Page{}, realtime.ErrResync
	}
	end := min(head, point.Position+realtime.PageSize)
	return realtime.Page{Head: head, Changes: append([]realtime.Change(nil), feed.changes[point.Position:end]...)}, nil
}

func (feed *eventFeed) Authorize(_ context.Context, _ access.LocalPrincipal, send func() error) error {
	feed.mu.Lock()
	defer feed.mu.Unlock()
	if feed.revoked {
		return access.ErrNotAdmitted
	}
	return send()
}

func (feed *eventFeed) append(user access.UserID) {
	feed.mu.Lock()
	defer feed.mu.Unlock()
	position := int64(len(feed.changes) + 1)
	feed.changes = append(feed.changes, realtime.Change{
		Point: realtime.Point{Position: position, Event: realtime.EventID{byte(position + 1)}},
		Kind:  realtime.Invalidate, Resource: realtime.SessionResource, Audience: user, Revision: position,
	})
}

type eventService struct {
	service  *realtime.Service
	codec    realtime.CursorCodec
	registry *realtime.Registry
}

func newEventService(t *testing.T, feed *eventFeed, capacity int) eventService {
	t.Helper()
	codec, err := realtime.NewCursorCodec([]byte("shared replica signing key for tests"))
	if err != nil {
		t.Fatal(err)
	}
	registry := realtime.NewRegistry(capacity)
	return eventService{realtime.NewService(feed, codec, registry), codec, registry}
}

func eventRequest(t *testing.T, endpoint, cursor string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Last-Event-ID", cursor)
	return request
}

func readEvent(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		frame.WriteString(line)
		if line == "\n" {
			return frame.String()
		}
	}
}

type pipeListener struct {
	incoming  chan net.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

func (listener *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-listener.incoming:
		return conn, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

func (listener *pipeListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return nil
}

func (listener *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 18080}
}

func eventHTTPServer(t *testing.T, handler http.Handler, writeTimeout time.Duration) (*http.Client, string) {
	t.Helper()
	listener := &pipeListener{incoming: make(chan net.Conn), closed: make(chan struct{})}
	server := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: handler, WriteTimeout: writeTimeout},
	}
	server.Start()
	t.Cleanup(server.Close)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			client, server := net.Pipe()
			select {
			case listener.incoming <- server:
				return client, nil
			case <-ctx.Done():
				client.Close()
				server.Close()
				return nil, ctx.Err()
			}
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Timeout: 8 * time.Second, Transport: transport}, server.URL
}

func TestEventsStreamRecoversLostWakesAndStopsOnRevocation(t *testing.T) {
	member := principal(t)
	feed := &eventFeed{}
	events := newEventService(t, feed, 1)
	handler := httpapi.NewHandler(nil, httpapi.WithIdentity(httpapi.IdentityDependencies{
		Authenticator: &fakeAuthenticator{principal: member},
	}), httpapi.WithRealtime(events.service))
	client, origin := eventHTTPServer(t, handler, 100*time.Millisecond)
	cursor := events.codec.Encode(member, initialPoint.Event)
	response, err := client.Do(eventRequest(t, origin+"/v1/events", cursor))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" ||
		response.Header.Get("Cache-Control") != "no-store, no-transform" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("SSE handshake = %d %v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	if frame := readEvent(t, reader); frame != ": connected\n\n" {
		t.Fatalf("handshake = %q", frame)
	}

	time.Sleep(150 * time.Millisecond)
	other, _ := access.ParseUserID("usr_00000000000000000000000000000002")
	feed.append(other)
	// No broker notification. The independent repair scan reaches the committed event.
	if frame := readEvent(t, reader); !strings.Contains(frame, "event: checkpoint\ndata: {}") || !strings.Contains(frame, "id: v1.") {
		t.Fatalf("checkpoint = %q", frame)
	}
	feed.append(member.UserID())
	events.registry.Notify(member.OrganizationID())
	if frame := readEvent(t, reader); !strings.Contains(frame, "event: invalidate") ||
		!strings.Contains(frame, `"revision":"2"`) || !strings.Contains(frame, member.UserID().String()) {
		t.Fatalf("invalidation = %q", frame)
	}

	feed.mu.Lock()
	feed.revoked = true
	feed.mu.Unlock()
	feed.append(member.UserID())
	events.registry.Notify(member.OrganizationID())
	if frame := readEvent(t, reader); frame != "event: access_revoked\ndata: {\"code\":\"ACCESS_REVOKED\"}\n\n" {
		t.Fatalf("terminal frame = %q", frame)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("revoked stream read = %v", err)
	}
}

func TestEventsRejectsScopeAndResyncsAfterBacklogOrListenerLoss(t *testing.T) {
	member := principal(t)
	feed := &eventFeed{}
	events := newEventService(t, feed, 1)
	handler := httpapi.NewHandler(nil, httpapi.WithIdentity(httpapi.IdentityDependencies{
		Authenticator: &fakeAuthenticator{principal: member},
	}), httpapi.WithRealtime(events.service))
	client, origin := eventHTTPServer(t, handler, time.Second)
	cursor := events.codec.Encode(member, initialPoint.Event)
	for _, test := range []struct {
		path, token, code string
		status            int
	}{
		{"/v1/events?organizationId=" + member.OrganizationID().String(), cursor, "INVALID_EVENT_REQUEST", 400},
		{"/v1/events", "tampered", "RESYNC_REQUIRED", 409},
	} {
		response, err := client.Do(eventRequest(t, origin+test.path, test.token))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != test.status || !strings.Contains(string(body), test.code) {
			t.Fatalf("rejection = %d %s %v", response.StatusCode, body, err)
		}
	}
	response, err := client.Do(eventRequest(t, origin+"/v1/events", cursor))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readEvent(t, reader)
	events.registry.ListenerLost()
	if frame := readEvent(t, reader); frame != "event: resync\ndata: {\"code\":\"RESYNC_REQUIRED\"}\n\n" {
		t.Fatalf("listener loss = %q", frame)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("listener loss stream read = %v", err)
	}

	for range realtime.MaxReplay + 1 {
		feed.append(member.UserID())
	}
	response, err = client.Do(eventRequest(t, origin+"/v1/events", cursor))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusConflict || !strings.Contains(string(body), "RESYNC_REQUIRED") {
		t.Fatalf("backlog response = %d %s %v", response.StatusCode, body, err)
	}
}

func TestEventDrainClosesStreamsAndRejectsNewAttachments(t *testing.T) {
	member := principal(t)
	events := newEventService(t, &eventFeed{}, 1)
	handler := httpapi.NewHandler(nil, httpapi.WithIdentity(httpapi.IdentityDependencies{
		Authenticator: &fakeAuthenticator{principal: member},
	}), httpapi.WithRealtime(events.service))
	client, origin := eventHTTPServer(t, handler, time.Second)
	cursor := events.codec.Encode(member, initialPoint.Event)
	response, err := client.Do(eventRequest(t, origin+"/v1/events", cursor))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readEvent(t, reader)
	handler.Drain()
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("drained stream read = %v", err)
	}
	response, err = client.Do(eventRequest(t, origin+"/v1/events", cursor))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("attachment during drain = %d", response.StatusCode)
	}
}

func TestSlowEventPeerReleasesItsSlot(t *testing.T) {
	member := principal(t)
	feed := &eventFeed{}
	events := newEventService(t, feed, 1)
	handler := httpapi.NewHandler(nil, httpapi.WithIdentity(httpapi.IdentityDependencies{
		Authenticator: &fakeAuthenticator{principal: member},
	}), httpapi.WithRealtime(events.service))
	client, origin := eventHTTPServer(t, handler, time.Second)
	cursor := events.codec.Encode(member, initialPoint.Event)
	requestURL := origin + "/v1/events"
	response, err := client.Do(eventRequest(t, requestURL, cursor))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	for range 128 {
		feed.append(member.UserID())
	}
	events.registry.Notify(member.OrganizationID())

	// The first client holds its body open without reading. The in-memory socket
	// has no peer-side queue, so each frame misses its bounded flush deadline.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		next, err := client.Do(eventRequest(t, requestURL, cursor))
		if err != nil {
			t.Fatal(err)
		}
		next.Body.Close()
		if next.StatusCode == http.StatusOK {
			return
		}
		if next.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("reattachment returned %d", next.StatusCode)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("slow peer held its stream slot past the write deadline")
}
