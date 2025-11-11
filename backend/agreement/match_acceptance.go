package agreement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MatchAcceptanceParams encapsulates the information required to project an
// accepted referral match into the agreements domain within a single
// transaction.
type MatchAcceptanceParams struct {
	MatchID          string
	RequestID        string
	CandidateUserID  string
	AcceptedByUserID string
	AcceptedAt       time.Time
}

var (
	errMatchCandidateMismatch = errors.New("agreement: match does not belong to candidate")
	errMatchRequestMismatch   = errors.New("agreement: match does not belong to referral request")
	errCandidateBrokerMissing = errors.New("agreement: candidate agent has no broker")
	errOwnerBrokerMissing     = errors.New("agreement: referral owner has no broker")
)

const (
	defaultMatchFeeRate    = 30.0
	defaultMatchProtectDay = 90
)

// CreateFromMatch materialises a new agreement for an accepted match. It is
// designed to be invoked inside the caller's transaction so we leverage the
// surrounding locks to uphold the partial uniqueness guarantees (Axiom P1) and
// append-only timeline/outbox behaviour (Axioms P3–P5).
func (r *Repository) CreateFromMatch(ctx context.Context, tx pgx.Tx, params MatchAcceptanceParams) (Record, error) {
	if params.MatchID == "" {
		return Record{}, fmt.Errorf("agreement: match acceptance missing match id")
	}
	if params.RequestID == "" {
		return Record{}, fmt.Errorf("agreement: match acceptance missing request id")
	}
	if params.CandidateUserID == "" {
		return Record{}, fmt.Errorf("agreement: match acceptance missing candidate user id")
	}

	var (
		matchRequestID string
		matchCandidate string
		matchState     string
	)
	const matchSQL = `
SELECT request_id::text, candidate_user_id::text, state::text
FROM referral_matches
WHERE id = $1
FOR UPDATE
`
	if err := tx.QueryRow(ctx, matchSQL, params.MatchID).Scan(&matchRequestID, &matchCandidate, &matchState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, fmt.Errorf("agreement: match %s not found", params.MatchID)
		}
		return Record{}, fmt.Errorf("agreement: load match: %w", err)
	}
	if matchRequestID != params.RequestID {
		return Record{}, errMatchRequestMismatch
	}
	if matchCandidate != params.CandidateUserID {
		return Record{}, errMatchCandidateMismatch
	}
	if matchState != "accepted" {
		return Record{}, fmt.Errorf("agreement: match %s is not accepted (state=%s)", params.MatchID, matchState)
	}

	const requestSQL = `
SELECT rr.created_by_user_id::text,
       owner.broker_id::text,
       candidate.broker_id::text,
       rr.status
FROM referral_requests rr
JOIN users owner ON owner.id = rr.created_by_user_id
JOIN users candidate ON candidate.id = $2
WHERE rr.id = $1
FOR UPDATE
`
	var (
		ownerUserID     string
		ownerBrokerID   *string
		candidateBroker *string
		currentStatus   string
	)
	if err := tx.QueryRow(ctx, requestSQL, params.RequestID, params.CandidateUserID).Scan(&ownerUserID, &ownerBrokerID, &candidateBroker, &currentStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, fmt.Errorf("agreement: referral request %s not found", params.RequestID)
		}
		return Record{}, fmt.Errorf("agreement: load referral request: %w", err)
	}
	if ownerBrokerID == nil || *ownerBrokerID == "" {
		return Record{}, errOwnerBrokerMissing
	}
	if candidateBroker == nil || *candidateBroker == "" {
		return Record{}, errCandidateBrokerMissing
	}

	// Idempotency: return existing active agreement if present. We prefer to do
	// this before mutating state to tolerate retries from the caller.
	const existingSQL = `
SELECT id, referral_id, from_broker_id, to_broker_id, fee_rate, protect_days, effective_at, created_at, updated_at
FROM agreements
WHERE referral_id = $1
  AND status IN ('pending_signature','effective')
LIMIT 1
`
	var existing Record
	switch err := tx.QueryRow(ctx, existingSQL, params.RequestID).Scan(
		&existing.ID,
		&existing.RequestID,
		&existing.ReferrerBrokerID,
		&existing.RefereeBrokerID,
		&existing.FeeRate,
		&existing.ProtectDays,
		&existing.EffectiveAt,
		&existing.CreatedAt,
		&existing.UpdatedAt,
	); {
	case err == nil:
		return existing, nil
	case errors.Is(err, pgx.ErrNoRows):
		// continue with insert
	default:
		return Record{}, fmt.Errorf("agreement: check existing agreement: %w", err)
	}

	const insertSQL = `
INSERT INTO agreements (referral_id, from_broker_id, to_broker_id, fee_rate, protect_days, status)
VALUES ($1, $2, $3, $4, $5, 'pending_signature')
RETURNING id, referral_id, from_broker_id, to_broker_id, fee_rate, protect_days, effective_at, created_at, updated_at
`

	var rec Record
	if err := tx.QueryRow(ctx, insertSQL,
		params.RequestID,
		*ownerBrokerID,
		*candidateBroker,
		defaultMatchFeeRate,
		defaultMatchProtectDay,
	).Scan(
		&rec.ID,
		&rec.RequestID,
		&rec.ReferrerBrokerID,
		&rec.RefereeBrokerID,
		&rec.FeeRate,
		&rec.ProtectDays,
		&rec.EffectiveAt,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	); err != nil {
		return Record{}, fmt.Errorf("agreement: insert from match: %w", err)
	}

	// Update the referral to matched if still open.
	if currentStatus == "open" {
		if _, err := tx.Exec(ctx, `
UPDATE referral_requests
SET status = 'matched',
    updated_at = get_tx_timestamp()
WHERE id = $1 AND status = 'open'
`, params.RequestID); err != nil {
			return Record{}, fmt.Errorf("agreement: tag referral matched: %w", err)
		}
	}

	// Append a creation timeline event.
	var acceptedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT get_tx_timestamp()`).Scan(&acceptedAt); err != nil {
		return Record{}, fmt.Errorf("agreement: fetch accepted timestamp: %w", err)
	}

	if err := setTimelineBroker(ctx, tx, *ownerBrokerID, *candidateBroker, &params.AcceptedByUserID); err != nil {
		return Record{}, err
	}

	timelinePayload := map[string]any{
		"source":              "match_acceptance",
		"match_id":            params.MatchID,
		"accepted_at":         acceptedAt.UTC(),
		"accepted_by_user_id": params.AcceptedByUserID,
		"referral_owner_id":   ownerUserID,
	}
	if err := insertTimelineEvent(ctx, tx, rec.ID, "AGREEMENT_CREATED", params.AcceptedByUserID, timelinePayload); err != nil {
		return Record{}, err
	}

	// Emit an outbox message for downstream delivery.
	outboxPayload := map[string]any{
		"agreement_id": rec.ID,
		"referral_id":  rec.RequestID,
		"match_id":     params.MatchID,
		"candidate_id": params.CandidateUserID,
		"status":       "pending_signature",
		"owner_id":     ownerUserID,
	}
	if err := enqueueOutbox(ctx, tx, "agreement.created", outboxPayload); err != nil {
		return Record{}, err
	}

	return rec, nil
}

func insertTimelineEvent(ctx context.Context, tx pgx.Tx, agreementID string, eventType string, actorID string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("agreement: marshal timeline payload: %w", err)
	}
	var actor any
	if actorID != "" {
		actor = actorID
	}
	const q = `
INSERT INTO timeline_events (agreement_id, type, payload, actor_id)
VALUES ($1, $2::event_type, $3::jsonb, $4::uuid)
`
	if _, err := tx.Exec(ctx, q, agreementID, eventType, body, actor); err != nil {
		return fmt.Errorf("agreement: insert timeline event: %w", err)
	}
	return nil
}

func enqueueOutbox(ctx context.Context, tx pgx.Tx, topic string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("agreement: marshal outbox payload: %w", err)
	}
	const q = `INSERT INTO outbox (topic, payload) VALUES ($1, $2::jsonb)`
	if _, err := tx.Exec(ctx, q, topic, body); err != nil {
		return fmt.Errorf("agreement: enqueue outbox: %w", err)
	}
	return nil
}
