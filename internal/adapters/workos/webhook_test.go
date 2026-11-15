package workos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shakcho/tacit/internal/access"
)

func signedWebhookHeader(payload []byte, secret string, timestamp time.Time) string {
	milliseconds := timestamp.UnixMilli()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.%s", milliseconds, payload)
	return fmt.Sprintf("t=%d, v1=%s", milliseconds, hex.EncodeToString(mac.Sum(nil)))
}

func webhookVerifier(t *testing.T, now time.Time) (*WebhookVerifier, string) {
	t.Helper()
	namespace, _ := access.ParseIdentityNamespace("workos-test")
	secret := "whsec_0123456789abcdef"
	verifier, err := NewWebhookVerifier(WebhookConfig{
		Namespace: namespace, Secrets: []string{secret}, Tolerance: 5 * time.Minute, Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return verifier, secret
}

func TestWebhookVerifierTreatsDeletedMembershipAsDenied(t *testing.T) {
	now := time.Date(2026, time.November, 15, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"object":"event","id":"event_1","event":"organization_membership.deleted","created_at":"2026-11-15T10:00:00Z","data":{"id":"om_1","user_id":"user_1","organization_id":"org_1","status":"active","updated_at":"2026-11-15T09:59:00Z"}}`)
	verifier, secret := webhookVerifier(t, now)
	event, err := verifier.Verify(payload, signedWebhookHeader(payload, secret, now))
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != access.LifecycleMembershipDenied || event.Membership.String() != "om_1" {
		t.Fatalf("event = %#v", event)
	}
}

func TestWebhookVerifierRejectsForgeryAndReplayWindow(t *testing.T) {
	now := time.Date(2026, time.November, 15, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"object":"event","id":"event_1","event":"session.revoked","created_at":"2026-11-15T10:00:00Z","data":{"id":"session_1"}}`)
	verifier, secret := webhookVerifier(t, now)
	for _, header := range []string{
		signedWebhookHeader(payload, "whsec_wrong_secret_123", now),
		signedWebhookHeader(payload, secret, now.Add(-10*time.Minute)),
		"t=invalid,v1=invalid",
	} {
		if _, err := verifier.Verify(payload, header); !errors.Is(err, ErrInvalidWebhook) {
			t.Fatalf("Verify(%q) error = %v", header, err)
		}
	}
}

func TestWebhookVerifierParsesSessionRevocation(t *testing.T) {
	now := time.Date(2026, time.November, 15, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"object":"event","id":"event_2","event":"session.revoked","created_at":"2026-11-15T10:00:00Z","data":{"id":"session_2"}}`)
	verifier, secret := webhookVerifier(t, now)
	event, err := verifier.Verify(payload, signedWebhookHeader(payload, secret, now))
	if err != nil || event.Kind != access.LifecycleSessionRevoked || event.Session.String() != "session_2" {
		t.Fatalf("event = %#v, %v", event, err)
	}
}

func TestWebhookVerifierParsesOrganizationDeletion(t *testing.T) {
	now := time.Date(2026, time.November, 15, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"object":"event","id":"event_3","event":"organization.deleted","created_at":"2026-11-15T10:00:00Z","data":{"object":"organization","id":"org_3","name":"Deleted"}}`)
	verifier, secret := webhookVerifier(t, now)
	event, err := verifier.Verify(payload, signedWebhookHeader(payload, secret, now))
	if err != nil || event.Kind != access.LifecycleOrganizationBlocked || event.Organization.String() != "org_3" {
		t.Fatalf("event = %#v, %v", event, err)
	}
}

func TestWebhookVerifierUsesAcceptedUserInsteadOfInvitationEmail(t *testing.T) {
	now := time.Date(2026, time.November, 15, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"object":"event","id":"event_4","event":"invitation.accepted","created_at":"2026-11-15T10:00:00Z","data":{"id":"invitation_4","email":"existing@example.com","accepted_user_id":"user_verified_4","organization_id":"org_4"}}`)
	verifier, secret := webhookVerifier(t, now)
	event, err := verifier.Verify(payload, signedWebhookHeader(payload, secret, now))
	if err != nil || event.Kind != access.LifecycleInvitationAccepted || event.Subject.String() != "user_verified_4" || event.Invitation.String() != "invitation_4" {
		t.Fatalf("event = %#v, %v", event, err)
	}
}

func TestWebhookVerifierAcceptsProviderMetadata(t *testing.T) {
	now := time.Date(2026, time.November, 15, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"object":"event","id":"event_5","event":"session.revoked","created_at":"2026-11-15T10:00:00Z","environment_id":"env_1","data":{"object":"session","id":"session_5","provider_metadata":"preserved"}}`)
	verifier, secret := webhookVerifier(t, now)
	event, err := verifier.Verify(payload, signedWebhookHeader(payload, secret, now))
	if err != nil || event.Kind != access.LifecycleSessionRevoked || event.Session.String() != "session_5" {
		t.Fatalf("event = %#v, %v", event, err)
	}
}

func TestWebhookVerifierRejectsNonEventEnvelope(t *testing.T) {
	now := time.Date(2026, time.November, 15, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"object":"session","id":"event_6","event":"session.revoked","created_at":"2026-11-15T10:00:00Z","data":{"id":"session_6"}}`)
	verifier, secret := webhookVerifier(t, now)
	if _, err := verifier.Verify(payload, signedWebhookHeader(payload, secret, now)); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("Verify() error = %v", err)
	}
}
