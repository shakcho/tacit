package identityprobe

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maximumTokenBytes = 16 * 1024

type Vendor uint8

const (
	VendorWorkOS Vendor = iota + 1
	VendorClerk
)

type Failure string

const (
	FailureMalformed        Failure = "malformed"
	FailureAlgorithm        Failure = "algorithm"
	FailureKey              Failure = "key"
	FailureSignature        Failure = "signature"
	FailureIssuer           Failure = "issuer"
	FailureParty            Failure = "party"
	FailureTime             Failure = "time"
	FailureIdentity         Failure = "identity"
	FailureSession          Failure = "session"
	FailureOrganization     Failure = "organization"
	FailureRevoked          Failure = "revoked"
	FailureUnavailable      Failure = "unavailable"
	FailureUnsupportedToken Failure = "unsupported_token"
)

type VerificationError struct{ failure Failure }

func (err *VerificationError) Error() string {
	return "identity token rejected: " + string(err.failure)
}
func (err *VerificationError) Failure() Failure { return err.failure }

func FailureOf(err error) (Failure, bool) {
	var verificationError *VerificationError
	if !errors.As(err, &verificationError) {
		return "", false
	}
	return verificationError.failure, true
}

func reject(failure Failure) error { return &VerificationError{failure: failure} }

type Profile struct {
	vendor       Vendor
	namespace    string
	issuer       string
	party        string
	maxLifetime  time.Duration
	clockSkew    time.Duration
	verification map[string]*rsa.PublicKey
}

func NewProfile(
	vendor Vendor,
	namespace string,
	issuer string,
	party string,
	maxLifetime time.Duration,
	clockSkew time.Duration,
	verificationKeys map[string]*rsa.PublicKey,
) (Profile, error) {
	if vendor != VendorWorkOS && vendor != VendorClerk {
		return Profile{}, errors.New("identity profile has an unknown vendor")
	}
	if strings.TrimSpace(namespace) != namespace || namespace == "" || len(namespace) > 64 || strings.ContainsRune(namespace, '\x00') {
		return Profile{}, errors.New("identity profile has an invalid namespace")
	}
	issuerURL, err := url.Parse(issuer)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Host == "" || issuerURL.String() != issuer {
		return Profile{}, errors.New("identity profile has an invalid issuer")
	}
	if strings.TrimSpace(party) != party || party == "" {
		return Profile{}, errors.New("identity profile has an invalid party")
	}
	if maxLifetime <= 0 || maxLifetime > 24*time.Hour {
		return Profile{}, errors.New("identity profile has an invalid maximum lifetime")
	}
	if clockSkew < 0 || clockSkew > 5*time.Minute {
		return Profile{}, errors.New("identity profile has an invalid clock skew")
	}
	if len(verificationKeys) == 0 {
		return Profile{}, errors.New("identity profile has no verification keys")
	}

	keys := make(map[string]*rsa.PublicKey, len(verificationKeys))
	for keyID, key := range verificationKeys {
		if strings.TrimSpace(keyID) != keyID || keyID == "" || key == nil || key.N == nil || key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
			return Profile{}, errors.New("identity profile has an invalid verification key")
		}
		keys[keyID] = &rsa.PublicKey{N: new(big.Int).Set(key.N), E: key.E}
	}

	return Profile{
		vendor:       vendor,
		namespace:    namespace,
		issuer:       issuer,
		party:        party,
		maxLifetime:  maxLifetime,
		clockSkew:    clockSkew,
		verification: keys,
	}, nil
}

type DenialQuery struct {
	Namespace string
	Subject   string
	Session   string
	IssuedAt  time.Time
}

type Denials interface {
	Denied(context.Context, DenialQuery) (bool, error)
}

type MemoryDenials struct {
	mutex    sync.RWMutex
	sessions map[sessionDenialKey]struct{}
	cutoffs  map[subjectDenialKey]time.Time
}

type sessionDenialKey struct{ namespace, session string }
type subjectDenialKey struct{ namespace, subject string }

func NewMemoryDenials() *MemoryDenials {
	return &MemoryDenials{
		sessions: make(map[sessionDenialKey]struct{}),
		cutoffs:  make(map[subjectDenialKey]time.Time),
	}
}

func (denials *MemoryDenials) DenySession(namespace, session string) {
	denials.mutex.Lock()
	defer denials.mutex.Unlock()
	denials.sessions[sessionDenialKey{namespace: namespace, session: session}] = struct{}{}
}

func (denials *MemoryDenials) DenySubjectThrough(namespace, subject string, cutoff time.Time) {
	denials.mutex.Lock()
	defer denials.mutex.Unlock()
	key := subjectDenialKey{namespace: namespace, subject: subject}
	if current, exists := denials.cutoffs[key]; !exists || cutoff.After(current) {
		denials.cutoffs[key] = cutoff
	}
}

func (denials *MemoryDenials) Denied(_ context.Context, query DenialQuery) (bool, error) {
	denials.mutex.RLock()
	defer denials.mutex.RUnlock()
	if _, denied := denials.sessions[sessionDenialKey{namespace: query.Namespace, session: query.Session}]; denied {
		return true, nil
	}
	cutoff, exists := denials.cutoffs[subjectDenialKey{namespace: query.Namespace, subject: query.Subject}]
	return exists && !query.IssuedAt.After(cutoff), nil
}

type Session struct {
	namespace    string
	subject      string
	session      string
	organization string
	issuedAt     time.Time
	expiresAt    time.Time
}

func (session Session) Namespace() string    { return session.namespace }
func (session Session) Subject() string      { return session.subject }
func (session Session) SessionID() string    { return session.session }
func (session Session) Organization() string { return session.organization }
func (session Session) IssuedAt() time.Time  { return session.issuedAt }
func (session Session) ExpiresAt() time.Time { return session.expiresAt }

type Verifier struct {
	profile Profile
	denials Denials
}

func NewVerifier(profile Profile, denials Denials) (*Verifier, error) {
	if denials == nil {
		return nil, errors.New("identity verifier requires denial state")
	}
	return &Verifier{profile: profile, denials: denials}, nil
}

func (verifier *Verifier) Verify(ctx context.Context, compact string, now time.Time) (Session, error) {
	if len(compact) == 0 || len(compact) > maximumTokenBytes {
		return Session{}, reject(FailureMalformed)
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Session{}, reject(FailureMalformed)
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Session{}, reject(FailureMalformed)
	}
	header, err := decodeObject(headerBytes)
	if err != nil {
		return Session{}, reject(FailureMalformed)
	}
	algorithm, err := requiredString(header, "alg")
	if err != nil || algorithm != "RS256" {
		return Session{}, reject(FailureAlgorithm)
	}
	keyID, err := requiredString(header, "kid")
	if err != nil {
		return Session{}, reject(FailureKey)
	}
	key, exists := verifier.profile.verification[keyID]
	if !exists {
		return Session{}, reject(FailureKey)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Session{}, reject(FailureMalformed)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return Session{}, reject(FailureSignature)
	}

	claimBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Session{}, reject(FailureMalformed)
	}
	claims, err := decodeObject(claimBytes)
	if err != nil {
		return Session{}, reject(FailureMalformed)
	}
	if _, impersonated := claims["act"]; impersonated {
		return Session{}, reject(FailureUnsupportedToken)
	}
	issuer, err := requiredString(claims, "iss")
	if err != nil || issuer != verifier.profile.issuer {
		return Session{}, reject(FailureIssuer)
	}
	subject, err := requiredString(claims, "sub")
	if err != nil {
		return Session{}, reject(FailureIdentity)
	}
	sessionID, err := requiredString(claims, "sid")
	if err != nil {
		return Session{}, reject(FailureSession)
	}
	issuedAt, err := requiredUnixTime(claims, "iat")
	if err != nil {
		return Session{}, reject(FailureTime)
	}
	expiresAt, err := requiredUnixTime(claims, "exp")
	if err != nil {
		return Session{}, reject(FailureTime)
	}
	notBefore, hasNotBefore, err := optionalUnixTime(claims, "nbf")
	if err != nil || verifier.profile.vendor == VendorClerk && !hasNotBefore {
		return Session{}, reject(FailureTime)
	}
	if !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > verifier.profile.maxLifetime || issuedAt.After(now.Add(verifier.profile.clockSkew)) || now.After(expiresAt.Add(verifier.profile.clockSkew)) {
		return Session{}, reject(FailureTime)
	}
	if hasNotBefore && (notBefore.After(now.Add(verifier.profile.clockSkew)) || notBefore.After(expiresAt)) {
		return Session{}, reject(FailureTime)
	}

	organization, err := verifier.profile.vendorClaims(claims)
	if err != nil {
		return Session{}, err
	}
	denied, err := verifier.denials.Denied(ctx, DenialQuery{
		Namespace: verifier.profile.namespace,
		Subject:   subject,
		Session:   sessionID,
		IssuedAt:  issuedAt,
	})
	if err != nil {
		return Session{}, reject(FailureUnavailable)
	}
	if denied {
		return Session{}, reject(FailureRevoked)
	}

	return Session{
		namespace:    verifier.profile.namespace,
		subject:      subject,
		session:      sessionID,
		organization: organization,
		issuedAt:     issuedAt,
		expiresAt:    expiresAt,
	}, nil
}

func (verifier *Verifier) VerifyForOrganization(ctx context.Context, compact string, now time.Time, externalOrganization string) (Session, error) {
	session, err := verifier.Verify(ctx, compact, now)
	if err != nil {
		return Session{}, err
	}
	if externalOrganization == "" || session.organization != externalOrganization {
		return Session{}, reject(FailureOrganization)
	}
	return session, nil
}

func (profile Profile) vendorClaims(claims map[string]json.RawMessage) (string, error) {
	switch profile.vendor {
	case VendorWorkOS:
		party, err := requiredString(claims, "client_id")
		if err != nil || party != profile.party {
			return "", reject(FailureParty)
		}
		organization, err := requiredString(claims, "org_id")
		if err != nil {
			return "", reject(FailureOrganization)
		}
		return organization, nil
	case VendorClerk:
		party, err := requiredString(claims, "azp")
		if err != nil || party != profile.party {
			return "", reject(FailureParty)
		}
		version, err := requiredInteger(claims, "v")
		if err != nil || version != 2 {
			return "", reject(FailureUnsupportedToken)
		}
		if _, legacy := claims["org_id"]; legacy {
			return "", reject(FailureUnsupportedToken)
		}
		organizationRaw, exists := claims["o"]
		if !exists {
			return "", reject(FailureOrganization)
		}
		organizationClaims, err := decodeObject(organizationRaw)
		if err != nil {
			return "", reject(FailureOrganization)
		}
		organization, err := requiredString(organizationClaims, "id")
		if err != nil {
			return "", reject(FailureOrganization)
		}
		if rawStatus, exists := claims["sts"]; exists {
			status, err := rawString(rawStatus)
			if err != nil || status != "active" {
				return "", reject(FailureUnsupportedToken)
			}
		}
		return organization, nil
	default:
		return "", reject(FailureUnsupportedToken)
	}
}

func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}
	values := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("expected JSON object key")
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("duplicate JSON object key %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errors.New("expected JSON object end")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected JSON value after object")
	}
	return values, nil
}

func requiredString(values map[string]json.RawMessage, key string) (string, error) {
	raw, exists := values[key]
	if !exists {
		return "", errors.New("missing string")
	}
	return rawString(raw)
}

func rawString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", errors.New("invalid string")
	}
	return value, nil
}

func requiredInteger(values map[string]json.RawMessage, key string) (int64, error) {
	raw, exists := values[key]
	if !exists {
		return 0, errors.New("missing integer")
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, errors.New("invalid integer")
	}
	return value, nil
}

func requiredUnixTime(values map[string]json.RawMessage, key string) (time.Time, error) {
	value, err := requiredInteger(values, key)
	if err != nil || value < 0 {
		return time.Time{}, errors.New("invalid time")
	}
	return time.Unix(value, 0).UTC(), nil
}

func optionalUnixTime(values map[string]json.RawMessage, key string) (time.Time, bool, error) {
	if _, exists := values[key]; !exists {
		return time.Time{}, false, nil
	}
	value, err := requiredUnixTime(values, key)
	return value, true, err
}
