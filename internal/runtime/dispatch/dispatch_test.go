package dispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shakcho/tacit/internal/runtime/dispatch"
	"github.com/shakcho/tacit/internal/runtime/durable"
)

type outboxStub struct {
	messages  []durable.OutboxMessage
	published []durable.EventID
	released  []durable.EventID
}

func (stub *outboxStub) ClaimOutbox(context.Context, durable.OutboxClaim) ([]durable.OutboxMessage, error) {
	return stub.messages, nil
}
func (stub *outboxStub) MarkOutboxPublished(_ context.Context, _ string, id durable.EventID) error {
	stub.published = append(stub.published, id)
	return nil
}
func (stub *outboxStub) ReleaseOutbox(_ context.Context, _ string, id durable.EventID, _ string, _ time.Time, _ uint16) error {
	stub.released = append(stub.released, id)
	return nil
}
func (stub *outboxStub) RequeuePublished(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

type publisherStub struct{ fail string }

func (stub publisherStub) Publish(_ context.Context, _ string, id string, _ []byte) error {
	if id == stub.fail {
		return errors.New("broker unavailable")
	}
	return nil
}

func TestDispatcherAcknowledgesSuccessAndReleasesFailure(t *testing.T) {
	t.Parallel()
	first, _ := durable.ParseEventID("evt_00000000000000000000000000000001")
	second, _ := durable.ParseEventID("evt_00000000000000000000000000000002")
	messages := []durable.OutboxMessage{
		{Subject: "tacit.runtime.jobs", Envelope: envelope(first), PublishAttempts: 1},
		{Subject: "tacit.runtime.jobs", Envelope: envelope(second), PublishAttempts: 1},
	}
	outbox := &outboxStub{messages: messages}
	relay, err := dispatch.New(outbox, publisherStub{fail: second.String()}, dispatch.Config{
		PublisherID: "dispatcher-1", LeaseDuration: time.Minute, BatchSize: 10,
		MaximumAttempts: 10, RetryBase: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	published, err := relay.RunOnce(t.Context())
	if published != 1 || err == nil {
		t.Fatalf("published = %d, error = %v", published, err)
	}
	if len(outbox.published) != 1 || outbox.published[0] != first {
		t.Fatalf("published IDs = %#v", outbox.published)
	}
	if len(outbox.released) != 1 || outbox.released[0] != second {
		t.Fatalf("released IDs = %#v", outbox.released)
	}
}

func envelope(id durable.EventID) durable.EventEnvelope {
	return durable.EventEnvelope{
		ID: id, Schema: "tacit.runtime.job.ready", SchemaVersion: 1,
		OrganizationID: "org_00000000000000000000000000000001",
		AggregateType:  "job", AggregateID: "job_00000000000000000000000000000001",
		AggregateRevision: 1, CorrelationID: "request_1", OccurredAt: time.Now().UTC(),
		Payload: json.RawMessage(`{"jobId":"job_00000000000000000000000000000001"}`),
	}
}
