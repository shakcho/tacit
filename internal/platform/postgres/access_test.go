package postgres

import (
	"testing"

	"github.com/shakcho/tacit/internal/access"
)

func TestRemoveMembershipFingerprint(t *testing.T) {
	t.Parallel()
	organization, _ := access.ParseOrganizationID("org_00000000000000000000000000000001")
	actor, _ := access.ParseUserID("usr_00000000000000000000000000000001")
	target, _ := access.ParseUserID("usr_00000000000000000000000000000002")
	ref := access.MembershipRef{Organization: organization, User: target}
	first := removeMembershipFingerprint(actor, ref)
	second := removeMembershipFingerprint(actor, ref)
	if first != second {
		t.Fatal("same command produced different fingerprints")
	}
	if first == removeMembershipFingerprint(target, ref) {
		t.Fatal("different command produced the same fingerprint")
	}
}
