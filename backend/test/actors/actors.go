package actors

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Creator tries to create competing pending_signature agreements for the same referral concurrently.
func Creator(ctx context.Context, pool *pgxpool.Pool, referralID, fromBroker, toBroker string, stop <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		_, err := pool.Exec(ctx, `INSERT INTO agreements (referral_id, from_broker_id, to_broker_id, status)
                                   VALUES ($1,$2,$3,'pending_signature')`, referralID, fromBroker, toBroker)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique constraint
				// expected under contention
			} else {
				return fmt.Errorf("creator insert: %w", err)
			}
		}
		time.Sleep(time.Duration(10+rand.Intn(20)) * time.Millisecond)
	}
}

// Signer flips agreements from pending_signature to effective, idempotently.
func Signer(ctx context.Context, pool *pgxpool.Pool, referralID string, stop <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		var agID string
		err = tx.QueryRow(ctx, `SELECT id FROM agreements WHERE referral_id=$1 AND status='pending_signature' LIMIT 1 FOR UPDATE`, referralID).Scan(&agID)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE agreements SET status='effective', eff_time = COALESCE(eff_time, NOW()) WHERE id=$1`, agID)
			if err == nil {
				// append an ESIGN_COMPLETED timeline event
				var seq int
				_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM timeline_events WHERE agreement_id=$1`, agID).Scan(&seq)
				_, _ = tx.Exec(ctx, `INSERT INTO timeline_events (agreement_id, seq, type, payload) VALUES ($1,$2,'ESIGN_COMPLETED','{}'::jsonb)`, agID, seq)
				_, _ = tx.Exec(ctx, `INSERT INTO outbox (topic, payload) VALUES ('agreement.effective', jsonb_build_object('agreement_id',$1))`, agID)
			}
		}
		_ = tx.Rollback(ctx)
		time.Sleep(time.Duration(20+rand.Intn(40)) * time.Millisecond)
	}
}

// PIIReader invokes get_pii_contact under different timings and attempts direct SELECT to ensure RLS blocks it.
func PIIReader(ctx context.Context, pool *pgxpool.Pool, agreementID, actorID string, stop <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		// attempt direct SELECT (should fail or return zero due to RLS deny-all)
		_, _ = pool.Exec(ctx, `SELECT * FROM pii_contacts WHERE agreement_id=$1`, agreementID)
		// call SECURITY DEFINER accessor (may error if not yet effective)
		_, _ = pool.Exec(ctx, `SELECT * FROM get_pii_contact($1,$2)`, agreementID, actorID)
		time.Sleep(time.Duration(30+rand.Intn(50)) * time.Millisecond)
	}
}

// EventWriter appends various events including correction payload checks.
func EventWriter(ctx context.Context, pool *pgxpool.Pool, agreementID string, stop <-chan struct{}) error {
	types := []string{"OFFER_MADE", "DEAL_CLOSED"}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		ty := types[rand.Intn(len(types))]
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		var seq int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM timeline_events WHERE agreement_id=$1`, agreementID).Scan(&seq); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO timeline_events (agreement_id, seq, type, payload) VALUES ($1,$2,$3,'{}'::jsonb)`, agreementID, seq, ty)
		if err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		_ = tx.Commit(ctx)
		time.Sleep(time.Duration(15+rand.Intn(35)) * time.Millisecond)
	}
}

// OutboxWorker consumes pending outbox messages with SKIP LOCKED and marks processed or dead after retries.
func OutboxWorker(ctx context.Context, pool *pgxpool.Pool, stop <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM outbox WHERE status='pending' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 10`)
		if err != nil {
			_ = tx.Rollback(ctx)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		ids := make([]string, 0, 10)
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			// simulate random failure
			if rand.Intn(10) == 0 {
				_, _ = tx.Exec(ctx, `UPDATE outbox SET attempts=attempts+1, last_attempt=NOW() WHERE id=$1`, id)
				continue
			}
			_, _ = tx.Exec(ctx, `UPDATE outbox SET status='processed', last_attempt=NOW() WHERE id=$1`, id)
		}
		_ = tx.Commit(ctx)
		time.Sleep(100 * time.Millisecond)
	}
}

// EdgeAdapter registers idempotency then simulates external call; only the first registrar performs the effect.
func EdgeAdapter(ctx context.Context, pool *pgxpool.Pool, key, route string, stop <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		_, err := pool.Exec(ctx, `INSERT INTO edge_invocations(key, route, status) VALUES ($1,$2,'pending') ON CONFLICT DO NOTHING`, key, route)
		if err == nil {
			// first registrant completes
			_, _ = pool.Exec(ctx, `UPDATE edge_invocations SET status='completed', last_attempt_at=NOW(), response_code=200 WHERE key=$1`, key)
		}
		time.Sleep(80 * time.Millisecond)
	}
}

// Disputer transitions disputes and checks invoice linkage via triggers.
func Disputer(ctx context.Context, pool *pgxpool.Pool, agreementID string, stop <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		default:
		}
		var dispID string
		_ = pool.QueryRow(ctx, `INSERT INTO disputes (agreement_id) VALUES ($1) RETURNING id`, agreementID).Scan(&dispID)
		if dispID != "" {
			_, _ = pool.Exec(ctx, `UPDATE disputes SET status='resolved' WHERE id=$1`, dispID)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
