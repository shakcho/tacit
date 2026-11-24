// Package durable defines Tacit's durable-work state machine and wire-neutral
// contracts. PostgreSQL owns canonical state; brokers only wake dispatchers and
// workers.
package durable

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/idempotency"
)

var (
	ErrContract            = errors.New("invalid durable-work contract")
	ErrIdempotencyConflict = errors.New("idempotency key was used for a different command")
	ErrLeaseLost           = errors.New("job lease is expired or replaced")
	ErrJobNotClaimable     = errors.New("job is not claimable")
	ErrJobTerminal         = errors.New("job is terminal")
	ErrDuplicateConflict   = errors.New("duplicate delivery payload conflicts with recorded receipt")
)

type JobID struct{ value [16]byte }

func ParseJobID(value string) (JobID, error) {
	parsed, err := parseID(value, "job_")
	return JobID{value: parsed}, err
}

func JobIDFromBytes(value [16]byte) (JobID, error) {
	if value == [16]byte{} {
		return JobID{}, ErrContract
	}
	return JobID{value: value}, nil
}

func (id JobID) String() string  { return formatID("job_", id.value) }
func (id JobID) Bytes() [16]byte { return id.value }
func (id JobID) Valid() bool     { return id.value != [16]byte{} }
func (id JobID) MarshalText() ([]byte, error) {
	if !id.Valid() {
		return nil, ErrContract
	}
	return []byte(id.String()), nil
}
func (id *JobID) UnmarshalText(value []byte) error {
	parsed, err := ParseJobID(string(value))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

type EventID struct{ value [16]byte }

func ParseEventID(value string) (EventID, error) {
	parsed, err := parseID(value, "evt_")
	return EventID{value: parsed}, err
}

func EventIDFromBytes(value [16]byte) (EventID, error) {
	if value == [16]byte{} {
		return EventID{}, ErrContract
	}
	return EventID{value: value}, nil
}

func (id EventID) String() string  { return formatID("evt_", id.value) }
func (id EventID) Bytes() [16]byte { return id.value }
func (id EventID) Valid() bool     { return id.value != [16]byte{} }
func (id EventID) MarshalText() ([]byte, error) {
	if !id.Valid() {
		return nil, ErrContract
	}
	return []byte(id.String()), nil
}
func (id *EventID) UnmarshalText(value []byte) error {
	parsed, err := ParseEventID(string(value))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func parseID(value, prefix string) ([16]byte, error) {
	var decoded [16]byte
	if len(value) != len(prefix)+32 || !strings.HasPrefix(value, prefix) {
		return decoded, ErrContract
	}
	if _, err := hex.Decode(decoded[:], []byte(value[len(prefix):])); err != nil || decoded == [16]byte{} {
		return [16]byte{}, ErrContract
	}
	return decoded, nil
}

func formatID(prefix string, value [16]byte) string {
	if value == [16]byte{} {
		return ""
	}
	return prefix + hex.EncodeToString(value[:])
}

type JobState uint8

const (
	JobStateUnknown JobState = iota
	JobStateQueued
	JobStateRetryWait
	JobStateRunning
	JobStateSucceeded
	JobStateFailed
	JobStateCanceled
)

func (state JobState) String() string {
	switch state {
	case JobStateQueued:
		return "queued"
	case JobStateRetryWait:
		return "retry_wait"
	case JobStateRunning:
		return "running"
	case JobStateSucceeded:
		return "succeeded"
	case JobStateFailed:
		return "failed"
	case JobStateCanceled:
		return "canceled"
	default:
		return ""
	}
}

func ParseJobState(value string) (JobState, error) {
	for state := JobStateQueued; state <= JobStateCanceled; state++ {
		if state.String() == value {
			return state, nil
		}
	}
	return JobStateUnknown, ErrContract
}

func (state JobState) Terminal() bool {
	return state == JobStateSucceeded || state == JobStateFailed || state == JobStateCanceled
}

type EnqueueCommand struct {
	Organization     access.OrganizationID
	Key              idempotency.Key
	OperationScope   string
	Kind             string
	Workload         string
	InputVersion     uint16
	Input            json.RawMessage
	ResourceType     string
	ResourceID       string
	ResourceRevision uint64
	CorrelationID    string
	CausationID      string
	Priority         int16
	AvailableAt      time.Time
	DeadlineAt       time.Time
	MaxAttempts      uint16
}

func (command EnqueueCommand) Validate() error {
	if !command.Organization.Valid() || command.Key.String() == "" ||
		!bounded(command.OperationScope, 1, 128) || !bounded(command.Kind, 1, 128) ||
		!bounded(command.Workload, 1, 64) || command.InputVersion == 0 ||
		!object(command.Input) || !bounded(command.ResourceType, 1, 64) ||
		!bounded(command.ResourceID, 1, 256) || command.ResourceRevision == 0 ||
		!bounded(command.CorrelationID, 1, 128) ||
		(command.CausationID != "" && !bounded(command.CausationID, 1, 128)) ||
		command.AvailableAt.IsZero() || command.MaxAttempts < 1 || command.MaxAttempts > 100 ||
		(!command.DeadlineAt.IsZero() && !command.DeadlineAt.After(command.AvailableAt)) {
		return ErrContract
	}
	return nil
}

func (command EnqueueCommand) Fingerprint() ([32]byte, error) {
	if err := command.Validate(); err != nil {
		return [32]byte{}, err
	}
	canonicalInput := bytes.NewBuffer(make([]byte, 0, len(command.Input)))
	if err := json.Compact(canonicalInput, command.Input); err != nil {
		return [32]byte{}, ErrContract
	}
	value := fmt.Sprintf("v1\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%d\x00%s\x00%s\x00%d",
		command.Kind, command.Workload, command.OperationScope, command.InputVersion, canonicalInput.Bytes(),
		command.ResourceType, command.ResourceID, command.ResourceRevision, command.CorrelationID,
		command.CausationID, command.Priority, command.AvailableAt.UTC().Format(time.RFC3339Nano),
		formatOptionalTime(command.DeadlineAt), command.MaxAttempts,
	)
	return sha256.Sum256([]byte(value)), nil
}

type Job struct {
	ID               JobID
	Organization     access.OrganizationID
	Kind             string
	Workload         string
	State            JobState
	EventRevision    uint64
	InputVersion     uint16
	Input            json.RawMessage
	ResourceType     string
	ResourceID       string
	ResourceRevision uint64
	CorrelationID    string
	CausationID      string
	Priority         int16
	AvailableAt      time.Time
	DeadlineAt       time.Time
	MaxAttempts      uint16
	AttemptCount     uint16
	Fence            uint64
	LeaseOwner       string
	LeaseExpiresAt   time.Time
	OutputVersion    uint16
	Output           json.RawMessage
	TerminalReason   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      time.Time
}

type EnqueueResult struct {
	Job      Job
	Replayed bool
}

type ClaimRequest struct {
	Workload      string
	WorkerID      string
	LeaseDuration time.Duration
}

func (request ClaimRequest) Validate() error {
	if !bounded(request.Workload, 1, 64) || !bounded(request.WorkerID, 1, 128) ||
		request.LeaseDuration < time.Second || request.LeaseDuration > 24*time.Hour {
		return ErrContract
	}
	return nil
}

type Assignment struct {
	Job            Job
	Attempt        uint16
	Fence          uint64
	WorkerID       string
	LeaseExpiresAt time.Time
}

type ResultKind uint8

const (
	ResultUnknown ResultKind = iota
	ResultSucceeded
	ResultRetry
	ResultTerminalFailure
)

type WorkerResult struct {
	kind          ResultKind
	outputVersion uint16
	output        json.RawMessage
	reason        string
	retryAt       time.Time
}

func Succeeded(outputVersion uint16, output json.RawMessage) (WorkerResult, error) {
	if outputVersion == 0 || !object(output) {
		return WorkerResult{}, ErrContract
	}
	return WorkerResult{kind: ResultSucceeded, outputVersion: outputVersion, output: cloneJSON(output)}, nil
}

func Retry(reason string, retryAt time.Time) (WorkerResult, error) {
	if !bounded(reason, 1, 2048) || retryAt.IsZero() {
		return WorkerResult{}, ErrContract
	}
	return WorkerResult{kind: ResultRetry, reason: reason, retryAt: retryAt.UTC()}, nil
}

func TerminalFailure(reason string) (WorkerResult, error) {
	if !bounded(reason, 1, 2048) {
		return WorkerResult{}, ErrContract
	}
	return WorkerResult{kind: ResultTerminalFailure, reason: reason}, nil
}

func (result WorkerResult) Kind() ResultKind        { return result.kind }
func (result WorkerResult) OutputVersion() uint16   { return result.outputVersion }
func (result WorkerResult) Output() json.RawMessage { return cloneJSON(result.output) }
func (result WorkerResult) Reason() string          { return result.reason }
func (result WorkerResult) RetryAt() time.Time      { return result.retryAt }

func (result WorkerResult) MarshalJSON() ([]byte, error) {
	switch result.kind {
	case ResultSucceeded:
		return json.Marshal(struct {
			Kind          string          `json:"kind"`
			OutputVersion uint16          `json:"outputVersion"`
			Output        json.RawMessage `json:"output"`
		}{Kind: "succeeded", OutputVersion: result.outputVersion, Output: result.output})
	case ResultRetry:
		return json.Marshal(struct {
			Kind    string    `json:"kind"`
			Reason  string    `json:"reason"`
			RetryAt time.Time `json:"retryAt"`
		}{Kind: "retry", Reason: result.reason, RetryAt: result.retryAt})
	case ResultTerminalFailure:
		return json.Marshal(struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}{Kind: "terminal_failure", Reason: result.reason})
	default:
		return nil, ErrContract
	}
}

func (result *WorkerResult) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Kind string `json:"kind"`
	}
	if err := strictJSON(data, &discriminator); err != nil {
		// The discriminator intentionally permits other fields. Decode it once
		// without strictness, then strictly decode the selected closed variant.
		if err := json.Unmarshal(data, &discriminator); err != nil {
			return ErrContract
		}
	}
	var parsed WorkerResult
	var err error
	switch discriminator.Kind {
	case "succeeded":
		var value struct {
			Kind          string          `json:"kind"`
			OutputVersion uint16          `json:"outputVersion"`
			Output        json.RawMessage `json:"output"`
		}
		if err = strictJSON(data, &value); err == nil {
			parsed, err = Succeeded(value.OutputVersion, value.Output)
		}
	case "retry":
		var value struct {
			Kind    string    `json:"kind"`
			Reason  string    `json:"reason"`
			RetryAt time.Time `json:"retryAt"`
		}
		if err = strictJSON(data, &value); err == nil {
			parsed, err = Retry(value.Reason, value.RetryAt)
		}
	case "terminal_failure":
		var value struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}
		if err = strictJSON(data, &value); err == nil {
			parsed, err = TerminalFailure(value.Reason)
		}
	default:
		err = ErrContract
	}
	if err != nil {
		return ErrContract
	}
	*result = parsed
	return nil
}

type Completion struct {
	Organization access.OrganizationID
	JobID        JobID
	Attempt      uint16
	Fence        uint64
	WorkerID     string
	Result       WorkerResult
}

func (completion Completion) Validate() error {
	if !completion.Organization.Valid() || !completion.JobID.Valid() || completion.Attempt == 0 ||
		completion.Fence == 0 || !bounded(completion.WorkerID, 1, 128) || completion.Result.kind == ResultUnknown {
		return ErrContract
	}
	return nil
}

type EventEnvelope struct {
	ID                EventID         `json:"eventId"`
	Schema            string          `json:"schema"`
	SchemaVersion     uint16          `json:"schemaVersion"`
	OrganizationID    string          `json:"organizationId"`
	AggregateType     string          `json:"aggregateType"`
	AggregateID       string          `json:"aggregateId"`
	AggregateRevision uint64          `json:"aggregateRevision"`
	CausationID       string          `json:"causationId,omitempty"`
	CorrelationID     string          `json:"correlationId"`
	OccurredAt        time.Time       `json:"occurredAt"`
	Payload           json.RawMessage `json:"payload"`
}

func (envelope EventEnvelope) Validate() error {
	organization, organizationError := access.ParseOrganizationID(envelope.OrganizationID)
	if !envelope.ID.Valid() || !organization.Valid() || organizationError != nil ||
		!bounded(envelope.Schema, 1, 128) || envelope.SchemaVersion == 0 ||
		!bounded(envelope.OrganizationID, 1, 64) || !bounded(envelope.AggregateType, 1, 64) ||
		!bounded(envelope.AggregateID, 1, 256) || envelope.AggregateRevision == 0 ||
		(envelope.CausationID != "" && !bounded(envelope.CausationID, 1, 128)) ||
		!bounded(envelope.CorrelationID, 1, 128) || envelope.OccurredAt.IsZero() || !object(envelope.Payload) {
		return ErrContract
	}
	return nil
}

type OutboxMessage struct {
	Subject         string
	Envelope        EventEnvelope
	PublisherID     string
	PublishAttempts uint16
	LeaseExpiresAt  time.Time
}

type OutboxClaim struct {
	PublisherID   string
	LeaseDuration time.Duration
	Limit         int
}

func (claim OutboxClaim) Validate() error {
	if !bounded(claim.PublisherID, 1, 128) || claim.LeaseDuration < time.Second ||
		claim.LeaseDuration > time.Hour || claim.Limit < 1 || claim.Limit > 500 {
		return ErrContract
	}
	return nil
}

type Ledger interface {
	Enqueue(context.Context, EnqueueCommand) (EnqueueResult, error)
	FindJob(context.Context, access.OrganizationID, JobID) (Job, bool, error)
	Claim(context.Context, ClaimRequest) (Assignment, bool, error)
	Renew(context.Context, Assignment, time.Duration) (Assignment, error)
	Complete(context.Context, Completion) (Job, error)
	Cancel(context.Context, access.OrganizationID, JobID, string) (Job, error)
}

type Outbox interface {
	ClaimOutbox(context.Context, OutboxClaim) ([]OutboxMessage, error)
	MarkOutboxPublished(context.Context, string, EventID) error
	ReleaseOutbox(context.Context, string, EventID, string, time.Time, uint16) error
	RequeuePublished(context.Context, time.Time, int) (int64, error)
}

type ConsumerReceipt struct {
	Consumer       string
	Event          EventEnvelope
	OutcomeVersion uint16
	Outcome        json.RawMessage
}

func (receipt ConsumerReceipt) Validate() error {
	if !bounded(receipt.Consumer, 1, 128) || receipt.Event.Validate() != nil ||
		receipt.OutcomeVersion == 0 || !object(receipt.Outcome) {
		return ErrContract
	}
	return nil
}

type ProviderReceipt struct {
	Provider        string
	AccountID       string
	ExternalEventID string
	PayloadHash     [32]byte
	OccurredAt      time.Time
	OutcomeVersion  uint16
	Outcome         json.RawMessage
}

func (receipt ProviderReceipt) Validate() error {
	if !bounded(receipt.Provider, 1, 64) || !bounded(receipt.AccountID, 1, 256) ||
		!bounded(receipt.ExternalEventID, 1, 512) || receipt.PayloadHash == [32]byte{} ||
		receipt.OccurredAt.IsZero() || receipt.OutcomeVersion == 0 || !object(receipt.Outcome) {
		return ErrContract
	}
	return nil
}

type Maintenance interface {
	ExpireDeadlines(context.Context, int) (int, error)
	ReplayDeadLetter(context.Context, access.OrganizationID, JobID, string, time.Time) (Job, error)
	SetConcurrencyLimit(context.Context, access.OrganizationID, string, int) error
}

func bounded(value string, minimum, maximum int) bool {
	return len(value) >= minimum && len(value) <= maximum
}

func object(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}' && json.Valid(trimmed)
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func strictJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrContract
	}
	return nil
}
