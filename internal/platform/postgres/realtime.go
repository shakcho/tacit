package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/realtime"
)

const realtimeOperationTimeout = 500 * time.Millisecond

type RealtimeFeed struct {
	store *Store
	slots chan struct{}
}

var _ realtime.Feed = (*RealtimeFeed)(nil)

func NewRealtimeFeed(store *Store) *RealtimeFeed {
	return &RealtimeFeed{store: store, slots: make(chan struct{}, 2)}
}

func appendSessionChange(ctx context.Context, tx pgx.Tx, org, user pgtype.UUID, kind realtime.ChangeKind) error {
	// Advance the head in the transaction that owns the canonical mutation. Its
	// row lock is held through commit, so assignment and commit order agree.
	var head, floor int64
	if err := tx.QueryRow(ctx, `
		UPDATE tacit_realtime_heads
		SET head = head + 1,
		    floor = GREATEST(floor, head + 1 - 1024),
		    floor_event_id = CASE WHEN head < 1024 THEN floor_event_id ELSE (
		        SELECT event_id FROM tacit_realtime_changes
		        WHERE organization_id = $1 AND position = head + 1 - 1024
		    ) END
		WHERE organization_id = $1
		RETURNING head, floor
	`, org).Scan(&head, &floor); err != nil {
		return fmt.Errorf("advance realtime head: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tacit_realtime_changes (
		    organization_id, position, kind, resource_type, resource_id, audience_user_id, resource_revision
		) VALUES ($1, $2, $3, $4, $5, $5, $2)
	`, org, head, string(kind), realtime.SessionResource, user); err != nil {
		return fmt.Errorf("append realtime session change: %w", err)
	}
	if floor > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM tacit_realtime_changes WHERE organization_id = $1 AND position <= $2
		`, org, floor); err != nil {
			return fmt.Errorf("prune realtime changes: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `SELECT pg_notify('tacit_realtime_v1', $1::uuid::text)`, org); err != nil {
		return fmt.Errorf("notify realtime change: %w", err)
	}
	return nil
}

func appendMembershipChange(ctx context.Context, tx pgx.Tx, ref access.MembershipRef, kind realtime.ChangeKind) error {
	return appendSessionChange(ctx, tx, databaseUUID(ref.Organization.Bytes()), databaseUUID(ref.User.Bytes()), kind)
}

func (feed *RealtimeFeed) transaction(parent context.Context, options pgx.TxOptions, operation func(context.Context, pgx.Tx) error) error {
	ctx, cancel := context.WithTimeout(parent, realtimeOperationTimeout)
	defer cancel()
	select {
	case feed.slots <- struct{}{}:
		defer func() { <-feed.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	tx, err := feed.store.pool.BeginTx(ctx, options)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := operation(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var realtimeRead = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

func (feed *RealtimeFeed) SessionSnapshot(ctx context.Context, principal access.LocalPrincipal) (snapshot realtime.Snapshot, err error) {
	err = feed.transaction(ctx, realtimeRead, func(ctx context.Context, tx pgx.Tx) error {
		fresh, err := currentSession(ctx, tx, principal)
		if err != nil {
			return err
		}
		snapshot.Principal = fresh
		return scanPoint(tx.QueryRow(ctx, `
			SELECT head, COALESCE(changes.event_id, floor_event_id)
			FROM tacit_realtime_heads AS heads
			LEFT JOIN tacit_realtime_changes AS changes
			  ON changes.organization_id = heads.organization_id AND changes.position = heads.head
			WHERE heads.organization_id = $1 AND (head = floor OR changes.event_id IS NOT NULL)
		`, databaseUUID(fresh.OrganizationID().Bytes())), &snapshot.Point)
	})
	return snapshot, err
}

func (feed *RealtimeFeed) ResolveCursor(ctx context.Context, org access.OrganizationID, event realtime.EventID) (point realtime.Point, err error) {
	err = feed.transaction(ctx, realtimeRead, func(ctx context.Context, tx pgx.Tx) error {
		return scanPoint(tx.QueryRow(ctx, `
			SELECT floor, floor_event_id FROM tacit_realtime_heads
			WHERE organization_id = $1 AND floor_event_id = $2
			UNION ALL
			SELECT position, event_id FROM tacit_realtime_changes WHERE organization_id = $1 AND event_id = $2
		`, databaseUUID(org.Bytes()), databaseUUID(event)), &point)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		err = realtime.ErrResync
	}
	return point, err
}

func scanPoint(row pgx.Row, point *realtime.Point) error {
	var event pgtype.UUID
	if err := row.Scan(&point.Position, &event); err != nil {
		return err
	}
	point.Event = event.Bytes
	return nil
}

func (feed *RealtimeFeed) Replay(ctx context.Context, org access.OrganizationID, after realtime.Point) (page realtime.Page, err error) {
	err = feed.transaction(ctx, realtimeRead, func(ctx context.Context, tx pgx.Tx) error {
		organization := databaseUUID(org.Bytes())
		var floor int64
		var previous pgtype.UUID
		err := tx.QueryRow(ctx, `
			SELECT head, floor, CASE WHEN floor = $2 THEN floor_event_id ELSE changes.event_id END
			FROM tacit_realtime_heads AS heads
			LEFT JOIN tacit_realtime_changes AS changes
			  ON changes.organization_id = heads.organization_id AND changes.position = $2
			WHERE heads.organization_id = $1
		`, organization, after.Position).Scan(&page.Head, &floor, &previous)
		if errors.Is(err, pgx.ErrNoRows) {
			return realtime.ErrResync
		}
		if err != nil {
			return err
		}
		if after.Position < floor || after.Position > page.Head || page.Head-after.Position > realtime.MaxReplay ||
			!previous.Valid || previous.Bytes != [16]byte(after.Event) {
			return realtime.ErrResync
		}
		rows, err := tx.Query(ctx, `
			SELECT position, event_id, kind, resource_type, resource_id, audience_user_id, resource_revision
			FROM tacit_realtime_changes
			WHERE organization_id = $1 AND position > $2 AND position <= $3
			ORDER BY position LIMIT $4
		`, organization, after.Position, page.Head, realtime.PageSize)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var change realtime.Change
			var event pgtype.UUID
			var resource, audience pgtype.UUID
			if err := rows.Scan(&change.Position, &event, &change.Kind, &change.Resource, &resource, &audience, &change.Revision); err != nil {
				return err
			}
			change.Event = event.Bytes
			if resource != audience || change.Position != after.Position+int64(len(page.Changes))+1 {
				return realtime.ErrResync
			}
			if audience.Valid {
				id, err := access.ParseUserID("usr_" + hex.EncodeToString(audience.Bytes[:]))
				if err != nil {
					return realtime.ErrResync
				}
				change.Audience = id
			}
			page.Changes = append(page.Changes, change)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if int64(len(page.Changes)) != min(int64(realtime.PageSize), page.Head-after.Position) {
			return realtime.ErrResync
		}
		return nil
	})
	return page, err
}

func appendIdentityChange(
	ctx context.Context, tx pgx.Tx, namespace access.IdentityNamespace,
	organization access.ExternalOrganizationID, subject access.ExternalSubject, kind realtime.ChangeKind,
) error {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT mappings.organization_id, subjects.user_id
		FROM tacit_identity_organizations AS mappings
		LEFT JOIN tacit_identity_subjects AS subjects
		  ON subjects.provider = mappings.namespace AND subjects.subject = $3
		WHERE mappings.namespace = $1
		  AND ($2 = '' OR mappings.external_organization_id = $2)
		  AND ($3 = '' OR (subjects.user_id IS NOT NULL AND EXISTS (
		      SELECT 1 FROM tacit_identity_membership_admissions AS admissions
		      WHERE admissions.namespace = mappings.namespace
		        AND admissions.external_organization_id = mappings.external_organization_id
		        AND admissions.subject = $3
		  )))
		ORDER BY mappings.organization_id
	`, namespace.String(), organization.String(), subject.String())
	if err != nil {
		return err
	}
	type target struct{ organization, user pgtype.UUID }
	targets, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (target, error) {
		var value target
		err := row.Scan(&value.organization, &value.user)
		return value, err
	})
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := appendSessionChange(ctx, tx, target.organization, target.user, kind); err != nil {
			return err
		}
	}
	return nil
}

func (feed *RealtimeFeed) Authorize(ctx context.Context, principal access.LocalPrincipal, deliver func() error) error {
	return feed.transaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, tx pgx.Tx) error {
		identity, ok := principal.RevalidationIdentity()
		if !ok {
			return access.ErrNotAdmitted
		}
		if err := lockIdentityNamespace(ctx, tx, identity.Namespace, true); err != nil {
			return err
		}
		var exists bool
		err := tx.QueryRow(ctx, `SELECT true FROM tacit_organizations WHERE id = $1 FOR SHARE`,
			databaseUUID(principal.OrganizationID().Bytes())).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return access.ErrNotAdmitted
		}
		if err != nil {
			return err
		}
		fresh, err := currentSession(ctx, tx, principal)
		if err != nil {
			return err
		}
		if !access.AllowsAuthenticationSession(fresh, access.ActionSubscribe, principal.UserID()) {
			return access.ErrNotAdmitted
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return deliver()
	})
}

func currentSession(ctx context.Context, query rowScannerQueryer, principal access.LocalPrincipal) (access.LocalPrincipal, error) {
	identity, ok := principal.RevalidationIdentity()
	if !ok || !principal.ValidAt(time.Now()) {
		return access.LocalPrincipal{}, access.ErrNotAdmitted
	}
	admission, found, err := resolveIdentity(ctx, query, identity, access.ExactOrganization(principal.OrganizationID()))
	if err != nil {
		return access.LocalPrincipal{}, err
	}
	fresh, same := principal.Revalidated(admission)
	if !found || !same || !fresh.ValidAt(time.Now()) ||
		!access.AllowsAuthenticationSession(fresh, access.ActionRead, principal.UserID()) {
		return access.LocalPrincipal{}, access.ErrNotAdmitted
	}
	return fresh, nil
}
