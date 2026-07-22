package application

import (
	"context"
	"errors"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const semanticEventSchemaVersion = 1

// SemanticWorkflowRequest identifies one completed fact analysis and the exact
// local policy inputs that control its semantic processing identity.
type SemanticWorkflowRequest struct {
	FactAnalysisID string
	Bounds         EntryBounds
	Route          domain.ValidatedModelRoute
}

type SemanticWorkflowResult struct {
	Record SemanticAnalysisRecord `json:"record"`
	Reused bool                   `json:"reused"`
}

// SemanticWorkflow adds durable reuse and failure recording around the local
// semantic admission boundary. It does not own provider configuration or a
// concrete remote model adapter.
type SemanticWorkflow struct {
	Source   SessionEvidenceSource
	Facts    FactAnalysisStore
	Store    SemanticAnalysisStore
	Analyzer SemanticAnalyzer
}

func (workflow SemanticWorkflow) Run(
	ctx context.Context,
	request SemanticWorkflowRequest,
) (SemanticWorkflowResult, error) {
	if request.FactAnalysisID == "" || workflow.Source == nil || workflow.Facts == nil || workflow.Store == nil {
		return SemanticWorkflowResult{}, errors.New("semantic workflow is unavailable")
	}
	// Route configuration is a caller/composition concern. Reject it before a
	// fact analysis is loaded so a bad configuration does not become a run.
	if err := validateValidatedSemanticRoute(request.Route, workflow.Analyzer.Privacy); err != nil {
		return SemanticWorkflowResult{}, err
	}
	schema, err := semanticClaimOutputSchema()
	if err != nil {
		return SemanticWorkflowResult{}, errors.New("semantic output schema is unavailable")
	}
	factAnalysis, err := workflow.Facts.LoadFactAnalysis(ctx, request.FactAnalysisID)
	if err != nil || factAnalysis.Run.Stage != domain.AnalysisStageFacts ||
		factAnalysis.Run.Status != domain.AnalysisCompleted || factAnalysis.Run.Revision == nil {
		return SemanticWorkflowResult{}, errors.New("completed fact analysis is unavailable")
	}
	startedAt := workflow.Analyzer.now()
	baseDetails := SemanticAnalysisDetails{Schema: schema.Identity, Route: request.Route}
	document, err := workflow.Source.Read(ctx, factAnalysis.Run.RequestedSourceIdentity)
	if err != nil || document.Revision.Identity() != factAnalysis.Run.Revision.Identity() {
		return SemanticWorkflowResult{}, workflow.recordFailure(
			ctx, nil, factAnalysis, baseDetails, nil, "source-revision-unavailable", startedAt,
		)
	}

	prepared, err := workflow.Analyzer.prepare(SemanticAnalysisRequest{
		FactAnalysis: factAnalysis,
		Document:     document,
		Bounds:       request.Bounds,
		Route:        request.Route,
	})
	details := semanticDetailsFromPreparation(baseDetails, prepared)
	if err != nil {
		return SemanticWorkflowResult{}, workflow.recordFailure(
			ctx, nil, factAnalysis, details, prepared.RunSelection,
			semanticPreparationFailureCategory(err), startedAt,
		)
	}

	attempt, err := workflow.Store.BeginSemanticAttempt(ctx)
	if err != nil {
		return SemanticWorkflowResult{}, errors.New("semantic analysis persistence unavailable")
	}
	defer attempt.Rollback(ctx)
	existing, found, err := attempt.FindCompleted(ctx, *prepared.ProcessingKey)
	if err != nil {
		return SemanticWorkflowResult{}, errors.New("semantic analysis persistence unavailable")
	}
	if found {
		if err := attempt.Rollback(ctx); err != nil {
			return SemanticWorkflowResult{}, errors.New("semantic analysis persistence unavailable")
		}
		return SemanticWorkflowResult{Record: existing, Reused: true}, nil
	}

	analysisID, err := workflow.Analyzer.newID()
	if err != nil || analysisID == "" {
		return SemanticWorkflowResult{}, errors.New("semantic analysis identity is unavailable")
	}
	generation, err := workflow.Analyzer.generatePrepared(ctx, prepared)
	if err != nil {
		return SemanticWorkflowResult{}, workflow.recordFailure(
			ctx, attempt, factAnalysis, details, prepared.RunSelection,
			"semantic-generation-failed", startedAt,
			analysisID,
		)
	}
	model := generation.Model
	details.Model = &model
	result, err := workflow.Analyzer.admitPrepared(prepared, generation, analysisID, startedAt)
	if err != nil {
		return SemanticWorkflowResult{}, workflow.recordFailure(
			ctx, attempt, factAnalysis, details, prepared.RunSelection,
			semanticAdmissionFailureCategory(err), startedAt,
			analysisID,
		)
	}
	claimIDs := append([]string{}, result.Analysis.Run.ClaimIDs...)
	details.ClaimIDs = &claimIDs
	events, err := buildSemanticEvents(result.Analysis)
	if err != nil {
		details.ClaimIDs = nil
		return SemanticWorkflowResult{}, workflow.recordFailure(
			ctx, attempt, factAnalysis, details, prepared.RunSelection,
			"semantic-event-invalid", startedAt,
			analysisID,
		)
	}
	details.AttemptedProcessingKey = nil
	record := SemanticAnalysisRecord{Analysis: result.Analysis, Details: details, Events: events}
	if err := attempt.Commit(ctx, record); err != nil {
		return SemanticWorkflowResult{}, errors.New("semantic analysis persistence unavailable")
	}
	return SemanticWorkflowResult{Record: record}, nil
}

func semanticDetailsFromPreparation(
	base SemanticAnalysisDetails,
	prepared preparedSemanticAnalysis,
) SemanticAnalysisDetails {
	details := base
	if prepared.Schema.Name != "" {
		details.Schema = prepared.Schema
	}
	if prepared.InputFactIDs != nil {
		value := append([]string{}, (*prepared.InputFactIDs)...)
		details.InputFactIDs = &value
	}
	if prepared.InputDigest != nil {
		value := *prepared.InputDigest
		details.InputDigest = &value
	}
	if prepared.Selection != nil {
		value := *prepared.Selection
		details.Selection = &value
	}
	if prepared.Privacy != nil {
		value := *prepared.Privacy
		details.Privacy = &value
	}
	if prepared.ProcessingKey != nil {
		value := *prepared.ProcessingKey
		details.AttemptedProcessingKey = &value
	}
	return details
}

func (workflow SemanticWorkflow) recordFailure(
	ctx context.Context,
	attempt SemanticAnalysisAttempt,
	factAnalysis domain.FactAnalysis,
	details SemanticAnalysisDetails,
	selection *domain.EvidenceSelection,
	category string,
	startedAt time.Time,
	ids ...string,
) error {
	analysisID := ""
	if len(ids) > 0 {
		analysisID = ids[0]
	} else {
		var err error
		analysisID, err = workflow.Analyzer.newID()
		if err != nil || analysisID == "" {
			return errors.New("semantic analysis persistence unavailable")
		}
	}
	run := failedSemanticRun(
		analysisID, factAnalysis, details, selection, category, startedAt, workflow.Analyzer.now(),
	)
	if attempt != nil {
		if err := attempt.RecordFailure(ctx, run, details); err != nil {
			return errors.New("semantic analysis persistence unavailable")
		}
	} else {
		record := SemanticAnalysisRecord{
			Analysis: domain.SemanticAnalysis{Run: run, Claims: []domain.Claim{}},
			Details:  details,
			Events:   []domain.Event{},
		}
		if err := workflow.Store.RecordSemanticFailure(ctx, record); err != nil {
			return errors.New("semantic analysis persistence unavailable")
		}
	}
	return AnalysisError{AnalysisID: analysisID, Category: category}
}

func failedSemanticRun(
	analysisID string,
	factAnalysis domain.FactAnalysis,
	details SemanticAnalysisDetails,
	selection *domain.EvidenceSelection,
	category string,
	startedAt, finishedAt time.Time,
) domain.AnalysisRun {
	revision := factAnalysis.Run.Revision
	var inputFactIDs []string
	if details.InputFactIDs != nil {
		inputFactIDs = append([]string{}, (*details.InputFactIDs)...)
	}
	var runSelection *domain.EvidenceSelection
	if selection != nil {
		value := *selection
		runSelection = &value
	}
	var model *domain.ModelExecutionMetadata
	if details.Model != nil {
		value := *details.Model
		model = &value
	}
	return domain.AnalysisRun{
		ID: analysisID, Stage: domain.AnalysisStageClaims,
		RequestedSourceIdentity: factAnalysis.Run.RequestedSourceIdentity,
		Revision:                revision, Selection: runSelection,
		ExtractorName: SemanticExtractorName, ExtractorVersion: SemanticExtractorVersion,
		SchemaVersion: SemanticClaimSchemaVersion, FactIDs: []string{}, InputFactIDs: inputFactIDs,
		Model:     model,
		Omissions: factAnalysis.Run.Omissions, Status: domain.AnalysisFailed,
		Error: sanitizeFailure(category), StartedAt: startedAt, FinishedAt: finishedAt,
	}
}

func semanticPreparationFailureCategory(err error) string {
	var privacy PrivacyViolation
	switch {
	case errors.As(err, &privacy):
		return "semantic-preflight-blocked"
	case errors.Is(err, ErrSourceRevisionUnavailable):
		return "source-revision-unavailable"
	case errors.Is(err, ErrSemanticInputTooLarge):
		return "semantic-input-too-large"
	default:
		return "semantic-input-invalid"
	}
}

func semanticAdmissionFailureCategory(err error) string {
	var privacy PrivacyViolation
	if errors.As(err, &privacy) {
		return "semantic-postflight-blocked"
	}
	if errors.Is(err, ErrClaimCandidateInvalid) {
		return "claim-admission-invalid"
	}
	return "semantic-admission-invalid"
}

func buildSemanticEvents(analysis domain.SemanticAnalysis) ([]domain.Event, error) {
	events := make([]domain.Event, 0, len(analysis.Claims)+1)
	for _, claim := range analysis.Claims {
		evidence := append([]domain.EvidenceRef{}, claim.SupportingEvidence...)
		evidence = append(evidence, claim.ContradictingEvidence...)
		event, err := newSemanticEvent(
			"claim.admitted", "claim", claim.ID,
			map[string]any{
				"schemaVersion": semanticEventSchemaVersion,
				"claimId":       claim.ID,
				"analysisId":    analysis.Run.ID,
			},
			evidence, claim.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	completed, err := newSemanticEvent(
		"analysis.completed", "analysis", analysis.Run.ID,
		map[string]any{
			"schemaVersion": semanticEventSchemaVersion,
			"analysisId":    analysis.Run.ID,
			"claimIds":      append([]string{}, analysis.Run.ClaimIDs...),
		},
		[]domain.EvidenceRef{}, analysis.Run.FinishedAt,
	)
	if err != nil {
		return nil, err
	}
	return append(events, completed), nil
}

func newSemanticEvent(
	eventType, subjectType, subjectID string,
	payload map[string]any,
	evidence []domain.EvidenceRef,
	createdAt time.Time,
) (domain.Event, error) {
	if eventType == "" || subjectID == "" ||
		(subjectType != "claim" && subjectType != "analysis") || createdAt.IsZero() {
		return domain.Event{}, errors.New("semantic event is invalid")
	}
	fingerprint, err := EventFingerprint(eventType, subjectType, subjectID, payload)
	if err != nil {
		return domain.Event{}, errors.New("semantic event identity is unavailable")
	}
	return domain.Event{
		ID: platform.DerivedID("evt_", fingerprint), Fingerprint: fingerprint,
		Type: eventType, SubjectType: subjectType, SubjectID: subjectID,
		Payload: payload, Evidence: evidence, CreatedAt: createdAt.UTC(),
	}, nil
}
