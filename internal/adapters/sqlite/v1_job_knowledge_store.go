package sqlite

import (
	"context"
	"reflect"
	"slices"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

// LoadV1JobKnowledge loads only the exact retained knowledge selected by a V1
// job. It does not resolve Sessions content or infer a legacy payload version.
func (store *Store) LoadV1JobKnowledge(
	ctx context.Context,
	jobID string,
) (application.V1JobKnowledgeInput, bool, error) {
	job, found, err := store.LoadV1Job(ctx, jobID)
	if err != nil || !found {
		return application.V1JobKnowledgeInput{}, found, err
	}
	record, err := loadSemanticAnalysis(ctx, store.database, job.Payload.Inputs.AnalysisRunID)
	if err != nil {
		return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
	}
	if record.Analysis.Run.Status != domain.AnalysisCompleted ||
		!slices.Equal(record.Analysis.Run.ClaimIDs, job.Payload.Inputs.ClaimIDs) ||
		len(record.Analysis.Claims) != len(job.Payload.Inputs.ClaimIDs) {
		return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
	}
	for index, claim := range record.Analysis.Claims {
		if claim.ID != job.Payload.Inputs.ClaimIDs[index] ||
			claim.AnalysisRunID != record.Analysis.Run.ID {
			return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
		}
	}

	if len(record.Events) == 0 {
		return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
	}
	trigger := record.Events[len(record.Events)-1]
	if trigger.ID != job.EventID ||
		trigger.SubjectType != "analysis" ||
		trigger.SubjectID != record.Analysis.Run.ID {
		return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
	}
	factIDs, ok := supportingFactIDs(record.Analysis.Claims, record.Analysis.Run.InputFactIDs)
	if !ok {
		return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
	}
	facts, err := loadFactsByID(ctx, store.database, factIDs)
	if err != nil {
		return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
	}
	if err := validateSupportingFactCohort(
		ctx,
		store.database,
		record.Analysis.Run,
		facts,
	); err != nil {
		return application.V1JobKnowledgeInput{}, false, ErrAgentRuntimeDataInvalid
	}
	return application.V1JobKnowledgeInput{
		Job:          job,
		TriggerEvent: trigger,
		Analysis:     record.Analysis,
		Facts:        facts,
	}, true, nil
}

func supportingFactIDs(claims []domain.Claim, inputFactIDs []string) ([]string, bool) {
	allowed := make(map[string]bool, len(inputFactIDs))
	for _, factID := range inputFactIDs {
		if factID == "" || allowed[factID] {
			return nil, false
		}
		allowed[factID] = true
	}
	result := make([]string, 0)
	seen := make(map[string]bool)
	for _, claim := range claims {
		local := make(map[string]bool, len(claim.SupportingFactIDs))
		for _, factID := range claim.SupportingFactIDs {
			if factID == "" || local[factID] || !allowed[factID] {
				return nil, false
			}
			local[factID] = true
			if !seen[factID] {
				seen[factID] = true
				result = append(result, factID)
			}
		}
	}
	return result, true
}

func loadFactsByID(
	ctx context.Context,
	queryer runtimeQueryer,
	factIDs []string,
) ([]domain.Fact, error) {
	facts := make([]domain.Fact, 0, len(factIDs))
	for _, factID := range factIDs {
		fact, err := readFact(queryer.QueryRowContext(ctx, `
			SELECT id, fingerprint, analysis_run_id, kind, schema_version,
			       value_json, outcome, extractor_name, extractor_version,
			       parse_rule, evidence_json, created_at
			  FROM facts
			 WHERE id = ?
		`, factID))
		if err != nil || fact.ID != factID {
			if err != nil {
				return nil, err
			}
			return nil, ErrAgentRuntimeDataInvalid
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func validateSupportingFactCohort(
	ctx context.Context,
	queryer semanticQueryer,
	semanticRun domain.AnalysisRun,
	facts []domain.Fact,
) error {
	if len(facts) == 0 {
		return nil
	}
	analysisID := facts[0].AnalysisRunID
	for _, fact := range facts {
		if fact.AnalysisRunID != analysisID {
			return ErrAgentRuntimeDataInvalid
		}
	}
	factRun, err := loadAnalysisRun(ctx, queryer, analysisID)
	if err != nil ||
		factRun.Stage != domain.AnalysisStageFacts ||
		factRun.Status != domain.AnalysisCompleted ||
		factRun.RequestedSourceIdentity != semanticRun.RequestedSourceIdentity ||
		!reflect.DeepEqual(factRun.Revision, semanticRun.Revision) ||
		!orderedSubset(semanticRun.InputFactIDs, factRun.FactIDs) {
		return ErrAgentRuntimeDataInvalid
	}
	available := make(map[string]bool, len(factRun.FactIDs))
	for _, factID := range factRun.FactIDs {
		if factID == "" || available[factID] {
			return ErrAgentRuntimeDataInvalid
		}
		available[factID] = true
	}
	for _, fact := range facts {
		if !available[fact.ID] {
			return ErrAgentRuntimeDataInvalid
		}
	}
	return nil
}

func orderedSubset(subset, values []string) bool {
	index := 0
	for _, value := range values {
		if index < len(subset) && subset[index] == value {
			index++
		}
	}
	return index == len(subset)
}
