package dispute

import "context"

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, ownerID, agreementID string) ([]Record, error) {
	return s.repo.List(ctx, ownerID, agreementID)
}

func (s *Service) Create(ctx context.Context, ownerID, agreementID string) (Record, error) {
	return s.repo.Create(ctx, ownerID, agreementID)
}

func (s *Service) Resolve(ctx context.Context, ownerID, disputeID string) (Record, error) {
	return s.repo.Resolve(ctx, ownerID, disputeID)
}
