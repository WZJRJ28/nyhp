package referral

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TimelineWriter interface {
	Append(ctx context.Context, tx pgx.Tx, agreementID string, eventType string, payload map[string]any) error
}

type OutboxWriter interface {
	Enqueue(ctx context.Context, tx pgx.Tx, topic string, payload map[string]any) error
}

type Service struct {
	pool          *pgxpool.Pool
	repo          Repository
	timeline      TimelineWriter
	outbox        OutboxWriter
	idGenerator   func() string
	now           func() time.Time
	defaultStatus Status
}

type CreateParams struct {
	CreatorUserID string
	Region        []string
	PriceMin      int64
	PriceMax      int64
	PropertyType  string
	DealType      string
	Languages     []string
	SLAHours      int
}

type ListResult struct {
	Items []Request
	Total int
}

func NewService(pool *pgxpool.Pool, repo Repository, timeline TimelineWriter, outbox OutboxWriter) *Service {
	if repo == nil {
		repo = NewRepository(pool)
	}
	return &Service{
		pool:          pool,
		repo:          repo,
		timeline:      timeline,
		outbox:        outbox,
		idGenerator:   func() string { return uuid.NewString() },
		now:           time.Now,
		defaultStatus: StatusOpen,
	}
}

func (s *Service) WithIDGenerator(gen func() string) *Service {
	s.idGenerator = gen
	return s
}

func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

func (s *Service) Create(ctx context.Context, params CreateParams) (Request, error) {
	if params.CreatorUserID == "" {
		return Request{}, fmt.Errorf("referral: missing creator user id")
	}
	if len(params.Region) == 0 {
		return Request{}, fmt.Errorf("referral: region required")
	}
	if params.PriceMin <= 0 || params.PriceMax <= 0 || params.PriceMin >= params.PriceMax {
		return Request{}, fmt.Errorf("referral: invalid price range")
	}
	if params.SLAHours <= 0 {
		return Request{}, fmt.Errorf("referral: invalid SLA hours")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("referral: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	req := Request{
		ID:            s.idGenerator(),
		CreatorUserID: params.CreatorUserID,
		Region:        params.Region,
		PriceMin:      params.PriceMin,
		PriceMax:      params.PriceMax,
		PropertyType:  params.PropertyType,
		DealType:      params.DealType,
		Languages:     params.Languages,
		SLAHours:      params.SLAHours,
		Status:        s.defaultStatus,
	}

	created, err := s.repo.Create(ctx, tx, req)
	if err != nil {
		return Request{}, err
	}

	if s.timeline != nil {
		payload := map[string]any{
			"referral_id": created.ID,
			"deal_type":   created.DealType,
			"region":      created.Region,
		}
		if err := s.timeline.Append(ctx, tx, created.ID, "REFERRAL_CREATED", payload); err != nil {
			return Request{}, fmt.Errorf("referral: append timeline: %w", err)
		}
	}
	if s.outbox != nil {
		payload := map[string]any{
			"referral_id": created.ID,
			"status":      created.Status,
		}
		if err := s.outbox.Enqueue(ctx, tx, "referral.created", payload); err != nil {
			return Request{}, fmt.Errorf("referral: enqueue outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("referral: commit tx: %w", err)
	}

	return created, nil
}

func (s *Service) List(ctx context.Context, filters Filters) (ListResult, error) {
	items, total, err := s.repo.List(ctx, filters)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total}, nil
}

type CancelParams struct {
	RequestID string
	ActorID   string
	ActorRole string
	Reason    *string
}

var (
	ErrCancelForbidden    = errors.New("referral: cancel forbidden")
	ErrCancelInvalidState = errors.New("referral: cancel invalid state")
)

func (s *Service) Cancel(ctx context.Context, params CancelParams) (Request, error) {
	if params.RequestID == "" {
		return Request{}, fmt.Errorf("referral: cancel missing request id")
	}
	if params.ActorID == "" {
		return Request{}, fmt.Errorf("referral: cancel missing actor id")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("referral: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	req, err := s.repo.GetForUpdate(ctx, tx, params.RequestID)
	if err != nil {
		return Request{}, err
	}

	actorRole := strings.ToLower(params.ActorRole)
	if actorRole != "agent" && actorRole != "broker_admin" {
		return Request{}, ErrCancelForbidden
	}
	if actorRole != "broker_admin" && req.CreatorUserID != params.ActorID {
		return Request{}, ErrCancelForbidden
	}

	if req.Status != StatusOpen && req.Status != StatusMatched {
		return Request{}, ErrCancelInvalidState
	}

	var reason *string
	if params.Reason != nil {
		trimmed := strings.TrimSpace(*params.Reason)
		if trimmed != "" {
			reason = &trimmed
		}
	}

	updated, err := s.repo.UpdateStatus(ctx, tx, params.RequestID, StatusCancelled, reason)
	if err != nil {
		return Request{}, err
	}

	if s.outbox != nil {
		payload := map[string]any{
			"referral_id": updated.ID,
			"status":      updated.Status,
		}
		if updated.CancelReason != nil {
			payload["reason"] = *updated.CancelReason
		}
		if err := s.outbox.Enqueue(ctx, tx, "referral.cancelled", payload); err != nil {
			return Request{}, fmt.Errorf("referral: enqueue cancel outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("referral: cancel commit: %w", err)
	}

	return updated, nil
}
