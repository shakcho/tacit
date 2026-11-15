package access

import (
	"context"
	"errors"
	"testing"
	"time"
)

type verifierStub struct {
	identity VerifiedIdentity
	err      error
}

func (stub verifierStub) Verify(context.Context, BearerToken) (VerifiedIdentity, error) {
	return stub.identity, stub.err
}

type resolverStub struct {
	principal LocalPrincipal
	found     bool
	err       error
}

type denialsStub struct{ err error }

func (stub denialsStub) DenySession(context.Context, VerifiedIdentity) error { return stub.err }

func (stub resolverStub) ResolveIdentity(context.Context, VerifiedIdentity, OrganizationTarget) (LocalPrincipal, bool, error) {
	return stub.principal, stub.found, stub.err
}

func TestAuthenticationServiceClassifiesFailures(t *testing.T) {
	token, _ := ParseBearerToken("header.payload.signature")
	tests := []struct {
		name     string
		verifier CredentialVerifier
		resolver AdmissionResolver
		target   OrganizationTarget
		want     error
	}{
		{"invalid token", verifierStub{err: ErrInvalidCredential}, resolverStub{}, TokenOrganization(), ErrUnauthenticated},
		{"verification outage", verifierStub{err: ErrVerificationUnavailable}, resolverStub{}, TokenOrganization(), ErrIdentityUnavailable},
		{"missing admission", verifierStub{}, resolverStub{}, TokenOrganization(), ErrNotAdmitted},
		{"storage outage", verifierStub{}, resolverStub{err: errors.New("database unavailable")}, TokenOrganization(), ErrIdentityUnavailable},
		{"invalid target", verifierStub{}, resolverStub{}, OrganizationTarget{}, ErrIdentityUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewAuthenticationService(test.verifier, test.resolver)
			_, err := service.Authenticate(t.Context(), token, test.target)
			if !errors.Is(err, test.want) {
				t.Fatalf("Authenticate error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestAuthenticationServiceReturnsOnlyLocalPrincipal(t *testing.T) {
	token, _ := ParseBearerToken("header.payload.signature")
	user, _ := ParseUserID("usr_0123456789abcdef0123456789abcdef")
	organization, _ := ParseOrganizationID("org_0123456789abcdef0123456789abcdef")
	roles, _ := NewRoles(RoleAdmin)
	principal := LocalPrincipal{User: user, Organization: organization, Roles: roles}
	service := NewAuthenticationService(verifierStub{identity: VerifiedIdentity{IssuedAt: time.Now()}}, resolverStub{principal: principal, found: true})
	got, err := service.Authenticate(t.Context(), token, ExactOrganization(organization))
	if err != nil || got.User != user || got.Organization != organization || !got.Roles.Has(RoleAdmin) {
		t.Fatalf("Authenticate = %#v, %v", got, err)
	}
}

func TestExternalValuesRejectWhitespaceAndControlCharacters(t *testing.T) {
	for _, value := range []string{"", " user_1", "user 1", "user_1\n"} {
		if _, err := ParseExternalSubject(value); err == nil {
			t.Fatalf("ParseExternalSubject(%q) succeeded", value)
		}
	}
	if subject, err := ParseExternalSubject("user_01HBEQKA6K4QJAS93VPE39W1JT"); err != nil || subject.String() == "" {
		t.Fatalf("valid subject = %q, %v", subject.String(), err)
	}
}

func TestSessionDenialVerifiesBeforeWriting(t *testing.T) {
	token, _ := ParseBearerToken("header.payload.signature")
	service := NewSessionDenialService(verifierStub{}, denialsStub{})
	if err := service.Deny(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		service *SessionDenialService
		want    error
	}{
		{NewSessionDenialService(verifierStub{err: ErrInvalidCredential}, denialsStub{}), ErrUnauthenticated},
		{NewSessionDenialService(verifierStub{err: ErrVerificationUnavailable}, denialsStub{}), ErrIdentityUnavailable},
		{NewSessionDenialService(verifierStub{}, denialsStub{err: errors.New("database unavailable")}), ErrIdentityUnavailable},
	} {
		if err := test.service.Deny(t.Context(), token); !errors.Is(err, test.want) {
			t.Fatalf("Deny error = %v, want %v", err, test.want)
		}
	}
}
