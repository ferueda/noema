package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

type semanticQueryer interface {
	analysisRunQueryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func insertSemanticDetails(
	ctx context.Context,
	writer analysisRunWriter,
	analysisID string,
	details application.SemanticAnalysisDetails,
) error {
	route, err := encodeJSON(details.Route)
	if err != nil {
		return err
	}
	inputFactIDs, err := encodeOptionalJSON(details.InputFactIDs)
	if err != nil {
		return err
	}
	claimIDs, err := encodeOptionalJSON(details.ClaimIDs)
	if err != nil {
		return err
	}
	selection, err := encodeOptionalJSON(details.Selection)
	if err != nil {
		return err
	}
	privacy, err := encodeOptionalJSON(details.Privacy)
	if err != nil {
		return err
	}
	model, err := encodeOptionalJSON(details.Model)
	if err != nil {
		return err
	}
	if _, err := writer.ExecContext(ctx, `
		INSERT INTO semantic_analysis_details (
			analysis_run_id, schema_name, schema_version, schema_disposition,
			schema_digest, route_config_json, route_config_digest,
			input_fact_ids_json, claim_ids_json, input_digest,
			semantic_selection_json, privacy_report_json, model_json,
			attempted_processing_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, analysisID, details.Schema.Name, details.Schema.Version, details.Schema.Disposition,
		details.Schema.Digest, route, details.Route.ConfigDigest, inputFactIDs, claimIDs,
		nullableString(details.InputDigest), selection, privacy, model,
		nullableString(details.AttemptedProcessingKey)); err != nil {
		return fmt.Errorf("insert semantic analysis details: %w", err)
	}
	return nil
}

func insertClaim(ctx context.Context, writer analysisRunWriter, claim domain.Claim, ordinal int) error {
	supportingEvidence, err := encodeJSON(claim.SupportingEvidence)
	if err != nil {
		return err
	}
	contradictingEvidence, err := encodeJSON(claim.ContradictingEvidence)
	if err != nil {
		return err
	}
	supportingFactIDs, err := encodeJSON(claim.SupportingFactIDs)
	if err != nil {
		return err
	}
	requestedRoute, err := encodeJSON(claim.RequestedRoute)
	if err != nil {
		return err
	}
	if _, err := writer.ExecContext(ctx, `
		INSERT INTO claims (
			id, fingerprint, analysis_run_id, ordinal, type, statement, status,
			confidence, supporting_evidence_json, contradicting_evidence_json,
			supporting_fact_ids_json, outcome, actor, origin, subject, scope,
			attribution, extractor_name, extractor_version, schema_version,
			prompt_version, requested_route_json, resolved_provider,
			resolved_model, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, claim.ID, claim.Fingerprint, claim.AnalysisRunID, ordinal, claim.Type,
		claim.Statement, claim.Status, claim.Confidence, supportingEvidence,
		contradictingEvidence, supportingFactIDs, claim.Outcome, claim.Actor,
		claim.Origin, claim.Subject, claim.Scope, claim.Attribution,
		claim.ExtractorName, claim.ExtractorVersion, claim.SchemaVersion,
		claim.PromptVersion, requestedRoute, claim.ResolvedProvider,
		claim.ResolvedModel, formatTime(claim.CreatedAt)); err != nil {
		return fmt.Errorf("insert claim: %w", err)
	}
	return nil
}

func loadSemanticAnalysis(
	ctx context.Context,
	queryer semanticQueryer,
	id string,
) (application.SemanticAnalysisRecord, error) {
	run, err := loadAnalysisRun(ctx, queryer, id)
	if err != nil {
		return application.SemanticAnalysisRecord{}, err
	}
	if run.Stage != domain.AnalysisStageClaims {
		return application.SemanticAnalysisRecord{}, fmt.Errorf("analysis %s has stage %s, want claims", id, run.Stage)
	}
	details, err := readSemanticDetails(queryer.QueryRowContext(ctx, `
		SELECT schema_name, schema_version, schema_disposition, schema_digest,
		       route_config_json, route_config_digest, input_fact_ids_json,
		       claim_ids_json, input_digest, semantic_selection_json,
		       privacy_report_json, model_json, attempted_processing_key
		  FROM semantic_analysis_details WHERE analysis_run_id = ?
	`, id))
	if err != nil {
		return application.SemanticAnalysisRecord{}, fmt.Errorf("read semantic details: %w", err)
	}
	if details.InputFactIDs != nil {
		run.InputFactIDs = append([]string{}, (*details.InputFactIDs)...)
	}
	if details.ClaimIDs != nil {
		run.ClaimIDs = append([]string{}, (*details.ClaimIDs)...)
	}
	if details.Model != nil {
		model := *details.Model
		run.Model = &model
	}
	claims, err := loadClaims(ctx, queryer, id)
	if err != nil {
		return application.SemanticAnalysisRecord{}, err
	}
	events, err := loadSemanticEvents(ctx, queryer, id, claims)
	if err != nil {
		return application.SemanticAnalysisRecord{}, err
	}
	record := application.SemanticAnalysisRecord{
		Analysis: domain.SemanticAnalysis{Run: run, Claims: claims},
		Details:  details,
		Events:   events,
	}
	if run.Status == domain.AnalysisCompleted {
		if err := validateCompletedSemanticRecord(record); err != nil {
			return application.SemanticAnalysisRecord{}, fmt.Errorf("validate completed semantic analysis: %w", err)
		}
	} else if err := validateFailedSemanticRecord(record); err != nil {
		return application.SemanticAnalysisRecord{}, fmt.Errorf("validate failed semantic analysis: %w", err)
	}
	return record, nil
}

func readSemanticDetails(row rowScanner) (application.SemanticAnalysisDetails, error) {
	var details application.SemanticAnalysisDetails
	var route, routeDigest string
	var inputFactIDs, claimIDs, inputDigest, selection, privacy, model, attemptedKey sql.NullString
	if err := row.Scan(
		&details.Schema.Name, &details.Schema.Version, &details.Schema.Disposition,
		&details.Schema.Digest, &route, &routeDigest, &inputFactIDs, &claimIDs,
		&inputDigest, &selection, &privacy, &model, &attemptedKey,
	); err != nil {
		return application.SemanticAnalysisDetails{}, err
	}
	if err := decodeJSON(route, &details.Route); err != nil {
		return application.SemanticAnalysisDetails{}, err
	}
	if details.Route.ConfigDigest != routeDigest {
		return application.SemanticAnalysisDetails{}, errors.New("stored route configuration digest mismatch")
	}
	if inputFactIDs.Valid {
		value := []string{}
		if err := decodeJSON(inputFactIDs.String, &value); err != nil {
			return application.SemanticAnalysisDetails{}, err
		}
		details.InputFactIDs = &value
	}
	if claimIDs.Valid {
		value := []string{}
		if err := decodeJSON(claimIDs.String, &value); err != nil {
			return application.SemanticAnalysisDetails{}, err
		}
		details.ClaimIDs = &value
	}
	if inputDigest.Valid {
		details.InputDigest = &inputDigest.String
	}
	if selection.Valid {
		details.Selection = new(application.SemanticSelection)
		if err := decodeJSON(selection.String, details.Selection); err != nil {
			return application.SemanticAnalysisDetails{}, err
		}
	}
	if privacy.Valid {
		details.Privacy = new(application.PrivacyReport)
		if err := decodeJSON(privacy.String, details.Privacy); err != nil {
			return application.SemanticAnalysisDetails{}, err
		}
	}
	if model.Valid {
		details.Model = new(domain.ModelExecutionMetadata)
		if err := decodeJSON(model.String, details.Model); err != nil {
			return application.SemanticAnalysisDetails{}, err
		}
	}
	if attemptedKey.Valid {
		details.AttemptedProcessingKey = &attemptedKey.String
	}
	return details, nil
}

func loadClaims(ctx context.Context, queryer semanticQueryer, analysisID string) ([]domain.Claim, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, fingerprint, analysis_run_id, type, statement, status,
		       confidence, supporting_evidence_json, contradicting_evidence_json,
		       supporting_fact_ids_json, outcome, actor, origin, subject, scope,
		       attribution, extractor_name, extractor_version, schema_version,
		       prompt_version, requested_route_json, resolved_provider,
		       resolved_model, created_at
		  FROM claims WHERE analysis_run_id = ? ORDER BY ordinal
	`, analysisID)
	if err != nil {
		return nil, fmt.Errorf("query claims: %w", err)
	}
	defer rows.Close()
	claims := make([]domain.Claim, 0)
	for rows.Next() {
		claim, err := readClaim(rows)
		if err != nil {
			return nil, fmt.Errorf("read claim: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claims: %w", err)
	}
	return claims, nil
}

func readClaim(row rowScanner) (domain.Claim, error) {
	var claim domain.Claim
	var supportingEvidence, contradictingEvidence, supportingFactIDs, requestedRoute, createdAt string
	if err := row.Scan(
		&claim.ID, &claim.Fingerprint, &claim.AnalysisRunID, &claim.Type,
		&claim.Statement, &claim.Status, &claim.Confidence, &supportingEvidence,
		&contradictingEvidence, &supportingFactIDs, &claim.Outcome, &claim.Actor,
		&claim.Origin, &claim.Subject, &claim.Scope, &claim.Attribution,
		&claim.ExtractorName, &claim.ExtractorVersion, &claim.SchemaVersion,
		&claim.PromptVersion, &requestedRoute, &claim.ResolvedProvider,
		&claim.ResolvedModel, &createdAt,
	); err != nil {
		return domain.Claim{}, err
	}
	if err := decodeJSON(supportingEvidence, &claim.SupportingEvidence); err != nil {
		return domain.Claim{}, err
	}
	if err := decodeJSON(contradictingEvidence, &claim.ContradictingEvidence); err != nil {
		return domain.Claim{}, err
	}
	if err := decodeJSON(supportingFactIDs, &claim.SupportingFactIDs); err != nil {
		return domain.Claim{}, err
	}
	if err := decodeJSON(requestedRoute, &claim.RequestedRoute); err != nil {
		return domain.Claim{}, err
	}
	var err error
	claim.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Claim{}, err
	}
	return claim, nil
}

func loadSemanticEvents(
	ctx context.Context,
	queryer semanticQueryer,
	analysisID string,
	claims []domain.Claim,
) ([]domain.Event, error) {
	events := make([]domain.Event, 0, len(claims)+1)
	for _, claim := range claims {
		event, err := loadEvent(ctx, queryer, "claim.admitted", "claim", claim.ID)
		if err != nil {
			return nil, fmt.Errorf("load claim event %s: %w", claim.ID, err)
		}
		events = append(events, event)
	}
	completed, err := loadEvent(ctx, queryer, "analysis.completed", "analysis", analysisID)
	if errors.Is(err, sql.ErrNoRows) {
		if len(events) == 0 {
			return []domain.Event{}, nil
		}
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("load analysis event %s: %w", analysisID, err)
	}
	events = append(events, completed)
	return events, nil
}

func loadEvent(
	ctx context.Context,
	queryer semanticQueryer,
	eventType, subjectType, subjectID string,
) (domain.Event, error) {
	var event domain.Event
	var payload, evidence, createdAt string
	if err := queryer.QueryRowContext(ctx, `
		SELECT events.id, events.fingerprint, events.type,
		       event_subject_types.subject_type, events.subject_id,
		       events.payload_json, events.evidence_json, events.created_at
		  FROM events
		  JOIN event_subject_types ON event_subject_types.event_id = events.id
		 WHERE events.type = ? AND event_subject_types.subject_type = ?
		   AND events.subject_id = ?
	`, eventType, subjectType, subjectID).Scan(
		&event.ID, &event.Fingerprint, &event.Type, &event.SubjectType,
		&event.SubjectID, &payload, &evidence, &createdAt,
	); err != nil {
		return domain.Event{}, err
	}
	if err := decodeJSON(payload, &event.Payload); err != nil {
		return domain.Event{}, err
	}
	if err := decodeJSON(evidence, &event.Evidence); err != nil {
		return domain.Event{}, err
	}
	var err error
	event.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Event{}, err
	}
	return event, nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
