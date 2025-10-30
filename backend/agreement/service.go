package agreement

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// EsignCompletionRequest captures the webhook payload normalized for the service.
type EsignCompletionRequest struct {
	AgreementID     string
	IdempotencyKey  string
	ActorID         *string
	TimelinePayload map[string]any
	OutboxTopic     string
	OutboxPayload   map[string]any
}

// TxBeginner abstracts pgxpool.Pool for testability.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// EsignRepository defines the data access required by the service.
type EsignRepository interface {
	InsertIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) error
	ExecuteEsignCompletionTx(ctx context.Context, tx pgx.Tx, params ExecuteEsignCompletionParams) error
}

type Service struct {
	pool TxBeginner
	repo EsignRepository
}

func NewService(pool TxBeginner, repo EsignRepository) *Service {
	if repo == nil {
		repo = NewRepository()
	}
	return &Service{
		pool: pool,
		repo: repo,
	}
}

// HandleEsignCompletionWebhook applies the full esign-completion transaction (Axioms A5 & A6).
func (s *Service) HandleEsignCompletionWebhook(ctx context.Context, req EsignCompletionRequest) error {
	if req.IdempotencyKey == "" {
		return fmt.Errorf("agreement: missing idempotency key")
	}
	if req.AgreementID == "" {
		return fmt.Errorf("agreement: missing agreement id")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agreement: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.InsertIdempotencyKey(ctx, tx, req.IdempotencyKey); err != nil {
		if errors.Is(err, ErrDuplicateIdempotencyKey) {
			return nil
		}
		return err
	}

	params := ExecuteEsignCompletionParams{
		AgreementID:     req.AgreementID,
		ActorID:         req.ActorID,
		TimelinePayload: req.TimelinePayload,
		OutboxTopic:     req.OutboxTopic,
		OutboxPayload:   req.OutboxPayload,
	}

	if err := s.repo.ExecuteEsignCompletionTx(ctx, tx, params); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agreement: commit tx: %w", err)
	}

	return nil
}
