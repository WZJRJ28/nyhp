package broker

import "time"

// Profile captures the subset of broker data exposed via the public API layer.
type Profile struct {
	ID        string
	Name      string
	Fein      string
	Verified  bool
	CreatedAt time.Time
}
