package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/runtime/realtime"
)

func (store *Store) ListenForRealtime(parent context.Context, url string, registry *realtime.Registry) (func(), error) {
	config := store.pool.Config().ConnConfig.Copy()
	if url != "" {
		var err error
		config, err = pgx.ParseConfig(url)
		if err != nil {
			return nil, fmt.Errorf("invalid realtime listener database URL")
		}
	}
	config.ConnectTimeout = 5 * time.Second
	config.RuntimeParams["application_name"] = "tacit-api-realtime-listener"
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			listenForChanges(ctx, config, registry)
			if ctx.Err() != nil {
				return
			}
			slog.Warn("realtime database listener unavailable")
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}, nil
}

func listenForChanges(ctx context.Context, config *pgx.ConnConfig, registry *realtime.Registry) {
	connect, cancel := context.WithTimeout(ctx, 5*time.Second)
	conn, err := pgx.ConnectConfig(connect, config)
	if err != nil {
		cancel()
		return
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = conn.Close(closeContext)
	}()
	_, err = conn.Exec(connect, `LISTEN tacit_realtime_v1`)
	cancel()
	if err != nil {
		return
	}
	for {
		change, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() == nil {
				registry.ListenerLost()
			}
			return
		}
		org, err := access.ParseOrganizationID("org_" + strings.ReplaceAll(change.Payload, "-", ""))
		if err == nil {
			registry.Notify(org)
		}
	}
}
