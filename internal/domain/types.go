package domain

import "time"

const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

type Event struct {
	ID          string
	Fingerprint string
	Type        string
	SubjectType string
	SubjectID   string
	Payload     map[string]any
	Evidence    []EvidenceRef
	CreatedAt   time.Time
}
