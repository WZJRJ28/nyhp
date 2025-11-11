package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrUserNotFound signals that the user does not exist.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrDuplicateEmail signals that the email is already registered.
	ErrDuplicateEmail = errors.New("auth: email already exists")
)

// Repository handles data access for authentication.
type Repository interface {
	CreateUser(ctx context.Context, params CreateUserParams) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, userID string) (User, error)
}

// CreateUserParams contains write parameters for creating users.
type CreateUserParams struct {
	Email        string
	FullName     string
	PasswordHash string
	Role         Role
}

// PGRepository implements Repository backed by PostgreSQL.
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a PostgreSQL-backed auth repository.
func NewRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

// CreateUser inserts a new user with hashed password.
func (r *PGRepository) CreateUser(ctx context.Context, params CreateUserParams) (User, error) {
	const insertSQL = `
		INSERT INTO users (email, full_name, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, full_name, password_hash, phone, languages, broker_id, rating, role, created_at, updated_at
	`

	user, err := scanUser(r.pool.QueryRow(ctx, insertSQL, params.Email, params.FullName, params.PasswordHash, params.Role))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrDuplicateEmail
		}
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}

	return user, nil
}

// GetUserByEmail retrieves a user by email address.
func (r *PGRepository) GetUserByEmail(ctx context.Context, email string) (User, error) {
	const selectSQL = `
		SELECT id, email, full_name, password_hash, phone, languages, broker_id, rating, role, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	user, err := scanUser(r.pool.QueryRow(ctx, selectSQL, email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("auth: get user by email: %w", err)
	}

	return user, nil
}

// GetUserByID retrieves a user by ID.
func (r *PGRepository) GetUserByID(ctx context.Context, userID string) (User, error) {
	const selectSQL = `
		SELECT id, email, full_name, password_hash, phone, languages, broker_id, rating, role, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	user, err := scanUser(r.pool.QueryRow(ctx, selectSQL, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("auth: get user by id: %w", err)
	}

	return user, nil
}

func scanUser(row pgx.Row) (User, error) {
	var (
		user      User
		phone     *string
		brokerID  *string
		languages []string
	)
	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.FullName,
		&user.PasswordHash,
		&phone,
		&languages,
		&brokerID,
		&user.Rating,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return User{}, err
	}

	user.Phone = phone
	user.Languages = languages
	user.BrokerID = brokerID
	return user, nil
}
