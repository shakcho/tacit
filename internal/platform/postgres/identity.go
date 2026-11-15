package postgres

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/shakcho/tacit/internal/access"
)

var (
	_ access.AdmissionResolver    = (*Store)(nil)
	_ access.SessionDenials       = (*Store)(nil)
	_ access.IdentityLifecycle    = (*Store)(nil)
	_ access.IdentityBootstrapper = (*Store)(nil)
	_ access.IdentityInvitations  = (*Store)(nil)
)

func (store *Store) ResolveIdentity(
	ctx context.Context,
	identity access.VerifiedIdentity,
	target access.OrganizationTarget,
) (access.LocalPrincipal, bool, error) {
	if !target.Valid() {
		return access.LocalPrincipal{}, false, errors.New("invalid organization target")
	}
	exactOrganization, exact := target.Organization()
	var requestedOrganization any
	if exact {
		requestedOrganization = databaseUUID(exactOrganization.Bytes())
	}
	var userUUID, organizationUUID pgtype.UUID
	var roleValues []string
	err := store.pool.QueryRow(ctx, `
        SELECT subjects.user_id, organizations.organization_id,
               array_agg(roles.role ORDER BY roles.role)
        FROM tacit_identity_subjects AS subjects
        JOIN tacit_identity_organizations AS organizations
          ON organizations.namespace = subjects.provider
         AND organizations.external_organization_id = $3
        JOIN tacit_identity_membership_admissions AS admissions
          ON admissions.namespace = subjects.provider
         AND admissions.external_organization_id = organizations.external_organization_id
         AND admissions.subject = subjects.subject
         AND admissions.state = 'active'
        JOIN tacit_organization_memberships AS memberships
          ON memberships.organization_id = organizations.organization_id
         AND memberships.user_id = subjects.user_id
        JOIN tacit_membership_roles AS roles
          ON roles.organization_id = memberships.organization_id
         AND roles.user_id = memberships.user_id
        LEFT JOIN tacit_identity_session_denials AS denials
          ON denials.namespace = subjects.provider
         AND denials.session_id = $4
        LEFT JOIN tacit_identity_subject_states AS subject_state
          ON subject_state.namespace = subjects.provider
         AND subject_state.subject = subjects.subject
		LEFT JOIN tacit_identity_organization_states AS organization_state
		  ON organization_state.namespace = subjects.provider
		 AND organization_state.external_organization_id = organizations.external_organization_id
        WHERE subjects.provider = $1
          AND subjects.subject = $2
          AND denials.session_id IS NULL
          AND COALESCE(subject_state.blocked, false) = false
		  AND COALESCE(organization_state.blocked, false) = false
          AND (subject_state.not_before IS NULL OR $5 > subject_state.not_before)
          AND ($6 = false OR organizations.organization_id = $7)
        GROUP BY subjects.user_id, organizations.organization_id`,
		identity.Namespace.String(), identity.Subject.String(), identity.Organization.String(), identity.Session.String(),
		identity.IssuedAt, exact, requestedOrganization,
	).Scan(&userUUID, &organizationUUID, &roleValues)
	if errors.Is(err, pgx.ErrNoRows) {
		return access.LocalPrincipal{}, false, nil
	}
	if err != nil {
		return access.LocalPrincipal{}, false, fmt.Errorf("resolve identity admission: %w", err)
	}
	user, err := access.ParseUserID("usr_" + hex.EncodeToString(userUUID.Bytes[:]))
	if err != nil {
		return access.LocalPrincipal{}, false, fmt.Errorf("parse stored user identity: %w", err)
	}
	organization, err := access.ParseOrganizationID("org_" + hex.EncodeToString(organizationUUID.Bytes[:]))
	if err != nil {
		return access.LocalPrincipal{}, false, fmt.Errorf("parse stored organization identity: %w", err)
	}
	roles, err := storedRoles(roleValues)
	if err != nil {
		return access.LocalPrincipal{}, false, err
	}
	return access.LocalPrincipal{User: user, Organization: organization, Roles: roles}, true, nil
}

func (store *Store) DenySession(ctx context.Context, identity access.VerifiedIdentity) error {
	if identity.Namespace.String() == "" || identity.Session.String() == "" {
		return errors.New("invalid identity session")
	}
	if _, err := store.pool.Exec(ctx, `
        INSERT INTO tacit_identity_session_denials (namespace, session_id)
        VALUES ($1, $2)
        ON CONFLICT (namespace, session_id) DO NOTHING`,
		identity.Namespace.String(), identity.Session.String(),
	); err != nil {
		return fmt.Errorf("deny identity session: %w", err)
	}
	return nil
}

func (store *Store) ApplyLifecycle(ctx context.Context, event access.LifecycleEvent) (access.LifecycleOutcome, error) {
	if err := event.Validate(); err != nil {
		return access.LifecycleUnavailable, err
	}
	result := access.LifecycleUnavailable
	err := store.withTransaction(ctx, func(transaction pgx.Tx) error {
		outcome := "applied"
		if event.Kind == access.LifecycleIgnored {
			outcome = "ignored"
		}
		tag, err := transaction.Exec(ctx, `
            INSERT INTO tacit_identity_event_inbox (
                namespace, event_id, event_kind, payload_hash, occurred_at, outcome
            ) VALUES ($1, $2, $3, $4, $5, $6)
            ON CONFLICT (namespace, event_id) DO NOTHING`,
			event.Namespace.String(), event.ID.String(), int16(event.Kind), event.PayloadHash[:], event.OccurredAt, outcome,
		)
		if err != nil {
			return fmt.Errorf("record identity event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			var existingHash []byte
			if err := transaction.QueryRow(ctx, `
                    SELECT payload_hash FROM tacit_identity_event_inbox
                    WHERE namespace = $1 AND event_id = $2`,
				event.Namespace.String(), event.ID.String(),
			).Scan(&existingHash); err != nil {
				return fmt.Errorf("read identity event replay: %w", err)
			}
			if !bytes.Equal(existingHash, event.PayloadHash[:]) {
				return access.ErrLifecycleConflict
			}
			result = access.LifecycleReplayed
			return nil
		}
		switch event.Kind {
		case access.LifecycleIgnored:
			result = access.LifecycleRecordedIgnored
		case access.LifecycleSessionRevoked:
			if _, err := transaction.Exec(ctx, `
                    INSERT INTO tacit_identity_session_denials (namespace, session_id)
                    VALUES ($1, $2) ON CONFLICT (namespace, session_id) DO NOTHING`,
				event.Namespace.String(), event.Session.String(),
			); err != nil {
				return fmt.Errorf("apply session revocation: %w", err)
			}
			result = access.LifecycleApplied
		case access.LifecycleSubjectBlocked:
			if _, err := transaction.Exec(ctx, `
                    INSERT INTO tacit_identity_subject_states (
                        namespace, subject, blocked, not_before, source_event_id
                    ) VALUES ($1, $2, true, $3, $4)
                    ON CONFLICT (namespace, subject) DO UPDATE SET
                        blocked = true,
                        not_before = GREATEST(tacit_identity_subject_states.not_before, EXCLUDED.not_before),
                        source_event_id = EXCLUDED.source_event_id,
                        updated_at = now()`,
				event.Namespace.String(), event.Subject.String(), event.OccurredAt, event.ID.String(),
			); err != nil {
				return fmt.Errorf("apply subject block: %w", err)
			}
			result = access.LifecycleApplied
		case access.LifecycleOrganizationBlocked:
			if _, err := transaction.Exec(ctx, `
                    INSERT INTO tacit_identity_organization_states (
                        namespace, external_organization_id, blocked, source_event_id
                    ) VALUES ($1, $2, true, $3)
                    ON CONFLICT (namespace, external_organization_id) DO UPDATE SET
                        blocked = true,
                        source_event_id = EXCLUDED.source_event_id,
                        updated_at = now()`,
				event.Namespace.String(), event.Organization.String(), event.ID.String(),
			); err != nil {
				return fmt.Errorf("apply organization block: %w", err)
			}
			if _, err := transaction.Exec(ctx, `
                    UPDATE tacit_identity_membership_admissions
                    SET state = 'denied', source_event_id = $3, updated_at = now()
                    WHERE namespace = $1 AND external_organization_id = $2`,
				event.Namespace.String(), event.Organization.String(), event.ID.String(),
			); err != nil {
				return fmt.Errorf("deny organization admissions: %w", err)
			}
			result = access.LifecycleApplied
		case access.LifecycleMembershipActivated:
			if err := applyMembershipActivation(ctx, transaction, event); err != nil {
				return err
			}
			result = access.LifecycleApplied
		case access.LifecycleMembershipDenied:
			if err := applyMembershipDenial(ctx, transaction, event); err != nil {
				return err
			}
			result = access.LifecycleApplied
		case access.LifecycleInvitationAccepted:
			if err := applyInvitationAcceptance(ctx, transaction, event); err != nil {
				return err
			}
			result = access.LifecycleApplied
		default:
			return access.ErrLifecycleEvent
		}
		return nil
	})
	return result, err
}

func (store *Store) RegisterInvitationGrant(ctx context.Context, grant access.InvitationGrant) error {
	if err := grant.Validate(); err != nil {
		return err
	}
	tag, err := store.pool.Exec(ctx, `
        INSERT INTO tacit_identity_invitation_grants (
            namespace, external_invitation_id, organization_id, role
        ) VALUES ($1, $2, $3, $4)
        ON CONFLICT (namespace, external_invitation_id) DO NOTHING`,
		grant.Namespace.String(), grant.Invitation.String(), databaseUUID(grant.Organization.Bytes()), grant.Role.String(),
	)
	if err != nil {
		return fmt.Errorf("register identity invitation grant: %w", err)
	}
	if tag.RowsAffected() != 0 {
		return nil
	}
	var organizationID pgtype.UUID
	var role string
	if err := store.pool.QueryRow(ctx, `
        SELECT organization_id, role
        FROM tacit_identity_invitation_grants
        WHERE namespace = $1 AND external_invitation_id = $2`,
		grant.Namespace.String(), grant.Invitation.String(),
	).Scan(&organizationID, &role); err != nil {
		return fmt.Errorf("read identity invitation grant: %w", err)
	}
	if organizationID != databaseUUID(grant.Organization.Bytes()) || role != grant.Role.String() {
		return access.ErrInvitationConflict
	}
	return nil
}

func applyInvitationAcceptance(ctx context.Context, transaction pgx.Tx, event access.LifecycleEvent) error {
	var grantOrganization, acceptedUser pgtype.UUID
	var role, state string
	var acceptedSubject pgtype.Text
	err := transaction.QueryRow(ctx, `
        SELECT organization_id, role, state, accepted_subject, accepted_user_id
        FROM tacit_identity_invitation_grants
        WHERE namespace = $1 AND external_invitation_id = $2
        FOR UPDATE`,
		event.Namespace.String(), event.Invitation.String(),
	).Scan(&grantOrganization, &role, &state, &acceptedSubject, &acceptedUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("identity invitation has no registered local grant")
	}
	if err != nil {
		return fmt.Errorf("lock identity invitation grant: %w", err)
	}
	var mappedOrganization pgtype.UUID
	err = transaction.QueryRow(ctx, `
        SELECT organization_id
        FROM tacit_identity_organizations
        WHERE namespace = $1 AND external_organization_id = $2`,
		event.Namespace.String(), event.Organization.String(),
	).Scan(&mappedOrganization)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("identity invitation organization is not mapped")
	}
	if err != nil {
		return fmt.Errorf("resolve identity invitation organization: %w", err)
	}
	if mappedOrganization != grantOrganization {
		return access.ErrInvitationConflict
	}
	if state == "accepted" {
		if !acceptedSubject.Valid || acceptedSubject.String != event.Subject.String() || !acceptedUser.Valid {
			return access.ErrInvitationConflict
		}
		return nil
	}
	userID, found, err := findIdentitySubject(ctx, transaction, event.Namespace, event.Subject)
	if err != nil {
		return err
	}
	if !found {
		generated, err := randomUUID()
		if err != nil {
			return err
		}
		userID = databaseUUID(generated)
		if _, err := transaction.Exec(ctx, `INSERT INTO tacit_users (id) VALUES ($1)`, userID); err != nil {
			return fmt.Errorf("create invited local user: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_identity_subjects (provider, subject, user_id)
            VALUES ($1, $2, $3)`,
			event.Namespace.String(), event.Subject.String(), userID,
		); err != nil {
			return fmt.Errorf("link invited identity subject: %w", err)
		}
	}
	var member bool
	if err := transaction.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM tacit_organization_memberships
            WHERE organization_id = $1 AND user_id = $2
        )`, grantOrganization, userID).Scan(&member); err != nil {
		return fmt.Errorf("find invited local membership: %w", err)
	}
	if !member {
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_organization_memberships (organization_id, user_id)
            VALUES ($1, $2)`, grantOrganization, userID,
		); err != nil {
			return fmt.Errorf("create invited local membership: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_membership_roles (organization_id, user_id, role)
            VALUES ($1, $2, $3)`, grantOrganization, userID, role,
		); err != nil {
			return fmt.Errorf("grant invited local role: %w", err)
		}
	}
	if _, err := transaction.Exec(ctx, `
        UPDATE tacit_identity_invitation_grants
        SET state = 'accepted', accepted_subject = $3, accepted_user_id = $4,
            accepted_at = $5
        WHERE namespace = $1 AND external_invitation_id = $2 AND state = 'pending'`,
		event.Namespace.String(), event.Invitation.String(), event.Subject.String(), userID, event.OccurredAt,
	); err != nil {
		return fmt.Errorf("accept identity invitation grant: %w", err)
	}
	return nil
}

func findIdentitySubject(
	ctx context.Context,
	transaction pgx.Tx,
	namespace access.IdentityNamespace,
	subject access.ExternalSubject,
) (pgtype.UUID, bool, error) {
	var userID pgtype.UUID
	err := transaction.QueryRow(ctx, `
        SELECT user_id FROM tacit_identity_subjects
        WHERE provider = $1 AND subject = $2`,
		namespace.String(), subject.String(),
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, false, nil
	}
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("resolve invited identity subject: %w", err)
	}
	return userID, true, nil
}

func applyMembershipActivation(ctx context.Context, transaction pgx.Tx, event access.LifecycleEvent) error {
	if _, err := transaction.Exec(ctx, `
        INSERT INTO tacit_identity_membership_admissions (
            namespace, external_organization_id, subject, external_membership_id,
            state, source_updated_at, source_event_id
        ) VALUES ($1, $2, $3, $4, 'active', $5, $6)
        ON CONFLICT (namespace, external_organization_id, subject) DO UPDATE SET
            external_membership_id = EXCLUDED.external_membership_id,
            state = 'active',
            source_updated_at = EXCLUDED.source_updated_at,
            source_event_id = EXCLUDED.source_event_id,
            updated_at = now()
        WHERE tacit_identity_membership_admissions.state <> 'denied'
          AND EXCLUDED.source_updated_at > tacit_identity_membership_admissions.source_updated_at`,
		event.Namespace.String(), event.Organization.String(), event.Subject.String(), event.Membership.String(),
		event.ResourceUpdated, event.ID.String(),
	); err != nil {
		return fmt.Errorf("apply membership activation: %w", err)
	}
	return nil
}

func applyMembershipDenial(ctx context.Context, transaction pgx.Tx, event access.LifecycleEvent) error {
	if _, err := transaction.Exec(ctx, `
        INSERT INTO tacit_identity_membership_admissions (
            namespace, external_organization_id, subject, external_membership_id,
            state, source_updated_at, source_event_id
        ) VALUES ($1, $2, $3, $4, 'denied', $5, $6)
        ON CONFLICT (namespace, external_organization_id, subject) DO UPDATE SET
            external_membership_id = CASE
                WHEN EXCLUDED.source_updated_at >= tacit_identity_membership_admissions.source_updated_at
                THEN EXCLUDED.external_membership_id
                ELSE tacit_identity_membership_admissions.external_membership_id
            END,
            state = 'denied',
            source_updated_at = GREATEST(
                tacit_identity_membership_admissions.source_updated_at,
                EXCLUDED.source_updated_at
            ),
            source_event_id = EXCLUDED.source_event_id,
            updated_at = now()`,
		event.Namespace.String(), event.Organization.String(), event.Subject.String(), event.Membership.String(),
		event.ResourceUpdated, event.ID.String(),
	); err != nil {
		return fmt.Errorf("apply membership denial: %w", err)
	}
	return nil
}

func (store *Store) BootstrapIdentity(ctx context.Context, bootstrap access.BootstrapIdentity) error {
	if bootstrap.Namespace.String() == "" || bootstrap.Subject.String() == "" ||
		bootstrap.ExternalOrganization.String() == "" || bootstrap.ExternalMembership.String() == "" {
		return errors.New("invalid identity bootstrap")
	}
	return store.withTransaction(ctx, func(transaction pgx.Tx) error {
		userID := databaseUUID(bootstrap.User.Bytes())
		organizationID := databaseUUID(bootstrap.Organization.Bytes())
		if _, err := transaction.Exec(ctx, `INSERT INTO tacit_users (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, userID); err != nil {
			return fmt.Errorf("bootstrap user: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_organizations (id, name) VALUES ($1, $2)
            ON CONFLICT (id) DO NOTHING`, organizationID, bootstrap.OrganizationName.String()); err != nil {
			return fmt.Errorf("bootstrap organization: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_organization_memberships (organization_id, user_id)
            VALUES ($1, $2) ON CONFLICT DO NOTHING`, organizationID, userID); err != nil {
			return fmt.Errorf("bootstrap membership: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
            INSERT INTO tacit_membership_roles (organization_id, user_id, role)
            VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`, organizationID, userID); err != nil {
			return fmt.Errorf("bootstrap role: %w", err)
		}
		if err := insertImmutableSubject(ctx, transaction, bootstrap.Namespace, bootstrap.Subject, userID); err != nil {
			return err
		}
		if err := insertImmutableOrganization(ctx, transaction, bootstrap.Namespace, bootstrap.ExternalOrganization, organizationID); err != nil {
			return err
		}
		now := time.Now().UTC()
		tag, err := transaction.Exec(ctx, `
            INSERT INTO tacit_identity_membership_admissions (
                namespace, external_organization_id, subject, external_membership_id,
                state, source_updated_at, source_event_id
            ) VALUES ($1, $2, $3, $4, 'active', $5, 'demo-bootstrap')
            ON CONFLICT (namespace, external_organization_id, subject) DO NOTHING`,
			bootstrap.Namespace.String(), bootstrap.ExternalOrganization.String(), bootstrap.Subject.String(),
			bootstrap.ExternalMembership.String(), now,
		)
		if err != nil {
			return fmt.Errorf("bootstrap identity admission: %w", err)
		}
		if tag.RowsAffected() == 0 {
			var membership, state string
			if err := transaction.QueryRow(ctx, `
                    SELECT external_membership_id, state
                    FROM tacit_identity_membership_admissions
                    WHERE namespace = $1 AND external_organization_id = $2 AND subject = $3`,
				bootstrap.Namespace.String(), bootstrap.ExternalOrganization.String(), bootstrap.Subject.String(),
			).Scan(&membership, &state); err != nil {
				return fmt.Errorf("read bootstrap identity admission: %w", err)
			}
			if membership != bootstrap.ExternalMembership.String() || state != "active" {
				return errors.New("identity bootstrap conflicts with existing admission")
			}
		}
		return nil
	})
}

func insertImmutableSubject(
	ctx context.Context,
	transaction pgx.Tx,
	namespace access.IdentityNamespace,
	subject access.ExternalSubject,
	userID pgtype.UUID,
) error {
	tag, err := transaction.Exec(ctx, `
        INSERT INTO tacit_identity_subjects (provider, subject, user_id)
        VALUES ($1, $2, $3) ON CONFLICT (provider, subject) DO NOTHING`,
		namespace.String(), subject.String(), userID,
	)
	if err != nil {
		return fmt.Errorf("bootstrap identity subject: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var existing pgtype.UUID
		if err := transaction.QueryRow(ctx, `
                SELECT user_id FROM tacit_identity_subjects WHERE provider = $1 AND subject = $2`,
			namespace.String(), subject.String(),
		).Scan(&existing); err != nil {
			return fmt.Errorf("read identity subject mapping: %w", err)
		}
		if existing != userID {
			return errors.New("identity subject mapping is immutable")
		}
	}
	return nil
}

func insertImmutableOrganization(
	ctx context.Context,
	transaction pgx.Tx,
	namespace access.IdentityNamespace,
	external access.ExternalOrganizationID,
	organizationID pgtype.UUID,
) error {
	tag, err := transaction.Exec(ctx, `
        INSERT INTO tacit_identity_organizations (namespace, external_organization_id, organization_id)
        VALUES ($1, $2, $3) ON CONFLICT (namespace, external_organization_id) DO NOTHING`,
		namespace.String(), external.String(), organizationID,
	)
	if err != nil {
		return fmt.Errorf("bootstrap identity organization: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var existing pgtype.UUID
		if err := transaction.QueryRow(ctx, `
                SELECT organization_id FROM tacit_identity_organizations
                WHERE namespace = $1 AND external_organization_id = $2`,
			namespace.String(), external.String(),
		).Scan(&existing); err != nil {
			return fmt.Errorf("read identity organization mapping: %w", err)
		}
		if existing != organizationID {
			return errors.New("identity organization mapping is immutable")
		}
	}
	return nil
}

func storedRoles(values []string) (access.Roles, error) {
	roles := make([]access.Role, 0, len(values))
	for _, value := range values {
		role, err := parseRole(value)
		if err != nil {
			return access.Roles{}, err
		}
		roles = append(roles, role)
	}
	parsed, err := access.NewRoles(roles...)
	if err != nil {
		return access.Roles{}, fmt.Errorf("stored identity membership has no valid roles: %w", err)
	}
	return parsed, nil
}
