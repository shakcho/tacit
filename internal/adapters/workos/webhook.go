package workos

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/shakcho/tacit/internal/access"
)

var ErrInvalidWebhook = errors.New("invalid WorkOS webhook")

type WebhookConfig struct {
	Namespace access.IdentityNamespace
	Secrets   []string
	Tolerance time.Duration
	Clock     func() time.Time
}

type WebhookVerifier struct {
	namespace access.IdentityNamespace
	secrets   [][]byte
	tolerance time.Duration
	clock     func() time.Time
}

func NewWebhookVerifier(config WebhookConfig) (*WebhookVerifier, error) {
	if config.Namespace.String() == "" || len(config.Secrets) == 0 {
		return nil, errors.New("workos webhook configuration is incomplete")
	}
	secrets := make([][]byte, 0, len(config.Secrets))
	for _, secret := range config.Secrets {
		if len(secret) < 16 || strings.TrimSpace(secret) != secret {
			return nil, errors.New("workos webhook secret is invalid")
		}
		secrets = append(secrets, []byte(secret))
	}
	if config.Tolerance <= 0 {
		config.Tolerance = 5 * time.Minute
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &WebhookVerifier{namespace: config.Namespace, secrets: secrets, tolerance: config.Tolerance, clock: config.Clock}, nil
}

func (verifier *WebhookVerifier) Verify(payload []byte, signatureHeader string) (access.LifecycleEvent, error) {
	if verifier == nil || len(payload) == 0 || signatureHeader == "" {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	timestamp, signatures, err := parseSignatureHeader(signatureHeader)
	if err != nil {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	issuedAt := time.UnixMilli(timestamp).UTC()
	difference := verifier.clock().UTC().Sub(issuedAt)
	if difference < -verifier.tolerance || difference > verifier.tolerance {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	signedPayload := []byte(strconv.FormatInt(timestamp, 10) + "." + string(payload))
	valid := false
	for _, secret := range verifier.secrets {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(signedPayload)
		expected := mac.Sum(nil)
		for _, signature := range signatures {
			if hmac.Equal(expected, signature) {
				valid = true
			}
		}
	}
	if !valid {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	return verifier.parseEvent(payload)
}

func parseSignatureHeader(value string) (int64, [][]byte, error) {
	var timestamp int64
	foundTimestamp := false
	signatures := make([][]byte, 0, 2)
	for _, part := range strings.Split(value, ",") {
		key, raw, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || raw == "" {
			return 0, nil, ErrInvalidWebhook
		}
		switch key {
		case "t":
			if foundTimestamp {
				return 0, nil, ErrInvalidWebhook
			}
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed <= 0 {
				return 0, nil, ErrInvalidWebhook
			}
			timestamp = parsed
			foundTimestamp = true
		case "v1":
			decoded, err := hex.DecodeString(raw)
			if err != nil || len(decoded) != sha256.Size {
				return 0, nil, ErrInvalidWebhook
			}
			signatures = append(signatures, decoded)
		default:
			return 0, nil, ErrInvalidWebhook
		}
	}
	if !foundTimestamp || len(signatures) == 0 {
		return 0, nil, ErrInvalidWebhook
	}
	return timestamp, signatures, nil
}

type webhookEnvelope struct {
	Object    string          `json:"object"`
	ID        string          `json:"id"`
	Event     string          `json:"event"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

type sessionData struct {
	ID string `json:"id"`
}

type userData struct {
	ID string `json:"id"`
}

type organizationData struct {
	ID string `json:"id"`
}

type membershipData struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	OrganizationID string    `json:"organization_id"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type invitationData struct {
	ID             string `json:"id"`
	AcceptedUserID string `json:"accepted_user_id"`
	OrganizationID string `json:"organization_id"`
}

func (verifier *WebhookVerifier) parseEvent(payload []byte) (access.LifecycleEvent, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var envelope webhookEnvelope
	if err := decoder.Decode(&envelope); err != nil || envelope.Object != "event" || envelope.Event == "" || envelope.CreatedAt.IsZero() || len(envelope.Data) == 0 {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	eventID, err := access.ParseExternalEventID(envelope.ID)
	if err != nil {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	event := access.LifecycleEvent{
		Namespace: verifier.namespace, ID: eventID, PayloadHash: sha256.Sum256(payload), OccurredAt: envelope.CreatedAt.UTC(),
	}
	switch envelope.Event {
	case "session.revoked":
		var data sessionData
		if err := decodeStrict(envelope.Data, &data); err != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		session, err := access.ParseExternalSessionID(data.ID)
		if err != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		event.Kind = access.LifecycleSessionRevoked
		event.Session = session
	case "user.deleted":
		var data userData
		if err := decodeStrict(envelope.Data, &data); err != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		subject, err := access.ParseExternalSubject(data.ID)
		if err != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		event.Kind = access.LifecycleSubjectBlocked
		event.Subject = subject
	case "organization.deleted":
		var data organizationData
		if err := decodeStrict(envelope.Data, &data); err != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		organization, err := access.ParseExternalOrganizationID(data.ID)
		if err != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		event.Kind = access.LifecycleOrganizationBlocked
		event.Organization = organization
	case "organization_membership.created", "organization_membership.updated", "organization_membership.deleted":
		var data membershipData
		if err := decodeStrict(envelope.Data, &data); err != nil || data.UpdatedAt.IsZero() {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		subject, subjectErr := access.ParseExternalSubject(data.UserID)
		organization, organizationErr := access.ParseExternalOrganizationID(data.OrganizationID)
		membership, membershipErr := access.ParseExternalMembershipID(data.ID)
		if subjectErr != nil || organizationErr != nil || membershipErr != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		event.Kind = access.LifecycleMembershipDenied
		if envelope.Event != "organization_membership.deleted" && data.Status == "active" {
			event.Kind = access.LifecycleMembershipActivated
		}
		event.Subject = subject
		event.Organization = organization
		event.Membership = membership
		event.ResourceUpdated = data.UpdatedAt.UTC()
	case "invitation.accepted":
		var data invitationData
		if err := decodeStrict(envelope.Data, &data); err != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		invitation, invitationErr := access.ParseExternalInvitationID(data.ID)
		subject, subjectErr := access.ParseExternalSubject(data.AcceptedUserID)
		organization, organizationErr := access.ParseExternalOrganizationID(data.OrganizationID)
		if invitationErr != nil || subjectErr != nil || organizationErr != nil {
			return access.LifecycleEvent{}, ErrInvalidWebhook
		}
		event.Kind = access.LifecycleInvitationAccepted
		event.Invitation = invitation
		event.Subject = subject
		event.Organization = organization
	default:
		event.Kind = access.LifecycleIgnored
	}
	if err := event.Validate(); err != nil {
		return access.LifecycleEvent{}, ErrInvalidWebhook
	}
	return event, nil
}

func decodeStrict(document []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	if err := decoder.Decode(output); err != nil {
		return err
	}
	return ensureJSONEnd(decoder)
}

func ensureJSONEnd(decoder *json.Decoder) error {
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return ErrInvalidWebhook
	}
	return nil
}
