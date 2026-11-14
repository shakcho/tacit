//go:build integration

package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

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
        TRUNCATE tacit_idempotency_records, tacit_access_audit, tacit_membership_external_ids,
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

func addMember(t *testing.T, store *Store, organization access.OrganizationID, actor, member access.UserID, memberRoles access.Roles) {
	t.Helper()
	if err := store.CreateUser(t.Context(), access.CreateUserCommand{ID: member}); err != nil {
		t.Fatal(err)
	}
	result, err := store.AddMembership(t.Context(), access.AddMembershipCommand{
		Actor: actor, Ref: access.MembershipRef{Organization: organization, User: member}, Roles: memberRoles,
	})
	if err != nil || result.Outcome() != access.MembershipChanged {
		t.Fatalf("add membership = %v, %v", result.Outcome(), err)
	}
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

	commands := []access.RemoveMembershipCommand{
		{Actor: first, Ref: access.MembershipRef{Organization: organization, User: second}, Key: key(t, "remove-second")},
		{Actor: second, Ref: access.MembershipRef{Organization: organization, User: first}, Key: key(t, "remove-first")},
	}
	results := make(chan access.MembershipResult, len(commands))
	errors := make(chan error, len(commands))
	start := make(chan struct{})
	var group sync.WaitGroup
	for _, command := range commands {
		group.Go(func() {
			<-start
			result, err := store.RemoveMembership(context.Background(), command)
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
	if counts[access.MembershipChanged] != 1 || counts[access.MembershipLastAdmin] != 1 {
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

func TestConcurrentDuplicateCommandHasOneEffect(t *testing.T) {
	store := integrationStore(t)
	organization := organizationID(t, 20)
	owner, member := userID(t, 20), userID(t, 21)
	seedOrganization(t, store, organization, owner)
	addMember(t, store, organization, owner, member, roles(t, access.RoleEditor))
	command := access.RemoveMembershipCommand{
		Actor: owner, Ref: access.MembershipRef{Organization: organization, User: member}, Key: key(t, "same-command"),
	}

	results := make(chan access.MembershipResult, 32)
	errors := make(chan error, 32)
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			result, err := store.RemoveMembership(context.Background(), command)
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

	other := command
	other.Ref.User = owner
	conflict, err := store.RemoveMembership(t.Context(), other)
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
