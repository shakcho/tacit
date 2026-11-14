package buildinfo

import "testing"

func TestNewPreservesRevision(t *testing.T) {
	t.Parallel()

	got := New("abc123")
	if got.Revision != "abc123" {
		t.Fatalf("Revision = %q, want abc123", got.Revision)
	}
}
