package access

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"
)

const (
	MaxBearerTokenBytes = 16 << 10
	maxExternalIDBytes  = 512
	maxNamespaceBytes   = 64
)

var (
	ErrBearerToken             = errors.New("invalid bearer token")
	ErrIdentityNamespace       = errors.New("invalid identity namespace")
	ErrExternalIdentityID      = errors.New("invalid external identity ID")
	ErrInvalidCredential       = errors.New("invalid credential")
	ErrVerificationUnavailable = errors.New("credential verification unavailable")
	ErrUnauthenticated         = errors.New("unauthenticated")
	ErrNotAdmitted             = errors.New("identity is not admitted")
	ErrIdentityUnavailable     = errors.New("identity service unavailable")
	ErrLifecycleEvent          = errors.New("invalid identity lifecycle event")
	ErrLifecycleConflict       = errors.New("identity lifecycle event conflict")
	ErrInvitationConflict      = errors.New("identity invitation grant conflict")
)

// BearerToken deliberately has no String method so credentials are not
// accidentally included in structured logs or errors.
type BearerToken struct{ value string }

func ParseBearerToken(value string) (BearerToken, error) {
	if len(value) < 1 || len(value) > MaxBearerTokenBytes || strings.TrimSpace(value) != value {
		return BearerToken{}, ErrBearerToken
	}
	for _, character := range value {
		if character <= ' ' || character == 0x7f {
			return BearerToken{}, ErrBearerToken
		}
	}
	return BearerToken{value: value}, nil
}

// UnsafeValue is restricted to credential adapters. Product code should pass
// BearerToken through Authenticator without inspecting it.
func (token BearerToken) UnsafeValue() string { return token.value }

type IdentityNamespace struct{ value string }

func ParseIdentityNamespace(value string) (IdentityNamespace, error) {
	if !validExternalValue(value, maxNamespaceBytes) {
		return IdentityNamespace{}, ErrIdentityNamespace
	}
	return IdentityNamespace{value: value}, nil
}

func (namespace IdentityNamespace) String() string { return namespace.value }

type ExternalSubject struct{ value string }

func ParseExternalSubject(value string) (ExternalSubject, error) {
	if !validExternalValue(value, maxExternalIDBytes) {
		return ExternalSubject{}, ErrExternalIdentityID
	}
	return ExternalSubject{value: value}, nil
}

func (subject ExternalSubject) String() string { return subject.value }

type ExternalOrganizationID struct{ value string }

func ParseExternalOrganizationID(value string) (ExternalOrganizationID, error) {
	if !validExternalValue(value, maxExternalIDBytes) {
		return ExternalOrganizationID{}, ErrExternalIdentityID
	}
	return ExternalOrganizationID{value: value}, nil
}

func (organization ExternalOrganizationID) String() string { return organization.value }

type ExternalSessionID struct{ value string }

func ParseExternalSessionID(value string) (ExternalSessionID, error) {
	if !validExternalValue(value, maxExternalIDBytes) {
		return ExternalSessionID{}, ErrExternalIdentityID
	}
	return ExternalSessionID{value: value}, nil
}

func (session ExternalSessionID) String() string { return session.value }

type ExternalMembershipID struct{ value string }

func ParseExternalMembershipID(value string) (ExternalMembershipID, error) {
	if !validExternalValue(value, maxExternalIDBytes) {
		return ExternalMembershipID{}, ErrExternalIdentityID
	}
	return ExternalMembershipID{value: value}, nil
}

func (membership ExternalMembershipID) String() string { return membership.value }

type ExternalEventID struct{ value string }

func ParseExternalEventID(value string) (ExternalEventID, error) {
	if !validExternalValue(value, maxExternalIDBytes) {
		return ExternalEventID{}, ErrExternalIdentityID
	}
	return ExternalEventID{value: value}, nil
}

func (event ExternalEventID) String() string { return event.value }

type ExternalInvitationID struct{ value string }

func ParseExternalInvitationID(value string) (ExternalInvitationID, error) {
	if !validExternalValue(value, maxExternalIDBytes) {
		return ExternalInvitationID{}, ErrExternalIdentityID
	}
	return ExternalInvitationID{value: value}, nil
}

func (invitation ExternalInvitationID) String() string { return invitation.value }

func validExternalValue(value string, maximum int) bool {
	if len(value) < 1 || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character == 0x7f {
			return false
		}
	}
	return true
}

type VerifiedIdentity struct {
	Namespace    IdentityNamespace
	Subject      ExternalSubject
	Organization ExternalOrganizationID
	Session      ExternalSessionID
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

type organizationTargetKind uint8

const (
	targetInvalid organizationTargetKind = iota
	targetTokenOrganization
	targetExactOrganization
)

// OrganizationTarget forces callers to distinguish session bootstrap from a
// tenant request. TokenOrganization is only for the bootstrap session route.
type OrganizationTarget struct {
	kind         organizationTargetKind
	organization OrganizationID
}

func TokenOrganization() OrganizationTarget {
	return OrganizationTarget{kind: targetTokenOrganization}
}

func ExactOrganization(organization OrganizationID) OrganizationTarget {
	return OrganizationTarget{kind: targetExactOrganization, organization: organization}
}

func (target OrganizationTarget) UsesTokenOrganization() bool {
	return target.kind == targetTokenOrganization
}

func (target OrganizationTarget) Organization() (OrganizationID, bool) {
	return target.organization, target.kind == targetExactOrganization
}

func (target OrganizationTarget) Valid() bool {
	return target.kind == targetTokenOrganization || target.kind == targetExactOrganization
}

type LocalPrincipal struct {
	User         UserID
	Organization OrganizationID
	Roles        Roles
}

type CredentialVerifier interface {
	Verify(context.Context, BearerToken) (VerifiedIdentity, error)
}

type AdmissionResolver interface {
	ResolveIdentity(context.Context, VerifiedIdentity, OrganizationTarget) (LocalPrincipal, bool, error)
}

type Authenticator interface {
	Authenticate(context.Context, BearerToken, OrganizationTarget) (LocalPrincipal, error)
}

type AuthenticationService struct {
	verifier CredentialVerifier
	resolver AdmissionResolver
}

func NewAuthenticationService(verifier CredentialVerifier, resolver AdmissionResolver) *AuthenticationService {
	return &AuthenticationService{verifier: verifier, resolver: resolver}
}

func (service *AuthenticationService) Authenticate(
	ctx context.Context,
	token BearerToken,
	target OrganizationTarget,
) (LocalPrincipal, error) {
	if service == nil || service.verifier == nil || service.resolver == nil || !target.Valid() {
		return LocalPrincipal{}, ErrIdentityUnavailable
	}
	identity, err := service.verifier.Verify(ctx, token)
	if err != nil {
		if errors.Is(err, ErrVerificationUnavailable) {
			return LocalPrincipal{}, ErrIdentityUnavailable
		}
		return LocalPrincipal{}, ErrUnauthenticated
	}
	principal, found, err := service.resolver.ResolveIdentity(ctx, identity, target)
	if err != nil {
		return LocalPrincipal{}, ErrIdentityUnavailable
	}
	if !found {
		return LocalPrincipal{}, ErrNotAdmitted
	}
	return principal, nil
}

type LifecycleKind uint8

const (
	LifecycleIgnored LifecycleKind = iota
	LifecycleSessionRevoked
	LifecycleSubjectBlocked
	LifecycleOrganizationBlocked
	LifecycleMembershipActivated
	LifecycleMembershipDenied
	LifecycleInvitationAccepted
)

type LifecycleEvent struct {
	Namespace       IdentityNamespace
	ID              ExternalEventID
	Kind            LifecycleKind
	PayloadHash     [sha256.Size]byte
	OccurredAt      time.Time
	ResourceUpdated time.Time
	Subject         ExternalSubject
	Organization    ExternalOrganizationID
	Session         ExternalSessionID
	Membership      ExternalMembershipID
	Invitation      ExternalInvitationID
}

func (event LifecycleEvent) Validate() error {
	if event.Namespace.String() == "" || event.ID.String() == "" || event.OccurredAt.IsZero() {
		return ErrLifecycleEvent
	}
	switch event.Kind {
	case LifecycleIgnored:
		return nil
	case LifecycleSessionRevoked:
		if event.Session.String() == "" {
			return ErrLifecycleEvent
		}
	case LifecycleSubjectBlocked:
		if event.Subject.String() == "" {
			return ErrLifecycleEvent
		}
	case LifecycleOrganizationBlocked:
		if event.Organization.String() == "" {
			return ErrLifecycleEvent
		}
	case LifecycleMembershipActivated, LifecycleMembershipDenied:
		if event.Subject.String() == "" || event.Organization.String() == "" || event.Membership.String() == "" || event.ResourceUpdated.IsZero() {
			return ErrLifecycleEvent
		}
	case LifecycleInvitationAccepted:
		if event.Subject.String() == "" || event.Organization.String() == "" || event.Invitation.String() == "" {
			return ErrLifecycleEvent
		}
	default:
		return ErrLifecycleEvent
	}
	return nil
}

type LifecycleOutcome uint8

const (
	LifecycleUnavailable LifecycleOutcome = iota
	LifecycleApplied
	LifecycleReplayed
	LifecycleRecordedIgnored
)

type IdentityLifecycle interface {
	ApplyLifecycle(context.Context, LifecycleEvent) (LifecycleOutcome, error)
}

type SessionDenials interface {
	DenySession(context.Context, VerifiedIdentity) error
}

type SessionDenier interface {
	Deny(context.Context, BearerToken) error
}

type SessionDenialService struct {
	verifier CredentialVerifier
	denials  SessionDenials
}

func NewSessionDenialService(verifier CredentialVerifier, denials SessionDenials) *SessionDenialService {
	return &SessionDenialService{verifier: verifier, denials: denials}
}

func (service *SessionDenialService) Deny(ctx context.Context, token BearerToken) error {
	if service == nil || service.verifier == nil || service.denials == nil {
		return ErrIdentityUnavailable
	}
	identity, err := service.verifier.Verify(ctx, token)
	if err != nil {
		if errors.Is(err, ErrVerificationUnavailable) {
			return ErrIdentityUnavailable
		}
		return ErrUnauthenticated
	}
	if err := service.denials.DenySession(ctx, identity); err != nil {
		return ErrIdentityUnavailable
	}
	return nil
}

type BootstrapIdentity struct {
	Namespace            IdentityNamespace
	Subject              ExternalSubject
	ExternalOrganization ExternalOrganizationID
	ExternalMembership   ExternalMembershipID
	User                 UserID
	Organization         OrganizationID
	OrganizationName     Name
}

type IdentityBootstrapper interface {
	BootstrapIdentity(context.Context, BootstrapIdentity) error
}

type InvitationGrant struct {
	Namespace    IdentityNamespace
	Invitation   ExternalInvitationID
	Organization OrganizationID
	Role         Role
}

func (grant InvitationGrant) Validate() error {
	if grant.Namespace.String() == "" || grant.Invitation.String() == "" || grant.Organization.String() == "" || grant.Role.String() == "" {
		return ErrLifecycleEvent
	}
	return nil
}

type IdentityInvitations interface {
	RegisterInvitationGrant(context.Context, InvitationGrant) error
}
