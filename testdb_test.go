package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

var (
	testAdminDSN string
	testAdmin    *pgxpool.Pool
	testPools    sync.Map // test name -> *pgxpool.Pool
)

func TestMain(m *testing.M) {
	goose.SetLogger(log.New(io.Discard, "", 0))
	_ = loadDotEnv(".env")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		os.Stderr.WriteString("postgres is required for tests: set DATABASE_URL in .env (see .env.example), or set TEST_DATABASE_URL\n")
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		os.Stderr.WriteString("could not connect to test postgres: " + err.Error() + "\n")
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		os.Stderr.WriteString("could not ping test postgres: " + err.Error() + "\n")
		os.Exit(1)
	}
	testAdminDSN = dsn
	testAdmin = pool

	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testAdmin == nil {
		t.Fatal("postgres is not available; set DATABASE_URL in .env or TEST_DATABASE_URL")
	}
	if v, ok := testPools.Load(t.Name()); ok {
		return v.(*pgxpool.Pool)
	}

	var b [8]byte
	_, _ = rand.Read(b[:])
	schema := "t" + hex.EncodeToString(b[:])

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := testAdmin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("CREATE SCHEMA %s: %v", schema, err)
	}

	cfg, err := pgxpool.ParseConfig(testAdminDSN)
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+ident)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("schema pool: %v", err)
	}
	if err := migrateDB(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate test schema: %v", err)
	}

	testPools.Store(t.Name(), pool)
	t.Cleanup(func() {
		pool.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_, _ = testAdmin.Exec(dropCtx, "DROP SCHEMA IF EXISTS "+ident+" CASCADE")
		testPools.Delete(t.Name())
	})
	return pool
}

func testApp(t *testing.T, runner Runner) *App {
	t.Helper()
	if runner == nil {
		runner = newResticRunner()
	}
	app, err := newAppWithRunner(t.TempDir(), testPool(t), runner)
	if err != nil {
		t.Fatalf("newAppWithRunner: %v", err)
	}
	return app
}
