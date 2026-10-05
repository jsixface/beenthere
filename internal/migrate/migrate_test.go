//go:build integration

package migrate

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BT_TEST_ADMIN_URL must point at a server where the test may create/drop a scratch database.
func TestApplyFreshAndIdempotent(t *testing.T) {
	admin := os.Getenv("BT_TEST_ADMIN_URL")
	if admin == "" {
		t.Skip("BT_TEST_ADMIN_URL not set")
	}
	ctx := context.Background()
	ap, err := pgxpool.New(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer ap.Close()
	_, _ = ap.Exec(ctx, `DROP DATABASE IF EXISTS bt_migrate_test`)
	if _, err := ap.Exec(ctx, `CREATE DATABASE bt_migrate_test`); err != nil {
		t.Fatal(err)
	}
	cfg := ap.Config().ConnConfig.Copy()
	cfg.Database = "bt_migrate_test"
	pc, _ := pgxpool.ParseConfig("")
	pc.ConnConfig = cfg
	p, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	created, err := Apply(ctx, p)
	if err != nil || !created {
		t.Fatalf("first apply: %v %v", created, err)
	}
	var tables int
	_ = p.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' AND table_name NOT IN ('spatial_ref_sys')`).Scan(&tables)
	if tables < 50 {
		t.Fatalf("only %d tables", tables)
	}
	var v string
	_ = p.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v)
	if v != SchemaVersion {
		t.Fatalf("version %s", v)
	}
	if created, err := Apply(ctx, p); err != nil || created {
		t.Fatalf("second apply must be a no-op: %v %v", created, err)
	}
}
