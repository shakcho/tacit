package workos

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shakcho/tacit/internal/access"
)

const maxJWKSBytes = 1 << 20

type VerifierConfig struct {
	Namespace        access.IdentityNamespace
	Issuer           string
	ClientID         string
	JWKSURL          string
	HTTPClient       *http.Client
	Clock            func() time.Time
	ClockSkew        time.Duration
	MaxTokenLifetime time.Duration
	KeyCacheTTL      time.Duration
	AllowHTTP        bool
}

// Verifier is the narrow WorkOS token-profile adapter. It intentionally keeps
// JOSE details out of access, but the TAC-007 production release gate still
// requires replacing this dependency-free implementation with the selected,
// maintained JOSE package once dependency fetching is available in CI.
type Verifier struct {
	namespace        access.IdentityNamespace
	issuer           string
	clientID         string
	keys             rsaKeyProvider
	clock            func() time.Time
	clockSkew        time.Duration
	maxTokenLifetime time.Duration
}

type rsaKeyProvider interface {
	Key(context.Context, string) (*rsa.PublicKey, error)
}

func NewVerifier(config VerifierConfig) (*Verifier, error) {
	if config.Namespace.String() == "" || config.Issuer == "" || config.ClientID == "" || config.JWKSURL == "" {
		return nil, errors.New("workos verifier configuration is incomplete")
	}
	issuer, err := url.Parse(config.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.RawQuery != "" || issuer.Fragment != "" {
		return nil, errors.New("workos issuer must be an absolute HTTPS URL")
	}
	jwksURL, err := url.Parse(config.JWKSURL)
	if err != nil || jwksURL.Host == "" || jwksURL.RawQuery != "" || jwksURL.Fragment != "" || (jwksURL.Scheme != "https" && !(config.AllowHTTP && jwksURL.Scheme == "http")) {
		return nil, errors.New("workos JWKS URL is invalid")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.ClockSkew <= 0 {
		config.ClockSkew = 30 * time.Second
	}
	if config.MaxTokenLifetime <= 0 {
		config.MaxTokenLifetime = 10 * time.Minute
	}
	if config.KeyCacheTTL <= 0 {
		config.KeyCacheTTL = 5 * time.Minute
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("JWKS redirects are not allowed")
			},
		}
	}
	return &Verifier{
		namespace:        config.Namespace,
		issuer:           config.Issuer,
		clientID:         config.ClientID,
		keys:             &remoteKeySet{url: jwksURL, client: client, ttl: config.KeyCacheTTL, clock: config.Clock},
		clock:            config.Clock,
		clockSkew:        config.ClockSkew,
		maxTokenLifetime: config.MaxTokenLifetime,
	}, nil
}

func (verifier *Verifier) Verify(ctx context.Context, token access.BearerToken) (access.VerifiedIdentity, error) {
	if verifier == nil || verifier.keys == nil {
		return access.VerifiedIdentity{}, access.ErrVerificationUnavailable
	}
	raw := token.UnsafeValue()
	if len(raw) < 1 || len(raw) > access.MaxBearerTokenBytes {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	header, err := decodeObject(headerBytes)
	if err != nil || !onlyHeaderFields(header) {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	algorithm, err := stringField(header, "alg")
	if err != nil || algorithm != "RS256" {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	keyID, err := stringField(header, "kid")
	if err != nil || keyID == "" {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	if typ, exists := header["typ"]; exists {
		value, err := decodeString(typ)
		if err != nil || value != "JWT" {
			return access.VerifiedIdentity{}, access.ErrInvalidCredential
		}
	}
	key, err := verifier.keys.Key(ctx, keyID)
	if err != nil {
		if errors.Is(err, access.ErrInvalidCredential) {
			return access.VerifiedIdentity{}, access.ErrInvalidCredential
		}
		return access.VerifiedIdentity{}, access.ErrVerificationUnavailable
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	claims, err := decodeObject(payloadBytes)
	if err != nil {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	identity, err := verifier.parseClaims(claims)
	if err != nil {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	return identity, nil
}

func (verifier *Verifier) parseClaims(claims map[string]json.RawMessage) (access.VerifiedIdentity, error) {
	issuer, err := requiredString(claims, "iss")
	if err != nil || issuer != verifier.issuer {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	clientID, err := requiredString(claims, "client_id")
	if err != nil || clientID != verifier.clientID {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	subjectValue, err := requiredString(claims, "sub")
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	sessionValue, err := requiredString(claims, "sid")
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	organizationValue, err := requiredString(claims, "org_id")
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	issuedSeconds, err := integerField(claims, "iat")
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	expiresSeconds, err := integerField(claims, "exp")
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	issuedAt := time.Unix(issuedSeconds, 0).UTC()
	expiresAt := time.Unix(expiresSeconds, 0).UTC()
	now := verifier.clock().UTC()
	if issuedAt.After(now.Add(verifier.clockSkew)) || !expiresAt.After(now.Add(-verifier.clockSkew)) || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > verifier.maxTokenLifetime {
		return access.VerifiedIdentity{}, access.ErrInvalidCredential
	}
	if rawNotBefore, exists := claims["nbf"]; exists {
		notBefore, err := decodeInteger(rawNotBefore)
		if err != nil || time.Unix(notBefore, 0).UTC().After(now.Add(verifier.clockSkew)) {
			return access.VerifiedIdentity{}, access.ErrInvalidCredential
		}
	}
	subject, err := access.ParseExternalSubject(subjectValue)
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	session, err := access.ParseExternalSessionID(sessionValue)
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	organization, err := access.ParseExternalOrganizationID(organizationValue)
	if err != nil {
		return access.VerifiedIdentity{}, err
	}
	return access.VerifiedIdentity{
		Namespace: verifier.namespace, Subject: subject, Organization: organization,
		Session: session, IssuedAt: issuedAt, ExpiresAt: expiresAt,
	}, nil
}

func onlyHeaderFields(header map[string]json.RawMessage) bool {
	for name := range header {
		switch name {
		case "alg", "kid", "typ":
		default:
			return false
		}
	}
	return true
}

func requiredString(object map[string]json.RawMessage, name string) (string, error) {
	raw, exists := object[name]
	if !exists {
		return "", access.ErrInvalidCredential
	}
	value, err := decodeString(raw)
	if err != nil || value == "" {
		return "", access.ErrInvalidCredential
	}
	return value, nil
}

func stringField(object map[string]json.RawMessage, name string) (string, error) {
	return requiredString(object, name)
}

func integerField(object map[string]json.RawMessage, name string) (int64, error) {
	raw, exists := object[name]
	if !exists {
		return 0, access.ErrInvalidCredential
	}
	return decodeInteger(raw)
}

func decodeString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func decodeInteger(raw json.RawMessage) (int64, error) {
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, err
	}
	return value, nil
}

func decodeObject(document []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, access.ErrInvalidCredential
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return nil, access.ErrInvalidCredential
		}
		name, ok := nameToken.(string)
		if !ok {
			return nil, access.ErrInvalidCredential
		}
		if _, duplicate := object[name]; duplicate {
			return nil, access.ErrInvalidCredential
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, access.ErrInvalidCredential
		}
		object[name] = raw
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, access.ErrInvalidCredential
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return nil, access.ErrInvalidCredential
	}
	return object, nil
}

type remoteKeySet struct {
	url    *url.URL
	client *http.Client
	ttl    time.Duration
	clock  func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func (set *remoteKeySet) Key(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	set.mu.Lock()
	defer set.mu.Unlock()
	if key := set.keys[keyID]; key != nil && set.clock().Sub(set.fetchedAt) < set.ttl {
		return key, nil
	}
	if err := set.refresh(ctx); err != nil {
		return nil, err
	}
	key := set.keys[keyID]
	if key == nil {
		return nil, access.ErrInvalidCredential
	}
	return key, nil
}

func (set *remoteKeySet) refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, set.url.String(), nil)
	if err != nil {
		return access.ErrVerificationUnavailable
	}
	request.Header.Set("Accept", "application/json")
	response, err := set.client.Do(request)
	if err != nil {
		return access.ErrVerificationUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request.URL.String() != set.url.String() {
		return access.ErrVerificationUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSBytes+1))
	if err != nil || len(body) > maxJWKSBytes {
		return access.ErrVerificationUnavailable
	}
	keys, err := parseJWKS(body)
	if err != nil {
		return access.ErrVerificationUnavailable
	}
	set.keys = keys
	set.fetchedAt = set.clock()
	return nil
}

type jsonWebKeySet struct {
	Keys []jsonWebKey `json:"keys"`
}

type jsonWebKey struct {
	Algorithm string `json:"alg"`
	Exponent  string `json:"e"`
	ID        string `json:"kid"`
	Modulus   string `json:"n"`
	Type      string `json:"kty"`
	Use       string `json:"use"`
}

func parseJWKS(document []byte) (map[string]*rsa.PublicKey, error) {
	var input jsonWebKeySet
	decoder := json.NewDecoder(bytes.NewReader(document))
	if err := decoder.Decode(&input); err != nil || len(input.Keys) == 0 || len(input.Keys) > 32 {
		return nil, errors.New("invalid JWKS")
	}
	keys := make(map[string]*rsa.PublicKey, len(input.Keys))
	for _, inputKey := range input.Keys {
		if inputKey.ID == "" || inputKey.Type != "RSA" || (inputKey.Algorithm != "" && inputKey.Algorithm != "RS256") || (inputKey.Use != "" && inputKey.Use != "sig") {
			return nil, errors.New("invalid JWKS key profile")
		}
		if _, duplicate := keys[inputKey.ID]; duplicate {
			return nil, errors.New("duplicate JWKS key ID")
		}
		modulusBytes, err := base64.RawURLEncoding.DecodeString(inputKey.Modulus)
		if err != nil {
			return nil, errors.New("invalid JWKS modulus")
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(inputKey.Exponent)
		if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
			return nil, errors.New("invalid JWKS exponent")
		}
		exponent := 0
		for _, value := range exponentBytes {
			exponent = exponent<<8 | int(value)
		}
		key := &rsa.PublicKey{N: new(big.Int).SetBytes(modulusBytes), E: exponent}
		if key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
			return nil, errors.New("weak JWKS key")
		}
		keys[inputKey.ID] = key
	}
	return keys, nil
}

func verifierWithKeys(config VerifierConfig, keys rsaKeyProvider) *Verifier {
	return &Verifier{
		namespace: config.Namespace, issuer: config.Issuer, clientID: config.ClientID, keys: keys,
		clock: config.Clock, clockSkew: config.ClockSkew, maxTokenLifetime: config.MaxTokenLifetime,
	}
}
