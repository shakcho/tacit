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
	admission LocalAdmission
	found     bool
	err       error
	called    *bool
}

type denialsStub struct{ err error }

func (stub denialsStub) DenySession(context.Context, VerifiedIdentity) error { return stub.err }

func (stub resolverStub) ResolveIdentity(context.Context, VerifiedIdentity, OrganizationTarget) (LocalAdmission, bool, error) {
	if stub.called != nil {
		*stub.called = true
	}
	return stub.admission, stub.found, stub.err
}

func validVerifiedIdentity(t *testing.T) VerifiedIdentity {
	t.Helper()
	namespace, _ := ParseIdentityNamespace("workos-test")
	subject, _ := ParseExternalSubject("user-test")
	externalOrganization, _ := ParseExternalOrganizationID("organization-test")
	session, _ := ParseExternalSessionID("session-test")
	return VerifiedIdentity{
		Namespace: namespace, Subject: subject, Organization: externalOrganization, Session: session,
		IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Minute),
	}
}

func TestAuthenticationServiceClassifiesFailures(t *testing.T) {
	token, _ := ParseBearerToken("header.payload.signature")
	identity := validVerifiedIdentity(t)
	tests := []struct {
		name     string
		verifier CredentialVerifier
		resolver AdmissionResolver
		target   OrganizationTarget
		want     error
	}{
		{"invalid token", verifierStub{err: ErrInvalidCredential}, resolverStub{}, TokenOrganization(), ErrUnauthenticated},
		{"verification outage", verifierStub{err: ErrVerificationUnavailable}, resolverStub{}, TokenOrganization(), ErrIdentityUnavailable},
		{"missing admission", verifierStub{identity: identity}, resolverStub{}, TokenOrganization(), ErrNotAdmitted},
		{"storage outage", verifierStub{identity: identity}, resolverStub{err: errors.New("database unavailable")}, TokenOrganization(), ErrIdentityUnavailable},
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

func TestAuthenticationServiceRejectsInvalidIdentityBeforeAdmissionLookup(t *testing.T) {
	token, _ := ParseBearerToken("header.payload.signature")
	identity := validVerifiedIdentity(t)
	identity.ExpiresAt = time.Now().Add(-time.Second)
	called := false
	service := NewAuthenticationService(
		verifierStub{identity: identity},
		resolverStub{called: &called},
	)

	if _, err := service.Authenticate(t.Context(), token, TokenOrganization()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate error = %v, want %v", err, ErrUnauthenticated)
	}
	if called {
		t.Fatal("admission resolver was called for an invalid verified identity")
	}
}

func TestVerifiedIdentityAllowsIssuerClockSkew(t *testing.T) {
	identity := validVerifiedIdentity(t)
	identity.IssuedAt = time.Now().Add(15 * time.Second)
	identity.ExpiresAt = time.Now().Add(time.Minute)
	if !identity.ValidAt(time.Now()) {
		t.Fatal("verified identity rejected issuer clock skew already accepted by the credential adapter")
	}
}

func TestAuthenticationServiceReturnsOnlyLocalPrincipal(t *testing.T) {
	token, _ := ParseBearerToken("header.payload.signature")
	user, _ := ParseUserID("usr_0123456789abcdef0123456789abcdef")
	organization, _ := ParseOrganizationID("org_0123456789abcdef0123456789abcdef")
	roles, _ := NewRoles(RoleAdmin)
	admission := LocalAdmission{User: user, Organization: organization, Roles: roles}
	identity := validVerifiedIdentity(t)
	service := NewAuthenticationService(
		verifierStub{identity: identity},
		resolverStub{admission: admission, found: true},
	)
	got, err := service.Authenticate(t.Context(), token, ExactOrganization(organization))
	if err != nil || got.UserID() != user || got.OrganizationID() != organization || !got.Roles().Has(RoleAdmin) {
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
