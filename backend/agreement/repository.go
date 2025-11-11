package agreement

import (
	"context"
	"database/sql"
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

	effTime, fromBrokerID, toBrokerID, err := r.markAgreementEffective(ctx, tx, params.AgreementID)
	if err != nil {
		return err
	}

	if err := r.appendTimelineEvent(ctx, tx, params, effTime, fromBrokerID, toBrokerID); err != nil {
		return err
	}

	if err := r.enqueueOutbox(ctx, tx, params, effTime); err != nil {
		return err
	}

	return nil
}

func (r *Repository) markAgreementEffective(ctx context.Context, tx pgx.Tx, agreementID string) (time.Time, string, string, error) {
	const updateSQL = `
UPDATE agreements
SET status = 'effective',
    effective_at = COALESCE(effective_at, get_tx_timestamp())
WHERE id = $1
RETURNING effective_at, from_broker_id::text, to_broker_id::text;
`

	var (
		effTime      time.Time
		fromBrokerID sql.NullString
		toBrokerID   sql.NullString
	)
	if err := tx.QueryRow(ctx, updateSQL, agreementID).Scan(&effTime, &fromBrokerID, &toBrokerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, "", "", ErrAgreementNotFound
		}
		return time.Time{}, "", "", fmt.Errorf("agreement: update effective: %w", err)
	}

	if !fromBrokerID.Valid || !toBrokerID.Valid {
		return time.Time{}, "", "", fmt.Errorf("agreement: broker linkage missing")
	}

	return effTime, fromBrokerID.String, toBrokerID.String, nil
}

func (r *Repository) appendTimelineEvent(ctx context.Context, tx pgx.Tx, params ExecuteEsignCompletionParams, effTime time.Time, fromBrokerID, toBrokerID string) error {
	if err := setTimelineBroker(ctx, tx, fromBrokerID, toBrokerID, params.ActorID); err != nil {
		return err
	}

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
INSERT INTO timeline_events (agreement_id, type, payload, actor_id)
VALUES ($1, 'ESIGN_COMPLETED', $2, $3);
`

	if _, err := tx.Exec(ctx, insertSQL, params.AgreementID, payloadBytes, actorID); err != nil {
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
