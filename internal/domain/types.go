package domain

import "time"

const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

type EvidenceChunk struct {
	ID                string
	Fingerprint       string
	DistillationKey   string
	Evidence          EvidenceRef
	CapturedAt        time.Time
	ProcessingSkipped bool
}

type Observation struct {
	ID              string
	Fingerprint     string
	DistillationKey string
	Kind            string
	Summary         string
	Confidence      float64
	Evidence        []EvidenceRef
	CreatedAt       time.Time
}

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

type JobPayload struct {
	ScanID         string   `json:"scanId"`
	ObservationIDs []string `json:"observationIds"`
}

type Job struct {
	ID           string
	Fingerprint  string
	EventID      string
	AgentName    string
	AgentVersion string
	Status       string
	Payload      JobPayload
	Error        string
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type AgentRun struct {
	ID           string
	JobID        string
	AgentName    string
	AgentVersion string
	Status       string
	Evidence     []EvidenceRef
	Output       any
	Error        string
	StartedAt    time.Time
	FinishedAt   time.Time
}

type FormatAngle struct {
	Suitable bool   `json:"suitable"`
	Angle    string `json:"angle"`
}

type ContentIdea struct {
	ID              string
	Fingerprint     string
	RunID           string
	Rank            int
	Concept         string
	CoreLesson      string
	AudienceBenefit string
	Hook            string
	Resonance       string
	Confidence      float64
	ShortPost       FormatAngle
	Thread          FormatAngle
	Article         FormatAngle
	Evidence        []EvidenceRef
	CreatedAt       time.Time
}

type Scan struct {
	ID                   string
	Fingerprint          string
	KnowledgeFingerprint string
	SourceKind           string
	After                time.Time
	Before               time.Time
	ContentScope         string
	Coverage             string
	Status               string
	SkippedCount         int
	ObservationIDs       []string
	JobID                string
	CreatedAt            time.Time
	FinishedAt           time.Time
}

type ScanCommit struct {
	Scan         Scan
	Chunks       []EvidenceChunk
	Observations []Observation
	Events       []Event
	Job          *Job
}

type JobCompletion struct {
	JobID string
	Run   AgentRun
	Ideas []ContentIdea
}
