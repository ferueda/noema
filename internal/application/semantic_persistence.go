package application

import (
	"context"
	"errors"

	"github.com/ferueda/noema/internal/domain"
)

// SemanticAnalysisDetails contains semantic-only lineage. Pointer fields keep
// unavailable values distinct from values that are known to be empty.
type SemanticAnalysisDetails struct {
	Schema                 domain.StructuredOutputSchemaIdentity `json:"schema"`
	Route                  domain.ValidatedModelRoute            `json:"route"`
	InputFactIDs           *[]string                             `json:"inputFactIds,omitempty"`
	ClaimIDs               *[]string                             `json:"claimIds,omitempty"`
	InputDigest            *string                               `json:"inputDigest,omitempty"`
	Selection              *SemanticSelection                    `json:"selection,omitempty"`
	Privacy                *PrivacyReport                        `json:"privacy,omitempty"`
	Model                  *domain.ModelExecutionMetadata        `json:"model,omitempty"`
	AttemptedProcessingKey *string                               `json:"attemptedProcessingKey,omitempty"`
}

type SemanticAnalysisRecord struct {
	Analysis domain.SemanticAnalysis `json:"analysis"`
	Details  SemanticAnalysisDetails `json:"details"`
	Events   []domain.Event          `json:"events"`
}

// SemanticAnalysisStore owns durable semantic analyses and the one-at-a-time
// V0 write boundary. Model generation remains in the application layer.
type SemanticAnalysisStore interface {
	BeginSemanticAttempt(context.Context) (SemanticAnalysisAttempt, error)
	LoadSemanticAnalysis(context.Context, string) (SemanticAnalysisRecord, error)
	AnalysisStage(context.Context, string) (string, error)
	RecordSemanticFailure(context.Context, SemanticAnalysisRecord) error
}

// SemanticAnalysisAttempt is backed by one immediate SQLite transaction.
type SemanticAnalysisAttempt interface {
	FindCompleted(context.Context, string) (SemanticAnalysisRecord, bool, error)
	Commit(context.Context, SemanticAnalysisRecord) error
	RecordFailure(context.Context, domain.AnalysisRun, SemanticAnalysisDetails) error
	Rollback(context.Context) error
}

// ValidateSemanticProcessingLineage verifies that a completed run's durable
// lineage still derives the processing key used for exact reuse.
func ValidateSemanticProcessingLineage(run domain.AnalysisRun, details SemanticAnalysisDetails) error {
	return validateSemanticProcessingLineage(run, details, run.ProcessingKey)
}

// ValidateSemanticAttemptedProcessingLineage verifies the failure-only reuse
// identity once preparation advanced far enough to establish it.
func ValidateSemanticAttemptedProcessingLineage(run domain.AnalysisRun, details SemanticAnalysisDetails) error {
	if details.AttemptedProcessingKey == nil {
		return errors.New("attempted semantic processing identity is unavailable")
	}
	return validateSemanticProcessingLineage(run, details, *details.AttemptedProcessingKey)
}

func validateSemanticProcessingLineage(
	run domain.AnalysisRun,
	details SemanticAnalysisDetails,
	processingKey string,
) error {
	if run.Revision == nil || run.Selection == nil || details.Selection == nil ||
		details.InputFactIDs == nil || details.InputDigest == nil || details.Privacy == nil {
		return errors.New("semantic processing lineage is incomplete")
	}
	if err := ValidateSemanticSelectionProjection(*details.Selection, *run.Selection); err != nil {
		return err
	}
	want, err := SemanticProcessingKey(
		run.Revision.Identity(), *details.Selection, *details.InputFactIDs,
		*details.InputDigest, details.Schema, details.Route, details.Privacy.PolicyVersion,
	)
	if err != nil || want != processingKey {
		return errors.New("semantic processing identity does not match durable lineage")
	}
	return nil
}

// ValidateSemanticSelectionProjection checks the inspectable AnalysisRun view
// against the exact selection that crossed the generation boundary.
func ValidateSemanticSelectionProjection(
	selection SemanticSelection,
	projection domain.EvidenceSelection,
) error {
	if (selection.Mode != "complete" && selection.Mode != "range") ||
		selection.SelectedEntries < 0 || selection.TotalEntries < 0 ||
		selection.SelectedEntries > selection.TotalEntries ||
		selection.OriginalTextUTF8Bytes < 0 || selection.EmittedTextUTF8Bytes < 0 ||
		selection.TruncatedTextSegments < 0 || selection.TruncatedFactTexts < 0 ||
		selection.CanonicalOmittedSegments < 0 || selection.ExcludedFactCount < 0 ||
		(selection.Coverage != domain.CoverageCompleteRetainedSnapshot && selection.Coverage != semanticCoveragePartial) {
		return errors.New("semantic selection is invalid")
	}
	if selection.SelectedEntries == 0 {
		if selection.FirstOrdinal != nil || selection.LastOrdinal != nil || selection.Mode != "complete" {
			return errors.New("semantic selection ordinals are invalid")
		}
	} else if selection.FirstOrdinal == nil || selection.LastOrdinal == nil ||
		*selection.FirstOrdinal < 0 || *selection.LastOrdinal < *selection.FirstOrdinal ||
		*selection.LastOrdinal >= selection.TotalEntries ||
		*selection.LastOrdinal-*selection.FirstOrdinal+1 != selection.SelectedEntries {
		return errors.New("semantic selection ordinals are invalid")
	}
	if selection.Mode == "complete" && selection.SelectedEntries != selection.TotalEntries {
		return errors.New("complete semantic selection is partial")
	}
	partial := selection.SelectedEntries != selection.TotalEntries ||
		selection.TruncatedTextSegments > 0 || selection.TruncatedFactTexts > 0 ||
		selection.ExcludedFactCount > 0
	wantCoverage := domain.CoverageCompleteRetainedSnapshot
	if partial {
		wantCoverage = semanticCoveragePartial
	}
	if selection.Coverage != wantCoverage || projection.Mode != selection.Mode ||
		projection.Entries.Selected != selection.SelectedEntries ||
		projection.Entries.Total != selection.TotalEntries ||
		projection.Entries.Truncated != partial ||
		!sameOptionalInt(projection.Entries.FirstOrdinal, selection.FirstOrdinal) ||
		!sameOptionalInt(projection.Entries.LastOrdinal, selection.LastOrdinal) ||
		projection.Relations.Selected != 0 ||
		projection.Relations.Truncated != (projection.Relations.Total > 0) ||
		projection.Segments.Selected < 0 || projection.Segments.Total < projection.Segments.Selected ||
		projection.Segments.Truncated != partial ||
		projection.SegmentText.EmittedUTF8Bytes != selection.EmittedTextUTF8Bytes ||
		projection.SegmentText.OriginalUTF8Bytes != selection.OriginalTextUTF8Bytes ||
		projection.SegmentText.Truncated != (selection.TruncatedTextSegments > 0) ||
		projection.CanonicalOmittedSegments != selection.CanonicalOmittedSegments ||
		projection.TruncatedTextSegments != selection.TruncatedTextSegments ||
		projection.Coverage != selection.Coverage {
		return errors.New("semantic selection projection does not match exact selection")
	}
	return nil
}
