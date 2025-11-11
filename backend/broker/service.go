package broker

import "context"

// ProfileReader abstracts repository operations for the service.
type ProfileReader interface {
	GetByID(ctx context.Context, id string) (Profile, error)
	List(ctx context.Context, limit int) ([]Profile, error)
}

// Service exposes business-level broker operations.
type Service struct {
	repo ProfileReader
}

// NewService builds a Service using the provided repository.
func NewService(repo ProfileReader) *Service {
	return &Service{repo: repo}
}

// GetByID returns the broker profile for the given identifier.
func (s *Service) GetByID(ctx context.Context, id string) (Profile, error) {
	return s.repo.GetByID(ctx, id)
}

// List returns up to limit broker profiles.
func (s *Service) List(ctx context.Context, limit int) ([]Profile, error) {
	return s.repo.List(ctx, limit)
}
