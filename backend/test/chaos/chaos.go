package chaos

import (
	"context"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Randomly terminates a backend connection belonging to our test application.
func TerminateRandomBackend(ctx context.Context, pool *pgxpool.Pool, appLike string, stop <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			if rand.Intn(5) == 0 {
				// terminate some backend of this DB (heuristic: random active backend not our own PID)
				_, _ = pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid() ORDER BY random() LIMIT 1`)
			}
		}
	}
}
