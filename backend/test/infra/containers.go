package infra

import (
	"context"
	"os"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type PGContainer struct {
	C *postgres.PostgresContainer
}

// StartPostgres16 starts a Postgres 16 container and returns a DSN. If overrideDSN or
// STRESS_TEST_PG_DSN is set, it reuses that database.
func StartPostgres16(ctx context.Context, overrideDSN string) (*PGContainer, string, error) {
	if overrideDSN != "" {
		return &PGContainer{}, overrideDSN, nil
	}
	if dsn := os.Getenv("STRESS_TEST_PG_DSN"); dsn != "" {
		return &PGContainer{}, dsn, nil
	}

	pw := "testpass"
	db := "testdb"
	user := "testuser"

	pgC, err := postgres.Run(ctx,
		"postgres:16",
		postgres.WithDatabase(db),
		postgres.WithUsername(user),
		postgres.WithPassword(pw),
	)
	if err != nil {
		return nil, "", err
	}

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pgC.Terminate(ctx)
		return nil, "", err
	}
	return &PGContainer{C: pgC}, dsn, nil
}

func (p *PGContainer) Terminate(ctx context.Context) error {
	if p == nil || p.C == nil {
		return nil
	}
	return p.C.Terminate(ctx)
}
