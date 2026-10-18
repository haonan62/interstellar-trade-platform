package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type pgStore struct {
	db *sql.DB
}

const pgTxTimeout = 30 * time.Second

// NewPGStore connects to PostgreSQL and applies pending migrations. schema is
// optional: when set, all objects live in (and the connection is pinned to)
// that schema, which is how tests get isolated databases.
func NewPGStore(ctx context.Context, dsn, schema string) (*Store, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if schema != "" {
		admin := stdlib.OpenDB(*cfg)
		_, err := admin.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteIdent(schema))
		_ = admin.Close()
		if err != nil {
			return nil, fmt.Errorf("create schema: %w", err)
		}
		if cfg.RuntimeParams == nil {
			cfg.RuntimeParams = map[string]string{}
		}
		cfg.RuntimeParams["search_path"] = schema
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	p := &pgStore{db: db}
	if err := p.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{pg: p}, nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// Ping checks that the backing store is reachable (readiness probe).
func (s *Store) Ping(ctx context.Context) error {
	if s.pg != nil {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return s.pg.db.PingContext(ctx)
	}
	return nil
}

// Close releases the connection pool (PG backend only).
func (s *Store) Close() error {
	if s.pg != nil {
		return s.pg.db.Close()
	}
	return nil
}

// dropSchema removes a test schema.
func (s *Store) dropSchema(ctx context.Context, schema string) error {
	_, err := s.pg.db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`)
	return err
}

func (p *pgStore) migrate(ctx context.Context) error {
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	// Serialise concurrent replicas starting at the same time.
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(727001)`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(727001)`) }()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("bad migration name %q", name)
		}
		var done bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// run executes fn against a state loaded for sc inside one transaction.
// Writes are persisted as a diff and committed atomically; any error rolls the
// whole request back (zero state change). Serialization failures and deadlocks
// are retried; with colony locks taken in sorted order they should not occur.
func (p *pgStore) run(sc Scope, write bool, fn func(*AppState) error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = p.once(sc, write, fn)
		if !retryablePG(err) {
			break
		}
		time.Sleep(time.Duration(5+rand.Intn(20)*(attempt+1)) * time.Millisecond) //nolint:gosec // jitter only
	}
	return mapPGError(err)
}

func (p *pgStore) once(sc Scope, write bool, fn func(*AppState) error) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), pgTxTimeout)
	defer cancel()
	opts := &sql.TxOptions{Isolation: sql.LevelReadCommitted}
	if !write {
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	tx, err := p.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if sc.Lock != "" {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, sc.Lock); err != nil {
			return err
		}
	}
	st, orig, err := p.load(ctx, tx, sc, write)
	if err != nil {
		return err
	}
	if err = fn(st); err != nil {
		return err
	}
	if write {
		if err = p.persist(ctx, tx, st, orig, sc); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func retryablePG(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "40001" || pe.Code == "40P01"
	}
	return false
}

// mapPGError turns constraint violations into the API's error types.
func mapPGError(err error) error {
	if err == nil {
		return nil
	}
	var app *AppError
	if errors.As(err, &app) {
		return err
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505":
			return conflict("already exists (" + pe.ConstraintName + ")")
		case "23514":
			return conflict("constraint violated (" + pe.ConstraintName + ")")
		case "23503":
			return conflict("referenced row missing (" + pe.ConstraintName + ")")
		}
	}
	return err
}

// dueTradeIDs lists unaccepted offers whose expiry has passed (index-backed).
func (p *pgStore) dueTradeIDs(now time.Time, limit int) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pgTxTimeout)
	defer cancel()
	rows, err := p.db.QueryContext(ctx, `SELECT id FROM trades WHERE status IN ($1, $2) AND expires_at < $3 ORDER BY expires_at LIMIT $4`,
		StatusOfferPendingExport, StatusOfferReceived, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
