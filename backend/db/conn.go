package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool constructs a pgx connection pool using the provided connection string.
func NewPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	if connString == "" {
		return nil, fmt.Errorf("db: empty connection string")
	}

	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}

	return pgxpool.NewWithConfig(ctx, cfg)
}
