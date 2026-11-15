package workos

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/shakcho/tacit/internal/access"
)

type staticKeyProvider struct {
	key *rsa.PublicKey
	err error
}

func (provider staticKeyProvider) Key(context.Context, string) (*rsa.PublicKey, error) {
	return provider.key, provider.err
}

func testVerifier(t *testing.T, key *rsa.PublicKey, now time.Time) *Verifier {
	t.Helper()
	namespace, _ := access.ParseIdentityNamespace("workos-test")
	return verifierWithKeys(VerifierConfig{
		Namespace: namespace, Issuer: "https://api.workos.com", ClientID: "client_test",
		Clock: func() time.Time { return now }, ClockSkew: 30 * time.Second, MaxTokenLifetime: 10 * time.Minute,
	}, staticKeyProvider{key: key})
}

func signToken(t *testing.T, key *rsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return signEncodedToken(t, key, headerJSON, claimsJSON)
}

func signEncodedToken(t *testing.T, key *rsa.PrivateKey, header, claims []byte) string {
	t.Helper()
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claims)
	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss": "https://api.workos.com", "client_id": "client_test",
		"sub": "user_01HBEQKA6K4QJAS93VPE39W1JT", "sid": "session_01HQSXZGF8FHF7A9ZZFCW4387R",
		"org_id": "org_01HRDMC6CM357W30QMHMQ96Q0S", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	}
}

func TestVerifierAcceptsPinnedWorkOSProfile(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	raw := signToken(t, key, map[string]any{"alg": "RS256", "kid": "key_1", "typ": "JWT"}, validClaims(now))
	token, _ := access.ParseBearerToken(raw)
	identity, err := testVerifier(t, &key.PublicKey, now).Verify(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Namespace.String() != "workos-test" || identity.Subject.String() == "" || identity.Organization.String() == "" || identity.Session.String() == "" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestVerifierRejectsInvalidProfiles(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	tests := []struct {
		name   string
		header map[string]any
		claims func() map[string]any
		signer *rsa.PrivateKey
	}{
		{"forged", map[string]any{"alg": "RS256", "kid": "key_1"}, func() map[string]any { return validClaims(now) }, otherKey},
		{"wrong algorithm", map[string]any{"alg": "PS256", "kid": "key_1"}, func() map[string]any { return validClaims(now) }, key},
		{"embedded key header", map[string]any{"alg": "RS256", "kid": "key_1", "jwk": map[string]any{}}, func() map[string]any { return validClaims(now) }, key},
		{"wrong issuer", map[string]any{"alg": "RS256", "kid": "key_1"}, func() map[string]any {
			value := validClaims(now)
			value["iss"] = "https://attacker.example"
			return value
		}, key},
		{"wrong party", map[string]any{"alg": "RS256", "kid": "key_1"}, func() map[string]any { value := validClaims(now); value["client_id"] = "client_other"; return value }, key},
		{"expired", map[string]any{"alg": "RS256", "kid": "key_1"}, func() map[string]any {
			value := validClaims(now)
			value["iat"] = now.Add(-10 * time.Minute).Unix()
			value["exp"] = now.Add(-time.Minute).Unix()
			return value
		}, key},
		{"future issued", map[string]any{"alg": "RS256", "kid": "key_1"}, func() map[string]any {
			value := validClaims(now)
			value["iat"] = now.Add(time.Minute).Unix()
			value["exp"] = now.Add(6 * time.Minute).Unix()
			return value
		}, key},
		{"excess lifetime", map[string]any{"alg": "RS256", "kid": "key_1"}, func() map[string]any {
			value := validClaims(now)
			value["exp"] = now.Add(11 * time.Minute).Unix()
			return value
		}, key},
		{"missing organization", map[string]any{"alg": "RS256", "kid": "key_1"}, func() map[string]any { value := validClaims(now); delete(value, "org_id"); return value }, key},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := signToken(t, test.signer, test.header, test.claims())
			token, _ := access.ParseBearerToken(raw)
			_, err := testVerifier(t, &key.PublicKey, now).Verify(t.Context(), token)
			if !errors.Is(err, access.ErrInvalidCredential) {
				t.Fatalf("Verify error = %v", err)
			}
		})
	}
}

func TestVerifierRejectsDuplicateSecurityClaim(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	claims := []byte(`{"iss":"https://api.workos.com","iss":"https://attacker.example","client_id":"client_test","sub":"user_1","sid":"session_1","org_id":"org_1","iat":1800000000,"exp":1800000300}`)
	raw := signEncodedToken(t, key, []byte(`{"alg":"RS256","kid":"key_1"}`), claims)
	token, _ := access.ParseBearerToken(raw)
	_, err = testVerifier(t, &key.PublicKey, now).Verify(t.Context(), token)
	if !errors.Is(err, access.ErrInvalidCredential) {
		t.Fatalf("Verify error = %v", err)
	}
}

func TestParseJWKSRejectsWeakAndDuplicateKeys(t *testing.T) {
	weak := base64.RawURLEncoding.EncodeToString(big.NewInt(17).Bytes())
	for _, document := range [][]byte{
		[]byte(`{"keys":[]}`),
		[]byte(`{"keys":[{"kty":"RSA","kid":"one","alg":"RS256","use":"sig","n":"` + weak + `","e":"AQAB"}]}`),
	} {
		if _, err := parseJWKS(document); err == nil {
			t.Fatalf("parseJWKS(%s) succeeded", document)
		}
	}
}
