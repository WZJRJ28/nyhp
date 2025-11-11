package referral

import "time"

type Status string

const (
	StatusOpen       Status = "open"
	StatusMatched    Status = "matched"
	StatusSigned     Status = "signed"
	StatusInProgress Status = "in_progress"
	StatusClosed     Status = "closed"
	StatusDisputed   Status = "disputed"
	StatusCancelled  Status = "cancelled"
)

type Request struct {
	ID            string
	CreatorUserID string
	Region        []string
	PriceMin      int64
	PriceMax      int64
	PropertyType  string
	DealType      string
	Languages     []string
	SLAHours      int
	Status        Status
	CancelReason  *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Filters struct {
	CreatorUserID string
	Status        Status
	Region        string
	DealType      string
	Page          int
	PageSize      int
	SortKey       string
	SortOrder     string
}
