package infra

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/jackc/pgx/v5"
)

// InitLocalDatabase checks if PostgreSQL is running and initializes the test database
func InitLocalDatabase(ctx context.Context) (string, error) {
	// Check if PostgreSQL is running
	if !isPostgresRunning() {
		return "", fmt.Errorf("PostgreSQL is not running")
	}

	adminDSNs := []string{
		"postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable",
		"postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable",
		fmt.Sprintf("postgres://%s@127.0.0.1:5432/postgres?sslmode=disable", os.Getenv("USER")),
		fmt.Sprintf("postgres://%s:postgres@127.0.0.1:5432/postgres?sslmode=disable", os.Getenv("USER")),
	}

	var adminConn *pgx.Conn
	var err error
	for _, dsn := range adminDSNs {
		adminConn, err = pgx.Connect(ctx, dsn)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to connect to postgres database: %w", err)
	}
	defer adminConn.Close(ctx)

	// Ensure test role exists
	if _, err := adminConn.Exec(ctx, "DO $$ BEGIN CREATE ROLE testuser WITH LOGIN PASSWORD 'pass'; EXCEPTION WHEN duplicate_object THEN NULL; END $$;"); err != nil {
		return "", fmt.Errorf("failed to create test role: %w", err)
	}

	// Drop lingering connections then recreate database fresh for each run
	_, _ = adminConn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'acn_stress' AND pid <> pg_backend_pid()")
	if _, err := adminConn.Exec(ctx, "DROP DATABASE IF EXISTS acn_stress"); err != nil {
		return "", fmt.Errorf("failed to drop existing database: %w", err)
	}

	createOwner := fmt.Sprintf("CREATE DATABASE acn_stress OWNER %s", pgx.Identifier{"testuser"}.Sanitize())
	if _, err := adminConn.Exec(ctx, createOwner); err != nil {
		return "", fmt.Errorf("failed to create test database: %w", err)
	}

	if _, err := adminConn.Exec(ctx, "GRANT ALL PRIVILEGES ON DATABASE acn_stress TO testuser"); err != nil {
		return "", fmt.Errorf("failed to grant privileges: %w", err)
	}

	return "postgres://testuser:pass@127.0.0.1:5432/acn_stress?sslmode=disable", nil
}

func isPostgresRunning() bool {
	// Try to connect to PostgreSQL
	cmd := exec.Command("pg_isready", "-h", "127.0.0.1", "-p", "5432")
	err := cmd.Run()
	return err == nil
}
