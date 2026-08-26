package core

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const pgUniqueViolation = "23505"
const pgForeignKeyViolation = "23503"

var migrateMu sync.Mutex

// ResolveDSN returns the Postgres connection string: -database if set, else
// DATABASE_URL. Empty means the process must refuse to start.
func ResolveDSN(flagValue string) string {
	if s := strings.TrimSpace(flagValue); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}

func MissingDSNMessage() string {
	return "a Postgres database is required: set DATABASE_URL in .env (see .env.example), or pass -database"
}

// OpenDB connects to Postgres, pings it, and applies embedded migrations.
func OpenDB(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("could not parse database URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("could not connect to database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("could not reach database: %w", err)
	}
	if err := migrateDB(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("could not apply database migrations: %w", err)
	}
	return pool, nil
}

func migrateDB(ctx context.Context, pool *pgxpool.Pool) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	goose.SetLogger(log.New(io.Discard, "", 0))
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

func isFKViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation
}

func dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}
