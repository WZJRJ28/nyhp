package broker

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound signals the requested broker does not exist.
var ErrNotFound = errors.New("broker: not found")

// Repository provides read access to broker profiles.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository wires a pgxpool-backed repository implementation.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetByID fetches a broker profile by its primary key.
func (r *Repository) GetByID(ctx context.Context, id string) (Profile, error) {
	const query = `
		SELECT id, name, fein, verified, created_at
		FROM brokers
		WHERE id = $1
	`

	var profile Profile
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&profile.ID,
		&profile.Name,
		&profile.Fein,
		&profile.Verified,
		&profile.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("broker: query by id: %w", err)
	}

	return profile, nil
}

// List fetches up to limit broker profiles ordered by name.
func (r *Repository) List(ctx context.Context, limit int) ([]Profile, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	const query = `
		SELECT id, name, fein, verified, created_at
		FROM brokers
		ORDER BY name ASC
		LIMIT $1
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("broker: list: %w", err)
	}
	defer rows.Close()

	profiles := make([]Profile, 0, limit)
	for rows.Next() {
		var profile Profile
		if err := rows.Scan(&profile.ID, &profile.Name, &profile.Fein, &profile.Verified, &profile.CreatedAt); err != nil {
			return nil, fmt.Errorf("broker: scan profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("broker: iterate profiles: %w", err)
	}

	return profiles, nil
}
