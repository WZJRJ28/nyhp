package infra

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Harness owns the lifecycle of the Postgres test container and pgx pool.
type Harness struct {
    container *postgres.PostgresContainer
    pool      *pgxpool.Pool
    dsn       string
}

// NewHarness boots a Postgres 16 container and applies embedded migrations.
func NewHarness(ctx context.Context) (*Harness, error) {
    pgContainer, err := postgres.RunContainer(ctx,
        postgres.WithImage("postgres:16-alpine"),
        postgres.WithDatabase("acn"),
        postgres.WithUsername("acn"),
        postgres.WithPassword("acn"),
    )
    if err != nil {
        return nil, fmt.Errorf("start postgres container: %w", err)
    }

    dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    if err != nil {
        pgContainer.Terminate(ctx)
        return nil, fmt.Errorf("resolve connection string: %w", err)
    }

    cfg, err := pgxpool.ParseConfig(dsn)
    if err != nil {
        pgContainer.Terminate(ctx)
        return nil, fmt.Errorf("parse pgx config: %w", err)
    }
    cfg.MaxConns = 64
    cfg.MaxConnIdleTime = 30 * time.Second
    cfg.MaxConnLifetime = 5 * time.Minute

    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        pgContainer.Terminate(ctx)
        return nil, fmt.Errorf("create pool: %w", err)
    }

    h := &Harness{
        container: pgContainer,
        pool:      pool,
        dsn:       dsn,
    }

    if err := h.applyMigrations(ctx); err != nil {
        h.Close(ctx)
        return nil, err
    }

    return h, nil
}

// Pool exposes the configured pgx pool.
func (h *Harness) Pool() *pgxpool.Pool {
    return h.pool
}

// DSN returns the connection string for direct connections (e.g., chaos).
func (h *Harness) DSN() string {
    return h.dsn
}

// Close tears down resources.
func (h *Harness) Close(ctx context.Context) {
    if h.pool != nil {
        h.pool.Close()
    }
    if h.container != nil {
        _ = h.container.Terminate(ctx)
    }
}

func (h *Harness) applyMigrations(ctx context.Context) error {
    conn, err := h.pool.Acquire(ctx)
    if err != nil {
        return fmt.Errorf("acquire conn: %w", err)
    }
    defer conn.Release()

    sql := strings.TrimSpace(MigrationsAll)
    if sql == "" {
        return fmt.Errorf("no migrations to apply")
    }

    pgConn := conn.Conn().PgConn()
    res := pgConn.Exec(ctx, sql)
    if _, err := res.ReadAll(); err != nil {
        return fmt.Errorf("apply migrations: %w", err)
    }

    return nil
}

// Reset truncates mutable tables to provide a clean slate for next epoch.
func (h *Harness) Reset(ctx context.Context) error {
    tables := []string{
        "audit_logs",
        "timeline_events",
        "outbox",
        "edge_invocations",
        "disputes",
        "invoices",
        "pii_contacts",
        "agreements",
        "referral_matches",
        "referral_requests",
        "broker_memberships",
        "agent_licenses",
        "brokers",
        "users",
    }

    tx, err := h.pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("reset begin: %w", err)
    }
    defer tx.Rollback(ctx)

    for _, tbl := range tables {
        if _, err := tx.Exec(ctx, "TRUNCATE TABLE "+tbl+" CASCADE"); err != nil {
            return fmt.Errorf("truncate %s: %w", tbl, err)
        }
    }

    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("reset commit: %w", err)
    }

    return nil
}
