package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/durable"
)

var _ durable.Maintenance = (*Store)(nil)

func (store *Store) ExpireDeadlines(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1_000 {
		return 0, durable.ErrContract
	}
	now := store.timeNow()
	expired := 0
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		rows, err := transaction.Query(ctx, `
            SELECT `+jobColumns+`
            FROM tacit_runtime_jobs
            WHERE deadline_at <= $1 AND state NOT IN ('succeeded', 'failed', 'canceled')
            ORDER BY deadline_at, id
            LIMIT $2
            FOR UPDATE SKIP LOCKED`, now, limit)
		if err != nil {
			return fmt.Errorf("select expired durable jobs: %w", err)
		}
		jobs := make([]durable.Job, 0, limit)
		for rows.Next() {
			job, err := scanJob(rows)
			if err != nil {
				rows.Close()
				return fmt.Errorf("scan expired durable job: %w", err)
			}
			jobs = append(jobs, job)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("read expired durable jobs: %w", err)
		}
		rows.Close()
		for _, job := range jobs {
			if err := failJob(ctx, transaction, job, now, "job deadline exceeded"); err != nil {
				return err
			}
			expired++
		}
		return nil
	})
	return expired, err
}

func (store *Store) ReplayDeadLetter(
	ctx context.Context,
	organization access.OrganizationID,
	jobID durable.JobID,
	operatorID string,
	availableAt time.Time,
) (durable.Job, error) {
	if !organization.Valid() || !jobID.Valid() || len(operatorID) < 1 || len(operatorID) > 128 || availableAt.IsZero() {
		return durable.Job{}, durable.ErrContract
	}
	now := store.timeNow()
	var replayed durable.Job
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		job, err := scanJob(transaction.QueryRow(ctx, `SELECT `+jobColumns+`
            FROM tacit_runtime_jobs WHERE organization_id = $1 AND id = $2 FOR UPDATE`,
			databaseUUID(organization.Bytes()), databaseUUID(jobID.Bytes()),
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return durable.ErrJobNotClaimable
		}
		if err != nil {
			return fmt.Errorf("lock dead-letter replay: %w", err)
		}
		if job.State != durable.JobStateFailed || job.MaxAttempts >= 100 {
			return durable.ErrJobTerminal
		}
		tag, err := transaction.Exec(ctx, `
            UPDATE tacit_runtime_dead_letters
            SET replay_count = replay_count + 1, replayed_at = $3, replayed_by = $4
            WHERE organization_id = $1 AND id = (
                SELECT id FROM tacit_runtime_dead_letters
                WHERE organization_id = $1 AND job_id = $2
                ORDER BY failed_at DESC, id DESC LIMIT 1
                FOR UPDATE
            )`,
			databaseUUID(organization.Bytes()), databaseUUID(jobID.Bytes()), now, operatorID,
		)
		if err != nil {
			return fmt.Errorf("record dead-letter replay: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return errors.New("failed durable job has no dead letter")
		}
		if _, err := transaction.Exec(ctx, `
            UPDATE tacit_runtime_jobs
            SET state = 'retry_wait', available_at = $3, deadline_at = NULL,
                max_attempts = max_attempts + 1, output_version = NULL, output = NULL,
                terminal_reason = NULL, completed_at = NULL,
                event_revision = event_revision + 1, updated_at = $4
            WHERE organization_id = $1 AND id = $2`,
			databaseUUID(organization.Bytes()), databaseUUID(jobID.Bytes()), availableAt.UTC(), now,
		); err != nil {
			return fmt.Errorf("replay dead-letter job: %w", err)
		}
		if err := insertJobEvent(ctx, transaction, organization, jobID, job.EventRevision+1,
			job.CorrelationID, job.CausationID, "tacit.runtime.job.ready", now, availableAt.UTC(),
			json.RawMessage(fmt.Sprintf(`{"jobId":%q,"workload":%q}`, job.ID.String(), job.Workload))); err != nil {
			return err
		}
		replayed, _, err = findJob(ctx, transaction, organization, jobID)
		return err
	})
	return replayed, err
}

// SetConcurrencyLimit upserts one workload limit. A zero organization sets the
// workload-wide limit; a valid organization sets its tenant limit. Passing
// maximumRunning zero removes that limit.
func (store *Store) SetConcurrencyLimit(
	ctx context.Context,
	organization access.OrganizationID,
	workload string,
	maximumRunning int,
) error {
	if len(workload) < 1 || len(workload) > 64 || maximumRunning < 0 {
		return durable.ErrContract
	}
	var organizationValue any
	lockScope := "global"
	if organization.Valid() {
		organizationValue = databaseUUID(organization.Bytes())
		lockScope = organization.String()
	}
	return store.withTransaction(ctx, func(transaction pgx.Tx) error {
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			"tacit-concurrency:"+lockScope+":"+workload); err != nil {
			return fmt.Errorf("lock concurrency limit: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
            DELETE FROM tacit_runtime_concurrency_limits
            WHERE organization_id IS NOT DISTINCT FROM $1 AND workload = $2`, organizationValue, workload); err != nil {
			return fmt.Errorf("replace concurrency limit: %w", err)
		}
		if maximumRunning == 0 {
			return nil
		}
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_runtime_concurrency_limits (organization_id, workload, maximum_running)
            VALUES ($1, $2, $3)`, organizationValue, workload, maximumRunning); err != nil {
			return fmt.Errorf("insert concurrency limit: %w", err)
		}
		return nil
	})
}
