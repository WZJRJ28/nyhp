package agreement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestEsignCompletion_Integration connects to a real PostgreSQL via DATABASE_URL
// and verifies the end-to-end repository + service behavior including idempotency.
func TestEsignCompletion_Integration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is empty; set it to a live PostgreSQL to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect pool: %v", err)
	}
	defer pool.Close()

	// Ensure schema exists (migrations applied)
	if !tableExists(ctx, t, pool, "agreements") || !tableExists(ctx, t, pool, "timeline_events") || !tableExists(ctx, t, pool, "outbox") || !tableExists(ctx, t, pool, "idempotency") {
		t.Skip("database schema missing; run migrations: migrate -path migrations -database \"$DATABASE_URL\" up")
	}

	// Seed minimal data set required by foreign keys
	var (
		userID      string
		fromBroker  string
		toBroker    string
		referralID  string
		agreementID string
	)

	mustQueryRow := func(query string, args ...any) pgx.Row {
		return pool.QueryRow(ctx, query, args...)
	}

	// user
	if err := mustQueryRow(`INSERT INTO users (email, full_name) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("alex+%d@example.com", time.Now().UnixNano()), "Alex Agent").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// brokers
	if err := mustQueryRow(`INSERT INTO brokers (name) VALUES ($1) RETURNING id`,
		fmt.Sprintf("Manhattan Realty %d", time.Now().UnixNano())).Scan(&fromBroker); err != nil {
		t.Fatalf("seed from broker: %v", err)
	}
	if err := mustQueryRow(`INSERT INTO brokers (name) VALUES ($1) RETURNING id`,
		fmt.Sprintf("Brooklyn Realty %d", time.Now().UnixNano())).Scan(&toBroker); err != nil {
		t.Fatalf("seed to broker: %v", err)
	}

	// referral
	if err := mustQueryRow(`INSERT INTO referrals (created_by_user_id, status) VALUES ($1, 'open') RETURNING id`, userID).Scan(&referralID); err != nil {
		t.Fatalf("seed referral: %v", err)
	}

	// agreement in pending_signature state
	if err := mustQueryRow(`
        INSERT INTO agreements (referral_id, from_broker_id, to_broker_id, status)
        VALUES ($1, $2, $3, 'pending_signature') RETURNING id
    `, referralID, fromBroker, toBroker).Scan(&agreementID); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}

	// Cleanup seeded rows after test (best-effort, ignore errors)
	t.Cleanup(func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		pool.Exec(ctx2, `DELETE FROM timeline_events WHERE agreement_id = $1`, agreementID)
		pool.Exec(ctx2, `DELETE FROM outbox WHERE payload->>'agreement_id' = $1`, agreementID)
		pool.Exec(ctx2, `DELETE FROM agreements WHERE id = $1`, agreementID)
		pool.Exec(ctx2, `DELETE FROM referrals WHERE id = $1`, referralID)
		pool.Exec(ctx2, `DELETE FROM brokers WHERE id IN ($1, $2)`, fromBroker, toBroker)
		pool.Exec(ctx2, `DELETE FROM users WHERE id = $1`, userID)
		// idempotency rows are keyed by unique test key; remove below after usage.
	})

	repo := NewRepository()
	svc := NewService(pool, repo)

	idemKey := fmt.Sprintf("itest-esign-%d", time.Now().UnixNano())
	actor := userID
	req := EsignCompletionRequest{
		AgreementID:     agreementID,
		IdempotencyKey:  idemKey,
		ActorID:         &actor,
		TimelinePayload: map[string]any{"test": "integration"},
		OutboxTopic:     "", // default to agreement.effective
		OutboxPayload:   map[string]any{"source": "go-test"},
	}

	// First invocation should perform writes and commit
	if err := svc.HandleEsignCompletionWebhook(ctx, req); err != nil {
		t.Fatalf("handle webhook (first): %v", err)
	}

	// Verify agreement is effective and eff_time set
	var status string
	var effTime *time.Time
	if err := mustQueryRow(`SELECT status, eff_time FROM agreements WHERE id = $1`, agreementID).Scan(&status, &effTime); err != nil {
		t.Fatalf("verify agreement: %v", err)
	}
	if status != "effective" {
		t.Fatalf("expected agreement status 'effective', got %q", status)
	}
	if effTime == nil || effTime.IsZero() {
		t.Fatalf("expected eff_time to be set")
	}

	// Verify one timeline event ESIGN_COMPLETED with seq=1
	var (
		evCount int
		evType  string
		evSeq   int
	)
	if err := mustQueryRow(`SELECT COUNT(*), MIN(type)::text, MIN(seq) FROM timeline_events WHERE agreement_id = $1`, agreementID).Scan(&evCount, &evType, &evSeq); err != nil {
		t.Fatalf("verify events: %v", err)
	}
	if evCount != 1 || evType != "ESIGN_COMPLETED" || evSeq != 1 {
		t.Fatalf("unexpected timeline events state: count=%d type=%s seq=%d", evCount, evType, evSeq)
	}

	// Verify one outbox message for agreement.effective
	var outCount int
	if err := mustQueryRow(`SELECT COUNT(*) FROM outbox WHERE topic = 'agreement.effective' AND payload->>'agreement_id' = $1`, agreementID).Scan(&outCount); err != nil {
		t.Fatalf("verify outbox: %v", err)
	}
	if outCount != 1 {
		t.Fatalf("expected 1 outbox message, got %d", outCount)
	}

	// Second invocation with the same idempotency key should be a no-op and not error
	if err := svc.HandleEsignCompletionWebhook(ctx, req); err != nil {
		t.Fatalf("handle webhook (second, idempotent): %v", err)
	}

	// Verify counts unchanged
	if err := mustQueryRow(`SELECT COUNT(*) FROM timeline_events WHERE agreement_id = $1`, agreementID).Scan(&evCount); err != nil {
		t.Fatalf("re-verify events: %v", err)
	}
	if evCount != 1 {
		t.Fatalf("expected timeline events to remain 1 after idempotent replay, got %d", evCount)
	}
	if err := mustQueryRow(`SELECT COUNT(*) FROM outbox WHERE topic = 'agreement.effective' AND payload->>'agreement_id' = $1`, agreementID).Scan(&outCount); err != nil {
		t.Fatalf("re-verify outbox: %v", err)
	}
	if outCount != 1 {
		t.Fatalf("expected outbox messages to remain 1 after idempotent replay, got %d", outCount)
	}

	// Cleanup idempotency key explicitly to avoid buildup
	_, _ = pool.Exec(ctx, `DELETE FROM idempotency WHERE key = $1`, idemKey)
}

func tableExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return exists
}
