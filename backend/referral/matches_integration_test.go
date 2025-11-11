package referral

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"brokerflow/agreement"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMatchAcceptanceCreatesAgreement(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect pool: %v", err)
	}
	defer pool.Close()

	requiredTables := []string{
		"users",
		"brokers",
		"referral_requests",
		"referral_matches",
		"agreements",
		"timeline_events",
		"outbox",
	}
	for _, tbl := range requiredTables {
		if !tableExists(ctx, pool, tbl) {
			t.Skipf("table %s does not exist; ensure migrations are applied", tbl)
		}
	}

	var (
		ownerBroker     string
		candidateBroker string
		ownerUser       string
		candidateUser   string
		requestID       string
		matchID         string
	)

	mustInsert := func(query string, args ...any) string {
		var id string
		if err := pool.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatalf("seed statement failed: %v", err)
		}
		return id
	}

	makeFein := func(prefix string) string {
		return fmt.Sprintf("%s-%07d", prefix, time.Now().UnixNano()%10000000)
	}

	ownerBroker = mustInsert(`INSERT INTO brokers (name, fein, verified) VALUES ($1, $2, $3) RETURNING id`,
		fmt.Sprintf("Owner Co %d", time.Now().UnixNano()), makeFein("33"), true)
	candidateBroker = mustInsert(`INSERT INTO brokers (name, fein, verified) VALUES ($1, $2, $3) RETURNING id`,
		fmt.Sprintf("Candidate Co %d", time.Now().UnixNano()), makeFein("44"), true)

	ownerUser = mustInsert(`INSERT INTO users (email, full_name, broker_id) VALUES ($1, $2, $3) RETURNING id`,
		fmt.Sprintf("owner+%d@example.com", time.Now().UnixNano()), "Owner Agent", ownerBroker)
	candidateUser = mustInsert(`INSERT INTO users (email, full_name, broker_id) VALUES ($1, $2, $3) RETURNING id`,
		fmt.Sprintf("candidate+%d@example.com", time.Now().UnixNano()), "Candidate Agent", candidateBroker)

	requestID = mustInsert(`
        INSERT INTO referral_requests (created_by_user_id, region, price_min, price_max, property_type, deal_type, languages, sla_hours, status)
        VALUES ($1, ARRAY['us-ea'], 200000, 300000, 'condo', 'buy', ARRAY['English'], 48, 'open')
        RETURNING id
    `, ownerUser)

	matchID = mustInsert(`
        INSERT INTO referral_matches (request_id, candidate_user_id, state, score)
        VALUES ($1, $2, 'invited', 0.82)
        RETURNING id
    `, requestID, candidateUser)

	t.Cleanup(func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		pool.Exec(ctx2, `DELETE FROM outbox WHERE payload->>'referral_id' = $1`, requestID)
		pool.Exec(ctx2, `DELETE FROM timeline_events WHERE payload->>'match_id' = $1`, matchID)
		pool.Exec(ctx2, `DELETE FROM agreements WHERE referral_id = $1`, requestID)
		pool.Exec(ctx2, `DELETE FROM referral_matches WHERE id = $1`, matchID)
		pool.Exec(ctx2, `DELETE FROM referral_requests WHERE id = $1`, requestID)
		pool.Exec(ctx2, `DELETE FROM users WHERE id IN ($1, $2)`, ownerUser, candidateUser)
		pool.Exec(ctx2, `DELETE FROM brokers WHERE id IN ($1, $2)`, ownerBroker, candidateBroker)
	})

	matchRepo := NewMatchRepository(pool)
	service := NewMatchService(matchRepo).WithAgreementRepository(agreement.NewRepository())

	result, err := service.UpdateState(ctx, UpdateMatchParams{
		MatchID:     matchID,
		CandidateID: candidateUser,
		NewState:    MatchStateAccepted,
		Pool:        pool,
	})
	if err != nil {
		t.Fatalf("accept match: %v", err)
	}
	if result.Match.State != MatchStateAccepted {
		t.Fatalf("expected match state accepted, got %s", result.Match.State)
	}
	if result.Agreement == nil {
		t.Fatalf("expected agreement to be created")
	}

	agreementID := result.Agreement.ID

	var status string
	var fromBroker string
	var toBroker string
	if err := pool.QueryRow(ctx, `SELECT status, from_broker_id, to_broker_id FROM agreements WHERE id = $1`, agreementID).Scan(&status, &fromBroker, &toBroker); err != nil {
		t.Fatalf("inspect agreement: %v", err)
	}
	if status != "pending_signature" {
		t.Fatalf("expected status pending_signature, got %s", status)
	}
	if fromBroker != ownerBroker || toBroker != candidateBroker {
		t.Fatalf("unexpected broker linkage: from=%s to=%s", fromBroker, toBroker)
	}

	var timelineCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM timeline_events WHERE agreement_id = $1 AND type = 'AGREEMENT_CREATED'`, agreementID).Scan(&timelineCount); err != nil {
		t.Fatalf("count timeline events: %v", err)
	}
	if timelineCount != 1 {
		t.Fatalf("expected exactly one AGREEMENT_CREATED event, got %d", timelineCount)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'agreement.created' AND payload->>'agreement_id' = $1`, agreementID).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox messages: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected one outbox message, got %d", outboxCount)
	}

	// Idempotent replay
	result, err = service.UpdateState(ctx, UpdateMatchParams{
		MatchID:     matchID,
		CandidateID: candidateUser,
		NewState:    MatchStateAccepted,
		Pool:        pool,
	})
	if err != nil {
		t.Fatalf("idempotent accept: %v", err)
	}
	if result.Match.State != MatchStateAccepted {
		t.Fatalf("expected accepted state after idempotent replay, got %s", result.Match.State)
	}
	if result.Agreement == nil || result.Agreement.ID != agreementID {
		t.Fatalf("expected same agreement on idempotent replay")
	}
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, name string) bool {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, name).Scan(&exists); err != nil {
		return false
	}
	return exists
}
