package realtime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/shakcho/tacit/internal/access"
)

const cursorVersion = "v1."

// CursorCodec signs event IDs for exactly one authenticated user and organization.
// The key digest identifies the signing generation and changes when the key rotates.
type CursorCodec struct {
	key   [sha256.Size]byte
	keyID [8]byte
}

func NewCursorCodec(secret []byte) (CursorCodec, error) {
	if len(secret) < 32 {
		return CursorCodec{}, errors.New("realtime cursor key must be at least 32 bytes")
	}
	key := sha256.Sum256(secret)
	digest := sha256.Sum256(append([]byte("tacit.realtime.cursor.key.v1\x00"), key[:]...))
	var keyID [8]byte
	copy(keyID[:], digest[:])
	return CursorCodec{key: key, keyID: keyID}, nil
}

type EventID [16]byte

type Point struct {
	Position int64
	Event    EventID
}

func (codec CursorCodec) Encode(principal access.LocalPrincipal, event EventID) string {
	var data [8 + len(EventID{}) + sha256.Size]byte
	copy(data[:8], codec.keyID[:])
	copy(data[8:24], event[:])
	copy(data[24:], codec.signature(principal, data[:24]))
	return cursorVersion + base64.RawURLEncoding.EncodeToString(data[:])
}

func (codec CursorCodec) Decode(principal access.LocalPrincipal, token string) (EventID, bool) {
	var event EventID
	const binarySize = 8 + len(EventID{}) + sha256.Size
	if len(token) != len(cursorVersion)+base64.RawURLEncoding.EncodedLen(binarySize) || token[:len(cursorVersion)] != cursorVersion {
		return event, false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(token[len(cursorVersion):])
	if err != nil || !hmac.Equal(data[:8], codec.keyID[:]) || !hmac.Equal(data[24:], codec.signature(principal, data[:24])) {
		return event, false
	}
	copy(event[:], data[8:24])
	return event, event != (EventID{})
}

func (codec CursorCodec) signature(principal access.LocalPrincipal, data []byte) []byte {
	mac := hmac.New(sha256.New, codec.key[:])
	user, organization := principal.UserID().Bytes(), principal.OrganizationID().Bytes()
	mac.Write([]byte("tacit.realtime.cursor.v1\x00" + access.PolicyRevision + "\x00"))
	mac.Write(user[:])
	mac.Write(organization[:])
	mac.Write(data)
	return mac.Sum(nil)
}
