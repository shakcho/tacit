//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/realtime"
)

func TestRealtimeSessionSnapshotAndCrossReplicaReplay(t *testing.T) {
	store := integrationStore(t)
	ctx := t.Context()
	org, owner, member := organizationID(t, 101), userID(t, 101), userID(t, 102)
	seedOrganization(t, store, org, owner)
	addMember(t, store, org, owner, member, roles(t, access.RoleViewer))
	admin := memberPrincipal(t, store, org, owner, 101)
	viewer := memberPrincipal(t, store, org, member, 102)
	feed := NewRealtimeFeed(store)
	other, err := Open(ctx, Config{
		URL: os.Getenv("TACIT_TEST_DATABASE_URL"), ApplicationName: "tacit-realtime-second-replica", MaxConnections: 4,
		ConnectTimeout: time.Second, HealthCheckPeriod: time.Second, PingTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	second := NewRealtimeFeed(other)
	codec, err := realtime.NewCursorCodec([]byte("shared replica signing key for tests"))
	if err != nil {
		t.Fatal(err)
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	ref := access.MembershipRef{Organization: org, User: member}
	var result access.MembershipResult
	if err := lockOrganization(ctx, tx, org); err != nil {
		t.Fatal(err)
	}
	if err := setMembershipRoles(ctx, tx, owner, ref, roles(t, access.RoleEditor), &result); err != nil {
		t.Fatal(err)
	}
	snapshot, err := feed.SessionSnapshot(ctx, viewer)
	if err != nil || !snapshot.Principal.Roles().Has(access.RoleViewer) {
		t.Fatalf("view before commit = %+v, %v", snapshot, err)
	}
	token := codec.Encode(viewer, snapshot.Point.Event)
	if _, ok := codec.Decode(admin, token); ok {
		t.Fatal("reader from a different account could decode cursor")
	}
	event, ok := codec.Decode(viewer, token)
	if !ok {
		t.Fatal("authorized cursor could not be decoded")
	}
	if _, ok := codec.Decode(viewer, token[:25]+"!"+token[26:]); ok {
		t.Fatal("tampered cursor accepted")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	start, err := second.ResolveCursor(ctx, org, event)
	if err != nil || start != snapshot.Point {
		t.Fatalf("resolved cursor = %+v, %v", start, err)
	}
	page, err := second.Replay(ctx, org, start)
	if err != nil || len(page.Changes) != 1 || page.Changes[0].Audience != member ||
		page.Changes[0].Position != start.Position+1 || page.Changes[0].Kind != realtime.Invalidate {
		t.Fatalf("replica replay = %+v, %v", page, err)
	}
	fresh, err := second.SessionSnapshot(ctx, viewer)
	if err != nil || !fresh.Principal.Roles().Has(access.RoleEditor) || fresh.Point != page.Changes[0].Point {
		t.Fatalf("view after commit = %+v, %v", fresh, err)
	}
	if err := store.withTransaction(ctx, func(tx pgx.Tx) error {
		if err := appendMembershipChange(ctx, tx, ref, realtime.ScopeReset); err != nil {
			return err
		}
		return context.Canceled
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	page, err = second.Replay(ctx, org, fresh.Point)
	if err != nil || len(page.Changes) != 0 {
		t.Fatalf("rollback leaked a change = %+v, %v", page, err)
	}
}

func TestRealtimeRevocationWaitsForAuthorizedFlush(t *testing.T) {
	store := integrationStore(t)
	bootstrap, identity := bootstrapIdentity(t, store, 111)
	token, _ := access.ParseBearerToken("test-realtime-revocation")
	auth := access.NewAuthenticationService(integrationVerifier{identity: identity}, store)
	principal, err := auth.Authenticate(t.Context(), token, access.TokenOrganization())
	if err != nil {
		t.Fatal(err)
	}
	feed := NewRealtimeFeed(store)
	entered, release := make(chan struct{}), make(chan struct{})
	delivered, revoked := make(chan error, 1), make(chan error, 1)
	go func() {
		delivered <- feed.Authorize(t.Context(), principal, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-delivered:
		t.Fatalf("authorize failed: %v", err)
	}
	go func() { revoked <- store.DenySession(t.Context(), identity) }()
	select {
	case err := <-revoked:
		close(release)
		t.Fatalf("revocation finished during a frame flush: %v", err)
	case <-time.After(50 * time.Millisecond):
		close(release)
	}
	if err := <-delivered; err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if err := feed.Authorize(t.Context(), principal, func() error {
		t.Error("revoked session delivered a frame")
		return nil
	}); !errors.Is(err, access.ErrNotAdmitted) {
		t.Fatalf("authorize after revocation = %v", err)
	}
	if _, err := feed.SessionSnapshot(t.Context(), principal); !errors.Is(err, access.ErrNotAdmitted) {
		t.Fatalf("snapshot after revocation in %s = %v", bootstrap.Organization.String(), err)
	}
}
