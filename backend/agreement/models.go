package agreement

import "time"

// Agreement mirrors the agreements table columns touched by the service.
type Agreement struct {
	ID                 string
	ReferralID         string
	Status             string
	EffTime            *time.Time
	PiiFirstAccessTime *time.Time
}

// TimelineEvent captures an immutable business event for an agreement.
type TimelineEvent struct {
	ID          int64
	AgreementID string
	Seq         int
	Type        string
	ActorID     *string
	CreatedAt   time.Time
	Payload     []byte
}

// OutboxMessage represents a transactional outbox entry.
type OutboxMessage struct {
	ID        string
	Topic     string
	Payload   []byte
	Status    string
	Attempts  int
	CreatedAt time.Time
}

// ExecuteEsignCompletionParams enumerates the writes executed inside a single transaction.
type ExecuteEsignCompletionParams struct {
	AgreementID     string
	ActorID         *string
	TimelinePayload map[string]any
	OutboxTopic     string
	OutboxPayload   map[string]any
}

const (
	// OutboxTopicAgreementEffective is published whenever an agreement becomes effective.
	OutboxTopicAgreementEffective = "agreement.effective"
)
