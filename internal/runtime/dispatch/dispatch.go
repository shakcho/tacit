// Package dispatch relays the transactional outbox to an at-least-once broker.
// A broker acknowledgement never changes canonical job state.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shakcho/tacit/internal/runtime/durable"
)

type Publisher interface {
	Publish(context.Context, string, string, []byte) error
}

type Config struct {
	PublisherID     string
	LeaseDuration   time.Duration
	BatchSize       int
	MaximumAttempts uint16
	RetryBase       time.Duration
	Clock           func() time.Time
}

type Dispatcher struct {
	outbox    durable.Outbox
	publisher Publisher
	config    Config
}

func New(outbox durable.Outbox, publisher Publisher, config Config) (*Dispatcher, error) {
	if outbox == nil || publisher == nil || config.PublisherID == "" ||
		config.LeaseDuration < time.Second || config.LeaseDuration > time.Hour ||
		config.BatchSize < 1 || config.BatchSize > 500 || config.MaximumAttempts == 0 ||
		config.RetryBase < time.Millisecond || config.RetryBase > time.Hour {
		return nil, durable.ErrContract
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Dispatcher{outbox: outbox, publisher: publisher, config: config}, nil
}

func (dispatcher *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	messages, err := dispatcher.outbox.ClaimOutbox(ctx, durable.OutboxClaim{
		PublisherID: dispatcher.config.PublisherID, LeaseDuration: dispatcher.config.LeaseDuration,
		Limit: dispatcher.config.BatchSize,
	})
	if err != nil {
		return 0, err
	}
	published := 0
	var failures []error
	for _, message := range messages {
		body, err := json.Marshal(message.Envelope)
		if err == nil {
			err = dispatcher.publisher.Publish(ctx, message.Subject, message.Envelope.ID.String(), body)
		}
		if err != nil {
			releaseError := dispatcher.outbox.ReleaseOutbox(
				ctx, dispatcher.config.PublisherID, message.Envelope.ID, boundedError(err),
				dispatcher.config.Clock().UTC().Add(dispatcher.backoff(message.PublishAttempts)),
				dispatcher.config.MaximumAttempts,
			)
			failures = append(failures, errors.Join(fmt.Errorf("publish %s: %w", message.Envelope.ID.String(), err), releaseError))
			continue
		}
		if err := dispatcher.outbox.MarkOutboxPublished(ctx, dispatcher.config.PublisherID, message.Envelope.ID); err != nil {
			failures = append(failures, fmt.Errorf("acknowledge %s: %w", message.Envelope.ID.String(), err))
			continue
		}
		published++
	}
	return published, errors.Join(failures...)
}

func (dispatcher *Dispatcher) backoff(attempt uint16) time.Duration {
	shift := attempt
	if shift > 8 {
		shift = 8
	}
	delay := dispatcher.config.RetryBase * time.Duration(1<<shift)
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func boundedError(err error) string {
	value := err.Error()
	if len(value) > 2048 {
		return value[:2048]
	}
	return value
}
