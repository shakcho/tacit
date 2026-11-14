package postgres

import (
	"testing"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/idempotency"
)

func TestRemoveMembershipFingerprint(t *testing.T) {
	t.Parallel()
	organization, _ := access.ParseOrganizationID("org_00000000000000000000000000000001")
	actor, _ := access.ParseUserID("usr_00000000000000000000000000000001")
	target, _ := access.ParseUserID("usr_00000000000000000000000000000002")
	key, _ := idempotency.ParseKey("remove-1")
	command := access.RemoveMembershipCommand{
		Actor: actor, Ref: access.MembershipRef{Organization: organization, User: target}, Key: key,
	}
	first := removeMembershipFingerprint(command)
	second := removeMembershipFingerprint(command)
	if first != second {
		t.Fatal("same command produced different fingerprints")
	}
	command.Actor = target
	if first == removeMembershipFingerprint(command) {
		t.Fatal("different command produced the same fingerprint")
	}
}
