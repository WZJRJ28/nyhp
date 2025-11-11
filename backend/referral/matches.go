package referral

import (
	"context"
	"errors"
	"fmt"
	"time"

	"brokerflow/agreement"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MatchState string

const (
	MatchStateInvited  MatchState = "invited"
	MatchStateAccepted MatchState = "accepted"
	MatchStateDeclined MatchState = "declined"
)

// Match represents a candidate agent associated with a referral request.
type Match struct {
	ID               string
	RequestID        string
	CandidateAgentID string
	State            MatchState
	Score            float64
	CreatedAt        time.Time
}

// CreateMatchParams enumerates the required fields to insert a new match.
type CreateMatchParams struct {
	RequestID        string
	OwnerUserID      string
	CandidateAgentID string
	Score            float64
	State            MatchState
}

type MatchRepository interface {
	List(ctx context.Context, requestID, ownerID string) ([]Match, error)
	Create(ctx context.Context, params CreateMatchParams) (Match, error)
	ListForCandidate(ctx context.Context, candidateID string) ([]Match, error)
	GetByID(ctx context.Context, matchID string) (Match, error)
	UpdateState(ctx context.Context, matchID string, state MatchState) (Match, error)
}

var (
	ErrMatchNotFound      = errors.New("referral: match not found")
	ErrMatchDuplicate     = errors.New("referral: match already exists")
	ErrMatchInvalidState  = errors.New("referral: invalid match state")
	ErrMatchInvalidScore  = errors.New("referral: invalid match score")
	ErrReferralNotOwned   = errors.New("referral: request not owned by user")
	ErrCandidateMandatory = errors.New("referral: candidate user id required")
)

type PGMatchRepository struct {
	pool *pgxpool.Pool
}

func NewMatchRepository(pool *pgxpool.Pool) *PGMatchRepository {
	return &PGMatchRepository{pool: pool}
}

func (r *PGMatchRepository) List(ctx context.Context, requestID, ownerID string) ([]Match, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM referral_requests WHERE id=$1 AND created_by_user_id=$2)`, requestID, ownerID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("referral: verify owner: %w", err)
	}
	if !exists {
		return nil, ErrReferralNotOwned
	}

	const query = `
		SELECT m.id, m.request_id, m.candidate_user_id, m.state::text, m.score, m.created_at
		FROM referral_matches m
		WHERE m.request_id = $1
		ORDER BY m.created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, requestID)
	if err != nil {
		return nil, fmt.Errorf("referral: list matches: %w", err)
	}
	defer rows.Close()

	matches := make([]Match, 0, 8)
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.ID, &m.RequestID, &m.CandidateAgentID, &m.State, &m.Score, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("referral: scan match: %w", err)
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("referral: iterate matches: %w", err)
	}
	return matches, nil
}

func (r *PGMatchRepository) Create(ctx context.Context, params CreateMatchParams) (Match, error) {
	if params.CandidateAgentID == "" {
		return Match{}, ErrCandidateMandatory
	}
	if params.State == "" {
		params.State = MatchStateInvited
	}
	if params.Score < 0 || params.Score > 1 {
		return Match{}, ErrMatchInvalidScore
	}
	if params.State != MatchStateInvited && params.State != MatchStateAccepted && params.State != MatchStateDeclined {
		return Match{}, ErrMatchInvalidState
	}

	const query = `
		INSERT INTO referral_matches (request_id, candidate_user_id, state, score)
		SELECT $1, $2, $3::referral_match_state, $4
		FROM referral_requests r
		WHERE r.id = $1 AND r.created_by_user_id = $5
		RETURNING id, request_id, candidate_user_id, state::text, score, created_at
	`

	var match Match
	err := r.pool.QueryRow(ctx, query,
		params.RequestID,
		params.CandidateAgentID,
		params.State,
		params.Score,
		params.OwnerUserID,
	).Scan(&match.ID, &match.RequestID, &match.CandidateAgentID, &match.State, &match.Score, &match.CreatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Match{}, ErrReferralNotOwned
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Match{}, ErrMatchDuplicate
		}
		return Match{}, fmt.Errorf("referral: create match: %w", err)
	}
	return match, nil
}

func (r *PGMatchRepository) ListForCandidate(ctx context.Context, candidateID string) ([]Match, error) {
	const query = `
		SELECT m.id, m.request_id, m.candidate_user_id, m.state::text, m.score, m.created_at
		FROM referral_matches m
		WHERE m.candidate_user_id = $1
		ORDER BY m.created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, candidateID)
	if err != nil {
		return nil, fmt.Errorf("referral: list matches for candidate: %w", err)
	}
	defer rows.Close()

	out := make([]Match, 0, 8)
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.ID, &m.RequestID, &m.CandidateAgentID, &m.State, &m.Score, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("referral: scan candidate match: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("referral: iterate candidate matches: %w", err)
	}
	return out, nil
}

func (r *PGMatchRepository) GetByID(ctx context.Context, matchID string) (Match, error) {
	const query = `
		SELECT id, request_id, candidate_user_id, state::text, score, created_at
		FROM referral_matches
		WHERE id = $1
	`
	var m Match
	if err := r.pool.QueryRow(ctx, query, matchID).Scan(&m.ID, &m.RequestID, &m.CandidateAgentID, &m.State, &m.Score, &m.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Match{}, ErrMatchNotFound
		}
		return Match{}, fmt.Errorf("referral: get match: %w", err)
	}
	return m, nil
}

func (r *PGMatchRepository) UpdateState(ctx context.Context, matchID string, state MatchState) (Match, error) {
	const query = `
		UPDATE referral_matches
		SET state = $2::referral_match_state
		WHERE id = $1
		RETURNING id, request_id, candidate_user_id, state::text, score, created_at
	`
	var m Match
	if err := r.pool.QueryRow(ctx, query, matchID, state).Scan(&m.ID, &m.RequestID, &m.CandidateAgentID, &m.State, &m.Score, &m.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Match{}, ErrMatchNotFound
		}
		return Match{}, fmt.Errorf("referral: update match state: %w", err)
	}
	return m, nil
}

type MatchService struct {
	repo     MatchRepository
	agRepo   agreementRepository
	now      func() time.Time
	idGen    func() string
	timeline referralTimeline
	outbox   referralOutbox
}

type agreementRepository interface {
	CreateFromMatch(ctx context.Context, tx pgx.Tx, params agreement.MatchAcceptanceParams) (agreement.Record, error)
}

type referralTimeline interface {
	Append(ctx context.Context, tx pgx.Tx, agreementID string, eventType string, payload map[string]any) error
}

type referralOutbox interface {
	Enqueue(ctx context.Context, tx pgx.Tx, topic string, payload map[string]any) error
}

func NewMatchService(repo MatchRepository) *MatchService {
	return &MatchService{
		repo:  repo,
		now:   time.Now,
		idGen: func() string { return uuid.NewString() },
	}
}

func (s *MatchService) WithAgreementRepository(repo agreementRepository) *MatchService {
	s.agRepo = repo
	return s
}

func (s *MatchService) WithTimelineAndOutbox(timeline referralTimeline, out referralOutbox) *MatchService {
	s.timeline = timeline
	s.outbox = out
	return s
}

func (s *MatchService) List(ctx context.Context, requestID, ownerID string) ([]Match, error) {
	return s.repo.List(ctx, requestID, ownerID)
}

func (s *MatchService) Create(ctx context.Context, params CreateMatchParams) (Match, error) {
	return s.repo.Create(ctx, params)
}

func (s *MatchService) ListForCandidate(ctx context.Context, candidateID string) ([]Match, error) {
	return s.repo.ListForCandidate(ctx, candidateID)
}

type UpdateMatchParams struct {
	MatchID     string
	CandidateID string
	NewState    MatchState
	Pool        *pgxpool.Pool
}

type MatchUpdateResult struct {
	Match     Match
	Agreement *agreement.Record
}

var (
	ErrMatchForbidden         = errors.New("referral: match forbidden")
	ErrMatchInvalidTransition = errors.New("referral: invalid match transition")
)

func (s *MatchService) UpdateState(ctx context.Context, params UpdateMatchParams) (MatchUpdateResult, error) {
	match, err := s.repo.GetByID(ctx, params.MatchID)
	if err != nil {
		return MatchUpdateResult{}, err
	}
	if match.CandidateAgentID != params.CandidateID {
		return MatchUpdateResult{}, ErrMatchForbidden
	}
	if params.NewState != MatchStateAccepted && params.NewState != MatchStateDeclined {
		return MatchUpdateResult{}, ErrMatchInvalidTransition
	}
	if match.State == params.NewState {
		return MatchUpdateResult{Match: match}, nil
	}

	if params.NewState == MatchStateAccepted && s.agRepo != nil && params.Pool != nil {
		return s.acceptMatchAndCreateAgreement(ctx, params, match)
	}

	updated, err := s.repo.UpdateState(ctx, params.MatchID, params.NewState)
	if err != nil {
		return MatchUpdateResult{}, err
	}

	return MatchUpdateResult{Match: updated}, nil
}

func (s *MatchService) acceptMatchAndCreateAgreement(ctx context.Context, params UpdateMatchParams, match Match) (MatchUpdateResult, error) {
	// Allow idempotent acceptance.
	if match.State == MatchStateAccepted && s.agRepo != nil {
		tx, err := params.Pool.Begin(ctx)
		if err != nil {
			return MatchUpdateResult{}, fmt.Errorf("match: begin agreement tx: %w", err)
		}
		defer tx.Rollback(ctx)

		rec, err := s.agRepo.CreateFromMatch(ctx, tx, agreement.MatchAcceptanceParams{
			MatchID:          match.ID,
			RequestID:        match.RequestID,
			CandidateUserID:  match.CandidateAgentID,
			AcceptedByUserID: match.CandidateAgentID,
			AcceptedAt:       s.now(),
		})
		if err != nil {
			return MatchUpdateResult{}, err
		}

		if err := tx.Commit(ctx); err != nil {
			return MatchUpdateResult{}, fmt.Errorf("match: commit agreement tx: %w", err)
		}

		refreshed, err := s.repo.GetByID(ctx, match.ID)
		if err != nil {
			return MatchUpdateResult{}, err
		}
		return MatchUpdateResult{
			Match:     refreshed,
			Agreement: &rec,
		}, nil
	}

	tx, err := params.Pool.Begin(ctx)
	if err != nil {
		return MatchUpdateResult{}, fmt.Errorf("match: begin acceptance tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const lockSQL = `
SELECT state::text
FROM referral_matches
WHERE id = $1
FOR UPDATE
`
	var currentState string
	if err := tx.QueryRow(ctx, lockSQL, match.ID).Scan(&currentState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MatchUpdateResult{}, ErrMatchNotFound
		}
		return MatchUpdateResult{}, fmt.Errorf("match: lock for acceptance: %w", err)
	}

	switch MatchState(currentState) {
	case MatchStateAccepted:
		// Already accepted, continue.
	case MatchStateInvited:
		if _, err := tx.Exec(ctx, `
UPDATE referral_matches
SET state = 'accepted'::referral_match_state
WHERE id = $1
`, match.ID); err != nil {
			return MatchUpdateResult{}, fmt.Errorf("match: mark accepted: %w", err)
		}
	default:
		return MatchUpdateResult{}, ErrMatchInvalidTransition
	}

	rec, err := s.agRepo.CreateFromMatch(ctx, tx, agreement.MatchAcceptanceParams{
		MatchID:          match.ID,
		RequestID:        match.RequestID,
		CandidateUserID:  match.CandidateAgentID,
		AcceptedByUserID: match.CandidateAgentID,
		AcceptedAt:       s.now(),
	})
	if err != nil {
		return MatchUpdateResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return MatchUpdateResult{}, fmt.Errorf("match: commit acceptance: %w", err)
	}

	accepted, err := s.repo.GetByID(ctx, match.ID)
	if err != nil {
		return MatchUpdateResult{}, err
	}
	return MatchUpdateResult{
		Match:     accepted,
		Agreement: &rec,
	}, nil
}
