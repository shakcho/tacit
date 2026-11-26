package realtime

import (
	"context"
	"errors"

	"github.com/shakcho/tacit/internal/access"
)

const (
	PageSize  = 32
	MaxReplay = 512
)

var ErrResync = errors.New("realtime snapshot required")

type ChangeKind string

const (
	Invalidate ChangeKind = "invalidate"
	ScopeReset ChangeKind = "scope_reset"
)

const SessionResource = "authentication_session"

type Change struct {
	Point
	Kind     ChangeKind
	Resource string
	Audience access.UserID
	Revision int64
}

type Page struct {
	Head    int64
	Changes []Change
}

type Snapshot struct {
	Principal access.LocalPrincipal
	Point     Point
}

type Feed interface {
	SessionSnapshot(context.Context, access.LocalPrincipal) (Snapshot, error)
	ResolveCursor(context.Context, access.OrganizationID, EventID) (Point, error)
	Replay(context.Context, access.OrganizationID, Point) (Page, error)
	// Authorize holds the same locks used by canonical writers through delivery.
	Authorize(context.Context, access.LocalPrincipal, func() error) error
}
