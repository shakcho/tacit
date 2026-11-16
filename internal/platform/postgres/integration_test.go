//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/idempotency"
)

func integrationStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("TACIT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TACIT_TEST_DATABASE_URL is required for integration tests")
	}
	if err := Migrate(t.Context(), databaseURL); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), Config{
		URL: databaseURL, ApplicationName: "tacit-integration", MaxConnections: 16,
		ConnectTimeout: 5 * time.Second, HealthCheckPeriod: time.Second, PingTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if _, err := store.pool.Exec(t.Context(), `
        TRUNCATE tacit_identity_event_inbox, tacit_identity_invitation_grants,
            tacit_identity_organization_states, tacit_identity_subject_states,
            tacit_identity_session_denials, tacit_identity_membership_admissions,
            tacit_identity_organizations, tacit_idempotency_records, tacit_access_audit, tacit_membership_external_ids,
            tacit_identity_subjects, tacit_membership_roles, tacit_organization_memberships,
            tacit_organizations, tacit_users CASCADE`); err != nil {
		t.Fatal(err)
	}
	return store
}

func organizationID(t *testing.T, value int) access.OrganizationID {
	t.Helper()
	id, err := access.ParseOrganizationID(fmt.Sprintf("org_%032x", value))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func userID(t *testing.T, value int) access.UserID {
	t.Helper()
	id, err := access.ParseUserID(fmt.Sprintf("usr_%032x", value))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func roles(t *testing.T, values ...access.Role) access.Roles {
	t.Helper()
	result, err := access.NewRoles(values...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func key(t *testing.T, value string) idempotency.Key {
	t.Helper()
	result, err := idempotency.ParseKey(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func seedOrganization(t *testing.T, store *Store, organization access.OrganizationID, owner access.UserID) {
	t.Helper()
	if err := store.CreateUser(t.Context(), access.CreateUserCommand{ID: owner}); err != nil {
		t.Fatal(err)
	}
	name, _ := access.ParseName("Integration organization")
	if err := store.CreateOrganization(t.Context(), access.CreateOrganizationCommand{ID: organization, Name: name, Owner: owner}); err != nil {
		t.Fatal(err)
	}
}

func addMember(t *testing.T, store *Store, organization access.OrganizationID, _ access.UserID, member access.UserID, memberRoles access.Roles) {
	t.Helper()
	if err := store.CreateUser(t.Context(), access.CreateUserCommand{ID: member}); err != nil {
		t.Fatal(err)
	}
	err := store.withTransaction(t.Context(), func(transaction pgx.Tx) error {
		if err := lockOrganization(t.Context(), transaction, organization); err != nil {
			return err
		}
		ref := access.MembershipRef{Organization: organization, User: member}
		if _, err := transaction.Exec(t.Context(),
			`INSERT INTO tacit_organization_memberships (organization_id, user_id) VALUES ($1, $2)`,
			databaseUUID(organization.Bytes()), databaseUUID(member.Bytes()),
		); err != nil {
			return err
		}
		return insertRoles(t.Context(), transaction, ref, memberRoles)
	})
	if err != nil {
		t.Fatalf("add fixture membership: %v", err)
	}
}

type integrationVerifier struct{ identity access.VerifiedIdentity }

func (verifier integrationVerifier) Verify(context.Context, access.BearerToken) (access.VerifiedIdentity, error) {
	return verifier.identity, nil
}

func memberPrincipal(
	t *testing.T,
	store *Store,
	organization access.OrganizationID,
	user access.UserID,
	suffix int,
) access.LocalPrincipal {
	t.Helper()
	namespace, _ := access.ParseIdentityNamespace("workos-integration")
	subject, _ := access.ParseExternalSubject(fmt.Sprintf("member_%d", suffix))
	externalOrganization, _ := access.ParseExternalOrganizationID("external_" + organization.String())
	externalMembership, _ := access.ParseExternalMembershipID(fmt.Sprintf("membership_%d", suffix))
	session, _ := access.ParseExternalSessionID(fmt.Sprintf("session_%d", suffix))
	now := time.Now().UTC()
	identity := access.VerifiedIdentity{
		Namespace: namespace, Subject: subject, Organization: externalOrganization, Session: session,
		IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
	if _, err := store.pool.Exec(t.Context(), `
        INSERT INTO tacit_identity_subjects (provider, subject, user_id)
        VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		namespace.String(), subject.String(), databaseUUID(user.Bytes()),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(t.Context(), `
        INSERT INTO tacit_identity_organizations (namespace, external_organization_id, organization_id)
        VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		namespace.String(), externalOrganization.String(), databaseUUID(organization.Bytes()),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(t.Context(), `
        INSERT INTO tacit_identity_membership_admissions (
            namespace, external_organization_id, subject, external_membership_id,
            state, source_updated_at, source_event_id
        ) VALUES ($1, $2, $3, $4, 'active', $5, $6)
        ON CONFLICT (namespace, external_organization_id, subject) DO UPDATE SET state = 'active'`,
		namespace.String(), externalOrganization.String(), subject.String(), externalMembership.String(), now,
		fmt.Sprintf("fixture_%d", suffix),
	); err != nil {
		t.Fatal(err)
	}
	service := access.NewAuthenticationService(integrationVerifier{identity: identity}, store)
	token, _ := access.ParseBearerToken("integration-token")
	principal, err := service.Authenticate(t.Context(), token, access.ExactOrganization(organization))
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func TestMigrationsAreRepeatable(t *testing.T) {
	store := integrationStore(t)
	if err := Migrate(t.Context(), os.Getenv("TACIT_TEST_DATABASE_URL")); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := store.pool.QueryRow(t.Context(), `SELECT MAX(version) FROM tacit_schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d", version)
	}
}

func TestPopulatedVersionOneUpgradesToVersionTwo(t *testing.T) {
	baseURL := os.Getenv("TACIT_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Fatal("TACIT_TEST_DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(t.Context(), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "tacit_upgrade_v1_fixture"
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(t.Context(), "DROP SCHEMA IF EXISTS "+identifier+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+identifier+" CASCADE")
		_ = admin.Close(context.Background())
	})

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schema)
	parsedURL.RawQuery = query.Encode()
	upgradeURL := parsedURL.String()
	connection, err := pgx.Connect(t.Context(), upgradeURL)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(t.Context(), `
        CREATE TABLE tacit_schema_migrations (
            version bigint PRIMARY KEY CHECK (version > 0),
            name text NOT NULL,
            checksum text NOT NULL CHECK (length(checksum) = 64),
            applied_at timestamptz NOT NULL DEFAULT now()
        )`); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(t.Context(), migrations[0].SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(t.Context(),
		`INSERT INTO tacit_schema_migrations (version, name, checksum) VALUES (1, $1, $2)`,
		migrations[0].Name, migrations[0].Checksum,
	); err != nil {
		t.Fatal(err)
	}
	fixtureStatements := []string{
		`INSERT INTO tacit_users (id) VALUES ('00000000-0000-4000-8000-000000000001')`,
		`INSERT INTO tacit_organizations (id, name) VALUES ('00000000-0000-4000-8000-000000000001', 'Version one')`,
		`INSERT INTO tacit_organization_memberships (organization_id, user_id) VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001')`,
		`INSERT INTO tacit_membership_roles (organization_id, user_id, role) VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'admin')`,
		`INSERT INTO tacit_identity_subjects (provider, subject, user_id) VALUES ('legacy-fixture', 'subject-1', '00000000-0000-4000-8000-000000000001')`,
	}
	for _, statement := range fixtureStatements {
		if _, err := connection.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(t.Context(), upgradeURL); err != nil {
		t.Fatal(err)
	}
	verified, err := pgx.Connect(t.Context(), upgradeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = verified.Close(context.Background()) }()
	var version, users, roles int
	var identityTableExists bool
	if err := verified.QueryRow(t.Context(), `SELECT MAX(version) FROM tacit_schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := verified.QueryRow(t.Context(), `SELECT count(*) FROM tacit_users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := verified.QueryRow(t.Context(), `SELECT count(*) FROM tacit_membership_roles WHERE role = 'admin'`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if err := verified.QueryRow(t.Context(), `SELECT to_regclass('tacit_identity_organizations') IS NOT NULL`).Scan(&identityTableExists); err != nil {
		t.Fatal(err)
	}
	if version != int(CurrentSchemaVersion) || users != 1 || roles != 1 || !identityTableExists {
		t.Fatalf("upgrade version=%d users=%d roles=%d identity_table=%v", version, users, roles, identityTableExists)
	}
}

func TestCompositeTenantForeignKey(t *testing.T) {
	store := integrationStore(t)
	firstOrganization, secondOrganization := organizationID(t, 1), organizationID(t, 2)
	firstOwner, secondOwner, member := userID(t, 1), userID(t, 2), userID(t, 3)
	seedOrganization(t, store, firstOrganization, firstOwner)
	seedOrganization(t, store, secondOrganization, secondOwner)
	addMember(t, store, firstOrganization, firstOwner, member, roles(t, access.RoleViewer))

	_, err := store.pool.Exec(t.Context(), `
        INSERT INTO tacit_membership_external_ids (organization_id, namespace, external_id, user_id)
        VALUES ($1, 'legacy-user', 'member-3', $2)`,
		databaseUUID(secondOrganization.Bytes()), databaseUUID(member.Bytes()),
	)
	if err == nil {
		t.Fatal("cross-organization external ID insert succeeded")
	}
	if _, err := store.pool.Exec(t.Context(), `
        INSERT INTO tacit_membership_external_ids (organization_id, namespace, external_id, user_id)
        VALUES ($1, 'legacy-user', 'member-3', $2)`,
		databaseUUID(firstOrganization.Bytes()), databaseUUID(member.Bytes()),
	); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAdminRemovalKeepsOneAdmin(t *testing.T) {
	store := integrationStore(t)
	organization := organizationID(t, 10)
	first, second := userID(t, 10), userID(t, 11)
	seedOrganization(t, store, organization, first)
	addMember(t, store, organization, first, second, roles(t, access.RoleAdmin))
	firstPrincipal := memberPrincipal(t, store, organization, first, 10)
	secondPrincipal := memberPrincipal(t, store, organization, second, 11)

	firstChange, _ := access.RemoveMember(second, key(t, "remove-second"))
	secondChange, _ := access.RemoveMember(first, key(t, "remove-first"))
	commands := []struct {
		principal access.LocalPrincipal
		change    access.MembershipChange
	}{
		{principal: firstPrincipal, change: firstChange},
		{principal: secondPrincipal, change: secondChange},
	}
	results := make(chan access.MembershipResult, len(commands))
	errors := make(chan error, len(commands))
	start := make(chan struct{})
	var group sync.WaitGroup
	for _, command := range commands {
		group.Go(func() {
			<-start
			result, err := store.AdministerMembership(context.Background(), command.principal, command.change)
			results <- result
			errors <- err
		})
	}
	close(start)
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	counts := map[access.MembershipOutcome]int{}
	for result := range results {
		counts[result.Outcome()]++
	}
	if counts[access.MembershipChanged] != 1 || counts[access.MembershipDenied] != 1 {
		t.Fatalf("outcomes = %v", counts)
	}
	var admins, removalAudits int
	if err := store.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM tacit_membership_roles WHERE organization_id = $1 AND role = 'admin'`,
		databaseUUID(organization.Bytes()),
	).Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM tacit_access_audit WHERE organization_id = $1 AND action = 'membership.removed'`,
		databaseUUID(organization.Bytes()),
	).Scan(&removalAudits); err != nil {
		t.Fatal(err)
	}
	if admins != 1 || removalAudits != 1 {
		t.Fatalf("admins = %d, removal audits = %d", admins, removalAudits)
	}
}

func TestAuthorizedMembershipAdministrationPreservesFinalAdminAndRechecksRevocation(t *testing.T) {
	store := integrationStore(t)
	organization := organizationID(t, 15)
	owner, member := userID(t, 15), userID(t, 16)
	seedOrganization(t, store, organization, owner)
	addMember(t, store, organization, owner, member, roles(t, access.RoleEditor))
	principal := memberPrincipal(t, store, organization, owner, 15)

	removeSelf, _ := access.RemoveMember(owner, key(t, "remove-final-admin"))
	result, err := store.AdministerMembership(t.Context(), principal, removeSelf)
	if err != nil || result.Outcome() != access.MembershipLastAdmin {
		t.Fatalf("remove final admin = %v, %v", result.Outcome(), err)
	}

	removeMember, _ := access.RemoveMember(member, key(t, "remove-before-revocation"))
	result, err = store.AdministerMembership(t.Context(), principal, removeMember)
	if err != nil || result.Outcome() != access.MembershipChanged {
		t.Fatalf("initial removal = %v, %v", result.Outcome(), err)
	}
	identity, ok := principal.RevalidationIdentity()
	if !ok {
		t.Fatal("principal has no revalidation identity")
	}
	if err := store.DenySession(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	replay, err := store.AdministerMembership(t.Context(), principal, removeMember)
	if err != nil || replay.Outcome() != access.MembershipDenied || replay.Replayed() {
		t.Fatalf("revoked replay = %v replayed=%v, %v", replay.Outcome(), replay.Replayed(), err)
	}

	var receipts, removalAudits int
	if err := store.pool.QueryRow(t.Context(), `
        SELECT count(*) FROM tacit_idempotency_records
        WHERE organization_id = $1 AND client_key = 'remove-before-revocation'`,
		databaseUUID(organization.Bytes()),
	).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(t.Context(), `
        SELECT count(*) FROM tacit_access_audit
        WHERE organization_id = $1 AND action = 'membership.removed'`,
		databaseUUID(organization.Bytes()),
	).Scan(&removalAudits); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || removalAudits != 1 {
		t.Fatalf("receipts=%d removalAudits=%d", receipts, removalAudits)
	}
}

func TestAuthorizedMembershipAdministrationAddsSetsAndDeniesNonAdmin(t *testing.T) {
	store := integrationStore(t)
	organization := organizationID(t, 18)
	owner, member, other := userID(t, 18), userID(t, 19), userID(t, 20)
	seedOrganization(t, store, organization, owner)
	if err := store.CreateUser(t.Context(), access.CreateUserCommand{ID: member}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUser(t.Context(), access.CreateUserCommand{ID: other}); err != nil {
		t.Fatal(err)
	}
	ownerPrincipal := memberPrincipal(t, store, organization, owner, 18)

	add, _ := access.AddMember(member, roles(t, access.RoleViewer))
	result, err := store.AdministerMembership(t.Context(), ownerPrincipal, add)
	if err != nil || result.Outcome() != access.MembershipChanged {
		t.Fatalf("add = %v, %v", result.Outcome(), err)
	}
	set, _ := access.SetMemberRoles(member, roles(t, access.RoleEditor))
	result, err = store.AdministerMembership(t.Context(), ownerPrincipal, set)
	if err != nil || result.Outcome() != access.MembershipChanged {
		t.Fatalf("set = %v, %v", result.Outcome(), err)
	}
	membership, found, err := store.FindMembership(t.Context(), access.MembershipRef{Organization: organization, User: member})
	if err != nil || !found || !membership.Roles.Has(access.RoleEditor) || membership.Roles.Has(access.RoleViewer) {
		t.Fatalf("membership = %#v found=%v, %v", membership, found, err)
	}

	editorPrincipal := memberPrincipal(t, store, organization, member, 19)
	unauthorizedAdd, _ := access.AddMember(other, roles(t, access.RoleViewer))
	result, err = store.AdministerMembership(t.Context(), editorPrincipal, unauthorizedAdd)
	if err != nil || result.Outcome() != access.MembershipDenied {
		t.Fatalf("editor add = %v, %v", result.Outcome(), err)
	}
	if _, found, err := store.FindMembership(t.Context(), access.MembershipRef{Organization: organization, User: other}); err != nil || found {
		t.Fatalf("unauthorized membership found=%v, %v", found, err)
	}
}

func TestConcurrentDuplicateCommandHasOneEffect(t *testing.T) {
	store := integrationStore(t)
	organization := organizationID(t, 20)
	owner, member := userID(t, 20), userID(t, 21)
	seedOrganization(t, store, organization, owner)
	addMember(t, store, organization, owner, member, roles(t, access.RoleEditor))
	principal := memberPrincipal(t, store, organization, owner, 20)
	command, _ := access.RemoveMember(member, key(t, "same-command"))

	results := make(chan access.MembershipResult, 32)
	errors := make(chan error, 32)
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			result, err := store.AdministerMembership(context.Background(), principal, command)
			results <- result
			errors <- err
		})
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	replayed := 0
	for result := range results {
		if result.Outcome() != access.MembershipChanged {
			t.Fatalf("outcome = %v", result.Outcome())
		}
		if result.Replayed() {
			replayed++
		}
	}
	if replayed != 31 {
		t.Fatalf("replayed = %d", replayed)
	}
	var receipts, removalAudits int
	if err := store.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM tacit_idempotency_records WHERE organization_id = $1`, databaseUUID(organization.Bytes()),
	).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM tacit_access_audit WHERE organization_id = $1 AND action = 'membership.removed'`,
		databaseUUID(organization.Bytes()),
	).Scan(&removalAudits); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || removalAudits != 1 {
		t.Fatalf("receipts = %d, removal audits = %d", receipts, removalAudits)
	}

	other, _ := access.RemoveMember(owner, key(t, "same-command"))
	conflict, err := store.AdministerMembership(t.Context(), principal, other)
	if err != nil || conflict.Outcome() != access.MembershipConflict {
		t.Fatalf("changed command = %v, %v", conflict.Outcome(), err)
	}
}

func TestTenantQueriesIgnorePooledSessionState(t *testing.T) {
	store := integrationStore(t)
	firstOrganization, secondOrganization := organizationID(t, 30), organizationID(t, 31)
	firstOwner, secondOwner := userID(t, 30), userID(t, 31)
	seedOrganization(t, store, firstOrganization, firstOwner)
	seedOrganization(t, store, secondOrganization, secondOwner)

	singleConnectionStore, err := Open(t.Context(), Config{
		URL: os.Getenv("TACIT_TEST_DATABASE_URL"), ApplicationName: "tacit-pool-isolation", MaxConnections: 1,
		ConnectTimeout: 5 * time.Second, HealthCheckPeriod: time.Second, PingTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(singleConnectionStore.Close)
	connection, err := singleConnectionStore.pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(t.Context(),
		`SELECT set_config('tacit.organization_id', $1, false)`, firstOrganization.String(),
	); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	connection.Release()

	secondMembership, found, err := singleConnectionStore.FindMembership(t.Context(), access.MembershipRef{
		Organization: secondOrganization, User: secondOwner,
	})
	if err != nil || !found || secondMembership.Ref.Organization != secondOrganization {
		t.Fatalf("second tenant membership = %#v, %v, %v", secondMembership, found, err)
	}
	if _, found, err := singleConnectionStore.FindMembership(t.Context(), access.MembershipRef{
		Organization: firstOrganization, User: secondOwner,
	}); err != nil || found {
		t.Fatalf("cross-tenant membership = %v, %v", found, err)
	}
}

func bootstrapIdentity(t *testing.T, store *Store, suffix int) (access.BootstrapIdentity, access.VerifiedIdentity) {
	t.Helper()
	namespace, _ := access.ParseIdentityNamespace("workos-test")
	subject, _ := access.ParseExternalSubject(fmt.Sprintf("user_%d", suffix))
	externalOrganization, _ := access.ParseExternalOrganizationID(fmt.Sprintf("workos_org_%d", suffix))
	externalMembership, _ := access.ParseExternalMembershipID(fmt.Sprintf("membership_%d", suffix))
	session, _ := access.ParseExternalSessionID(fmt.Sprintf("session_%d", suffix))
	name, _ := access.ParseName("Identity integration organization")
	bootstrap := access.BootstrapIdentity{
		Namespace: namespace, Subject: subject, ExternalOrganization: externalOrganization,
		ExternalMembership: externalMembership, User: userID(t, 100+suffix),
		Organization: organizationID(t, 100+suffix), OrganizationName: name,
	}
	identity := access.VerifiedIdentity{
		Namespace: namespace, Subject: subject, Organization: externalOrganization, Session: session,
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	return bootstrap, identity
}

func TestIdentityAdmissionUsesMappingsMembershipAndSessionDenial(t *testing.T) {
	store := integrationStore(t)
	bootstrap, identity := bootstrapIdentity(t, store, 1)
	if err := store.BootstrapIdentity(t.Context(), bootstrap); err != nil {
		t.Fatal(err)
	}
	if err := store.BootstrapIdentity(t.Context(), bootstrap); err != nil {
		t.Fatalf("idempotent bootstrap: %v", err)
	}
	principal, found, err := store.ResolveIdentity(t.Context(), identity, access.TokenOrganization())
	if err != nil || !found || principal.User != bootstrap.User || principal.Organization != bootstrap.Organization || !principal.Roles.Has(access.RoleAdmin) {
		t.Fatalf("ResolveIdentity = %#v, %v, %v", principal, found, err)
	}
	wrongOrganization := organizationID(t, 999)
	if _, found, err := store.ResolveIdentity(t.Context(), identity, access.ExactOrganization(wrongOrganization)); err != nil || found {
		t.Fatalf("wrong organization admission = %v, %v", found, err)
	}
	if err := store.DenySession(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ResolveIdentity(t.Context(), identity, access.TokenOrganization()); err != nil || found {
		t.Fatalf("denied session admission = %v, %v", found, err)
	}
}

func TestIdentitySubjectMappingCannotBeMovedOrDeleted(t *testing.T) {
	store := integrationStore(t)
	bootstrap, _ := bootstrapIdentity(t, store, 4)
	if err := store.BootstrapIdentity(t.Context(), bootstrap); err != nil {
		t.Fatal(err)
	}
	otherUser := userID(t, 904)
	if err := store.CreateUser(t.Context(), access.CreateUserCommand{ID: otherUser}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE tacit_identity_subjects SET user_id = $3 WHERE provider = $1 AND subject = $2`,
		`DELETE FROM tacit_identity_subjects WHERE provider = $1 AND subject = $2 AND $3::uuid IS NOT NULL`,
	} {
		if _, err := store.pool.Exec(t.Context(), statement,
			bootstrap.Namespace.String(), bootstrap.Subject.String(), databaseUUID(otherUser.Bytes()),
		); err == nil {
			t.Fatalf("identity mutation succeeded: %s", statement)
		}
	}
	if _, found, err := store.ResolveIdentity(t.Context(), access.VerifiedIdentity{
		Namespace: bootstrap.Namespace, Subject: bootstrap.Subject,
		Organization: bootstrap.ExternalOrganization, Session: mustExternalSession(t, "session_immutable"),
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}, access.TokenOrganization()); err != nil || !found {
		t.Fatalf("immutable identity admission = %v, %v", found, err)
	}
}

func mustExternalSession(t *testing.T, value string) access.ExternalSessionID {
	t.Helper()
	session, err := access.ParseExternalSessionID(value)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestLifecycleReplayAndDenialWinOverNewerActivation(t *testing.T) {
	store := integrationStore(t)
	bootstrap, identity := bootstrapIdentity(t, store, 2)
	if err := store.BootstrapIdentity(t.Context(), bootstrap); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	deniedEventID, _ := access.ParseExternalEventID("event_denied")
	denied := access.LifecycleEvent{
		Namespace: bootstrap.Namespace, ID: deniedEventID, Kind: access.LifecycleMembershipDenied,
		PayloadHash: [32]byte{1}, OccurredAt: now, ResourceUpdated: now,
		Subject: bootstrap.Subject, Organization: bootstrap.ExternalOrganization, Membership: bootstrap.ExternalMembership,
	}
	if outcome, err := store.ApplyLifecycle(t.Context(), denied); err != nil || outcome != access.LifecycleApplied {
		t.Fatalf("deny lifecycle = %v, %v", outcome, err)
	}
	if outcome, err := store.ApplyLifecycle(t.Context(), denied); err != nil || outcome != access.LifecycleReplayed {
		t.Fatalf("replay lifecycle = %v, %v", outcome, err)
	}
	conflict := denied
	conflict.PayloadHash = [32]byte{2}
	if _, err := store.ApplyLifecycle(t.Context(), conflict); !errors.Is(err, access.ErrLifecycleConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	activatedEventID, _ := access.ParseExternalEventID("event_active_newer")
	activated := denied
	activated.ID = activatedEventID
	activated.Kind = access.LifecycleMembershipActivated
	activated.PayloadHash = [32]byte{3}
	activated.OccurredAt = now.Add(time.Minute)
	activated.ResourceUpdated = now.Add(time.Minute)
	if outcome, err := store.ApplyLifecycle(t.Context(), activated); err != nil || outcome != access.LifecycleApplied {
		t.Fatalf("later active lifecycle = %v, %v", outcome, err)
	}
	if _, found, err := store.ResolveIdentity(t.Context(), identity, access.TokenOrganization()); err != nil || found {
		t.Fatalf("denial cleared by active event = %v, %v", found, err)
	}
	var adminRoles int
	if err := store.pool.QueryRow(t.Context(), `
        SELECT count(*) FROM tacit_membership_roles
        WHERE organization_id = $1 AND user_id = $2 AND role = 'admin'`,
		databaseUUID(bootstrap.Organization.Bytes()), databaseUUID(bootstrap.User.Bytes()),
	).Scan(&adminRoles); err != nil {
		t.Fatal(err)
	}
	if adminRoles != 1 {
		t.Fatalf("local admin roles = %d", adminRoles)
	}
}

func TestInvitationAcceptanceUsesRegisteredRoleWithoutEmailLinkingOrEscalation(t *testing.T) {
	store := integrationStore(t)
	bootstrap, _ := bootstrapIdentity(t, store, 3)
	if err := store.BootstrapIdentity(t.Context(), bootstrap); err != nil {
		t.Fatal(err)
	}
	invitation, _ := access.ParseExternalInvitationID("invitation_viewer")
	grant := access.InvitationGrant{
		Namespace: bootstrap.Namespace, Invitation: invitation,
		Organization: bootstrap.Organization, Role: access.RoleViewer,
	}
	if err := store.RegisterInvitationGrant(t.Context(), grant); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterInvitationGrant(t.Context(), grant); err != nil {
		t.Fatalf("idempotent invitation registration: %v", err)
	}
	conflictingGrant := grant
	conflictingGrant.Role = access.RoleAdmin
	if err := store.RegisterInvitationGrant(t.Context(), conflictingGrant); !errors.Is(err, access.ErrInvitationConflict) {
		t.Fatalf("conflicting invitation registration error = %v", err)
	}
	invitedSubject, _ := access.ParseExternalSubject("user_invited")
	acceptedEventID, _ := access.ParseExternalEventID("event_invitation_accepted")
	now := time.Now().UTC()
	accepted := access.LifecycleEvent{
		Namespace: bootstrap.Namespace, ID: acceptedEventID, Kind: access.LifecycleInvitationAccepted,
		PayloadHash: [32]byte{4}, OccurredAt: now, Subject: invitedSubject,
		Organization: bootstrap.ExternalOrganization, Invitation: invitation,
	}
	if outcome, err := store.ApplyLifecycle(t.Context(), accepted); err != nil || outcome != access.LifecycleApplied {
		t.Fatalf("accept invitation = %v, %v", outcome, err)
	}
	var invitedUser pgtype.UUID
	if err := store.pool.QueryRow(t.Context(), `
        SELECT user_id FROM tacit_identity_subjects
        WHERE provider = $1 AND subject = $2`,
		bootstrap.Namespace.String(), invitedSubject.String(),
	).Scan(&invitedUser); err != nil {
		t.Fatal(err)
	}
	var viewerRoles, adminRoles int
	if err := store.pool.QueryRow(t.Context(), `
        SELECT count(*) FILTER (WHERE role = 'viewer'), count(*) FILTER (WHERE role = 'admin')
        FROM tacit_membership_roles
        WHERE organization_id = $1 AND user_id = $2`,
		databaseUUID(bootstrap.Organization.Bytes()), invitedUser,
	).Scan(&viewerRoles, &adminRoles); err != nil {
		t.Fatal(err)
	}
	if viewerRoles != 1 || adminRoles != 0 {
		t.Fatalf("invited roles viewer=%d admin=%d", viewerRoles, adminRoles)
	}
	invitedSession, _ := access.ParseExternalSessionID("session_invited")
	invitedIdentity := access.VerifiedIdentity{
		Namespace: bootstrap.Namespace, Subject: invitedSubject, Organization: bootstrap.ExternalOrganization,
		Session: invitedSession, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
	if _, found, err := store.ResolveIdentity(t.Context(), invitedIdentity, access.TokenOrganization()); err != nil || found {
		t.Fatalf("invited identity admitted before provider membership = %v, %v", found, err)
	}
	externalMembership, _ := access.ParseExternalMembershipID("membership_invited")
	membershipEventID, _ := access.ParseExternalEventID("event_invited_membership")
	activeMembership := access.LifecycleEvent{
		Namespace: bootstrap.Namespace, ID: membershipEventID, Kind: access.LifecycleMembershipActivated,
		PayloadHash: [32]byte{5}, OccurredAt: now.Add(time.Second), ResourceUpdated: now.Add(time.Second),
		Subject: invitedSubject, Organization: bootstrap.ExternalOrganization, Membership: externalMembership,
	}
	if _, err := store.ApplyLifecycle(t.Context(), activeMembership); err != nil {
		t.Fatal(err)
	}
	if principal, found, err := store.ResolveIdentity(t.Context(), invitedIdentity, access.TokenOrganization()); err != nil || !found || !principal.Roles.Has(access.RoleViewer) {
		t.Fatalf("invited identity admission = %#v, %v, %v", principal, found, err)
	}
	adminInvitation, _ := access.ParseExternalInvitationID("invitation_admin")
	if err := store.RegisterInvitationGrant(t.Context(), access.InvitationGrant{
		Namespace: bootstrap.Namespace, Invitation: adminInvitation,
		Organization: bootstrap.Organization, Role: access.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	adminEventID, _ := access.ParseExternalEventID("event_admin_invitation")
	adminAccepted := accepted
	adminAccepted.ID = adminEventID
	adminAccepted.Invitation = adminInvitation
	adminAccepted.PayloadHash = [32]byte{6}
	adminAccepted.OccurredAt = now.Add(2 * time.Second)
	if _, err := store.ApplyLifecycle(t.Context(), adminAccepted); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(t.Context(), `
        SELECT count(*) FROM tacit_membership_roles
        WHERE organization_id = $1 AND user_id = $2 AND role = 'admin'`,
		databaseUUID(bootstrap.Organization.Bytes()), invitedUser,
	).Scan(&adminRoles); err != nil {
		t.Fatal(err)
	}
	if adminRoles != 0 {
		t.Fatalf("existing membership was escalated to admin")
	}
}
