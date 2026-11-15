package identityprobe

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fixture struct {
	vendor     Vendor
	now        time.Time
	keyID      string
	privateKey *rsa.PrivateKey
	profile    Profile
	denials    *MemoryDenials
	verifier   *Verifier
}

type fixtureCase func(*testing.T, *fixture)

var fixtureCases = map[string]fixtureCase{
	"session.sign-in":             fixtureSignIn,
	"session.sign-out":            fixtureSignOut,
	"session.remote-revoke":       fixtureRemoteRevoke,
	"session.organization-switch": fixtureOrganizationSwitch,
	"token.forged":                fixtureForged,
	"token.wrong-issuer":          fixtureWrongIssuer,
	"token.wrong-party":           fixtureWrongParty,
	"token.expired":               fixtureExpired,
	"token.future-not-before":     fixtureFutureNotBefore,
	"token.future-issued-at":      fixtureFutureIssuedAt,
	"token.excessive-lifetime":    fixtureExcessiveLifetime,
	"token.missing-subject":       fixtureMissingSubject,
	"token.missing-session":       fixtureMissingSession,
	"token.missing-organization":  fixtureMissingOrganization,
	"tenant.cross-organization":   fixtureCrossOrganization,
	"keys.rotation":               fixtureKeyRotation,
}

func TestFixtureConformance(t *testing.T) {
	cases, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	required := make(map[string]struct{})
	for _, testCase := range cases {
		if hasEvidence(testCase, EvidenceFixture) {
			required[testCase.ID] = struct{}{}
			if _, implemented := fixtureCases[testCase.ID]; !implemented {
				t.Fatalf("fixture case %q is not implemented", testCase.ID)
			}
		}
	}
	for caseID := range fixtureCases {
		if _, documented := required[caseID]; !documented {
			t.Fatalf("fixture case %q is not in cases.json", caseID)
		}
	}

	for _, vendor := range []Vendor{VendorWorkOS, VendorClerk} {
		vendor := vendor
		t.Run(vendorName(vendor), func(t *testing.T) {
			for _, testCase := range cases {
				implementation, fixtureRequired := fixtureCases[testCase.ID]
				if !fixtureRequired {
					continue
				}
				t.Run(testCase.ID, func(t *testing.T) {
					implementation(t, newFixture(t, vendor))
				})
			}
		})
	}
}

func TestDuplicateSecurityClaimIsRejected(t *testing.T) {
	for _, vendor := range []Vendor{VendorWorkOS, VendorClerk} {
		fixture := newFixture(t, vendor)
		claims := fixture.claimsJSON(`"sub":"user_one","sub":"user_two"`)
		token := fixture.signRaw(t, fixture.privateKey, fixture.keyID, claims, "RS256")
		expectFailure(t, fixture.verify(token), FailureMalformed)
	}
}

func TestUnsupportedAlgorithmIsRejected(t *testing.T) {
	fixture := newFixture(t, VendorWorkOS)
	raw, err := json.Marshal(fixture.validClaims())
	if err != nil {
		t.Fatal(err)
	}
	token := fixture.signRaw(t, fixture.privateKey, fixture.keyID, string(raw), "HS256")
	expectFailure(t, fixture.verify(token), FailureAlgorithm)
}

func TestDenialStateOnlyAdvances(t *testing.T) {
	denials := NewMemoryDenials()
	newer := time.Unix(200, 0).UTC()
	denials.DenySubjectThrough("workos:test", "user_1", newer)
	denials.DenySubjectThrough("workos:test", "user_1", time.Unix(100, 0).UTC())

	denied, err := denials.Denied(context.Background(), DenialQuery{
		Namespace: "workos:test",
		Subject:   "user_1",
		Session:   "session_1",
		IssuedAt:  time.Unix(150, 0).UTC(),
	})
	if err != nil || !denied {
		t.Fatalf("denied = %v, %v", denied, err)
	}
}

func TestVerifierFailsClosedWhenDenialStateFails(t *testing.T) {
	fixture := newFixture(t, VendorWorkOS)
	verifier, err := NewVerifier(fixture.profile, failingDenials{})
	if err != nil {
		t.Fatal(err)
	}
	token := fixture.signClaims(t, fixture.privateKey, fixture.keyID, fixture.validClaims())
	_, err = verifier.Verify(context.Background(), token, fixture.now)
	expectFailure(t, err, FailureUnavailable)
}

type failingDenials struct{}

func (failingDenials) Denied(context.Context, DenialQuery) (bool, error) {
	return false, errors.New("unavailable")
}

func newFixture(t *testing.T, vendor Vendor) *fixture {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	issuer, party, namespace := vendorSettings(vendor)
	profile, err := NewProfile(vendor, namespace, issuer, party, 5*time.Minute, 30*time.Second, map[string]*rsa.PublicKey{
		"key-current": &privateKey.PublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	denials := NewMemoryDenials()
	verifier, err := NewVerifier(profile, denials)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		vendor:     vendor,
		now:        now,
		keyID:      "key-current",
		privateKey: privateKey,
		profile:    profile,
		denials:    denials,
		verifier:   verifier,
	}
}

func (fixture *fixture) validClaims() map[string]any {
	issuer, party, _ := vendorSettings(fixture.vendor)
	claims := map[string]any{
		"exp": fixture.now.Add(4 * time.Minute).Unix(),
		"iat": fixture.now.Add(-time.Minute).Unix(),
		"iss": issuer,
		"sid": "session_01",
		"sub": "user_01",
	}
	if fixture.vendor == VendorWorkOS {
		claims["client_id"] = party
		claims["org_id"] = "organization_a"
	} else {
		claims["azp"] = party
		claims["nbf"] = fixture.now.Add(-time.Minute).Unix()
		claims["o"] = map[string]any{"id": "organization_a"}
		claims["v"] = 2
	}
	return claims
}

func (fixture *fixture) signClaims(t *testing.T, key *rsa.PrivateKey, keyID string, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return fixture.signRaw(t, key, keyID, string(raw), "RS256")
}

func (fixture *fixture) signRaw(t *testing.T, key *rsa.PrivateKey, keyID, rawClaims, algorithm string) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": algorithm, "kid": keyID, "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedClaims := base64.RawURLEncoding.EncodeToString([]byte(rawClaims))
	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (fixture *fixture) verify(token string) error {
	_, err := fixture.verifier.Verify(context.Background(), token, fixture.now)
	return err
}

func (fixture *fixture) claimsJSON(extra string) string {
	issuer, party, _ := vendorSettings(fixture.vendor)
	if fixture.vendor == VendorWorkOS {
		return fmt.Sprintf(`{"iss":%q,"client_id":%q,"sid":"session_01","org_id":"organization_a","iat":%d,"exp":%d,%s}`,
			issuer, party, fixture.now.Add(-time.Minute).Unix(), fixture.now.Add(4*time.Minute).Unix(), extra)
	}
	return fmt.Sprintf(`{"iss":%q,"azp":%q,"sid":"session_01","o":{"id":"organization_a"},"v":2,"nbf":%d,"iat":%d,"exp":%d,%s}`,
		issuer, party, fixture.now.Add(-time.Minute).Unix(), fixture.now.Add(-time.Minute).Unix(), fixture.now.Add(4*time.Minute).Unix(), extra)
}

func fixtureSignIn(t *testing.T, fixture *fixture) {
	token := fixture.signClaims(t, fixture.privateKey, fixture.keyID, fixture.validClaims())
	session, err := fixture.verifier.Verify(context.Background(), token, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if session.Subject() != "user_01" || session.SessionID() != "session_01" || session.Organization() != "organization_a" {
		t.Fatalf("unexpected session: %q %q %q", session.Subject(), session.SessionID(), session.Organization())
	}
}

func fixtureSignOut(t *testing.T, fixture *fixture) {
	token := fixture.signClaims(t, fixture.privateKey, fixture.keyID, fixture.validClaims())
	if err := fixture.verify(token); err != nil {
		t.Fatal(err)
	}
	fixture.denials.DenySession(fixture.profile.namespace, "session_01")
	expectFailure(t, fixture.verify(token), FailureRevoked)
}

func fixtureRemoteRevoke(t *testing.T, fixture *fixture) {
	token := fixture.signClaims(t, fixture.privateKey, fixture.keyID, fixture.validClaims())
	fixture.denials.DenySubjectThrough(fixture.profile.namespace, "user_01", fixture.now)
	expectFailure(t, fixture.verify(token), FailureRevoked)
}

func fixtureOrganizationSwitch(t *testing.T, fixture *fixture) {
	first := fixture.validClaims()
	second := fixture.validClaims()
	setOrganization(fixture.vendor, second, "organization_b")
	firstToken := fixture.signClaims(t, fixture.privateKey, fixture.keyID, first)
	secondToken := fixture.signClaims(t, fixture.privateKey, fixture.keyID, second)
	firstSession, err := fixture.verifier.Verify(context.Background(), firstToken, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := fixture.verifier.Verify(context.Background(), secondToken, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if firstSession.Organization() != "organization_a" || secondSession.Organization() != "organization_b" {
		t.Fatalf("organization switch = %q -> %q", firstSession.Organization(), secondSession.Organization())
	}
}

func fixtureForged(t *testing.T, fixture *fixture) {
	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token := fixture.signClaims(t, attacker, fixture.keyID, fixture.validClaims())
	expectFailure(t, fixture.verify(token), FailureSignature)
}

func fixtureWrongIssuer(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	claims["iss"] = "https://attacker.example"
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureIssuer)
}

func fixtureWrongParty(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	if fixture.vendor == VendorWorkOS {
		claims["client_id"] = "client_other"
	} else {
		claims["azp"] = "https://other.example"
	}
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureParty)
}

func fixtureExpired(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	claims["iat"] = fixture.now.Add(-5 * time.Minute).Unix()
	claims["exp"] = fixture.now.Add(-time.Minute).Unix()
	if fixture.vendor == VendorClerk {
		claims["nbf"] = fixture.now.Add(-5 * time.Minute).Unix()
	}
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureTime)
}

func fixtureFutureNotBefore(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	claims["nbf"] = fixture.now.Add(time.Minute).Unix()
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureTime)
}

func fixtureFutureIssuedAt(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	claims["iat"] = fixture.now.Add(time.Minute).Unix()
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureTime)
}

func fixtureExcessiveLifetime(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	claims["exp"] = fixture.now.Add(10 * time.Minute).Unix()
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureTime)
}

func fixtureMissingSubject(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	delete(claims, "sub")
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureIdentity)
}

func fixtureMissingSession(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	delete(claims, "sid")
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureSession)
}

func fixtureMissingOrganization(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	if fixture.vendor == VendorWorkOS {
		delete(claims, "org_id")
	} else {
		delete(claims, "o")
	}
	expectFailure(t, fixture.verify(fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)), FailureOrganization)
}

func fixtureCrossOrganization(t *testing.T, fixture *fixture) {
	claims := fixture.validClaims()
	setOrganization(fixture.vendor, claims, "organization_b")
	token := fixture.signClaims(t, fixture.privateKey, fixture.keyID, claims)
	_, err := fixture.verifier.VerifyForOrganization(context.Background(), token, fixture.now, "organization_a")
	expectFailure(t, err, FailureOrganization)
}

func fixtureKeyRotation(t *testing.T, fixture *fixture) {
	rotated, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer, party, namespace := vendorSettings(fixture.vendor)
	profile, err := NewProfile(fixture.vendor, namespace, issuer, party, 5*time.Minute, 30*time.Second, map[string]*rsa.PublicKey{
		fixture.keyID: &fixture.privateKey.PublicKey,
		"key-rotated": &rotated.PublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(profile, fixture.denials)
	if err != nil {
		t.Fatal(err)
	}
	rotatedToken := fixture.signClaims(t, rotated, "key-rotated", fixture.validClaims())
	if _, err := verifier.Verify(context.Background(), rotatedToken, fixture.now); err != nil {
		t.Fatal(err)
	}
	unknownToken := fixture.signClaims(t, rotated, "key-unknown", fixture.validClaims())
	_, err = verifier.Verify(context.Background(), unknownToken, fixture.now)
	expectFailure(t, err, FailureKey)
}

func expectFailure(t *testing.T, err error, expected Failure) {
	t.Helper()
	actual, ok := FailureOf(err)
	if !ok || actual != expected {
		t.Fatalf("failure = %q, %v; want %q", actual, err, expected)
	}
}

func hasEvidence(testCase Case, expected Evidence) bool {
	for _, evidence := range testCase.Evidence {
		if evidence == expected {
			return true
		}
	}
	return false
}

func vendorName(vendor Vendor) string {
	if vendor == VendorWorkOS {
		return "workos"
	}
	return "clerk"
}

func vendorSettings(vendor Vendor) (issuer, party, namespace string) {
	if vendor == VendorWorkOS {
		return "https://api.workos.com", "client_tacit_test", "workos:test"
	}
	return "https://tacit-test.clerk.accounts.dev", "https://tacit.example", "clerk:test"
}

func setOrganization(vendor Vendor, claims map[string]any, organization string) {
	if vendor == VendorWorkOS {
		claims["org_id"] = organization
		return
	}
	claims["o"] = map[string]any{"id": organization}
}
