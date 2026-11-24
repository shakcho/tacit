package postgres

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/durable"
)

const jobColumns = `
    organization_id, id, kind, workload, state, event_revision, input_version, input,
    resource_type, resource_id, resource_revision, correlation_id, causation_id,
    priority, available_at, deadline_at, max_attempts, attempt_count, fence,
    lease_owner, lease_expires_at, output_version, output, terminal_reason,
    created_at, updated_at, completed_at`

var _ durable.Ledger = (*Store)(nil)

type rowScanner interface {
	Scan(...any) error
}

type jobQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (store *Store) Enqueue(ctx context.Context, command durable.EnqueueCommand) (durable.EnqueueResult, error) {
	if err := command.Validate(); err != nil {
		return durable.EnqueueResult{}, err
	}
	fingerprint, err := command.Fingerprint()
	if err != nil {
		return durable.EnqueueResult{}, err
	}
	now := store.timeNow()
	var result durable.EnqueueResult
	err = store.withTransaction(ctx, func(transaction pgx.Tx) error {
		lockName := command.Organization.String() + ":" + command.OperationScope + ":" + command.Key.String()
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockName); err != nil {
			return fmt.Errorf("lock durable command: %w", err)
		}

		priorFingerprint, priorJobID, found, err := findCommandOutcome(ctx, transaction, command)
		if err != nil {
			return err
		}
		if found {
			if !bytes.Equal(priorFingerprint, fingerprint[:]) {
				return durable.ErrIdempotencyConflict
			}
			job, exists, err := findJob(ctx, transaction, command.Organization, priorJobID)
			if err != nil {
				return err
			}
			if !exists {
				return errors.New("durable command outcome references a missing job")
			}
			result = durable.EnqueueResult{Job: job, Replayed: true}
			return nil
		}

		jobBytes, err := randomUUID()
		if err != nil {
			return err
		}
		jobID, _ := durable.JobIDFromBytes(jobBytes)
		deadline := nullableTime(command.DeadlineAt)
		causation := nullableString(command.CausationID)
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_runtime_jobs (
                organization_id, id, kind, workload, state, input_version, input,
                resource_type, resource_id, resource_revision, correlation_id, causation_id,
                priority, available_at, deadline_at, max_attempts
            ) VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			databaseUUID(command.Organization.Bytes()), databaseUUID(jobBytes), command.Kind, command.Workload,
			command.InputVersion, command.Input, command.ResourceType, command.ResourceID, command.ResourceRevision,
			command.CorrelationID, causation, command.Priority, command.AvailableAt.UTC(), deadline, command.MaxAttempts,
		); err != nil {
			return fmt.Errorf("insert durable job: %w", err)
		}

		outcome, err := json.Marshal(struct {
			JobID string `json:"jobId"`
		}{JobID: jobID.String()})
		if err != nil {
			return fmt.Errorf("encode durable command outcome: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_runtime_command_outcomes (
                organization_id, operation_scope, client_key, fingerprint_version,
                request_fingerprint, outcome_version, outcome
            ) VALUES ($1, $2, $3, 1, $4, 1, $5)`,
			databaseUUID(command.Organization.Bytes()), command.OperationScope, command.Key.String(), fingerprint[:], outcome,
		); err != nil {
			return fmt.Errorf("record durable command outcome: %w", err)
		}
		if err := insertJobEvent(ctx, transaction, command.Organization, jobID, 1, command.CorrelationID,
			command.CausationID, "tacit.runtime.job.ready", now, command.AvailableAt, json.RawMessage(fmt.Sprintf(
				`{"jobId":%q,"workload":%q}`, jobID.String(), command.Workload,
			))); err != nil {
			return err
		}
		job, _, err := findJob(ctx, transaction, command.Organization, jobID)
		if err != nil {
			return err
		}
		result = durable.EnqueueResult{Job: job}
		return nil
	})
	return result, err
}

func findCommandOutcome(
	ctx context.Context,
	query jobQueryer,
	command durable.EnqueueCommand,
) ([]byte, durable.JobID, bool, error) {
	var fingerprint []byte
	var outcome []byte
	err := query.QueryRow(ctx, `
        SELECT request_fingerprint, outcome
        FROM tacit_runtime_command_outcomes
        WHERE organization_id = $1 AND operation_scope = $2 AND client_key = $3`,
		databaseUUID(command.Organization.Bytes()), command.OperationScope, command.Key.String(),
	).Scan(&fingerprint, &outcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, durable.JobID{}, false, nil
	}
	if err != nil {
		return nil, durable.JobID{}, false, fmt.Errorf("find durable command outcome: %w", err)
	}
	var decoded struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(outcome, &decoded); err != nil {
		return nil, durable.JobID{}, false, fmt.Errorf("decode durable command outcome: %w", err)
	}
	jobID, err := durable.ParseJobID(decoded.JobID)
	if err != nil {
		return nil, durable.JobID{}, false, errors.New("stored durable command outcome is invalid")
	}
	return fingerprint, jobID, true, nil
}

func (store *Store) FindJob(
	ctx context.Context,
	organization access.OrganizationID,
	jobID durable.JobID,
) (durable.Job, bool, error) {
	if !organization.Valid() || !jobID.Valid() {
		return durable.Job{}, false, durable.ErrContract
	}
	return findJob(ctx, store.pool, organization, jobID)
}

func findJob(
	ctx context.Context,
	query jobQueryer,
	organization access.OrganizationID,
	jobID durable.JobID,
) (durable.Job, bool, error) {
	row := query.QueryRow(ctx, `SELECT `+jobColumns+`
        FROM tacit_runtime_jobs WHERE organization_id = $1 AND id = $2`,
		databaseUUID(organization.Bytes()), databaseUUID(jobID.Bytes()),
	)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return durable.Job{}, false, nil
	}
	if err != nil {
		return durable.Job{}, false, fmt.Errorf("find durable job: %w", err)
	}
	return job, true, nil
}

func (store *Store) Claim(ctx context.Context, request durable.ClaimRequest) (durable.Assignment, bool, error) {
	if err := request.Validate(); err != nil {
		return durable.Assignment{}, false, err
	}
	now := store.timeNow()
	leaseExpiresAt := now.Add(request.LeaseDuration)
	var assignment durable.Assignment
	claimed := false
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		if _, err := transaction.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "tacit-workload:"+request.Workload,
		); err != nil {
			return fmt.Errorf("lock workload claim: %w", err)
		}
		row := transaction.QueryRow(ctx, `
            SELECT `+jobColumns+`
            FROM tacit_runtime_jobs candidate
            WHERE candidate.workload = $1
              AND (candidate.deadline_at IS NULL OR candidate.deadline_at > $2)
              AND (
                    (candidate.state IN ('queued', 'retry_wait') AND candidate.available_at <= $2
                        AND candidate.attempt_count < candidate.max_attempts)
                 OR (candidate.state = 'running' AND candidate.lease_expires_at <= $2)
              )
              AND (
                    SELECT count(*) FROM tacit_runtime_jobs active
                    WHERE active.workload = candidate.workload
                      AND active.state = 'running' AND active.lease_expires_at > $2
              ) < COALESCE((
                    SELECT maximum_running FROM tacit_runtime_concurrency_limits limits
                    WHERE limits.organization_id IS NULL AND limits.workload = candidate.workload
              ), 2147483647)
              AND (
                    SELECT count(*) FROM tacit_runtime_jobs active
                    WHERE active.organization_id = candidate.organization_id
                      AND active.workload = candidate.workload
                      AND active.state = 'running' AND active.lease_expires_at > $2
              ) < COALESCE((
                    SELECT maximum_running FROM tacit_runtime_concurrency_limits limits
                    WHERE limits.organization_id = candidate.organization_id
                      AND limits.workload = candidate.workload
              ), 2147483647)
            ORDER BY candidate.priority DESC, candidate.available_at, candidate.created_at, candidate.id
            LIMIT 1
            FOR UPDATE SKIP LOCKED`, request.Workload, now)
		job, err := scanJob(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select durable job claim: %w", err)
		}
		if job.State == durable.JobStateRunning {
			if job.AttemptCount >= job.MaxAttempts {
				if err := failJob(ctx, transaction, job, now, "maximum attempts exhausted after lease expiry"); err != nil {
					return err
				}
				return nil
			}
			if _, err := transaction.Exec(ctx, `
                    UPDATE tacit_runtime_job_attempts
                    SET state = 'expired', finished_at = $4, reason = 'lease expired'
                    WHERE organization_id = $1 AND job_id = $2 AND attempt = $3 AND state = 'running'`,
				databaseUUID(job.Organization.Bytes()), databaseUUID(job.ID.Bytes()), job.AttemptCount, now,
			); err != nil {
				return fmt.Errorf("expire durable job attempt: %w", err)
			}
		}
		attempt := job.AttemptCount + 1
		fence := job.Fence + 1
		if _, err := transaction.Exec(ctx, `
                UPDATE tacit_runtime_jobs
                SET state = 'running', attempt_count = $3, fence = $4,
                    lease_owner = $5, lease_expires_at = $6, updated_at = $7
                WHERE organization_id = $1 AND id = $2`,
			databaseUUID(job.Organization.Bytes()), databaseUUID(job.ID.Bytes()), attempt, fence,
			request.WorkerID, leaseExpiresAt, now,
		); err != nil {
			return fmt.Errorf("claim durable job: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
                INSERT INTO tacit_runtime_job_attempts (
                    organization_id, job_id, attempt, fence, worker_id, state, started_at, lease_expires_at
                ) VALUES ($1, $2, $3, $4, $5, 'running', $6, $7)`,
			databaseUUID(job.Organization.Bytes()), databaseUUID(job.ID.Bytes()), attempt, fence,
			request.WorkerID, now, leaseExpiresAt,
		); err != nil {
			return fmt.Errorf("insert durable job attempt: %w", err)
		}
		job, _, err = findJob(ctx, transaction, job.Organization, job.ID)
		if err != nil {
			return err
		}
		assignment = durable.Assignment{
			Job: job, Attempt: attempt, Fence: fence, WorkerID: request.WorkerID, LeaseExpiresAt: leaseExpiresAt,
		}
		claimed = true
		return nil
	})
	return assignment, claimed, err
}

func (store *Store) Renew(
	ctx context.Context,
	assignment durable.Assignment,
	duration time.Duration,
) (durable.Assignment, error) {
	if !assignment.Job.ID.Valid() || !assignment.Job.Organization.Valid() || assignment.Attempt == 0 ||
		assignment.Fence == 0 || assignment.WorkerID == "" || duration < time.Second || duration > 24*time.Hour {
		return durable.Assignment{}, durable.ErrContract
	}
	now := store.timeNow()
	leaseExpiresAt := now.Add(duration)
	tag, err := store.pool.Exec(ctx, `
        WITH renewed AS (
            UPDATE tacit_runtime_jobs
            SET lease_expires_at = $6, updated_at = $5
            WHERE organization_id = $1 AND id = $2 AND state = 'running'
              AND attempt_count = $3 AND fence = $4 AND lease_owner = $7 AND lease_expires_at > $5
            RETURNING organization_id, id
        )
        UPDATE tacit_runtime_job_attempts attempt
        SET lease_expires_at = $6
        FROM renewed
        WHERE attempt.organization_id = renewed.organization_id AND attempt.job_id = renewed.id
          AND attempt.attempt = $3 AND attempt.fence = $4 AND attempt.state = 'running'`,
		databaseUUID(assignment.Job.Organization.Bytes()), databaseUUID(assignment.Job.ID.Bytes()),
		assignment.Attempt, assignment.Fence, now, leaseExpiresAt, assignment.WorkerID,
	)
	if err != nil {
		return durable.Assignment{}, fmt.Errorf("renew durable job lease: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return durable.Assignment{}, durable.ErrLeaseLost
	}
	assignment.LeaseExpiresAt = leaseExpiresAt
	assignment.Job.LeaseExpiresAt = leaseExpiresAt
	assignment.Job.UpdatedAt = now
	return assignment, nil
}

func (store *Store) Complete(ctx context.Context, completion durable.Completion) (durable.Job, error) {
	if err := completion.Validate(); err != nil {
		return durable.Job{}, err
	}
	now := store.timeNow()
	var completed durable.Job
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		row := transaction.QueryRow(ctx, `SELECT `+jobColumns+`
            FROM tacit_runtime_jobs WHERE organization_id = $1 AND id = $2 FOR UPDATE`,
			databaseUUID(completion.Organization.Bytes()), databaseUUID(completion.JobID.Bytes()),
		)
		job, err := scanJob(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return durable.ErrLeaseLost
		}
		if err != nil {
			return fmt.Errorf("lock durable job completion: %w", err)
		}
		if job.State != durable.JobStateRunning || job.AttemptCount != completion.Attempt ||
			job.Fence != completion.Fence || job.LeaseOwner != completion.WorkerID ||
			!job.LeaseExpiresAt.After(now) {
			replayed, err := completionMatches(ctx, transaction, completion)
			if err != nil {
				return err
			}
			if replayed {
				completed = job
				return nil
			}
			return durable.ErrLeaseLost
		}

		switch completion.Result.Kind() {
		case durable.ResultSucceeded:
			if err := finishAttempt(ctx, transaction, job, "succeeded", now, "", time.Time{},
				completion.Result.OutputVersion(), completion.Result.Output()); err != nil {
				return err
			}
			if _, err := transaction.Exec(ctx, `
                    UPDATE tacit_runtime_jobs
                    SET state = 'succeeded', lease_owner = NULL, lease_expires_at = NULL,
                        output_version = $3, output = $4, terminal_reason = NULL,
                        event_revision = event_revision + 1,
                        updated_at = $5, completed_at = $5
                    WHERE organization_id = $1 AND id = $2`,
				databaseUUID(job.Organization.Bytes()), databaseUUID(job.ID.Bytes()),
				completion.Result.OutputVersion(), completion.Result.Output(), now,
			); err != nil {
				return fmt.Errorf("complete durable job: %w", err)
			}
			if err := insertJobEvent(ctx, transaction, job.Organization, job.ID, job.EventRevision+1,
				job.CorrelationID, job.CausationID, "tacit.runtime.job.succeeded", now, now,
				json.RawMessage(fmt.Sprintf(`{"attempt":%d,"fence":%d,"jobId":%q}`,
					job.AttemptCount, job.Fence, job.ID.String()))); err != nil {
				return err
			}
		case durable.ResultRetry:
			terminal := job.AttemptCount >= job.MaxAttempts ||
				(!job.DeadlineAt.IsZero() && !completion.Result.RetryAt().Before(job.DeadlineAt))
			if terminal {
				if err := failJob(ctx, transaction, job, now, completion.Result.Reason()); err != nil {
					return err
				}
			} else {
				if err := finishAttempt(ctx, transaction, job, "retry", now, completion.Result.Reason(),
					completion.Result.RetryAt(), 0, nil); err != nil {
					return err
				}
				if _, err := transaction.Exec(ctx, `
                        UPDATE tacit_runtime_jobs
                        SET state = 'retry_wait', lease_owner = NULL, lease_expires_at = NULL,
                            available_at = $3, terminal_reason = $4,
                            event_revision = event_revision + 1, updated_at = $5
                        WHERE organization_id = $1 AND id = $2`,
					databaseUUID(job.Organization.Bytes()), databaseUUID(job.ID.Bytes()),
					completion.Result.RetryAt(), completion.Result.Reason(), now,
				); err != nil {
					return fmt.Errorf("schedule durable job retry: %w", err)
				}
				if err := insertJobEvent(ctx, transaction, job.Organization, job.ID, job.EventRevision+1,
					job.CorrelationID, job.CausationID, "tacit.runtime.job.ready", now, completion.Result.RetryAt(),
					json.RawMessage(fmt.Sprintf(`{"jobId":%q,"workload":%q}`, job.ID.String(), job.Workload))); err != nil {
					return err
				}
			}
		case durable.ResultTerminalFailure:
			if err := failJob(ctx, transaction, job, now, completion.Result.Reason()); err != nil {
				return err
			}
		default:
			return durable.ErrContract
		}
		completed, _, err = findJob(ctx, transaction, job.Organization, job.ID)
		return err
	})
	return completed, err
}

func finishAttempt(
	ctx context.Context,
	transaction pgx.Tx,
	job durable.Job,
	state string,
	now time.Time,
	reason string,
	retryAt time.Time,
	outputVersion uint16,
	output json.RawMessage,
) error {
	var version any
	var result any
	var retry any
	if !retryAt.IsZero() {
		retry = retryAt.UTC()
	}
	if outputVersion > 0 {
		version = outputVersion
		result = output
	}
	if _, err := transaction.Exec(ctx, `
        UPDATE tacit_runtime_job_attempts
        SET state = $4, finished_at = $5, reason = $6, retry_at = $7,
            output_version = $8, output = $9
        WHERE organization_id = $1 AND job_id = $2 AND attempt = $3 AND state = 'running'`,
		databaseUUID(job.Organization.Bytes()), databaseUUID(job.ID.Bytes()), job.AttemptCount,
		state, now, nullableString(reason), retry, version, result,
	); err != nil {
		return fmt.Errorf("finish durable job attempt: %w", err)
	}
	return nil
}

func completionMatches(ctx context.Context, query jobQueryer, completion durable.Completion) (bool, error) {
	var state string
	var reason *string
	var retryAt *time.Time
	var outputVersion *int16
	var output []byte
	err := query.QueryRow(ctx, `
        SELECT state, reason, retry_at, output_version, output
        FROM tacit_runtime_job_attempts
        WHERE organization_id = $1 AND job_id = $2 AND attempt = $3
          AND fence = $4 AND worker_id = $5`,
		databaseUUID(completion.Organization.Bytes()), databaseUUID(completion.JobID.Bytes()),
		completion.Attempt, completion.Fence, completion.WorkerID,
	).Scan(&state, &reason, &retryAt, &outputVersion, &output)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read durable completion receipt: %w", err)
	}
	storedReason := ""
	if reason != nil {
		storedReason = *reason
	}
	switch completion.Result.Kind() {
	case durable.ResultSucceeded:
		return state == "succeeded" && outputVersion != nil &&
			uint16(*outputVersion) == completion.Result.OutputVersion() &&
			jsonEqual(output, completion.Result.Output()), nil
	case durable.ResultRetry:
		return state == "retry" && storedReason == completion.Result.Reason() && retryAt != nil &&
			retryAt.Equal(completion.Result.RetryAt()), nil
	case durable.ResultTerminalFailure:
		return state == "failed" && storedReason == completion.Result.Reason(), nil
	default:
		return false, durable.ErrContract
	}
}

func failJob(ctx context.Context, transaction pgx.Tx, job durable.Job, now time.Time, reason string) error {
	if err := finishAttempt(ctx, transaction, job, "failed", now, reason, time.Time{}, 0, nil); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
        UPDATE tacit_runtime_jobs
        SET state = 'failed', lease_owner = NULL, lease_expires_at = NULL,
            terminal_reason = $3, event_revision = event_revision + 1,
            updated_at = $4, completed_at = $4
        WHERE organization_id = $1 AND id = $2`,
		databaseUUID(job.Organization.Bytes()), databaseUUID(job.ID.Bytes()), reason, now,
	); err != nil {
		return fmt.Errorf("fail durable job: %w", err)
	}
	deadLetterID, err := randomUUID()
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
        INSERT INTO tacit_runtime_dead_letters (organization_id, id, job_id, attempt, reason, failed_at)
        VALUES ($1, $2, $3, $4, $5, $6)`,
		databaseUUID(job.Organization.Bytes()), databaseUUID(deadLetterID), databaseUUID(job.ID.Bytes()),
		job.AttemptCount, reason, now,
	); err != nil {
		return fmt.Errorf("dead-letter durable job: %w", err)
	}
	return insertJobEvent(ctx, transaction, job.Organization, job.ID, job.EventRevision+1,
		job.CorrelationID, job.CausationID, "tacit.runtime.job.failed", now, now,
		json.RawMessage(fmt.Sprintf(`{"attempt":%d,"jobId":%q,"reason":%q}`,
			job.AttemptCount, job.ID.String(), reason)))
}

func (store *Store) Cancel(
	ctx context.Context,
	organization access.OrganizationID,
	jobID durable.JobID,
	reason string,
) (durable.Job, error) {
	if !organization.Valid() || !jobID.Valid() || len(reason) < 1 || len(reason) > 2048 {
		return durable.Job{}, durable.ErrContract
	}
	now := store.timeNow()
	var canceled durable.Job
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		job, err := scanJob(transaction.QueryRow(ctx, `SELECT `+jobColumns+`
            FROM tacit_runtime_jobs WHERE organization_id = $1 AND id = $2 FOR UPDATE`,
			databaseUUID(organization.Bytes()), databaseUUID(jobID.Bytes()),
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return durable.ErrJobNotClaimable
		}
		if err != nil {
			return fmt.Errorf("lock durable job cancellation: %w", err)
		}
		if job.State == durable.JobStateCanceled {
			canceled = job
			return nil
		}
		if job.State.Terminal() {
			return durable.ErrJobTerminal
		}
		if job.State == durable.JobStateRunning {
			if err := finishAttempt(ctx, transaction, job, "canceled", now, reason, time.Time{}, 0, nil); err != nil {
				return err
			}
		}
		if _, err := transaction.Exec(ctx, `
            UPDATE tacit_runtime_jobs
            SET state = 'canceled', lease_owner = NULL, lease_expires_at = NULL,
                terminal_reason = $3, event_revision = event_revision + 1,
                updated_at = $4, completed_at = $4
            WHERE organization_id = $1 AND id = $2`,
			databaseUUID(organization.Bytes()), databaseUUID(jobID.Bytes()), reason, now,
		); err != nil {
			return fmt.Errorf("cancel durable job: %w", err)
		}
		if err := insertJobEvent(ctx, transaction, organization, jobID, job.EventRevision+1,
			job.CorrelationID, job.CausationID, "tacit.runtime.job.canceled", now, now,
			json.RawMessage(fmt.Sprintf(`{"jobId":%q,"reason":%q}`, jobID.String(), reason))); err != nil {
			return err
		}
		canceled, _, err = findJob(ctx, transaction, organization, jobID)
		return err
	})
	return canceled, err
}

func insertJobEvent(
	ctx context.Context,
	transaction pgx.Tx,
	organization access.OrganizationID,
	jobID durable.JobID,
	revision uint64,
	correlationID string,
	causationID string,
	schema string,
	occurredAt time.Time,
	availableAt time.Time,
	payload json.RawMessage,
) error {
	eventID, err := randomUUID()
	if err != nil {
		return err
	}
	subject := "tacit.runtime.jobs"
	if _, err := transaction.Exec(ctx, `
        INSERT INTO tacit_runtime_outbox (
            event_id, organization_id, subject, schema_name, schema_version,
            aggregate_type, aggregate_id, aggregate_revision, causation_id,
            correlation_id, occurred_at, payload, available_at
        ) VALUES ($1, $2, $3, $4, 1, 'job', $5, $6, $7, $8, $9, $10, $11)`,
		databaseUUID(eventID), databaseUUID(organization.Bytes()), subject, schema, jobID.String(), revision,
		nullableString(causationID), correlationID, occurredAt, payload, availableAt,
	); err != nil {
		return fmt.Errorf("insert durable job outbox event: %w", err)
	}
	return nil
}

func scanJob(row rowScanner) (durable.Job, error) {
	var organizationID pgtype.UUID
	var jobID pgtype.UUID
	var state string
	var eventRevision int64
	var input []byte
	var inputVersion int16
	var resourceRevision int64
	var causationID *string
	var deadlineAt *time.Time
	var maxAttempts int16
	var attemptCount int16
	var fence int64
	var leaseOwner *string
	var leaseExpiresAt *time.Time
	var outputVersion *int16
	var output []byte
	var terminalReason *string
	var completedAt *time.Time
	job := durable.Job{}
	if err := row.Scan(
		&organizationID, &jobID, &job.Kind, &job.Workload, &state, &eventRevision, &inputVersion, &input,
		&job.ResourceType, &job.ResourceID, &resourceRevision, &job.CorrelationID, &causationID,
		&job.Priority, &job.AvailableAt, &deadlineAt, &maxAttempts, &attemptCount, &fence,
		&leaseOwner, &leaseExpiresAt, &outputVersion, &output, &terminalReason,
		&job.CreatedAt, &job.UpdatedAt, &completedAt,
	); err != nil {
		return durable.Job{}, err
	}
	organization, err := access.ParseOrganizationID("org_" + hex.EncodeToString(organizationID.Bytes[:]))
	if err != nil {
		return durable.Job{}, errors.New("stored durable job organization is invalid")
	}
	id, err := durable.JobIDFromBytes(jobID.Bytes)
	if err != nil {
		return durable.Job{}, errors.New("stored durable job ID is invalid")
	}
	parsedState, err := durable.ParseJobState(state)
	if err != nil || eventRevision < 1 || resourceRevision < 1 || fence < 0 {
		return durable.Job{}, errors.New("stored durable job state is invalid")
	}
	job.ID = id
	job.Organization = organization
	job.State = parsedState
	job.EventRevision = uint64(eventRevision)
	job.InputVersion = uint16(inputVersion)
	job.Input = append(json.RawMessage(nil), input...)
	job.ResourceRevision = uint64(resourceRevision)
	job.MaxAttempts = uint16(maxAttempts)
	job.AttemptCount = uint16(attemptCount)
	job.Fence = uint64(fence)
	job.Output = append(json.RawMessage(nil), output...)
	if causationID != nil {
		job.CausationID = *causationID
	}
	if deadlineAt != nil {
		job.DeadlineAt = deadlineAt.UTC()
	}
	if leaseOwner != nil {
		job.LeaseOwner = *leaseOwner
	}
	if leaseExpiresAt != nil {
		job.LeaseExpiresAt = leaseExpiresAt.UTC()
	}
	if outputVersion != nil {
		job.OutputVersion = uint16(*outputVersion)
	}
	if terminalReason != nil {
		job.TerminalReason = *terminalReason
	}
	if completedAt != nil {
		job.CompletedAt = completedAt.UTC()
	}
	return job, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
