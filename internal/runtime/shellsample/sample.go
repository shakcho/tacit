package shellsample

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

var (
	ErrID             = errors.New("invalid sample ID")
	ErrMediaTime      = errors.New("invalid media time")
	ErrIdempotencyKey = errors.New("invalid idempotency key")
)

type ID [16]byte

func ParseID(value string) (ID, error) {
	var id ID
	if len(value) != 36 || !strings.HasPrefix(value, "smp_") {
		return id, ErrID
	}
	for _, char := range value[4:] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return id, ErrID
		}
	}
	_, err := hex.Decode(id[:], []byte(value[4:]))
	return id, err
}

func (id ID) String() string { return "smp_" + hex.EncodeToString(id[:]) }

// MediaTime is an offset from the beginning of a recording, in microseconds.
type MediaTime struct{ microseconds int64 }

func ParseMediaTime(value string) (MediaTime, error) {
	microseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || microseconds < 0 || strconv.FormatInt(microseconds, 10) != value {
		return MediaTime{}, ErrMediaTime
	}
	return MediaTime{microseconds: microseconds}, nil
}

func (offset MediaTime) Microseconds() int64 { return offset.microseconds }
func (offset MediaTime) String() string      { return strconv.FormatInt(offset.microseconds, 10) }

type IdempotencyKey struct{ value string }

func ParseIdempotencyKey(value string) (IdempotencyKey, error) {
	if len(value) < 1 || len(value) > 128 {
		return IdempotencyKey{}, ErrIdempotencyKey
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~", char)) {
			return IdempotencyKey{}, ErrIdempotencyKey
		}
	}
	return IdempotencyKey{value: value}, nil
}

type Sample struct {
	ID     ID
	Offset MediaTime
}

type CreateCommand struct {
	ID     ID
	Offset MediaTime
	Key    IdempotencyKey
}

type Outcome uint8

const (
	Unavailable Outcome = iota
	Created
	Replayed
	IdempotencyConflict
	SampleConflict
)

// The zero result is unavailable, so every constructible result has an outcome.
type CreateResult struct {
	outcome Outcome
	sample  Sample
}

func NewCreated(sample Sample) CreateResult {
	return CreateResult{outcome: Created, sample: sample}
}

func NewReplayed(sample Sample) CreateResult {
	return CreateResult{outcome: Replayed, sample: sample}
}

func NewIdempotencyConflict() CreateResult {
	return CreateResult{outcome: IdempotencyConflict}
}

func NewSampleConflict() CreateResult        { return CreateResult{outcome: SampleConflict} }
func NewUnavailable() CreateResult           { return CreateResult{} }
func (result CreateResult) Outcome() Outcome { return result.outcome }

func (result CreateResult) Sample() (Sample, bool) {
	return result.sample, result.outcome == Created || result.outcome == Replayed
}

type Samples interface {
	Create(context.Context, CreateCommand) (CreateResult, error)
	Find(context.Context, ID) (Sample, bool, error)
}
