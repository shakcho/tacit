package realtime

import (
	"context"
	"errors"
	"sync"

	"github.com/shakcho/tacit/internal/access"
)

var ErrUnavailable = errors.New("realtime attachment unavailable")

type Registry struct {
	mu         sync.Mutex
	limit      int
	subs       map[*subscription]access.OrganizationID
	drain      context.Context
	cancel     context.CancelFunc
	generation chan struct{}
}

func NewRegistry(limit int) *Registry {
	drain, cancel := context.WithCancel(context.Background())
	return &Registry{
		limit: limit, subs: make(map[*subscription]access.OrganizationID),
		drain: drain, cancel: cancel, generation: make(chan struct{}),
	}
}

type subscription struct {
	wake chan struct{}
	lost <-chan struct{}
}

func (registry *Registry) attach(parent context.Context, org access.OrganizationID) (context.Context, *subscription, func(), error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.drain.Err() != nil || len(registry.subs) >= registry.limit {
		return nil, nil, nil, ErrUnavailable
	}
	sub := &subscription{wake: make(chan struct{}, 1), lost: registry.generation}
	registry.subs[sub] = org
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(registry.drain, cancel)
	return ctx, sub, func() {
		stop()
		cancel()
		registry.mu.Lock()
		delete(registry.subs, sub)
		registry.mu.Unlock()
	}, nil
}

func (registry *Registry) Notify(org access.OrganizationID) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for sub, organization := range registry.subs {
		if org == organization {
			select {
			case sub.wake <- struct{}{}:
			default:
			}
		}
	}
}

func (registry *Registry) ListenerLost() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	close(registry.generation)
	registry.generation = make(chan struct{})
}

func (registry *Registry) Drain() { registry.cancel() }
