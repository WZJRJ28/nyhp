package agreement

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StatusService handles status transitions on agreements ensuring timeline and
// outbox writes are captured in the same transaction.
type StatusService struct {
	pool *pgxpool.Pool
}

func NewStatusService(pool *pgxpool.Pool) *StatusService {
	return &StatusService{pool: pool}
}

type TransitionParams struct {
	AgreementID string
	ActorID     string
	NextStatus  string
	Payload     map[string]any
}

func (s *StatusService) Transition(ctx context.Context, params TransitionParams) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		current      string
		fromBrokerID sql.NullString
		toBrokerID   sql.NullString
	)
	if err := tx.QueryRow(ctx, `SELECT status, from_broker_id::text, to_broker_id::text FROM agreements WHERE id=$1 FOR UPDATE`, params.AgreementID).
		Scan(&current, &fromBrokerID, &toBrokerID); err != nil {
		return fmt.Errorf("agreement: fetch current status: %w", err)
	}
	if !fromBrokerID.Valid || !toBrokerID.Valid {
		return fmt.Errorf("agreement: broker linkage missing")
	}

	var ok bool
	if err := tx.QueryRow(ctx, `SELECT agreement_validate_transition($1::agreement_status,$2::agreement_status)`, current, params.NextStatus).Scan(&ok); err != nil {
		return fmt.Errorf("agreement: validate transition: %w", err)
	}
	if !ok {
		return fmt.Errorf("agreement: invalid transition %s -> %s", current, params.NextStatus)
	}

	if _, err := tx.Exec(ctx, `
        UPDATE agreements
        SET status=$1::agreement_status,
            status_updated_at=get_tx_timestamp(),
            status_updated_by=$2::uuid,
            updated_at=get_tx_timestamp()
        WHERE id=$3
    `, params.NextStatus, params.ActorID, params.AgreementID); err != nil {
		return fmt.Errorf("agreement: update status: %w", err)
	}

	var actorPtr *string
	if params.ActorID != "" {
		actorPtr = &params.ActorID
	}
	if err := setTimelineBroker(ctx, tx, fromBrokerID.String, toBrokerID.String, actorPtr); err != nil {
		return err
	}

	payload := map[string]any{
		"previous_status": current,
		"next_status":     params.NextStatus,
	}
	for k, v := range params.Payload {
		payload[k] = v
	}
	if params.ActorID != "" {
		payload["actor_id"] = params.ActorID
	}

	if _, err := tx.Exec(ctx, `
        INSERT INTO timeline_events (agreement_id, type, payload, actor_id)
        VALUES ($1,'AGREEMENT_STATUS_CHANGED',$2::jsonb,$3::uuid)
    `, params.AgreementID, toJSON(payload), actorPtr); err != nil {
		return fmt.Errorf("agreement: insert timeline: %w", err)
	}

	outboxPayload := map[string]any{
		"agreement_id": params.AgreementID,
		"previous":     current,
		"next":         params.NextStatus,
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO outbox (topic, payload)
        VALUES ('agreement.status_changed',$1::jsonb)
    `, toJSON(outboxPayload)); err != nil {
		return fmt.Errorf("agreement: enqueue outbox: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agreement: commit transition: %w", err)
	}

	return nil
}

func toJSON(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}
