package domain

import "time"

const (
	AnalysisStageFacts       = "facts"
	AnalysisStageClaims      = "claims"
	AnalysisCompleted        = "completed"
	AnalysisFailed           = "failed"
	FactOutcomeNotApplicable = "not-applicable"
	FactOutcomeSuccess       = "success"
	FactOutcomeFailure       = "failure"
	FactOutcomeUnknown       = "unknown"
)

type AnalysisOmissions struct {
	CanonicalSegments            int  `json:"canonicalSegments"`
	OmittedTextFactCount         int  `json:"omittedTextFactCount"`
	OmittedTextOriginalUTF8Bytes int  `json:"omittedTextOriginalUtf8Bytes"`
	UnknownLineage               bool `json:"unknownLineage"`
}

type AnalysisRun struct {
	ID                      string                  `json:"id"`
	ProcessingKey           string                  `json:"processingKey,omitempty"`
	Stage                   string                  `json:"stage"`
	RequestedSourceIdentity string                  `json:"requestedSourceIdentity"`
	Revision                *EvidenceRevision       `json:"revision,omitempty"`
	Selection               *EvidenceSelection      `json:"selection,omitempty"`
	ExtractorName           string                  `json:"extractorName"`
	ExtractorVersion        string                  `json:"extractorVersion"`
	SchemaVersion           int                     `json:"schemaVersion"`
	FactIDs                 []string                `json:"factIds"`
	InputFactIDs            []string                `json:"inputFactIds,omitempty"`
	ClaimIDs                []string                `json:"claimIds,omitempty"`
	Model                   *ModelExecutionMetadata `json:"model,omitempty"`
	Omissions               AnalysisOmissions       `json:"omissions"`
	Status                  string                  `json:"status"`
	Error                   string                  `json:"error,omitempty"`
	StartedAt               time.Time               `json:"startedAt"`
	FinishedAt              time.Time               `json:"finishedAt"`
}

type ToolFactValue struct {
	Kind                string `json:"kind"`
	Name                string `json:"name,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	CallID              string `json:"callId,omitempty"`
	RelatedEntryOrdinal *int   `json:"relatedEntryOrdinal,omitempty"`
}

type TestFactValue struct {
	Framework string        `json:"framework"`
	Command   *SelectedText `json:"command,omitempty"`
	Passed    *int          `json:"passed,omitempty"`
	Failed    *int          `json:"failed,omitempty"`
	Skipped   *int          `json:"skipped,omitempty"`
}

type FactValue struct {
	Tool     *ToolFactValue `json:"tool,omitempty"`
	Command  *SelectedText  `json:"command,omitempty"`
	Test     *TestFactValue `json:"test,omitempty"`
	ExitCode *int           `json:"exitCode,omitempty"`
	Error    *SelectedText  `json:"error,omitempty"`
}

type Fact struct {
	ID               string        `json:"id"`
	Fingerprint      string        `json:"fingerprint"`
	AnalysisRunID    string        `json:"analysisRunId"`
	Kind             string        `json:"kind"`
	SchemaVersion    int           `json:"schemaVersion"`
	Value            FactValue     `json:"value"`
	Outcome          string        `json:"outcome"`
	ExtractorName    string        `json:"extractorName"`
	ExtractorVersion string        `json:"extractorVersion"`
	ParseRule        string        `json:"parseRule"`
	Evidence         []EvidenceRef `json:"evidence"`
	CreatedAt        time.Time     `json:"createdAt"`
}

// FactDraft is deterministic extractor output before run identity and timestamps are assigned.
type FactDraft struct {
	Kind      string        `json:"kind"`
	Value     FactValue     `json:"value"`
	Outcome   string        `json:"outcome"`
	ParseRule string        `json:"parseRule"`
	Evidence  []EvidenceRef `json:"evidence"`
}

type FactAnalysis struct {
	Run   AnalysisRun `json:"run"`
	Facts []Fact      `json:"facts"`
}
