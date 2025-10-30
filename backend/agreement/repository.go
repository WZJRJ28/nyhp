package agreement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrDuplicateIdempotencyKey signals the idempotency insert hit the existing key guardrail (Axiom A6).
	ErrDuplicateIdempotencyKey = errors.New("agreement: duplicate idempotency key")
	// ErrAgreementNotFound is returned when no agreement row exists for the provided identifier.
	ErrAgreementNotFound = errors.New("agreement: not found")
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

// InsertIdempotencyKey attempts to reserve the idempotency key inside the active transaction.
func (r *Repository) InsertIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) error {
	if key == "" {
		return fmt.Errorf("agreement: empty idempotency key")
	}

	_, err := tx.Exec(ctx, `INSERT INTO idempotency (key) VALUES ($1)`, key)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("agreement: insert idempotency key: %w", err)
	}

	return nil
}

// ExecuteEsignCompletionTx performs the status transition, event append, and outbox write for e-sign completion.
func (r *Repository) ExecuteEsignCompletionTx(ctx context.Context, tx pgx.Tx, params ExecuteEsignCompletionParams) error {
	if params.AgreementID == "" {
		return fmt.Errorf("agreement: missing agreement id")
	}

	effTime, err := r.markAgreementEffective(ctx, tx, params.AgreementID)
	if err != nil {
		return err
	}

	nextSeq, err := r.nextTimelineSequence(ctx, tx, params.AgreementID)
	if err != nil {
		return err
	}

	if err := r.appendTimelineEvent(ctx, tx, params, nextSeq, effTime); err != nil {
		return err
	}

	if err := r.enqueueOutbox(ctx, tx, params, effTime); err != nil {
		return err
	}

	return nil
}

func (r *Repository) markAgreementEffective(ctx context.Context, tx pgx.Tx, agreementID string) (time.Time, error) {
	const updateSQL = `
UPDATE agreements
SET status = 'effective',
    eff_time = COALESCE(eff_time, get_tx_timestamp())
WHERE id = $1
RETURNING eff_time;
`

	var effTime time.Time
	if err := tx.QueryRow(ctx, updateSQL, agreementID).Scan(&effTime); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, ErrAgreementNotFound
		}
		return time.Time{}, fmt.Errorf("agreement: update effective: %w", err)
	}

	return effTime, nil
}

func (r *Repository) nextTimelineSequence(ctx context.Context, tx pgx.Tx, agreementID string) (int, error) {
	const seqSQL = `
SELECT COALESCE(MAX(seq), 0) + 1
FROM timeline_events
WHERE agreement_id = $1;
`

	var seq int
	if err := tx.QueryRow(ctx, seqSQL, agreementID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("agreement: fetch next timeline seq: %w", err)
	}

	return seq, nil
}

func (r *Repository) appendTimelineEvent(ctx context.Context, tx pgx.Tx, params ExecuteEsignCompletionParams, seq int, effTime time.Time) error {
	payload := params.TimelinePayload
	if payload == nil {
		payload = make(map[string]any, 3)
	}
	payload["agreement_id"] = params.AgreementID
	payload["effective_at"] = effTime.UTC()

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("agreement: marshal timeline payload: %w", err)
	}

	var actorID any
	if params.ActorID != nil {
		actorID = *params.ActorID
	}

	const insertSQL = `
INSERT INTO timeline_events (agreement_id, seq, type, payload, actor_id)
VALUES ($1, $2, 'ESIGN_COMPLETED', $3, $4);
`

	if _, err := tx.Exec(ctx, insertSQL, params.AgreementID, seq, payloadBytes, actorID); err != nil {
		return fmt.Errorf("agreement: insert timeline event: %w", err)
	}

	return nil
}

func (r *Repository) enqueueOutbox(ctx context.Context, tx pgx.Tx, params ExecuteEsignCompletionParams, effTime time.Time) error {
	payload := params.OutboxPayload
	if payload == nil {
		payload = make(map[string]any, 3)
	}
	payload["agreement_id"] = params.AgreementID
	payload["effective_at"] = effTime.UTC()

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("agreement: marshal outbox payload: %w", err)
	}

	topic := params.OutboxTopic
	if topic == "" {
		topic = OutboxTopicAgreementEffective
	}

	const insertSQL = `
INSERT INTO outbox (topic, payload)
VALUES ($1, $2);
`

	if _, err := tx.Exec(ctx, insertSQL, topic, payloadBytes); err != nil {
		return fmt.Errorf("agreement: insert outbox message: %w", err)
	}

	return nil
}
