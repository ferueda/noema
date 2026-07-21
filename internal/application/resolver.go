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

type Resolver struct {
	Source SessionEvidenceSource
	Store  FactAnalysisStore
}

func (resolver Resolver) Resolve(ctx context.Context, analysisID string) (ResolvedFactAnalysis, error) {
	analysis, err := resolver.Store.LoadFactAnalysis(ctx, analysisID)
	if err != nil {
		return ResolvedFactAnalysis{}, err
	}
	if analysis.Run.Revision == nil {
		return ResolvedFactAnalysis{}, ErrSourceRevisionUnavailable
	}
	document, err := resolver.Source.Read(ctx, analysis.Run.RequestedSourceIdentity)
	if err != nil || document.Revision.DocumentDigest != analysis.Run.Revision.DocumentDigest {
		return ResolvedFactAnalysis{}, ErrSourceRevisionUnavailable
	}
	resolved := make([]ResolvedEvidence, 0)
	seen := make(map[string]bool)
	for _, fact := range analysis.Facts {
		for _, ref := range fact.Evidence {
			if seen[ref.ID] {
				continue
			}
			if ref.EntryOrdinal < 0 || ref.EntryOrdinal >= len(document.Entries) {
				return ResolvedFactAnalysis{}, ErrSourceRevisionUnavailable
			}
			entry := document.Entries[ref.EntryOrdinal]
			if ref.SegmentOrdinal == nil {
				resolved = append(resolved, ResolvedEvidence{Reference: ref})
				seen[ref.ID] = true
				continue
			}
			if *ref.SegmentOrdinal < 0 || *ref.SegmentOrdinal >= len(entry.Content) {
				return ResolvedFactAnalysis{}, ErrSourceRevisionUnavailable
			}
			segment := entry.Content[*ref.SegmentOrdinal]
			if segment.Text == nil || segment.Text.ContentHash.Digest != ref.ContentHash {
				return ResolvedFactAnalysis{}, ErrSourceRevisionUnavailable
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
			seen[ref.ID] = true
		}
	}
	return ResolvedFactAnalysis{Analysis: analysis, Evidence: resolved}, nil
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
