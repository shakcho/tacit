package realtime

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/shakcho/tacit/internal/access"
)

type FrameKind string

const (
	Open          FrameKind = "open"
	Heartbeat     FrameKind = "heartbeat"
	Checkpoint    FrameKind = "checkpoint"
	Invalidation  FrameKind = "invalidate"
	Resync        FrameKind = "resync"
	AccessRevoked FrameKind = "access_revoked"
)

type Frame struct {
	Kind     FrameKind
	Cursor   string
	Resource access.UserID
	Revision int64
}

type Service struct {
	feed     Feed
	codec    CursorCodec
	registry *Registry
}

func NewService(feed Feed, codec CursorCodec, registry *Registry) *Service {
	return &Service{feed: feed, codec: codec, registry: registry}
}

func (service *Service) SessionSnapshot(ctx context.Context, principal access.LocalPrincipal) (Snapshot, string, error) {
	snapshot, err := service.feed.SessionSnapshot(ctx, principal)
	if err != nil {
		return Snapshot{}, "", err
	}
	return snapshot, service.codec.Encode(snapshot.Principal, snapshot.Point.Event), nil
}

func (service *Service) Drain() { service.registry.Drain() }

type phase uint8

const (
	replaying phase = iota
	scanning
	waiting
	closed
)

type Stream struct {
	service    *Service
	principal  access.LocalPrincipal
	ctx        context.Context
	sub        *subscription
	release    func()
	phase      phase
	point      Point
	delivered  Point
	page       Page
	replayHead int64
}

func (service *Service) Attach(ctx context.Context, principal access.LocalPrincipal, cursor string) (*Stream, error) {
	ctx, sub, release, err := service.registry.attach(ctx, principal.OrganizationID())
	if err != nil {
		return nil, err
	}
	event, valid := service.codec.Decode(principal, cursor)
	if !valid {
		release()
		return nil, ErrResync
	}
	point, err := service.feed.ResolveCursor(ctx, principal.OrganizationID(), event)
	if err != nil {
		release()
		return nil, err
	}
	page, err := service.feed.Replay(ctx, principal.OrganizationID(), point)
	if err != nil {
		release()
		return nil, err
	}
	return &Stream{
		service: service, principal: principal, ctx: ctx, sub: sub, release: release,
		point: point, delivered: point, page: page, phase: replaying, replayHead: page.Head,
	}, nil
}

func (stream *Stream) Run(send func(Frame) error) error {
	defer stream.release()
	if err := stream.authorized(send, Frame{Kind: Open}); err != nil {
		return err
	}
	poll := time.NewTimer(pollInterval())
	defer poll.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()

	for stream.phase != closed {
		if stream.ctx.Err() != nil {
			return nil
		}
		if err := stream.listenerStatus(); err != nil {
			return stream.terminate(send, err)
		}
		var err error
		switch stream.phase {
		case replaying, scanning:
			err = stream.scan(send)
		case waiting:
			select {
			case <-stream.ctx.Done():
				stream.phase = closed
			case <-stream.sub.lost:
				err = ErrResync
			case <-stream.sub.wake:
				err = stream.readPage()
			case <-poll.C:
				poll.Reset(pollInterval())
				err = stream.service.feed.Authorize(stream.ctx, stream.principal, func() error { return nil })
				if err == nil {
					err = stream.readPage()
				}
			case <-heartbeat.C:
				err = stream.authorized(send, Frame{Kind: Heartbeat})
			}
		}
		if err != nil {
			return stream.terminate(send, err)
		}
	}
	return nil
}

func pollInterval() time.Duration {
	return 1750*time.Millisecond + rand.N(500*time.Millisecond)
}

func (stream *Stream) listenerStatus() error {
	select {
	case <-stream.sub.lost:
		return ErrResync
	default:
		return nil
	}
}

func (stream *Stream) scan(send func(Frame) error) error {
	for _, change := range stream.page.Changes {
		if err := stream.listenerStatus(); err != nil {
			return err
		}
		if change.Resource != SessionResource || (change.Kind != Invalidate && change.Kind != ScopeReset) ||
			change.Position != stream.point.Position+1 || change.Revision != change.Position || change.Event == (EventID{}) {
			return ErrResync
		}
		stream.point = change.Point
		if stream.phase == replaying && stream.point.Position >= stream.replayHead {
			stream.phase = scanning
		}
		if change.Audience.Valid() && change.Audience != stream.principal.UserID() {
			continue
		}
		if change.Kind == ScopeReset {
			return ErrResync
		}
		frame := Frame{
			Kind: Invalidation, Cursor: stream.cursor(), Resource: stream.principal.UserID(), Revision: change.Revision,
		}
		if err := stream.authorized(send, frame); err != nil {
			return err
		}
		stream.delivered = stream.point
	}
	if stream.point != stream.delivered {
		if err := stream.authorized(send, Frame{Kind: Checkpoint, Cursor: stream.cursor()}); err != nil {
			return err
		}
		stream.delivered = stream.point
	}
	if stream.point.Position < stream.page.Head {
		return stream.readPage()
	}
	stream.phase = waiting
	return nil
}

func (stream *Stream) cursor() string {
	return stream.service.codec.Encode(stream.principal, stream.point.Event)
}

func (stream *Stream) readPage() error {
	page, err := stream.service.feed.Replay(stream.ctx, stream.principal.OrganizationID(), stream.point)
	if err != nil {
		return err
	}
	stream.page = page
	if stream.phase == waiting {
		stream.phase = scanning
	}
	return nil
}

type sendError struct{ error }

func (stream *Stream) authorized(send func(Frame) error, frame Frame) error {
	return stream.service.feed.Authorize(stream.ctx, stream.principal, func() error {
		if err := send(frame); err != nil {
			return sendError{err}
		}
		return nil
	})
}

func (stream *Stream) terminate(send func(Frame) error, err error) error {
	stream.phase = closed
	var writeFailure sendError
	switch {
	case stream.ctx.Err() != nil, errors.As(err, &writeFailure):
		return nil
	case errors.Is(err, access.ErrNotAdmitted):
		return send(Frame{Kind: AccessRevoked})
	case errors.Is(err, ErrResync):
		err := stream.authorized(send, Frame{Kind: Resync})
		if errors.Is(err, access.ErrNotAdmitted) {
			return send(Frame{Kind: AccessRevoked})
		}
		if errors.Is(err, access.ErrUnauthenticated) {
			return send(Frame{Kind: Resync})
		}
		return err
	default:
		return send(Frame{Kind: Resync})
	}
}
