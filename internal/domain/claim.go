package domain

import (
	"regexp"
	"time"
)

const maxModelCostUSDBytes = 64

var modelCostUSDPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

type ClaimType string

const (
	ClaimTypeProblem       ClaimType = "problem"
	ClaimTypeSymptom       ClaimType = "symptom"
	ClaimTypeHypothesis    ClaimType = "hypothesis"
	ClaimTypeFailedAttempt ClaimType = "failed-attempt"
	ClaimTypeRootCause     ClaimType = "root-cause"
	ClaimTypeDecision      ClaimType = "decision"
	ClaimTypeSolution      ClaimType = "solution"
	ClaimTypeVerification  ClaimType = "verification"
	ClaimTypeLesson        ClaimType = "lesson"
)

func (value ClaimType) Valid() bool {
	switch value {
	case ClaimTypeProblem, ClaimTypeSymptom, ClaimTypeHypothesis, ClaimTypeFailedAttempt,
		ClaimTypeRootCause, ClaimTypeDecision, ClaimTypeSolution, ClaimTypeVerification,
		ClaimTypeLesson:
		return true
	default:
		return false
	}
}

type ClaimStatus string

const (
	ClaimStatusObserved  ClaimStatus = "observed"
	ClaimStatusInferred  ClaimStatus = "inferred"
	ClaimStatusUncertain ClaimStatus = "uncertain"
)

func (value ClaimStatus) Valid() bool {
	switch value {
	case ClaimStatusObserved, ClaimStatusInferred, ClaimStatusUncertain:
		return true
	default:
		return false
	}
}

type ClaimAttribution string

const (
	ClaimAttributionUser        ClaimAttribution = "user"
	ClaimAttributionAgent       ClaimAttribution = "agent"
	ClaimAttributionEnvironment ClaimAttribution = "environment"
	ClaimAttributionMixed       ClaimAttribution = "mixed"
	ClaimAttributionUnknown     ClaimAttribution = "unknown"
)

func (value ClaimAttribution) Valid() bool {
	switch value {
	case ClaimAttributionUser, ClaimAttributionAgent, ClaimAttributionEnvironment,
		ClaimAttributionMixed, ClaimAttributionUnknown:
		return true
	default:
		return false
	}
}

// RequestedModelRoute records the exact route Noema asked the gateway to use.
type RequestedModelRoute struct {
	Alias                string `json:"alias"`
	Gateway              string `json:"gateway"`
	Model                string `json:"model"`
	Provider             string `json:"provider"`
	RouteVersion         string `json:"routeVersion"`
	PrivacyPolicyVersion string `json:"privacyPolicyVersion"`
}

// ModelExecutionMetadata records what happened during one model request.
type ModelExecutionMetadata struct {
	RequestedRoute      RequestedModelRoute `json:"requestedRoute"`
	ResolvedProvider    string              `json:"resolvedProvider,omitempty"`
	ResolvedModel       string              `json:"resolvedModel,omitempty"`
	PromptVersion       string              `json:"promptVersion"`
	RequestID           string              `json:"requestId,omitempty"`
	InputTokens         *int                `json:"inputTokens,omitempty"`
	OutputTokens        *int                `json:"outputTokens,omitempty"`
	TotalTokens         *int                `json:"totalTokens,omitempty"`
	LatencyMilliseconds *int64              `json:"latencyMilliseconds,omitempty"`
	CostUSD             *string             `json:"costUsd,omitempty"`
}

// ValidModelCostUSD keeps money metadata exact and JSON-safe without binary
// floating-point conversion. Empty is not a valid present value.
func ValidModelCostUSD(value string) bool {
	return len(value) > 0 && len(value) <= maxModelCostUSDBytes && modelCostUSDPattern.MatchString(value)
}

// ClaimCandidate is untrusted structured model output before local admission.
type ClaimCandidate struct {
	Type                     ClaimType        `json:"type"`
	Statement                string           `json:"statement"`
	Status                   ClaimStatus      `json:"status"`
	Confidence               float64          `json:"confidence"`
	SupportingEvidenceIDs    []string         `json:"supportingEvidenceIds"`
	ContradictingEvidenceIDs []string         `json:"contradictingEvidenceIds"`
	SupportingFactIDs        []string         `json:"supportingFactIds,omitempty"`
	Outcome                  string           `json:"outcome,omitempty"`
	Actor                    string           `json:"actor,omitempty"`
	Origin                   string           `json:"origin,omitempty"`
	Subject                  string           `json:"subject,omitempty"`
	Scope                    string           `json:"scope,omitempty"`
	Attribution              ClaimAttribution `json:"attribution,omitempty"`
}

// Claim is a candidate admitted after local schema, evidence, privacy, and consistency checks.
type Claim struct {
	ID                    string              `json:"id"`
	Fingerprint           string              `json:"fingerprint"`
	AnalysisRunID         string              `json:"analysisRunId"`
	Type                  ClaimType           `json:"type"`
	Statement             string              `json:"statement"`
	Status                ClaimStatus         `json:"status"`
	Confidence            float64             `json:"confidence"`
	SupportingEvidence    []EvidenceRef       `json:"supportingEvidence"`
	ContradictingEvidence []EvidenceRef       `json:"contradictingEvidence"`
	SupportingFactIDs     []string            `json:"supportingFactIds,omitempty"`
	Outcome               string              `json:"outcome,omitempty"`
	Actor                 string              `json:"actor,omitempty"`
	Origin                string              `json:"origin,omitempty"`
	Subject               string              `json:"subject,omitempty"`
	Scope                 string              `json:"scope,omitempty"`
	Attribution           ClaimAttribution    `json:"attribution,omitempty"`
	ExtractorName         string              `json:"extractorName"`
	ExtractorVersion      string              `json:"extractorVersion"`
	SchemaVersion         int                 `json:"schemaVersion"`
	PromptVersion         string              `json:"promptVersion"`
	RequestedRoute        RequestedModelRoute `json:"requestedRoute"`
	ResolvedProvider      string              `json:"resolvedProvider,omitempty"`
	ResolvedModel         string              `json:"resolvedModel,omitempty"`
	CreatedAt             time.Time           `json:"createdAt"`
}

type SemanticAnalysis struct {
	Run    AnalysisRun `json:"run"`
	Claims []Claim     `json:"claims"`
}
