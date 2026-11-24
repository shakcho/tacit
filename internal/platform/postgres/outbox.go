package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/durable"
)

var _ durable.Outbox = (*Store)(nil)

const outboxColumns = `
    event_id, organization_id, subject, schema_name, schema_version,
    aggregate_type, aggregate_id, aggregate_revision, causation_id,
    correlation_id, occurred_at, payload, publisher_id, publish_attempts,
    publish_lease_expires_at`

const outboxReturningColumns = `
    outbox.event_id, outbox.organization_id, outbox.subject, outbox.schema_name, outbox.schema_version,
    outbox.aggregate_type, outbox.aggregate_id, outbox.aggregate_revision, outbox.causation_id,
    outbox.correlation_id, outbox.occurred_at, outbox.payload, outbox.publisher_id, outbox.publish_attempts,
    outbox.publish_lease_expires_at`

func (store *Store) ClaimOutbox(ctx context.Context, claim durable.OutboxClaim) ([]durable.OutboxMessage, error) {
	if err := claim.Validate(); err != nil {
		return nil, err
	}
	now := store.timeNow()
	leaseExpiresAt := now.Add(claim.LeaseDuration)
	messages := make([]durable.OutboxMessage, 0, claim.Limit)
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		rows, err := transaction.Query(ctx, `
            WITH candidates AS (
                SELECT event_id
                FROM tacit_runtime_outbox
                WHERE available_at <= $1
                  AND (state = 'pending' OR (state = 'publishing' AND publish_lease_expires_at <= $1))
                ORDER BY available_at, occurred_at, event_id
                LIMIT $2
                FOR UPDATE SKIP LOCKED
            ), claimed AS (
                UPDATE tacit_runtime_outbox outbox
                SET state = 'publishing', publisher_id = $3, publish_lease_expires_at = $4,
                    publish_attempts = outbox.publish_attempts + 1, last_error = NULL
                FROM candidates
                WHERE outbox.event_id = candidates.event_id
                RETURNING `+outboxReturningColumns+`
            )
            SELECT `+outboxColumns+` FROM claimed ORDER BY occurred_at, event_id`,
			now, claim.Limit, claim.PublisherID, leaseExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("claim durable outbox: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			message, err := scanOutbox(rows)
			if err != nil {
				return fmt.Errorf("scan durable outbox: %w", err)
			}
			messages = append(messages, message)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read durable outbox: %w", err)
		}
		return nil
	})
	return messages, err
}

func (store *Store) MarkOutboxPublished(
	ctx context.Context,
	publisherID string,
	eventID durable.EventID,
) error {
	if publisherID == "" || !eventID.Valid() {
		return durable.ErrContract
	}
	now := store.timeNow()
	tag, err := store.pool.Exec(ctx, `
        UPDATE tacit_runtime_outbox
        SET state = 'published', publisher_id = NULL, publish_lease_expires_at = NULL,
            published_at = $3, last_error = NULL
        WHERE event_id = $1 AND state = 'publishing' AND publisher_id = $2
          AND publish_lease_expires_at > $3`,
		databaseUUID(eventID.Bytes()), publisherID, now,
	)
	if err != nil {
		return fmt.Errorf("mark durable outbox published: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return durable.ErrLeaseLost
	}
	return nil
}

func (store *Store) ReleaseOutbox(
	ctx context.Context,
	publisherID string,
	eventID durable.EventID,
	reason string,
	retryAt time.Time,
	maximumAttempts uint16,
) error {
	if publisherID == "" || !eventID.Valid() || len(reason) < 1 || len(reason) > 2048 ||
		retryAt.IsZero() || maximumAttempts == 0 {
		return durable.ErrContract
	}
	now := store.timeNow()
	tag, err := store.pool.Exec(ctx, `
        UPDATE tacit_runtime_outbox
        SET state = CASE WHEN publish_attempts >= $5 THEN 'dead' ELSE 'pending' END,
            publisher_id = NULL, publish_lease_expires_at = NULL,
            available_at = CASE WHEN publish_attempts >= $5 THEN available_at ELSE $4 END,
            last_error = $3
        WHERE event_id = $1 AND state = 'publishing' AND publisher_id = $2
          AND publish_lease_expires_at > $6`,
		databaseUUID(eventID.Bytes()), publisherID, reason, retryAt.UTC(), maximumAttempts, now,
	)
	if err != nil {
		return fmt.Errorf("release durable outbox: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return durable.ErrLeaseLost
	}
	return nil
}

func (store *Store) RequeuePublished(ctx context.Context, publishedBefore time.Time, limit int) (int64, error) {
	if publishedBefore.IsZero() || limit < 1 || limit > 10_000 {
		return 0, durable.ErrContract
	}
	now := store.timeNow()
	tag, err := store.pool.Exec(ctx, `
        WITH replay AS (
            SELECT event_id FROM tacit_runtime_outbox
            WHERE state = 'published' AND published_at <= $1
            ORDER BY published_at, event_id
            LIMIT $2
            FOR UPDATE SKIP LOCKED
        )
        UPDATE tacit_runtime_outbox outbox
        SET state = 'pending', available_at = $3, published_at = NULL,
            publisher_id = NULL, publish_lease_expires_at = NULL
        FROM replay WHERE outbox.event_id = replay.event_id`,
		publishedBefore.UTC(), limit, now,
	)
	if err != nil {
		return 0, fmt.Errorf("requeue published durable events: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RecordConsumerReceipt records a broker delivery outcome. Domain consumers use
// recordConsumerReceipt inside the same owner transaction as their canonical
// effect; this public operation is for consumers whose receipt is the effect.
func (store *Store) RecordConsumerReceipt(ctx context.Context, receipt durable.ConsumerReceipt) (bool, error) {
	if err := receipt.Validate(); err != nil {
		return false, err
	}
	replayed := false
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		var err error
		replayed, err = recordConsumerReceipt(ctx, transaction, receipt)
		return err
	})
	return replayed, err
}

func recordConsumerReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	receipt durable.ConsumerReceipt,
) (bool, error) {
	payload, err := json.Marshal(receipt.Event)
	if err != nil {
		return false, fmt.Errorf("encode consumer event: %w", err)
	}
	hash := sha256.Sum256(payload)
	tag, err := transaction.Exec(ctx, `
        INSERT INTO tacit_runtime_consumer_inbox (
            consumer, event_id, payload_hash, outcome_version, outcome
        ) VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (consumer, event_id) DO NOTHING`,
		receipt.Consumer, databaseUUID(receipt.Event.ID.Bytes()), hash[:], receipt.OutcomeVersion, receipt.Outcome,
	)
	if err != nil {
		return false, fmt.Errorf("record consumer receipt: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return false, nil
	}
	var priorHash []byte
	var priorVersion int16
	var priorOutcome []byte
	if err := transaction.QueryRow(ctx, `
        SELECT payload_hash, outcome_version, outcome
        FROM tacit_runtime_consumer_inbox
        WHERE consumer = $1 AND event_id = $2`,
		receipt.Consumer, databaseUUID(receipt.Event.ID.Bytes()),
	).Scan(&priorHash, &priorVersion, &priorOutcome); err != nil {
		return false, fmt.Errorf("read consumer receipt: %w", err)
	}
	if !bytes.Equal(priorHash, hash[:]) || priorVersion != int16(receipt.OutcomeVersion) ||
		!jsonEqual(priorOutcome, receipt.Outcome) {
		return false, durable.ErrDuplicateConflict
	}
	return true, nil
}

// RecordProviderReceipt is the receipt-only variant used when no canonical
// product effect is required. Provider owner operations call
// recordProviderReceipt inside their own transaction with the state change.
func (store *Store) RecordProviderReceipt(ctx context.Context, receipt durable.ProviderReceipt) (bool, error) {
	if err := receipt.Validate(); err != nil {
		return false, err
	}
	replayed := false
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		var err error
		replayed, err = recordProviderReceipt(ctx, transaction, receipt)
		return err
	})
	return replayed, err
}

func recordProviderReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	receipt durable.ProviderReceipt,
) (bool, error) {
	tag, err := transaction.Exec(ctx, `
        INSERT INTO tacit_runtime_provider_inbox (
            provider, account_id, external_event_id, payload_hash, occurred_at,
            outcome_version, outcome
        ) VALUES ($1, $2, $3, $4, $5, $6, $7)
        ON CONFLICT (provider, account_id, external_event_id) DO NOTHING`,
		receipt.Provider, receipt.AccountID, receipt.ExternalEventID, receipt.PayloadHash[:],
		receipt.OccurredAt.UTC(), receipt.OutcomeVersion, receipt.Outcome,
	)
	if err != nil {
		return false, fmt.Errorf("record provider receipt: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return false, nil
	}
	var priorHash []byte
	var priorVersion int16
	var priorOutcome []byte
	if err := transaction.QueryRow(ctx, `
        SELECT payload_hash, outcome_version, outcome
        FROM tacit_runtime_provider_inbox
        WHERE provider = $1 AND account_id = $2 AND external_event_id = $3`,
		receipt.Provider, receipt.AccountID, receipt.ExternalEventID,
	).Scan(&priorHash, &priorVersion, &priorOutcome); err != nil {
		return false, fmt.Errorf("read provider receipt: %w", err)
	}
	if !bytes.Equal(priorHash, receipt.PayloadHash[:]) || priorVersion != int16(receipt.OutcomeVersion) ||
		!jsonEqual(priorOutcome, receipt.Outcome) {
		return false, durable.ErrDuplicateConflict
	}
	return true, nil
}

func scanOutbox(row rowScanner) (durable.OutboxMessage, error) {
	var eventID pgtype.UUID
	var organizationID pgtype.UUID
	var schemaVersion int16
	var aggregateRevision int64
	var causationID *string
	var payload []byte
	var publisherID *string
	var publishAttempts int16
	var leaseExpiresAt *time.Time
	message := durable.OutboxMessage{}
	envelope := durable.EventEnvelope{}
	if err := row.Scan(
		&eventID, &organizationID, &message.Subject, &envelope.Schema, &schemaVersion,
		&envelope.AggregateType, &envelope.AggregateID, &aggregateRevision, &causationID,
		&envelope.CorrelationID, &envelope.OccurredAt, &payload, &publisherID, &publishAttempts,
		&leaseExpiresAt,
	); err != nil {
		return durable.OutboxMessage{}, err
	}
	id, err := durable.EventIDFromBytes(eventID.Bytes)
	if err != nil || !organizationID.Valid || aggregateRevision < 1 || publishAttempts < 1 {
		return durable.OutboxMessage{}, errors.New("stored durable outbox message is invalid")
	}
	organization, err := access.ParseOrganizationID("org_" + hex.EncodeToString(organizationID.Bytes[:]))
	if err != nil {
		return durable.OutboxMessage{}, errors.New("stored durable outbox tenant is invalid")
	}
	envelope.ID = id
	envelope.SchemaVersion = uint16(schemaVersion)
	envelope.OrganizationID = organization.String()
	envelope.AggregateRevision = uint64(aggregateRevision)
	envelope.Payload = append(json.RawMessage(nil), payload...)
	if causationID != nil {
		envelope.CausationID = *causationID
	}
	message.Envelope = envelope
	message.PublishAttempts = uint16(publishAttempts)
	if publisherID != nil {
		message.PublisherID = *publisherID
	}
	if leaseExpiresAt != nil {
		message.LeaseExpiresAt = leaseExpiresAt.UTC()
	}
	return message, nil
}

func jsonEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil &&
		reflect.DeepEqual(leftValue, rightValue)
}
