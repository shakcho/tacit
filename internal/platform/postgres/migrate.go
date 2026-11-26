package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const CurrentSchemaVersion int64 = 4

var (
	ErrMigrationChecksum = errors.New("migration checksum changed")
	ErrSchemaVersion     = errors.New("unsupported schema version")
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		parts := strings.SplitN(strings.TrimSuffix(entry.Name(), ".sql"), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration name %q", entry.Name())
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version %q", entry.Name())
		}
		contents, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}
		checksum := sha256.Sum256(contents)
		migrations = append(migrations, migration{
			Version: version, Name: parts[1], SQL: string(contents), Checksum: hex.EncodeToString(checksum[:]),
		})
	}
	sort.Slice(migrations, func(left, right int) bool { return migrations[left].Version < migrations[right].Version })
	for index, migration := range migrations {
		if migration.Version != int64(index+1) {
			return nil, fmt.Errorf("migration sequence has version %d at position %d", migration.Version, index+1)
		}
	}
	if len(migrations) == 0 || migrations[len(migrations)-1].Version != CurrentSchemaVersion {
		return nil, fmt.Errorf("migration inventory does not end at version %d", CurrentSchemaVersion)
	}
	return migrations, nil
}

func Migrate(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return errors.New("database URL is required")
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect for migration: %w", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()

	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tacit-schema-migrations', 0))`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS tacit_schema_migrations (
            version bigint PRIMARY KEY CHECK (version > 0),
            name text NOT NULL,
            checksum text NOT NULL CHECK (length(checksum) = 64),
            applied_at timestamptz NOT NULL DEFAULT now()
        )`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	applied, err := appliedMigrations(ctx, transaction)
	if err != nil {
		return err
	}
	for version := range applied {
		if version < 1 || version > int64(len(migrations)) {
			return fmt.Errorf("%w: unknown migration %d", ErrSchemaVersion, version)
		}
	}
	for _, migration := range migrations {
		if checksum, ok := applied[migration.Version]; ok {
			if checksum != migration.Checksum {
				return fmt.Errorf("%w: version %d", ErrMigrationChecksum, migration.Version)
			}
			continue
		}
		if _, err := transaction.Exec(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %d: %w", migration.Version, err)
		}
		if _, err := transaction.Exec(ctx,
			`INSERT INTO tacit_schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
			migration.Version, migration.Name, migration.Checksum,
		); err != nil {
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}
	}
	if err := rejectFutureSchema(ctx, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func appliedMigrations(ctx context.Context, transaction pgx.Tx) (map[int64]string, error) {
	rows, err := transaction.Query(ctx, `SELECT version, checksum FROM tacit_schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()
	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan migration ledger: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	return applied, nil
}

func rejectFutureSchema(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	var version int64
	if err := query.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM tacit_schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version != CurrentSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrSchemaVersion, version, CurrentSchemaVersion)
	}
	return nil
}
