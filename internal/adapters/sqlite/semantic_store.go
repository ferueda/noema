package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

type semanticQueryer interface {
	analysisRunQueryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type semanticAttempt struct {
	connection *sql.Conn
	closed     bool
}

func (store *Store) AnalysisStage(ctx context.Context, id string) (string, error) {
	var stage string
	if err := store.database.QueryRowContext(ctx,
		"SELECT stage FROM analysis_runs WHERE id = ?", id,
	).Scan(&stage); err != nil {
		return "", fmt.Errorf("read analysis stage %s: %w", id, err)
	}
	return stage, nil
}

func (store *Store) LoadSemanticAnalysis(
	ctx context.Context,
	id string,
) (application.SemanticAnalysisRecord, error) {
	record, err := loadSemanticAnalysis(ctx, store.database, id)
	if err != nil {
		return application.SemanticAnalysisRecord{}, fmt.Errorf("read semantic analysis %s: %w", id, err)
	}
	return record, nil
}

func (store *Store) RecordSemanticFailure(
	ctx context.Context,
	record application.SemanticAnalysisRecord,
) error {
	if err := validateFailedSemanticRecord(record); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin semantic failure transaction: %w", err)
	}
	defer transaction.Rollback()
	if _, err := insertAnalysisRun(ctx, transaction, record.Analysis.Run, false); err != nil {
		return err
	}
	if err := insertSemanticDetails(ctx, transaction, record.Analysis.Run.ID, record.Details); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit semantic failure transaction: %w", err)
	}
	return nil
}

func (store *Store) BeginSemanticAttempt(ctx context.Context) (application.SemanticAnalysisAttempt, error) {
	connection, err := store.database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire semantic attempt connection: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		connection.Close()
		return nil, fmt.Errorf("begin immediate semantic attempt: %w", err)
	}
	return &semanticAttempt{connection: connection}, nil
}

func (attempt *semanticAttempt) FindCompleted(
	ctx context.Context,
	processingKey string,
) (application.SemanticAnalysisRecord, bool, error) {
	if err := attempt.requireOpen(); err != nil {
		return application.SemanticAnalysisRecord{}, false, err
	}
	var id string
	err := attempt.connection.QueryRowContext(ctx, `
		SELECT id FROM analysis_runs
		 WHERE processing_key = ? AND status = 'completed' AND stage = 'claims'
	`, processingKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SemanticAnalysisRecord{}, false, nil
	}
	if err != nil {
		return application.SemanticAnalysisRecord{}, false, fmt.Errorf("find completed semantic analysis: %w", err)
	}
	record, err := loadSemanticAnalysis(ctx, attempt.connection, id)
	if err != nil {
		return application.SemanticAnalysisRecord{}, false, err
	}
	return record, true, nil
}

func (attempt *semanticAttempt) Commit(
	ctx context.Context,
	record application.SemanticAnalysisRecord,
) error {
	if err := attempt.requireOpen(); err != nil {
		return err
	}
	if err := validateCompletedSemanticRecord(record); err != nil {
		return err
	}
	if _, err := insertAnalysisRun(ctx, attempt.connection, record.Analysis.Run, false); err != nil {
		return err
	}
	if err := insertSemanticDetails(ctx, attempt.connection, record.Analysis.Run.ID, record.Details); err != nil {
		return err
	}
	for ordinal, claim := range record.Analysis.Claims {
		if err := insertClaim(ctx, attempt.connection, claim, ordinal); err != nil {
			return err
		}
	}
	for _, event := range record.Events {
		if err := insertEvent(ctx, attempt.connection, event); err != nil {
			return err
		}
	}
	return attempt.commit(ctx)
}

func (attempt *semanticAttempt) RecordFailure(
	ctx context.Context,
	run domain.AnalysisRun,
	details application.SemanticAnalysisDetails,
) error {
	if err := attempt.requireOpen(); err != nil {
		return err
	}
	record := application.SemanticAnalysisRecord{
		Analysis: domain.SemanticAnalysis{Run: run, Claims: []domain.Claim{}},
		Details:  details,
		Events:   []domain.Event{},
	}
	if err := validateFailedSemanticRecord(record); err != nil {
		return err
	}
	if _, err := insertAnalysisRun(ctx, attempt.connection, run, false); err != nil {
		return err
	}
	if err := insertSemanticDetails(ctx, attempt.connection, run.ID, details); err != nil {
		return err
	}
	return attempt.commit(ctx)
}

func (attempt *semanticAttempt) Rollback(ctx context.Context) error {
	if attempt.closed {
		return nil
	}
	attempt.closed = true
	_, rollbackErr := attempt.connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
	closeErr := attempt.connection.Close()
	if rollbackErr != nil {
		return fmt.Errorf("rollback semantic attempt: %w", rollbackErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close semantic attempt connection: %w", closeErr)
	}
	return nil
}

func (attempt *semanticAttempt) requireOpen() error {
	if attempt.closed || attempt.connection == nil {
		return errors.New("semantic attempt is closed")
	}
	return nil
}

func (attempt *semanticAttempt) commit(ctx context.Context) error {
	if _, err := attempt.connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit semantic attempt: %w", err)
	}
	attempt.closed = true
	_ = attempt.connection.Close()
	return nil
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
	fingerprint, err := platform.Fingerprint(struct {
		Type        string
		SubjectType string
		SubjectID   string
		Payload     map[string]any
	}{event.Type, event.SubjectType, event.SubjectID, event.Payload})
	return err == nil && fingerprint == event.Fingerprint &&
		event.ID == platform.DerivedID("evt_", fingerprint)
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
