package agreement

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Record struct {
	ID               string
	RequestID        string
	ReferrerBrokerID string
	RefereeBrokerID  string
	FeeRate          float64
	ProtectDays      int
	EffectiveAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateParams struct {
	RequestID        string
	ReferrerBrokerID string
	RefereeBrokerID  string
	FeeRate          float64
	ProtectDays      int
}

type ListFilters struct {
	CreatorUserID string
	Page          int
	PageSize      int
}

type CRUDService struct {
	pool *pgxpool.Pool
}

func NewCRUDService(pool *pgxpool.Pool) *CRUDService {
	return &CRUDService{pool: pool}
}

func (s *CRUDService) Create(ctx context.Context, userID string, params CreateParams) (Record, error) {
	if params.RequestID == "" {
		return Record{}, fmt.Errorf("agreement: request id required")
	}
	if params.ReferrerBrokerID == "" || params.RefereeBrokerID == "" {
		return Record{}, fmt.Errorf("agreement: broker ids required")
	}
	if params.FeeRate < 0 {
		return Record{}, fmt.Errorf("agreement: invalid fee rate")
	}
	if params.ProtectDays < 0 {
		return Record{}, fmt.Errorf("agreement: invalid protect days")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Record{}, fmt.Errorf("agreement: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var owner string
	err = tx.QueryRow(ctx, `SELECT created_by_user_id FROM referral_requests WHERE id=$1`, params.RequestID).Scan(&owner)
	if err != nil {
		return Record{}, fmt.Errorf("agreement: ensure referral: %w", err)
	}
	if owner != userID {
		return Record{}, fmt.Errorf("agreement: referral does not belong to user")
	}

	var rec Record
	insertSQL := `
        INSERT INTO agreements (referral_id, from_broker_id, to_broker_id, fee_rate, protect_days, status)
        VALUES ($1,$2,$3,$4,$5,'draft')
        RETURNING id, referral_id, from_broker_id, to_broker_id, fee_rate, protect_days, effective_at, created_at, updated_at
    `
	if err := tx.QueryRow(ctx, insertSQL,
		params.RequestID,
		params.ReferrerBrokerID,
		params.RefereeBrokerID,
		params.FeeRate,
		params.ProtectDays,
	).Scan(&rec.ID, &rec.RequestID, &rec.ReferrerBrokerID, &rec.RefereeBrokerID, &rec.FeeRate, &rec.ProtectDays, &rec.EffectiveAt, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return Record{}, fmt.Errorf("agreement: insert: %w", err)
	}

	if err := setTimelineBroker(ctx, tx, params.ReferrerBrokerID, params.RefereeBrokerID, nil); err != nil {
		return Record{}, err
	}
	payload := map[string]any{
		"referral_id":  params.RequestID,
		"fee_rate":     params.FeeRate,
		"protect_days": params.ProtectDays,
	}

	if _, err := tx.Exec(ctx, `INSERT INTO timeline_events (agreement_id, type, payload) VALUES ($1,'AGREEMENT_CREATED',$2::jsonb)`, rec.ID, mustJSON(payload)); err != nil {
		return Record{}, fmt.Errorf("agreement: timeline insert: %w", err)
	}

	outboxPayload := map[string]any{
		"agreement_id": rec.ID,
		"referral_id":  rec.RequestID,
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox (topic, payload) VALUES ('agreement.created',$1::jsonb)`, mustJSON(outboxPayload)); err != nil {
		return Record{}, fmt.Errorf("agreement: outbox insert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Record{}, fmt.Errorf("agreement: commit: %w", err)
	}

	return rec, nil
}

func (s *CRUDService) List(ctx context.Context, filters ListFilters) ([]Record, int, error) {
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 || filters.PageSize > 100 {
		filters.PageSize = 20
	}

	query := `
        SELECT a.id, a.referral_id, a.from_broker_id, a.to_broker_id, a.fee_rate, a.protect_days, a.effective_at, a.created_at, a.updated_at
        FROM agreements a
        JOIN referral_requests r ON r.id = a.referral_id
        WHERE r.created_by_user_id = $1
        ORDER BY a.created_at DESC
        LIMIT $2 OFFSET $3
    `

	rows, err := s.pool.Query(ctx, query, filters.CreatorUserID, filters.PageSize, (filters.Page-1)*filters.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("agreement: list: %w", err)
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.ID, &rec.RequestID, &rec.ReferrerBrokerID, &rec.RefereeBrokerID, &rec.FeeRate, &rec.ProtectDays, &rec.EffectiveAt, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, 0, err
		}
		records = append(records, rec)
	}

	countQuery := `SELECT COUNT(*) FROM agreements a JOIN referral_requests r ON r.id=a.referral_id WHERE r.created_by_user_id=$1`
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, filters.CreatorUserID).Scan(&total); err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

func mustJSON(payload map[string]any) string {
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(b)
}
