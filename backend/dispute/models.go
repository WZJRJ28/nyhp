package dispute

import "time"

// Status represents the lifecycle of a dispute record.
type Status string

const (
	StatusUnderReview Status = "under_review"
	StatusResolved    Status = "resolved"
)

// Record mirrors the disputes table.
type Record struct {
	ID          string
	AgreementID string
	Status      Status
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ResolvedAt  *time.Time
}
