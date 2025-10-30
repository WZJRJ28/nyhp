package test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"brokerflow/test/actors"
	"brokerflow/test/chaos"
	"brokerflow/test/infra"
	"brokerflow/test/oracles"
)

var (
	flDuration    = flag.Duration("duration", 90*time.Second, "how long to run stress")
	flConcurrency = flag.Int("concurrency", 8, "number of concurrent actors")
	flSeed        = flag.Int64("seed", time.Now().UnixNano(), "random seed")
	flDSN         = flag.String("dsn", "", "existing Postgres DSN to reuse (avoids Docker)")
)

func seedRNG(seed int64) { rand.Seed(seed) }

func TestACNConcurrency(t *testing.T) {
	flag.Parse()
	seed := *flSeed
	seedRNG(seed)

	var (
		pgC        *infra.PGContainer
		dsn        string
		err        error
		usedShared bool
	)
	ctx, cancel := context.WithTimeout(context.Background(), *flDuration+60*time.Second)
	defer cancel()

	switch {
	case *flDSN != "":
		dsn = *flDSN
		usedShared = true
		pgC = &infra.PGContainer{}
	case os.Getenv("STRESS_TEST_PG_DSN") != "":
		dsn = os.Getenv("STRESS_TEST_PG_DSN")
		usedShared = true
		pgC = &infra.PGContainer{}
	default:
		if dockerAvailable(ctx) {
			pgC, dsn, err = infra.StartPostgres16(ctx, "")
			if err != nil {
				t.Fatalf("start postgres: %v", err)
			}
		} else {
			dsn, err = infra.InitLocalDatabase(ctx)
			if err != nil {
				t.Fatalf("init local database: %v", err)
			}
			pgC = &infra.PGContainer{}
		}
	}
	defer pgC.Terminate(context.Background())

	// migrations
	pool, teardown, err := infra.ApplyMigrations(ctx, dsn, usedShared)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	defer pool.Close()
	defer func() {
		if err := teardown(context.Background()); err != nil {
			t.Logf("teardown warning: %v", err)
		}
	}()

	// seed minimal data
	seedData := mustSeed(t, ctx, pool)

	// run actors
	g, ctx2 := errgroup.WithContext(ctx)
	stop := make(chan struct{})

	// creators and signers battling over same referral
	for i := 0; i < *flConcurrency; i++ {
		g.Go(func() error {
			return actors.Creator(ctx2, pool, seedData.referralID, seedData.fromBroker, seedData.toBroker, stop)
		})
		g.Go(func() error { return actors.Signer(ctx2, pool, seedData.referralID, stop) })
	}

	// pii reader
	g.Go(func() error { return actors.PIIReader(ctx2, pool, seedData.agreementID, seedData.userID, stop) })
	// event writer
	g.Go(func() error { return actors.EventWriter(ctx2, pool, seedData.agreementID, stop) })
	// outbox worker
	g.Go(func() error { return actors.OutboxWorker(ctx2, pool, stop) })
	// edge adapter
	g.Go(func() error {
		return actors.EdgeAdapter(ctx2, pool, fmt.Sprintf("edge-%s", seedData.agreementID), "/thirdparty/notify", stop)
	})
	// disputer
	g.Go(func() error { return actors.Disputer(ctx2, pool, seedData.agreementID, stop) })
	// chaos: kill random backend
	go chaos.TerminateRandomBackend(ctx2, pool, "", stop)

	// schedule oracle checks until duration reached
	deadline := time.Now().Add(*flDuration)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var failed bool
loop:
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			name, row, err := oracles.Run(ctx2, pool)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					break loop
				}
				t.Fatalf("oracle error: %v", err)
			}
			if name != "" {
				failed = true
				dumpRecent(t, ctx2, pool)
				t.Fatalf("Oracle %s failed. First row: %s (seed=%d)", name, row, seed)
			}
		}
	}

	close(stop)
	if err := g.Wait(); err != nil && !failed {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("actors errored: %v", err)
		}
	}
}

func dockerAvailable(ctx context.Context) bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	c := exec.CommandContext(ctx, "docker", "info")
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	return c.Run() == nil
}

type seedIDs struct {
	userID      string
	fromBroker  string
	toBroker    string
	referralID  string
	agreementID string
}

func mustSeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) seedIDs {
	t.Helper()
	var s seedIDs
	// user
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, full_name) VALUES ($1,$2) RETURNING id`, fmt.Sprintf("u%d@example.com", rand.Int63()), "Stress User").Scan(&s.userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// brokers
	if err := pool.QueryRow(ctx, `INSERT INTO brokers (name) VALUES ($1) RETURNING id`, fmt.Sprintf("From %d", rand.Int63())).Scan(&s.fromBroker); err != nil {
		t.Fatalf("seed broker from: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO brokers (name) VALUES ($1) RETURNING id`, fmt.Sprintf("To %d", rand.Int63())).Scan(&s.toBroker); err != nil {
		t.Fatalf("seed broker to: %v", err)
	}
	// referral
	if err := pool.QueryRow(ctx, `INSERT INTO referrals (created_by_user_id, status) VALUES ($1,'open') RETURNING id`, s.userID).Scan(&s.referralID); err != nil {
		t.Fatalf("seed referral: %v", err)
	}
	// initial pending_signature agreement
	if err := pool.QueryRow(ctx, `INSERT INTO agreements (referral_id, from_broker_id, to_broker_id, status) VALUES ($1,$2,$3,'pending_signature') RETURNING id`, s.referralID, s.fromBroker, s.toBroker).Scan(&s.agreementID); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	// attach a pii contact row to exercise gate after effective
	_, _ = pool.Exec(ctx, `INSERT INTO pii_contacts (agreement_id, client_name, client_email) VALUES ($1,'Alice','alice@example.com') ON CONFLICT DO NOTHING`, s.agreementID)
	// invoice to test dispute linkage
	_, _ = pool.Exec(ctx, `INSERT INTO invoices (agreement_id, amount, status) VALUES ($1, 100, 'open')`, s.agreementID)
	return s
}

func dumpRecent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	type dump struct {
		name string
		sql  string
	}
	dumps := []dump{
		{"timeline_events", `SELECT id, agreement_id, seq, type, ts FROM timeline_events ORDER BY id DESC LIMIT 50`},
		{"outbox", `SELECT id, topic, status, attempts, created_at FROM outbox ORDER BY created_at DESC LIMIT 50`},
		{"edge_invocations", `SELECT key, route, status, last_attempt_at FROM edge_invocations ORDER BY last_attempt_at DESC LIMIT 50`},
		{"audit_logs", `SELECT id, agreement_id, action, ts FROM audit_logs ORDER BY id DESC LIMIT 50`},
	}
	for _, d := range dumps {
		rows, err := pool.Query(ctx, d.sql)
		if err != nil {
			t.Logf("dump %s error: %v", d.name, err)
			continue
		}
		cols := rows.FieldDescriptions()
		t.Logf("-- %s --", d.name)
		for rows.Next() {
			vals, _ := rows.Values()
			// compact print
			buf := make([]any, 0, len(vals))
			for i := range vals {
				buf = append(buf, fmt.Sprintf("%s=%v", string(cols[i].Name), vals[i]))
			}
			t.Logf("%s", buf)
		}
		rows.Close()
	}
}
