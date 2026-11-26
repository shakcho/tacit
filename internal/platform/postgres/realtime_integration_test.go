//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
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

	service := realtime.NewService(second, codec, realtime.NewRegistry(1))
	stream, err := service.Attach(ctx, viewer, token)
	if err != nil {
		t.Fatal(err)
	}
	const writes = 8
	toViewer, _ := access.SetMemberRoles(member, roles(t, access.RoleViewer))
	toEditor, _ := access.SetMemberRoles(member, roles(t, access.RoleEditor))
	written := make(chan error, 1)
	go func() {
		for index := range writes {
			change := toViewer
			if index%2 == 1 {
				change = toEditor
			}
			result, err := store.AdministerMembership(ctx, admin, change)
			if err != nil || result.Outcome() != access.MembershipChanged {
				written <- fmt.Errorf("commit membership change %d: outcome=%v err=%w", index, result.Outcome(), err)
				return
			}
		}
		written <- nil
	}()
	streamContext, stop := context.WithCancel(ctx)
	defer stop()
	frames := make(chan realtime.Frame, writes+2)
	finished := make(chan error, 1)
	go func() { finished <- stream.Run(collectFrame(streamContext, frames)) }()
	expectFrame(t, frames, realtime.Open, time.Second)
	started := time.Now()
	for revision := start.Position + 1; revision <= start.Position+writes+1; revision++ {
		frame := expectFrame(t, frames, realtime.Invalidation, 5*time.Second)
		if frame.Revision != revision {
			t.Fatalf("reconnected stream revision = %d, want %d", frame.Revision, revision)
		}
	}
	if duration := time.Since(started); duration > 5*time.Second {
		t.Fatalf("reconnected stream took %v to converge", duration)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	removal, _ := access.RemoveMember(member, key(t, "realtime-member-removal"))
	if result, err := store.AdministerMembership(ctx, admin, removal); err != nil || result.Outcome() != access.MembershipChanged {
		t.Fatalf("commit membership removal = %+v, %v", result, err)
	}
	expectFrame(t, frames, realtime.AccessRevoked, 4*time.Second)
	if _, err := second.SessionSnapshot(ctx, viewer); !errors.Is(err, access.ErrNotAdmitted) {
		t.Fatalf("removed member's canonical snapshot = %v", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestRealtimeRevocationWaitsForAuthorizedFlush(t *testing.T) {
	store := integrationStore(t)
	bootstrap, identity := bootstrapIdentity(t, store, 111)
	if err := store.BootstrapIdentity(t.Context(), bootstrap); err != nil {
		t.Fatal(err)
	}
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

func TestRealtimeRetentionFloorAndGap(t *testing.T) {
	store := integrationStore(t)
	organization, owner := organizationID(t, 121), userID(t, 121)
	seedOrganization(t, store, organization, owner)
	principal := memberPrincipal(t, store, organization, owner, 121)
	feed := NewRealtimeFeed(store)
	ctx := t.Context()
	initial, err := feed.SessionSnapshot(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	var floor realtime.Point
	ref := access.MembershipRef{Organization: organization, User: owner}
	if err := store.withTransaction(ctx, func(tx pgx.Tx) error {
		for index := range 1025 {
			if err := appendMembershipChange(ctx, tx, ref, realtime.Invalidate); err != nil {
				return err
			}
			if index == 0 {
				if err := scanPoint(tx.QueryRow(ctx, `
					SELECT position, event_id FROM tacit_realtime_changes WHERE organization_id = $1 AND position = $2
				`, databaseUUID(organization.Bytes()), initial.Point.Position+1), &floor); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := feed.ResolveCursor(ctx, organization, initial.Point.Event); !errors.Is(err, realtime.ErrResync) {
		t.Fatalf("expired cursor resolution = %v", err)
	}
	if resolved, err := feed.ResolveCursor(ctx, organization, floor.Event); err != nil || resolved != floor {
		t.Fatalf("floor cursor resolution = %+v, %v", resolved, err)
	}
	if _, err := feed.Replay(ctx, organization, floor); !errors.Is(err, realtime.ErrResync) {
		t.Fatalf("oversized replay = %v", err)
	}

	org := databaseUUID(organization.Bytes())
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM tacit_realtime_changes WHERE organization_id = $1`, org).Scan(&count); err != nil || count != 1024 {
		t.Fatalf("retained rows = %d, %v", count, err)
	}
	var recent realtime.Point
	if err := scanPoint(store.pool.QueryRow(ctx, `
		SELECT position, event_id FROM tacit_realtime_changes
		WHERE organization_id = $1 ORDER BY position DESC LIMIT 1 OFFSET 3
	`, org), &recent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM tacit_realtime_changes WHERE organization_id = $1 AND position = $2`, org, recent.Position+1); err != nil {
		t.Fatal(err)
	}
	if _, err := feed.Replay(ctx, organization, recent); !errors.Is(err, realtime.ErrResync) {
		t.Fatalf("missing change replay = %v", err)
	}
}

func TestRealtimeDatabaseListenerDeliversWakeAndReportsLoss(t *testing.T) {
	store := integrationStore(t)
	organization, owner, member := organizationID(t, 131), userID(t, 131), userID(t, 132)
	seedOrganization(t, store, organization, owner)
	addMember(t, store, organization, owner, member, roles(t, access.RoleViewer))
	admin := memberPrincipal(t, store, organization, owner, 131)
	viewer := memberPrincipal(t, store, organization, member, 132)
	codec, err := realtime.NewCursorCodec([]byte("shared replica signing key for tests"))
	if err != nil {
		t.Fatal(err)
	}
	registry := realtime.NewRegistry(1)
	service := realtime.NewService(NewRealtimeFeed(store), codec, registry)
	defer service.Drain()
	stopListener, err := store.ListenForRealtime(t.Context(), "", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer stopListener()

	wait, cancelWait := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelWait()
	var pid int
	for wait.Err() == nil {
		err := store.pool.QueryRow(wait, `
			SELECT pid FROM pg_stat_activity
			WHERE application_name = 'tacit-api-realtime-listener'
			  AND state = 'idle' AND query = 'LISTEN tacit_realtime_v1'
		`).Scan(&pid)
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("dedicated PostgreSQL listener did not establish LISTEN")
	}
	_, cursor, err := service.SessionSnapshot(t.Context(), viewer)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := service.Attach(t.Context(), viewer, cursor)
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan realtime.Frame, 4)
	finished := make(chan error, 1)
	go func() { finished <- stream.Run(collectFrame(t.Context(), frames)) }()
	expectFrame(t, frames, realtime.Open, time.Second)
	change, _ := access.SetMemberRoles(member, roles(t, access.RoleEditor))
	if result, err := store.AdministerMembership(t.Context(), admin, change); err != nil || result.Outcome() != access.MembershipChanged {
		t.Fatalf("commit membership change = %+v, %v", result, err)
	}
	expectFrame(t, frames, realtime.Invalidation, 1500*time.Millisecond)

	var killed bool
	if err := store.pool.QueryRow(t.Context(), `SELECT pg_terminate_backend($1)`, pid).Scan(&killed); err != nil || !killed {
		t.Fatalf("interrupt listener = %t, %v", killed, err)
	}
	expectFrame(t, frames, realtime.Resync, 1500*time.Millisecond)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func collectFrame(ctx context.Context, frames chan<- realtime.Frame) func(realtime.Frame) error {
	return func(frame realtime.Frame) error {
		select {
		case frames <- frame:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func expectFrame(t *testing.T, frames <-chan realtime.Frame, kind realtime.FrameKind, timeout time.Duration) realtime.Frame {
	t.Helper()
	select {
	case frame := <-frames:
		if frame.Kind != kind {
			t.Fatalf("frame = %s, want %s", frame.Kind, kind)
		}
		return frame
	case <-time.After(timeout):
		t.Fatalf("did not receive %s frame", kind)
		return realtime.Frame{}
	}
}
