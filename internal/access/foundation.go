package access

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/shakcho/tacit/internal/runtime/idempotency"
)

var (
	ErrOrganizationID = errors.New("invalid organization ID")
	ErrUserID         = errors.New("invalid user ID")
	ErrName           = errors.New("invalid name")
	ErrRoles          = errors.New("invalid membership roles")
)

type OrganizationID struct{ value [16]byte }

func ParseOrganizationID(value string) (OrganizationID, error) {
	bytes, err := parseID(value, "org_", ErrOrganizationID)
	return OrganizationID{value: bytes}, err
}

func (id OrganizationID) String() string  { return formatID("org_", id.value) }
func (id OrganizationID) Bytes() [16]byte { return id.value }

type UserID struct{ value [16]byte }

func ParseUserID(value string) (UserID, error) {
	bytes, err := parseID(value, "usr_", ErrUserID)
	return UserID{value: bytes}, err
}

func (id UserID) String() string  { return formatID("usr_", id.value) }
func (id UserID) Bytes() [16]byte { return id.value }

func parseID(value, prefix string, invalid error) ([16]byte, error) {
	var decoded [16]byte
	if len(value) != len(prefix)+32 || !strings.HasPrefix(value, prefix) {
		return decoded, invalid
	}
	for _, char := range value[len(prefix):] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return decoded, invalid
		}
	}
	if _, err := hex.Decode(decoded[:], []byte(value[len(prefix):])); err != nil {
		return [16]byte{}, invalid
	}
	return decoded, nil
}

func formatID(prefix string, value [16]byte) string {
	return prefix + hex.EncodeToString(value[:])
}

type Name struct{ value string }

func ParseName(value string) (Name, error) {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 120 {
		return Name{}, ErrName
	}
	return Name{value: value}, nil
}

func (name Name) String() string { return name.value }

type Role uint8

const (
	RoleAdmin Role = iota + 1
	RoleEditor
	RoleViewer
)

func (role Role) String() string {
	switch role {
	case RoleAdmin:
		return "admin"
	case RoleEditor:
		return "editor"
	case RoleViewer:
		return "viewer"
	default:
		return ""
	}
}

type Roles struct{ bits uint8 }

func NewRoles(values ...Role) (Roles, error) {
	var roles Roles
	for _, role := range values {
		if role < RoleAdmin || role > RoleViewer {
			return Roles{}, ErrRoles
		}
		roles.bits |= 1 << (role - 1)
	}
	if roles.bits == 0 {
		return Roles{}, ErrRoles
	}
	return roles, nil
}

func (roles Roles) Has(role Role) bool {
	return role >= RoleAdmin && role <= RoleViewer && roles.bits&(1<<(role-1)) != 0
}

func (roles Roles) Values() []Role {
	values := make([]Role, 0, 3)
	for role := RoleAdmin; role <= RoleViewer; role++ {
		if roles.Has(role) {
			values = append(values, role)
		}
	}
	return values
}

type MembershipRef struct {
	Organization OrganizationID
	User         UserID
}

type Membership struct {
	Ref   MembershipRef
	Roles Roles
}

type CreateUserCommand struct{ ID UserID }

type CreateOrganizationCommand struct {
	ID    OrganizationID
	Name  Name
	Owner UserID
}

type AddMembershipCommand struct {
	Actor UserID
	Ref   MembershipRef
	Roles Roles
}

type SetMembershipRolesCommand struct {
	Actor UserID
	Ref   MembershipRef
	Roles Roles
}

type RemoveMembershipCommand struct {
	Actor UserID
	Ref   MembershipRef
	Key   idempotency.Key
}

type MembershipOutcome uint8

const (
	MembershipUnavailable MembershipOutcome = iota
	MembershipChanged
	MembershipUnchanged
	MembershipLastAdmin
	MembershipNotFound
	MembershipConflict
)

type MembershipResult struct {
	outcome  MembershipOutcome
	replayed bool
}

func NewMembershipResult(outcome MembershipOutcome) MembershipResult {
	if outcome > MembershipConflict {
		return MembershipResult{}
	}
	return MembershipResult{outcome: outcome}
}

func NewReplayedMembershipResult(outcome MembershipOutcome) MembershipResult {
	result := NewMembershipResult(outcome)
	result.replayed = true
	return result
}

func (result MembershipResult) Outcome() MembershipOutcome { return result.outcome }
func (result MembershipResult) Replayed() bool             { return result.replayed }

type Directory interface {
	CreateUser(context.Context, CreateUserCommand) error
	CreateOrganization(context.Context, CreateOrganizationCommand) error
	AddMembership(context.Context, AddMembershipCommand) (MembershipResult, error)
	SetMembershipRoles(context.Context, SetMembershipRolesCommand) (MembershipResult, error)
	RemoveMembership(context.Context, RemoveMembershipCommand) (MembershipResult, error)
	FindMembership(context.Context, MembershipRef) (Membership, bool, error)
}
