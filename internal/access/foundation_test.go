package access_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shakcho/tacit/internal/runtime/idempotency"

	"github.com/shakcho/tacit/internal/access"
)

func TestIdentifiers(t *testing.T) {
	t.Parallel()
	organizationValue := "org_0123456789abcdef0123456789abcdef"
	organization, err := access.ParseOrganizationID(organizationValue)
	if err != nil || organization.String() != organizationValue {
		t.Fatalf("organization = %q, %v", organization.String(), err)
	}
	userValue := "usr_fedcba9876543210fedcba9876543210"
	user, err := access.ParseUserID(userValue)
	if err != nil || user.String() != userValue {
		t.Fatalf("user = %q, %v", user.String(), err)
	}
	for _, value := range []string{"", "org_00000000000000000000000000000000", "org_0123456789ABCDEF0123456789abcdef", "usr_0123"} {
		if _, err := access.ParseOrganizationID(value); !errors.Is(err, access.ErrOrganizationID) {
			t.Fatalf("ParseOrganizationID(%q) error = %v", value, err)
		}
	}
}

func TestMembershipChangesAreClosedAndValidated(t *testing.T) {
	t.Parallel()
	user, _ := access.ParseUserID("usr_0123456789abcdef0123456789abcdef")
	roles, _ := access.NewRoles(access.RoleEditor)
	key, _ := idempotency.ParseKey("remove-user")

	add, err := access.AddMember(user, roles)
	if err != nil || !add.Valid() || add.Kind() != access.MembershipChangeAdd {
		t.Fatalf("add = %#v, %v", add, err)
	}
	set, err := access.SetMemberRoles(user, roles)
	if err != nil || !set.Valid() || set.Kind() != access.MembershipChangeSetRoles {
		t.Fatalf("set = %#v, %v", set, err)
	}
	remove, err := access.RemoveMember(user, key)
	if err != nil || !remove.Valid() || remove.Kind() != access.MembershipChangeRemove {
		t.Fatalf("remove = %#v, %v", remove, err)
	}
	if _, err := access.AddMember(access.UserID{}, roles); err == nil {
		t.Fatal("zero target accepted")
	}
}

func TestRolesAreASet(t *testing.T) {
	t.Parallel()
	roles, err := access.NewRoles(access.RoleEditor, access.RoleAdmin, access.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}
	if !roles.Has(access.RoleAdmin) || !roles.Has(access.RoleEditor) || roles.Has(access.RoleViewer) {
		t.Fatalf("unexpected roles: %v", roles.Values())
	}
	values := roles.Values()
	if len(values) != 2 || values[0] != access.RoleAdmin || values[1] != access.RoleEditor {
		t.Fatalf("role order = %v", values)
	}
	if _, err := access.NewRoles(); !errors.Is(err, access.ErrRoles) {
		t.Fatalf("empty roles error = %v", err)
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	name, err := access.ParseName("  Customer research  ")
	if err != nil || name.String() != "Customer research" {
		t.Fatalf("name = %q, %v", name.String(), err)
	}
	for _, value := range []string{"", strings.Repeat("x", 121)} {
		if _, err := access.ParseName(value); !errors.Is(err, access.ErrName) {
			t.Fatalf("ParseName error = %v", err)
		}
	}
}
