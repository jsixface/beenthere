// Package migrate creates the Dawarich schema in an empty database so Beenthere
// can run without a Rails install. Databases that already have a users table
// (an existing Dawarich install) are never modified.
package migrate

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/0001_dawarich_schema.sql
var initialSchema string

// SchemaVersion is the Rails schema version the bundled SQL corresponds to.
const SchemaVersion = "20260923180000"

// Apply installs the schema when the database is empty. It reports whether it
// created anything.
func Apply(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = 'users')`).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Serialize concurrent first starts.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7243001)`); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, initialSchema); err != nil {
		return false, fmt.Errorf("apply schema: %w", err)
	}
	return true, tx.Commit(ctx)
}
