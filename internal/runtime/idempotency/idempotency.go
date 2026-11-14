package idempotency

import (
	"errors"
	"strings"
)

var ErrKey = errors.New("invalid idempotency key")

type Key struct{ value string }

func ParseKey(value string) (Key, error) {
	if len(value) < 1 || len(value) > 128 {
		return Key{}, ErrKey
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~", char)) {
			return Key{}, ErrKey
		}
	}
	return Key{value: value}, nil
}

func (key Key) String() string { return key.value }
