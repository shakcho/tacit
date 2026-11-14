package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/shakcho/tacit/internal/access"
)

const removeMembershipScope = "access.remove-membership.v1"

var _ access.Directory = (*Store)(nil)

func (store *Store) CreateUser(ctx context.Context, command access.CreateUserCommand) error {
	_, err := store.pool.Exec(ctx, `INSERT INTO tacit_users (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, databaseUUID(command.ID.Bytes()))
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (store *Store) CreateOrganization(ctx context.Context, command access.CreateOrganizationCommand) error {
	return store.withTransaction(ctx, func(transaction pgx.Tx) error {
		organizationID := databaseUUID(command.ID.Bytes())
		ownerID := databaseUUID(command.Owner.Bytes())
		if _, err := transaction.Exec(ctx,
			`INSERT INTO tacit_organizations (id, name) VALUES ($1, $2)`, organizationID, command.Name.String(),
		); err != nil {
			return fmt.Errorf("insert organization: %w", err)
		}
		if _, err := transaction.Exec(ctx,
			`INSERT INTO tacit_organization_memberships (organization_id, user_id) VALUES ($1, $2)`, organizationID, ownerID,
		); err != nil {
			return fmt.Errorf("insert owner membership: %w", err)
		}
		if _, err := transaction.Exec(ctx,
			`INSERT INTO tacit_membership_roles (organization_id, user_id, role) VALUES ($1, $2, 'admin')`, organizationID, ownerID,
		); err != nil {
			return fmt.Errorf("insert owner role: %w", err)
		}
		return insertAudit(ctx, transaction, command.ID, command.Owner, command.Owner, "organization.created")
	})
}

func (store *Store) AddMembership(ctx context.Context, command access.AddMembershipCommand) (access.MembershipResult, error) {
	result := access.NewMembershipResult(access.MembershipUnavailable)
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		if err := lockOrganization(ctx, transaction, command.Ref.Organization); err != nil {
			return err
		}
		organizationID := databaseUUID(command.Ref.Organization.Bytes())
		userID := databaseUUID(command.Ref.User.Bytes())
		tag, err := transaction.Exec(ctx,
			`INSERT INTO tacit_organization_memberships (organization_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			organizationID, userID,
		)
		if err != nil {
			return fmt.Errorf("insert membership: %w", err)
		}
		if tag.RowsAffected() == 0 {
			result = access.NewMembershipResult(access.MembershipConflict)
			return nil
		}
		if err := insertRoles(ctx, transaction, command.Ref, command.Roles); err != nil {
			return err
		}
		if err := insertAudit(ctx, transaction, command.Ref.Organization, command.Actor, command.Ref.User, "membership.added"); err != nil {
			return err
		}
		result = access.NewMembershipResult(access.MembershipChanged)
		return nil
	})
	return result, err
}

func (store *Store) SetMembershipRoles(ctx context.Context, command access.SetMembershipRolesCommand) (access.MembershipResult, error) {
	result := access.NewMembershipResult(access.MembershipUnavailable)
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		if err := lockOrganization(ctx, transaction, command.Ref.Organization); err != nil {
			return err
		}
		current, found, err := findRoles(ctx, transaction, command.Ref)
		if err != nil {
			return err
		}
		if !found {
			result = access.NewMembershipResult(access.MembershipNotFound)
			return nil
		}
		if sameRoles(current, command.Roles) {
			result = access.NewMembershipResult(access.MembershipUnchanged)
			return nil
		}
		if current.Has(access.RoleAdmin) && !command.Roles.Has(access.RoleAdmin) {
			last, err := isLastAdmin(ctx, transaction, command.Ref.Organization)
			if err != nil {
				return err
			}
			if last {
				result = access.NewMembershipResult(access.MembershipLastAdmin)
				return nil
			}
		}
		organizationID := databaseUUID(command.Ref.Organization.Bytes())
		userID := databaseUUID(command.Ref.User.Bytes())
		if _, err := transaction.Exec(ctx,
			`DELETE FROM tacit_membership_roles WHERE organization_id = $1 AND user_id = $2`, organizationID, userID,
		); err != nil {
			return fmt.Errorf("replace membership roles: %w", err)
		}
		if err := insertRoles(ctx, transaction, command.Ref, command.Roles); err != nil {
			return err
		}
		if _, err := transaction.Exec(ctx,
			`UPDATE tacit_organization_memberships SET updated_at = now() WHERE organization_id = $1 AND user_id = $2`, organizationID, userID,
		); err != nil {
			return fmt.Errorf("update membership timestamp: %w", err)
		}
		if err := insertAudit(ctx, transaction, command.Ref.Organization, command.Actor, command.Ref.User, "membership.roles.changed"); err != nil {
			return err
		}
		result = access.NewMembershipResult(access.MembershipChanged)
		return nil
	})
	return result, err
}

func (store *Store) RemoveMembership(ctx context.Context, command access.RemoveMembershipCommand) (access.MembershipResult, error) {
	result := access.NewMembershipResult(access.MembershipUnavailable)
	fingerprint := removeMembershipFingerprint(command)
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		lockName := command.Ref.Organization.String() + ":" + removeMembershipScope + ":" + command.Key.String()
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockName); err != nil {
			return fmt.Errorf("lock idempotency key: %w", err)
		}
		priorFingerprint, priorCode, found, err := findReceipt(ctx, transaction, command)
		if err != nil {
			return err
		}
		if found {
			if !bytes.Equal(priorFingerprint, fingerprint[:]) {
				result = access.NewMembershipResult(access.MembershipConflict)
				return nil
			}
			if access.MembershipOutcome(priorCode) != access.MembershipChanged {
				return fmt.Errorf("stored membership result is invalid")
			}
			result = access.NewReplayedMembershipResult(access.MembershipChanged)
			return nil
		}
		if err := lockOrganization(ctx, transaction, command.Ref.Organization); err != nil {
			return err
		}
		roles, memberFound, err := findRoles(ctx, transaction, command.Ref)
		if err != nil {
			return err
		}
		if !memberFound {
			result = access.NewMembershipResult(access.MembershipNotFound)
			return nil
		}
		if roles.Has(access.RoleAdmin) {
			last, err := isLastAdmin(ctx, transaction, command.Ref.Organization)
			if err != nil {
				return err
			}
			if last {
				result = access.NewMembershipResult(access.MembershipLastAdmin)
				return nil
			}
		}
		organizationID := databaseUUID(command.Ref.Organization.Bytes())
		userID := databaseUUID(command.Ref.User.Bytes())
		if _, err := transaction.Exec(ctx,
			`DELETE FROM tacit_organization_memberships WHERE organization_id = $1 AND user_id = $2`, organizationID, userID,
		); err != nil {
			return fmt.Errorf("remove membership: %w", err)
		}
		if err := insertAudit(ctx, transaction, command.Ref.Organization, command.Actor, command.Ref.User, "membership.removed"); err != nil {
			return err
		}
		result = access.NewMembershipResult(access.MembershipChanged)
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_idempotency_records (
                organization_id, operation_scope, client_key, fingerprint_version,
                request_fingerprint, result_version, result_code
            ) VALUES ($1, $2, $3, 1, $4, 1, $5)`,
			organizationID, removeMembershipScope, command.Key.String(), fingerprint[:], result.Outcome(),
		); err != nil {
			return fmt.Errorf("record membership command: %w", err)
		}
		return nil
	})
	return result, err
}

func (store *Store) FindMembership(ctx context.Context, ref access.MembershipRef) (access.Membership, bool, error) {
	roles, found, err := findRoles(ctx, store.pool, ref)
	if err != nil {
		return access.Membership{}, false, err
	}
	if !found {
		return access.Membership{}, false, nil
	}
	return access.Membership{Ref: ref, Roles: roles}, true, nil
}

func (store *Store) withTransaction(ctx context.Context, operation func(pgx.Tx) error) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin database transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if err := operation(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit database transaction: %w", err)
	}
	return nil
}

func lockOrganization(ctx context.Context, transaction pgx.Tx, organization access.OrganizationID) error {
	var exists bool
	err := transaction.QueryRow(ctx,
		`SELECT true FROM tacit_organizations WHERE id = $1 FOR UPDATE`, databaseUUID(organization.Bytes()),
	).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("organization not found")
	}
	if err != nil {
		return fmt.Errorf("lock organization: %w", err)
	}
	return nil
}

type rowQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func findRoles(ctx context.Context, query rowQueryer, ref access.MembershipRef) (access.Roles, bool, error) {
	organizationID := databaseUUID(ref.Organization.Bytes())
	userID := databaseUUID(ref.User.Bytes())
	var member bool
	err := queryMembership(ctx, query, organizationID, userID, &member)
	if err != nil {
		return access.Roles{}, false, err
	}
	if !member {
		return access.Roles{}, false, nil
	}
	rows, err := query.Query(ctx,
		`SELECT role FROM tacit_membership_roles WHERE organization_id = $1 AND user_id = $2 ORDER BY role`,
		organizationID, userID,
	)
	if err != nil {
		return access.Roles{}, false, fmt.Errorf("query membership roles: %w", err)
	}
	defer rows.Close()
	values := make([]access.Role, 0, 3)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return access.Roles{}, false, fmt.Errorf("scan membership role: %w", err)
		}
		role, err := parseRole(value)
		if err != nil {
			return access.Roles{}, false, err
		}
		values = append(values, role)
	}
	if err := rows.Err(); err != nil {
		return access.Roles{}, false, fmt.Errorf("query membership roles: %w", err)
	}
	roles, err := access.NewRoles(values...)
	if err != nil {
		return access.Roles{}, false, fmt.Errorf("stored membership has no valid roles: %w", err)
	}
	return roles, true, nil
}

func queryMembership(ctx context.Context, query rowQueryer, organizationID, userID pgtype.UUID, exists *bool) error {
	rows, err := query.Query(ctx,
		`SELECT true FROM tacit_organization_memberships WHERE organization_id = $1 AND user_id = $2`,
		organizationID, userID,
	)
	if err != nil {
		return fmt.Errorf("find membership: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(exists); err != nil {
			return fmt.Errorf("scan membership: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("find membership: %w", err)
	}
	return nil
}

func insertRoles(ctx context.Context, transaction pgx.Tx, ref access.MembershipRef, roles access.Roles) error {
	for _, role := range roles.Values() {
		if _, err := transaction.Exec(ctx,
			`INSERT INTO tacit_membership_roles (organization_id, user_id, role) VALUES ($1, $2, $3)`,
			databaseUUID(ref.Organization.Bytes()), databaseUUID(ref.User.Bytes()), role.String(),
		); err != nil {
			return fmt.Errorf("insert membership role: %w", err)
		}
	}
	return nil
}

func isLastAdmin(ctx context.Context, transaction pgx.Tx, organization access.OrganizationID) (bool, error) {
	var count int
	if err := transaction.QueryRow(ctx,
		`SELECT count(*) FROM tacit_membership_roles WHERE organization_id = $1 AND role = 'admin'`,
		databaseUUID(organization.Bytes()),
	).Scan(&count); err != nil {
		return false, fmt.Errorf("count organization administrators: %w", err)
	}
	return count <= 1, nil
}

func insertAudit(ctx context.Context, transaction pgx.Tx, organization access.OrganizationID, actor, target access.UserID, action string) error {
	auditID, err := randomUUID()
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
        INSERT INTO tacit_access_audit (organization_id, id, actor_user_id, target_user_id, action)
        VALUES ($1, $2, $3, $4, $5)`,
		databaseUUID(organization.Bytes()), databaseUUID(auditID), databaseUUID(actor.Bytes()), databaseUUID(target.Bytes()), action,
	); err != nil {
		return fmt.Errorf("insert access audit: %w", err)
	}
	return nil
}

func randomUUID() ([16]byte, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return value, fmt.Errorf("generate audit ID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return value, nil
}

func findReceipt(ctx context.Context, transaction pgx.Tx, command access.RemoveMembershipCommand) ([]byte, int16, bool, error) {
	var fingerprint []byte
	var resultCode int16
	err := transaction.QueryRow(ctx, `
        SELECT request_fingerprint, result_code
        FROM tacit_idempotency_records
        WHERE organization_id = $1 AND operation_scope = $2 AND client_key = $3`,
		databaseUUID(command.Ref.Organization.Bytes()), removeMembershipScope, command.Key.String(),
	).Scan(&fingerprint, &resultCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("find idempotency record: %w", err)
	}
	return fingerprint, resultCode, true, nil
}

func removeMembershipFingerprint(command access.RemoveMembershipCommand) [32]byte {
	value := command.Actor.String() + "\x00" + command.Ref.Organization.String() + "\x00" + command.Ref.User.String()
	return sha256.Sum256([]byte(value))
}

func databaseUUID(value [16]byte) pgtype.UUID { return pgtype.UUID{Bytes: value, Valid: true} }

func parseRole(value string) (access.Role, error) {
	switch value {
	case "admin":
		return access.RoleAdmin, nil
	case "editor":
		return access.RoleEditor, nil
	case "viewer":
		return access.RoleViewer, nil
	default:
		return 0, fmt.Errorf("unknown stored role %q", value)
	}
}

func sameRoles(left, right access.Roles) bool {
	return slices.Equal(left.Values(), right.Values())
}
