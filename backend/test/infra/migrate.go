package infra

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	coreMigrationsDir string
	testMigrationsDir string
)

func init() {
	if _, file, _, ok := runtime.Caller(0); ok {
		base := filepath.Dir(file)
		coreMigrationsDir = filepath.Join(base, "..", "..", "migrations")
		testMigrationsDir = filepath.Join(base, "..", "migrations")
	}
}

// ApplyMigrations executes SQL files from the migrations folders against the DSN.
// When isolate is true, a per-run schema is created and dropped via the returned teardown func.
func ApplyMigrations(ctx context.Context, dsn string, isolate bool) (*pgxpool.Pool, func(context.Context) error, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("parse pool config: %w", err)
	}

	cleanup := func(context.Context) error { return nil }

	if isolate {
		schema := fmt.Sprintf("stress_run_%d", time.Now().UnixNano())
		ident := pgx.Identifier{schema}.Sanitize()

		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("connect for schema: %w", err)
		}
		if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", ident)); err != nil {
			conn.Close(ctx)
			return nil, nil, fmt.Errorf("create schema %s: %w", schema, err)
		}
		conn.Close(ctx)

		setPath := fmt.Sprintf("SET search_path TO %s", ident)
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, setPath)
			return err
		}

		cleanup = func(ctx context.Context) error {
			dropConn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				return err
			}
			defer dropConn.Close(ctx)
			_, err = dropConn.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", ident))
			return err
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect pool: %w", err)
	}

	if err := execDir(ctx, pool, coreMigrationsDir); err != nil {
		pool.Close()
		return nil, nil, err
	}
	if err := execDir(ctx, pool, testMigrationsDir); err != nil {
		pool.Close()
		return nil, nil, err
	}

	return pool, cleanup, nil
}

func execDir(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("apply %s: %w", e.Name(), err)
		}
	}

	return nil
}
