package shellsample_test

import (
	"strings"
	"testing"

	"github.com/shakcho/tacit/internal/runtime/shellsample"
)

func TestID(t *testing.T) {
	valid := "smp_0123456789abcdef0123456789abcdef"
	id, err := shellsample.ParseID(valid)
	if err != nil || id.String() != valid {
		t.Fatalf("ID round trip = %q, %v", id, err)
	}
	for _, value := range []string{"", "smp_0123", strings.ToUpper(valid), "smp_0123456789abcdef0123456789abcdeg", " " + valid} {
		if _, err := shellsample.ParseID(value); err != shellsample.ErrID {
			t.Errorf("ParseID(%q) error = %v", value, err)
		}
	}
}

func TestMediaTime(t *testing.T) {
	for _, value := range []string{"0", "1", "1234567", "9223372036854775807"} {
		offset, err := shellsample.ParseMediaTime(value)
		if err != nil || offset.String() != value {
			t.Errorf("ParseMediaTime(%q) = %v, %v", value, offset, err)
		}
	}
	for _, value := range []string{"", "-1", "+1", "01", "-0", "1.5", "1e3", " 1", "9223372036854775808"} {
		if _, err := shellsample.ParseMediaTime(value); err != shellsample.ErrMediaTime {
			t.Errorf("ParseMediaTime(%q) error = %v", value, err)
		}
	}
	offset, _ := shellsample.ParseMediaTime("1234567")
	if offset.Microseconds() != 1234567 {
		t.Fatal("media time lost microsecond precision")
	}
}

func TestIdempotencyKey(t *testing.T) {
	for _, value := range []string{"a", "Request-42._~", strings.Repeat("a", 128)} {
		if _, err := shellsample.ParseIdempotencyKey(value); err != nil {
			t.Errorf("ParseIdempotencyKey(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "has space", "has,comma", "line\nbreak", "secret:token", "☃", strings.Repeat("a", 129)} {
		if _, err := shellsample.ParseIdempotencyKey(value); err != shellsample.ErrIdempotencyKey {
			t.Errorf("ParseIdempotencyKey(%q) error = %v", value, err)
		}
	}
}

func TestConstructibleResults(t *testing.T) {
	sample := shellsample.Sample{}
	for _, result := range []shellsample.CreateResult{shellsample.NewCreated(sample), shellsample.NewReplayed(sample)} {
		if got, ok := result.Sample(); !ok || got != sample {
			t.Fatalf("successful result lost sample: %v, %v", got, ok)
		}
	}
	for _, result := range []shellsample.CreateResult{{}, shellsample.NewUnavailable(), shellsample.NewSampleConflict(), shellsample.NewIdempotencyConflict()} {
		if _, ok := result.Sample(); ok {
			t.Fatal("refusal exposed a sample")
		}
	}
}
