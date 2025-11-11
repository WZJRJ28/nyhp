package referral

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("referral: not found")
)

type Repository interface {
	Create(ctx context.Context, tx pgx.Tx, req Request) (Request, error)
	List(ctx context.Context, filters Filters) ([]Request, int, error)
	GetForUpdate(ctx context.Context, tx pgx.Tx, id string) (Request, error)
	UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status Status, cancelReason *string) (Request, error)
}

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) Create(ctx context.Context, tx pgx.Tx, req Request) (Request, error) {
	const query = `
        INSERT INTO referral_requests (id, created_by_user_id, region, price_min, price_max, property_type,
            deal_type, languages, sla_hours, status, cancel_reason)
        VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING id, created_by_user_id, region, price_min, price_max, property_type, deal_type, languages, sla_hours, status, cancel_reason, created_at, updated_at
    `

	row := tx.QueryRow(ctx, query,
		req.ID,
		req.CreatorUserID,
		req.Region,
		req.PriceMin,
		req.PriceMax,
		req.PropertyType,
		req.DealType,
		req.Languages,
		req.SLAHours,
		req.Status,
		req.CancelReason,
	)

	return scanRequest(row)
}

func (r *PGRepository) List(ctx context.Context, filters Filters) ([]Request, int, error) {
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 || filters.PageSize > 100 {
		filters.PageSize = 20
	}
	if filters.SortKey == "" {
		filters.SortKey = "created_at"
	}
	if filters.SortOrder == "" {
		filters.SortOrder = "desc"
	}

	base := `SELECT id, created_by_user_id, region, price_min, price_max, property_type, deal_type, languages, sla_hours, status, cancel_reason, created_at, updated_at
             FROM referral_requests`
	where := []string{"1=1"}
	args := []any{}

	if filters.CreatorUserID != "" {
		where = append(where, fmt.Sprintf("created_by_user_id=$%d", len(args)+1))
		args = append(args, filters.CreatorUserID)
	}
	if filters.Status != "" {
		where = append(where, fmt.Sprintf("status=$%d", len(args)+1))
		args = append(args, filters.Status)
	}
	if filters.Region != "" {
		where = append(where, fmt.Sprintf("$%d = ANY(region)", len(args)+1))
		args = append(args, filters.Region)
	}
	if filters.DealType != "" {
		where = append(where, fmt.Sprintf("deal_type=$%d", len(args)+1))
		args = append(args, filters.DealType)
	}

	whereClause := " WHERE " + strings.Join(where, " AND ")

	sortKey := mapSortKey(filters.SortKey)
	sortOrder := strings.ToUpper(filters.SortOrder)
	if sortOrder != "ASC" && sortOrder != "DESC" {
		sortOrder = "DESC"
	}

	limit := filters.PageSize
	offset := (filters.Page - 1) * filters.PageSize

	query := fmt.Sprintf(`%s%s ORDER BY %s %s LIMIT %d OFFSET %d`, base, whereClause, sortKey, sortOrder, limit, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("referral: query list: %w", err)
	}
	defer rows.Close()

	list := []Request{}
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, req)
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM referral_requests%s", whereClause)
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("referral: count list: %w", err)
	}

	return list, total, nil
}

func (r *PGRepository) GetForUpdate(ctx context.Context, tx pgx.Tx, id string) (Request, error) {
	const query = `
		SELECT id, created_by_user_id, region, price_min, price_max, property_type, deal_type, languages, sla_hours, status, cancel_reason, created_at, updated_at
		FROM referral_requests
		WHERE id = $1
		FOR UPDATE
	`

	row := tx.QueryRow(ctx, query, id)
	req, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Request{}, ErrNotFound
		}
		return Request{}, fmt.Errorf("referral: get for update: %w", err)
	}
	return req, nil
}

func (r *PGRepository) UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status Status, cancelReason *string) (Request, error) {
	const query = `
		UPDATE referral_requests
		SET status = $2,
		    cancel_reason = $3,
		    updated_at = get_tx_timestamp()
		WHERE id = $1
		RETURNING id, created_by_user_id, region, price_min, price_max, property_type, deal_type, languages, sla_hours, status, cancel_reason, created_at, updated_at
	`

	row := tx.QueryRow(ctx, query, id, status, cancelReason)
	req, err := scanRequest(row)
	if err != nil {
		return Request{}, fmt.Errorf("referral: update status: %w", err)
	}
	return req, nil
}

func scanRequest(row pgx.Row) (Request, error) {
	var req Request
	return req, row.Scan(
		&req.ID,
		&req.CreatorUserID,
		&req.Region,
		&req.PriceMin,
		&req.PriceMax,
		&req.PropertyType,
		&req.DealType,
		&req.Languages,
		&req.SLAHours,
		&req.Status,
		&req.CancelReason,
		&req.CreatedAt,
		&req.UpdatedAt,
	)
}

func mapSortKey(key string) string {
	switch key {
	case "priceMin":
		return "price_min"
	case "priceMax":
		return "price_max"
	case "propertyType":
		return "property_type"
	case "dealType":
		return "deal_type"
	case "slaHours":
		return "sla_hours"
	case "status":
		return "status"
	case "updatedAt":
		return "updated_at"
	case "createdAt":
		fallthrough
	default:
		return "created_at"
	}
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
