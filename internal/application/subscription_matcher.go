package application

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

// AgentJobRecordV1 is the durable V1 job envelope. It contains immutable
// knowledge references and configuration identity, not evidence bodies.
type AgentJobRecordV1 struct {
	ID          string                   `json:"id"`
	Fingerprint string                   `json:"fingerprint"`
	EventID     string                   `json:"eventId"`
	Agent       domain.AgentIdentity     `json:"agent"`
	Status      string                   `json:"status"`
	Payload     domain.AgentJobPayloadV1 `json:"payload"`
	CreatedAt   time.Time                `json:"createdAt"`
	StartedAt   *time.Time               `json:"startedAt,omitempty"`
	FinishedAt  *time.Time               `json:"finishedAt,omitempty"`
}

type SubscriptionMatchStore interface {
	LoadSemanticAnalysis(context.Context, string) (SemanticAnalysisRecord, error)
	CreateOrReuseV1Job(context.Context, AgentJobRecordV1) (bool, error)
}

type SubscriptionMatcher struct {
	Store SubscriptionMatchStore
	Now   func() time.Time
}

type SubscriptionJobMatch struct {
	ID      string `json:"id"`
	Created bool   `json:"created"`
}

type SubscriptionMatchResult struct {
	AnalysisID          string                 `json:"analysisId"`
	EventID             string                 `json:"eventId"`
	ConfigurationDigest string                 `json:"configurationDigest"`
	Jobs                []SubscriptionJobMatch `json:"jobs"`
}

// MatchContentScout deterministically matches one retained completion event.
// It does not publish an event or invoke an executor.
func (matcher SubscriptionMatcher) MatchContentScout(
	ctx context.Context,
	analysisID string,
	configuration ContentScoutConfiguration,
) (SubscriptionMatchResult, error) {
	if matcher.Store == nil || matcher.Now == nil || analysisID == "" ||
		configuration.Agent != (domain.AgentIdentity{
			Name: ContentScoutAgentName, Version: ContentScoutAgentVersion,
		}) ||
		configuration.Identity.Validate() != nil {
		return SubscriptionMatchResult{}, errors.New("Content Scout match request is invalid")
	}
	record, err := matcher.Store.LoadSemanticAnalysis(ctx, analysisID)
	if err != nil {
		return SubscriptionMatchResult{}, err
	}
	event, claimIDs, err := validateContentScoutCompletion(record, analysisID)
	if err != nil {
		return SubscriptionMatchResult{}, err
	}
	payload := domain.AgentJobPayloadV1{
		SchemaVersion: domain.AgentJobPayloadSchemaVersion,
		Inputs: domain.KnowledgeInputRefsV1{
			AnalysisRunID: analysisID,
			ClaimIDs:      append([]string{}, claimIDs...),
		},
		Configuration: configuration.Identity,
	}
	fingerprint, err := domain.AgentJobFingerprint(event.ID, configuration.Agent, payload)
	if err != nil {
		return SubscriptionMatchResult{}, errors.New("Content Scout job identity is unavailable")
	}
	job := AgentJobRecordV1{
		ID:          platform.DerivedID("job_", fingerprint),
		Fingerprint: fingerprint,
		EventID:     event.ID,
		Agent:       configuration.Agent,
		Status:      domain.JobPending,
		Payload:     payload,
		CreatedAt:   matcher.Now().UTC(),
	}
	if err := ValidateAgentJobRecordV1(job); err != nil {
		return SubscriptionMatchResult{}, err
	}
	created, err := matcher.Store.CreateOrReuseV1Job(ctx, job)
	if err != nil {
		return SubscriptionMatchResult{}, err
	}
	return SubscriptionMatchResult{
		AnalysisID:          analysisID,
		EventID:             event.ID,
		ConfigurationDigest: configuration.Identity.Digest,
		Jobs:                []SubscriptionJobMatch{{ID: job.ID, Created: created}},
	}, nil
}

// ValidateAgentJobRecordV1 checks the immutable identity and safe lifecycle
// projection used by V1 persistence and inspection.
func ValidateAgentJobRecordV1(value AgentJobRecordV1) error {
	if value.ID == "" || value.EventID == "" || value.Agent.Validate() != nil ||
		value.Payload.Validate() != nil || value.CreatedAt.IsZero() {
		return errors.New("V1 agent job is invalid")
	}
	fingerprint, err := domain.AgentJobFingerprint(value.EventID, value.Agent, value.Payload)
	if err != nil || fingerprint != value.Fingerprint ||
		value.ID != platform.DerivedID("job_", fingerprint) {
		return errors.New("V1 agent job identity is invalid")
	}
	switch value.Status {
	case domain.JobPending:
		if value.StartedAt != nil || value.FinishedAt != nil {
			return errors.New("pending V1 agent job has lifecycle timestamps")
		}
	case domain.JobRunning:
		if value.StartedAt == nil || value.FinishedAt != nil ||
			value.StartedAt.Before(value.CreatedAt) {
			return errors.New("running V1 agent job lifecycle is invalid")
		}
	case domain.JobSucceeded, domain.JobFailed:
		if value.StartedAt == nil || value.FinishedAt == nil ||
			value.StartedAt.Before(value.CreatedAt) ||
			value.FinishedAt.Before(*value.StartedAt) {
			return errors.New("terminal V1 agent job lifecycle is invalid")
		}
	default:
		return errors.New("V1 agent job status is invalid")
	}
	return nil
}

func validateContentScoutCompletion(
	record SemanticAnalysisRecord,
	analysisID string,
) (domain.Event, []string, error) {
	run := record.Analysis.Run
	if run.ID != analysisID || run.Stage != domain.AnalysisStageClaims ||
		run.Status != domain.AnalysisCompleted || record.Details.ClaimIDs == nil ||
		!slices.Equal(run.ClaimIDs, *record.Details.ClaimIDs) ||
		len(record.Analysis.Claims) != len(run.ClaimIDs) {
		return domain.Event{}, nil, errors.New("completed semantic analysis is invalid")
	}
	seenClaims := make(map[string]bool, len(run.ClaimIDs))
	for index, claim := range record.Analysis.Claims {
		if claim.ID == "" || claim.ID != run.ClaimIDs[index] ||
			claim.AnalysisRunID != analysisID || seenClaims[claim.ID] {
			return domain.Event{}, nil, errors.New("completed semantic claim order is invalid")
		}
		seenClaims[claim.ID] = true
	}
	var completed *domain.Event
	for index := range record.Events {
		if record.Events[index].Type != ContentScoutEventType {
			continue
		}
		if completed != nil {
			return domain.Event{}, nil, errors.New("semantic analysis has multiple completion events")
		}
		completed = &record.Events[index]
	}
	if completed == nil || completed.SubjectType != "analysis" ||
		completed.SubjectID != analysisID || len(completed.Evidence) != 0 ||
		!completed.CreatedAt.Equal(run.FinishedAt) ||
		!validCompletedEventPayload(completed.Payload, analysisID, run.ClaimIDs) {
		return domain.Event{}, nil, errors.New("semantic analysis completion event is invalid")
	}
	fingerprint, err := EventFingerprint(
		completed.Type, completed.SubjectType, completed.SubjectID, completed.Payload,
	)
	if err != nil || fingerprint != completed.Fingerprint ||
		completed.ID != platform.DerivedID("evt_", fingerprint) {
		return domain.Event{}, nil, errors.New("semantic analysis completion event identity is invalid")
	}
	return *completed, append([]string{}, run.ClaimIDs...), nil
}

func validCompletedEventPayload(
	payload map[string]any,
	analysisID string,
	claimIDs []string,
) bool {
	if len(payload) != 3 || payload["analysisId"] != analysisID {
		return false
	}
	switch version := payload["schemaVersion"].(type) {
	case int:
		if version != 1 {
			return false
		}
	case float64:
		if version != 1 {
			return false
		}
	default:
		return false
	}
	switch values := payload["claimIds"].(type) {
	case []string:
		return slices.Equal(values, claimIDs)
	case []any:
		if len(values) != len(claimIDs) {
			return false
		}
		for index, value := range values {
			if value != claimIDs[index] {
				return false
			}
		}
		return true
	default:
		return false
	}
}
