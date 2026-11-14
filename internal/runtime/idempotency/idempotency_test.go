package idempotency_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shakcho/tacit/internal/runtime/idempotency"
)

func TestKey(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"request-1", "A_b.c~9", strings.Repeat("x", 128)} {
		key, err := idempotency.ParseKey(value)
		if err != nil || key.String() != value {
			t.Fatalf("ParseKey(%q) = %q, %v", value, key.String(), err)
		}
	}
	for _, value := range []string{"", "bad key", "slash/key", strings.Repeat("x", 129)} {
		if _, err := idempotency.ParseKey(value); !errors.Is(err, idempotency.ErrKey) {
			t.Fatalf("ParseKey(%q) error = %v", value, err)
		}
	}
}
