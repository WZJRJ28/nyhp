package dispute

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("dispute: not found")
	ErrForbidden = errors.New("dispute: forbidden")
	ErrBadStatus = errors.New("dispute: invalid status transition")
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, ownerID string, agreementID string) ([]Record, error) {
	query := `
		SELECT d.id, d.agreement_id, d.status::text, d.created_at, d.updated_at, d.resolved_at
		FROM disputes d
		JOIN agreements a ON a.id = d.agreement_id
		JOIN referral_requests rr ON rr.id = a.referral_id
		WHERE rr.created_by_user_id = $1
	`
	args := []any{ownerID}
	if agreementID != "" {
		query += " AND d.agreement_id = $2"
		args = append(args, agreementID)
	}
	query += " ORDER BY d.created_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("dispute: list: %w", err)
	}
	defer rows.Close()

	out := make([]Record, 0, 8)
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.ID, &rec.AgreementID, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt, &rec.ResolvedAt); err != nil {
			return nil, fmt.Errorf("dispute: scan: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dispute: iterate: %w", err)
	}
	return out, nil
}

func (r *Repository) Create(ctx context.Context, ownerID, agreementID string) (Record, error) {
	const query = `
		INSERT INTO disputes (agreement_id, status)
		SELECT $1, 'under_review'
		FROM agreements a
		JOIN referral_requests rr ON rr.id = a.referral_id
		WHERE a.id = $1 AND rr.created_by_user_id = $2
		RETURNING id, agreement_id, status::text, created_at, updated_at, resolved_at
	`

	var rec Record
	err := r.pool.QueryRow(ctx, query, agreementID, ownerID).
		Scan(&rec.ID, &rec.AgreementID, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt, &rec.ResolvedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, ErrForbidden
		}
		return Record{}, fmt.Errorf("dispute: create: %w", err)
	}
	return rec, nil
}

func (r *Repository) Resolve(ctx context.Context, ownerID, disputeID string) (Record, error) {
	const query = `
		UPDATE disputes d
		SET status = 'resolved'
		FROM agreements a
		JOIN referral_requests rr ON rr.id = a.referral_id
		WHERE d.id = $1
		  AND d.agreement_id = a.id
		  AND rr.created_by_user_id = $2
		  AND d.status <> 'resolved'
		RETURNING d.id, d.agreement_id, d.status::text, d.created_at, d.updated_at, d.resolved_at
	`

	var rec Record
	err := r.pool.QueryRow(ctx, query, disputeID, ownerID).
		Scan(&rec.ID, &rec.AgreementID, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt, &rec.ResolvedAt)
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Record{}, fmt.Errorf("dispute: resolve: %w", err)
	}

	const check = `
		SELECT d.status::text
		FROM disputes d
		JOIN agreements a ON a.id = d.agreement_id
		JOIN referral_requests rr ON rr.id = a.referral_id
		WHERE d.id = $1 AND rr.created_by_user_id = $2
	`
	var status Status
	if err := r.pool.QueryRow(ctx, check, disputeID, ownerID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, ErrForbidden
		}
		return Record{}, fmt.Errorf("dispute: resolve fetch: %w", err)
	}
	if status == StatusResolved {
		return Record{}, ErrBadStatus
	}
	return Record{}, ErrForbidden
}
