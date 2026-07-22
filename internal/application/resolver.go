package application

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/ferueda/noema/internal/domain"
)

var ErrSourceRevisionUnavailable = errors.New("source-revision-unavailable")

const maxResolvedSegmentBytes = 8 * 1024

type ResolvedEvidence struct {
	Reference         domain.EvidenceRef `json:"reference"`
	Text              string             `json:"text"`
	OriginalUTF8Bytes int                `json:"originalUtf8Bytes"`
	EmittedUTF8Bytes  int                `json:"emittedUtf8Bytes"`
	Truncated         bool               `json:"truncated"`
}

type ResolvedFactAnalysis struct {
	Analysis domain.FactAnalysis `json:"analysis"`
	Evidence []ResolvedEvidence  `json:"resolvedEvidence"`
}

type ResolvedSemanticAnalysis struct {
	Record   SemanticAnalysisRecord `json:"record"`
	Evidence []ResolvedEvidence     `json:"resolvedEvidence"`
}

type Resolver struct {
	Source SessionEvidenceSource
	Store  FactAnalysisStore
}

// EvidenceResolver resolves a bounded set of references against exactly one
// retained source revision. Callers own which admitted records supplied the
// references; this helper owns only the fail-closed source checks.
type EvidenceResolver struct {
	Source SessionEvidenceSource
}

type SemanticResolver struct {
	Source SessionEvidenceSource
	Store  SemanticAnalysisStore
}

func (resolver Resolver) Resolve(ctx context.Context, analysisID string) (ResolvedFactAnalysis, error) {
	analysis, err := resolver.Store.LoadFactAnalysis(ctx, analysisID)
	if err != nil {
		return ResolvedFactAnalysis{}, err
	}
	if analysis.Run.Revision == nil {
		return ResolvedFactAnalysis{}, ErrSourceRevisionUnavailable
	}
	references := make([]domain.EvidenceRef, 0)
	for _, fact := range analysis.Facts {
		references = append(references, fact.Evidence...)
	}
	resolved, err := (EvidenceResolver{Source: resolver.Source}).Resolve(
		ctx,
		analysis.Run.RequestedSourceIdentity,
		*analysis.Run.Revision,
		references,
	)
	if err != nil {
		return ResolvedFactAnalysis{}, err
	}
	return ResolvedFactAnalysis{Analysis: analysis, Evidence: resolved}, nil
}

func (resolver SemanticResolver) Resolve(ctx context.Context, analysisID string) (ResolvedSemanticAnalysis, error) {
	record, err := resolver.Store.LoadSemanticAnalysis(ctx, analysisID)
	if err != nil {
		return ResolvedSemanticAnalysis{}, err
	}
	run := record.Analysis.Run
	if run.Revision == nil {
		return ResolvedSemanticAnalysis{}, ErrSourceRevisionUnavailable
	}
	references := make([]domain.EvidenceRef, 0)
	for _, claim := range record.Analysis.Claims {
		references = append(references, claim.SupportingEvidence...)
		references = append(references, claim.ContradictingEvidence...)
	}
	resolved, err := (EvidenceResolver{Source: resolver.Source}).Resolve(
		ctx,
		run.RequestedSourceIdentity,
		*run.Revision,
		references,
	)
	if err != nil {
		return ResolvedSemanticAnalysis{}, err
	}
	return ResolvedSemanticAnalysis{Record: record, Evidence: resolved}, nil
}

func (resolver EvidenceResolver) Resolve(
	ctx context.Context,
	requestedSourceIdentity string,
	revision domain.EvidenceRevision,
	references []domain.EvidenceRef,
) ([]ResolvedEvidence, error) {
	if resolver.Source == nil || requestedSourceIdentity == "" {
		return nil, ErrSourceRevisionUnavailable
	}
	document, err := resolver.Source.Read(ctx, requestedSourceIdentity)
	if err != nil || document.Revision.Identity() != revision.Identity() {
		return nil, ErrSourceRevisionUnavailable
	}
	resolved := make([]ResolvedEvidence, 0, len(references))
	seen := make(map[string]domain.EvidenceRef, len(references))
	for _, ref := range references {
		if prior, ok := seen[ref.ID]; ok {
			if !sameEvidenceReference(prior, ref) {
				return nil, ErrSourceRevisionUnavailable
			}
			continue
		}
		if !referenceMatchesRevision(ref, revision) ||
			ref.EntryOrdinal < 0 || ref.EntryOrdinal >= len(document.Entries) {
			return nil, ErrSourceRevisionUnavailable
		}
		entry := document.Entries[ref.EntryOrdinal]
		if !referenceMatchesEntry(ref, entry) {
			return nil, ErrSourceRevisionUnavailable
		}
		if ref.SegmentOrdinal == nil {
			resolved = append(resolved, ResolvedEvidence{Reference: ref})
			seen[ref.ID] = ref
			continue
		}
		if *ref.SegmentOrdinal < 0 || *ref.SegmentOrdinal >= len(entry.Content) {
			return nil, ErrSourceRevisionUnavailable
		}
		segment := entry.Content[*ref.SegmentOrdinal]
		if segment.Text == nil || segment.Origin != ref.Origin ||
			segment.OriginConfidence != ref.OriginConfidence ||
			segment.Text.ContentHash.Scheme != ref.ContentHashScheme ||
			segment.Text.ContentHash.Digest != ref.ContentHash {
			return nil, ErrSourceRevisionUnavailable
		}
		text := segment.Text.Text
		emitted := len([]byte(text))
		truncated := false
		if emitted > maxResolvedSegmentBytes {
			text = truncateUTF8(text, maxResolvedSegmentBytes)
			emitted = len([]byte(text))
			truncated = true
		}
		resolved = append(resolved, ResolvedEvidence{
			Reference: ref, Text: text, OriginalUTF8Bytes: segment.Text.OriginalUTF8Bytes,
			EmittedUTF8Bytes: emitted, Truncated: truncated,
		})
		seen[ref.ID] = ref
	}
	return resolved, nil
}

func referenceMatchesRevision(ref domain.EvidenceRef, revision domain.EvidenceRevision) bool {
	return ref.ID != "" && ref.SourceKind == revision.SourceKind &&
		ref.SourceIdentity == revision.CanonicalID &&
		ref.DocumentDigestScheme == revision.DocumentDigest.Scheme &&
		ref.DocumentDigest == revision.DocumentDigest.Digest
}

func referenceMatchesEntry(ref domain.EvidenceRef, entry domain.EvidenceEntry) bool {
	return ref.EntryKind == entry.Kind && ref.Actor == entry.Actor &&
		sameOptionalInt(ref.RelatedEntryOrdinal, entry.RelatedEntryOrdinal) &&
		ref.ToolCallID == entry.ToolCallID && ref.ToolName == entry.ToolName &&
		ref.ToolNamespace == entry.ToolNamespace
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
