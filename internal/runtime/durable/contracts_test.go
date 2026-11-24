package durable_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/durable"
	"github.com/shakcho/tacit/internal/runtime/idempotency"
)

func TestClosedWorkerResults(t *testing.T) {
	t.Parallel()

	success, err := durable.Succeeded(1, json.RawMessage(`{"artifact":"object://result"}`))
	if err != nil || success.Kind() != durable.ResultSucceeded {
		t.Fatalf("success = %#v, %v", success, err)
	}
	retryAt := time.Now().UTC().Add(time.Minute)
	retry, err := durable.Retry("provider unavailable", retryAt)
	if err != nil || retry.Kind() != durable.ResultRetry || !retry.RetryAt().Equal(retryAt) {
		t.Fatalf("retry = %#v, %v", retry, err)
	}
	failure, err := durable.TerminalFailure("invalid input")
	if err != nil || failure.Kind() != durable.ResultTerminalFailure {
		t.Fatalf("failure = %#v, %v", failure, err)
	}
	if _, err := durable.Succeeded(0, json.RawMessage(`{}`)); !errors.Is(err, durable.ErrContract) {
		t.Fatalf("invalid success error = %v", err)
	}
}

func TestWorkerResultJSONRejectsOpenVariants(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		`{"kind":"succeeded","outputVersion":1,"output":{"asset":"a"}}`,
		`{"kind":"retry","reason":"busy","retryAt":"2026-11-24T18:01:00Z"}`,
		`{"kind":"terminal_failure","reason":"invalid input"}`,
	} {
		var result durable.WorkerResult
		if err := json.Unmarshal([]byte(value), &result); err != nil {
			t.Fatalf("decode %s: %v", value, err)
		}
		encoded, err := json.Marshal(result)
		if err != nil || !json.Valid(encoded) {
			t.Fatalf("encode %#v: %s, %v", result, encoded, err)
		}
	}
	for _, value := range []string{
		`{"kind":"partial","progress":50}`,
		`{"kind":"succeeded","outputVersion":1,"output":{},"unexpected":true}`,
	} {
		var result durable.WorkerResult
		if err := json.Unmarshal([]byte(value), &result); err == nil {
			t.Fatalf("open worker result was accepted: %s", value)
		}
	}
}

func TestEnqueueFingerprintChangesWithSemanticInput(t *testing.T) {
	t.Parallel()
	organization, _ := access.ParseOrganizationID("org_00000000000000000000000000000001")
	key, _ := idempotency.ParseKey("render-1")
	command := durable.EnqueueCommand{
		Organization: organization, Key: key, OperationScope: "library.render.v1",
		Kind: "highlight.render", Workload: "media", InputVersion: 1,
		Input: json.RawMessage(`{"generationId":"gen_1"}`), ResourceType: "highlight",
		ResourceID: "highlight_1", ResourceRevision: 1, CorrelationID: "request_1",
		AvailableAt: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC), MaxAttempts: 3,
	}
	first, err := command.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	command.ResourceRevision = 2
	second, err := command.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("fingerprint did not bind resource revision")
	}
}

func TestEventEnvelopeUsesStableStringIdentifier(t *testing.T) {
	t.Parallel()
	eventID, _ := durable.ParseEventID("evt_00000000000000000000000000000001")
	envelope := durable.EventEnvelope{
		ID: eventID, Schema: "tacit.runtime.job.ready", SchemaVersion: 1,
		OrganizationID: "org_00000000000000000000000000000001",
		AggregateType:  "job", AggregateID: "job_00000000000000000000000000000001",
		AggregateRevision: 1, CorrelationID: "request_1", OccurredAt: time.Now().UTC(),
		Payload: json.RawMessage(`{"jobId":"job_00000000000000000000000000000001"}`),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["eventId"] != eventID.String() {
		t.Fatalf("eventId = %#v", decoded["eventId"])
	}
}
