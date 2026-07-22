package sqlite

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func validateCompletedSemanticRecord(record application.SemanticAnalysisRecord) error {
	run := record.Analysis.Run
	if err := validateSemanticBase(run, record.Details); err != nil {
		return err
	}
	if run.Status != domain.AnalysisCompleted || run.ProcessingKey == "" ||
		record.Details.InputFactIDs == nil || record.Details.ClaimIDs == nil ||
		record.Details.InputDigest == nil || record.Details.Selection == nil ||
		record.Details.Privacy == nil || record.Details.Model == nil ||
		record.Details.AttemptedProcessingKey != nil {
		return errors.New("completed semantic analysis details are incomplete")
	}
	if !slices.Equal(run.InputFactIDs, *record.Details.InputFactIDs) ||
		!slices.Equal(run.ClaimIDs, *record.Details.ClaimIDs) ||
		len(record.Analysis.Claims) != len(*record.Details.ClaimIDs) {
		return errors.New("semantic analysis ordered identities do not match details")
	}
	if run.Revision == nil || run.Selection == nil || run.Model == nil ||
		!reflect.DeepEqual(run.Model, record.Details.Model) ||
		run.SchemaVersion != record.Details.Schema.Version ||
		record.Details.Privacy.PolicyVersion != record.Details.Route.Requested.PrivacyPolicyVersion ||
		record.Details.Model.RequestedRoute != record.Details.Route.Requested {
		return errors.New("semantic analysis completed lineage does not match details")
	}
	if err := application.ValidateSemanticProcessingLineage(run, record.Details); err != nil {
		return err
	}
	inputFacts := make(map[string]bool, len(*record.Details.InputFactIDs))
	for _, id := range *record.Details.InputFactIDs {
		if id == "" || inputFacts[id] {
			return errors.New("semantic input fact identities are invalid")
		}
		inputFacts[id] = true
	}
	for index, claim := range record.Analysis.Claims {
		fingerprint, err := application.ClaimFingerprint(run.ProcessingKey, claim)
		if err != nil || fingerprint != claim.Fingerprint {
			return errors.New("stored claim fingerprint does not match claim contents")
		}
		if claim.ID != (*record.Details.ClaimIDs)[index] || claim.AnalysisRunID != run.ID ||
			claim.ID != platform.DerivedID("claim_", fingerprint) ||
			claim.Fingerprint == "" || !claim.Type.Valid() || !claim.Status.Valid() ||
			claim.ExtractorName != run.ExtractorName || claim.ExtractorVersion != run.ExtractorVersion ||
			claim.SchemaVersion != run.SchemaVersion || claim.PromptVersion != record.Details.Model.PromptVersion ||
			claim.RequestedRoute != record.Details.Route.Requested ||
			claim.ResolvedProvider != record.Details.Model.ResolvedProvider ||
			claim.ResolvedModel != record.Details.Model.ResolvedModel {
			return errors.New("stored claim identity or order is invalid")
		}
		for _, factID := range claim.SupportingFactIDs {
			if !inputFacts[factID] {
				return errors.New("claim references a fact outside the semantic input")
			}
		}
	}
	if len(record.Events) != len(record.Analysis.Claims)+1 {
		return errors.New("semantic event count is invalid")
	}
	for index, claim := range record.Analysis.Claims {
		event := record.Events[index]
		wantEvidence := append([]domain.EvidenceRef{}, claim.SupportingEvidence...)
		wantEvidence = append(wantEvidence, claim.ContradictingEvidence...)
		if event.Type != "claim.admitted" || event.SubjectType != "claim" || event.SubjectID != claim.ID ||
			!eventSchemaVersionOne(event.Payload) || eventPayloadString(event.Payload, "claimId") != claim.ID ||
			eventPayloadString(event.Payload, "analysisId") != run.ID ||
			!reflect.DeepEqual(event.Evidence, wantEvidence) || event.CreatedAt != claim.CreatedAt ||
			!validSemanticEventIdentity(event) {
			return errors.New("claim event subject or order is invalid")
		}
	}
	completed := record.Events[len(record.Events)-1]
	if completed.Type != "analysis.completed" || completed.SubjectType != "analysis" ||
		completed.SubjectID != run.ID || !eventSchemaVersionOne(completed.Payload) ||
		eventPayloadString(completed.Payload, "analysisId") != run.ID ||
		!slices.Equal(eventPayloadStrings(completed.Payload, "claimIds"), run.ClaimIDs) ||
		len(completed.Evidence) != 0 || completed.CreatedAt != run.FinishedAt ||
		!validSemanticEventIdentity(completed) {
		return errors.New("analysis completion event subject is invalid")
	}
	return nil
}

func eventSchemaVersionOne(payload map[string]any) bool {
	value, ok := payload["schemaVersion"]
	if !ok {
		return false
	}
	switch version := value.(type) {
	case int:
		return version == 1
	case float64:
		return version == 1
	default:
		return false
	}
}

func validateFailedSemanticRecord(record application.SemanticAnalysisRecord) error {
	run := record.Analysis.Run
	if err := validateSemanticBase(run, record.Details); err != nil {
		return err
	}
	if run.Status != domain.AnalysisFailed || run.ProcessingKey != "" ||
		record.Details.ClaimIDs != nil || len(record.Analysis.Claims) != 0 ||
		len(record.Events) != 0 || run.Revision == nil || run.Error == "" ||
		len(run.Error) > 1024 || !sameOptionalStrings(run.InputFactIDs, record.Details.InputFactIDs) ||
		!reflect.DeepEqual(run.Model, record.Details.Model) {
		return errors.New("failed semantic analysis contains admitted output")
	}
	if (run.Selection == nil) != (record.Details.Selection == nil) {
		return errors.New("failed semantic analysis selection lineage is incomplete")
	}
	if record.Details.Selection != nil {
		if err := application.ValidateSemanticSelectionProjection(*record.Details.Selection, *run.Selection); err != nil {
			return err
		}
	}
	if record.Details.Selection != nil &&
		(record.Details.InputFactIDs == nil || record.Details.Privacy == nil) {
		return errors.New("failed semantic analysis selection lacks preparation lineage")
	}
	if record.Details.InputDigest != nil && record.Details.Selection == nil {
		return errors.New("failed semantic analysis digest lacks selection")
	}
	if record.Details.AttemptedProcessingKey != nil && record.Details.InputDigest == nil {
		return errors.New("failed semantic analysis processing key lacks input digest")
	}
	if record.Details.Model != nil && record.Details.AttemptedProcessingKey == nil {
		return errors.New("failed semantic analysis model lacks processing identity")
	}
	if record.Details.AttemptedProcessingKey != nil {
		if err := application.ValidateSemanticAttemptedProcessingLineage(run, record.Details); err != nil {
			return err
		}
	}
	return nil
}

func validateSemanticBase(run domain.AnalysisRun, details application.SemanticAnalysisDetails) error {
	if run.ID == "" || run.Stage != domain.AnalysisStageClaims || run.RequestedSourceIdentity == "" ||
		run.ExtractorName == "" || run.ExtractorVersion == "" || run.SchemaVersion < 1 ||
		details.Schema.Name != application.SemanticClaimSchemaName ||
		details.Schema.Version != application.SemanticClaimSchemaVersion ||
		details.Schema.Disposition != domain.StructuredOutputDispositionStrict ||
		details.Schema.Digest == "" || details.Route.ConfigDigest == "" ||
		details.Route.Requested.Alias == "" || len(details.Route.SanitizedConfig) == 0 ||
		!json.Valid(details.Route.SanitizedConfig) || run.SchemaVersion != details.Schema.Version ||
		run.StartedAt.IsZero() || run.FinishedAt.IsZero() || run.FinishedAt.Before(run.StartedAt) {
		return errors.New("semantic analysis lineage is incomplete")
	}
	var routeObject map[string]any
	if err := json.Unmarshal(details.Route.SanitizedConfig, &routeObject); err != nil || routeObject == nil {
		return errors.New("semantic route configuration is invalid")
	}
	digest, err := platform.Fingerprint(json.RawMessage(details.Route.SanitizedConfig))
	if err != nil || digest != details.Route.ConfigDigest {
		return errors.New("semantic route configuration identity is invalid")
	}
	return nil
}

func sameOptionalStrings(run []string, details *[]string) bool {
	if details == nil {
		return run == nil
	}
	return run != nil && slices.Equal(run, *details)
}

func eventPayloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func eventPayloadStrings(payload map[string]any, key string) []string {
	switch values := payload[key].(type) {
	case []string:
		return append([]string{}, values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil
			}
			result = append(result, text)
		}
		return result
	default:
		return nil
	}
}

func validSemanticEventIdentity(event domain.Event) bool {
	fingerprint, err := application.EventFingerprint(
		event.Type,
		event.SubjectType,
		event.SubjectID,
		event.Payload,
	)
	return err == nil && fingerprint == event.Fingerprint &&
		event.ID == platform.DerivedID("evt_", fingerprint)
}
