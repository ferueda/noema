package evidence

import (
	"errors"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

// SessionsReference builds the stable reference for one canonical Sessions
// entry or segment. Its identity shape is part of Noema's durable evidence
// contract, so changing the fingerprint fields or their order changes IDs.
func SessionsReference(
	document domain.EvidenceDocument,
	entryOrdinal int,
	segmentOrdinal *int,
) (domain.EvidenceRef, error) {
	if entryOrdinal < 0 || entryOrdinal >= len(document.Entries) {
		return domain.EvidenceRef{}, errors.New("evidence entry ordinal is out of range")
	}
	entry := document.Entries[entryOrdinal]
	if entry.Ordinal != entryOrdinal {
		return domain.EvidenceRef{}, errors.New("evidence entry coordinate is inconsistent")
	}

	ref := domain.EvidenceRef{
		SourceKind:           domain.EvidenceSourceSessions,
		SourceIdentity:       document.Revision.CanonicalID,
		DocumentDigestScheme: document.Revision.DocumentDigest.Scheme,
		DocumentDigest:       document.Revision.DocumentDigest.Digest,
		EntryOrdinal:         entry.Ordinal,
		EntryKind:            entry.Kind,
		Actor:                entry.Actor,
		RelatedEntryOrdinal:  cloneOptionalInt(entry.RelatedEntryOrdinal),
		ToolCallID:           entry.ToolCallID,
		ToolName:             entry.ToolName,
		ToolNamespace:        entry.ToolNamespace,
	}
	if segmentOrdinal != nil {
		if *segmentOrdinal < 0 || *segmentOrdinal >= len(entry.Content) {
			return domain.EvidenceRef{}, errors.New("evidence segment ordinal is out of range")
		}
		segment := entry.Content[*segmentOrdinal]
		if segment.Ordinal != *segmentOrdinal {
			return domain.EvidenceRef{}, errors.New("evidence segment coordinate is inconsistent")
		}
		ordinal := segment.Ordinal
		ref.SegmentOrdinal = &ordinal
		ref.Origin = segment.Origin
		ref.OriginConfidence = segment.OriginConfidence
		if segment.Text != nil {
			ref.ContentHashScheme = segment.Text.ContentHash.Scheme
			ref.ContentHash = segment.Text.ContentHash.Digest
		}
	}

	fingerprint, err := platform.Fingerprint(struct {
		Source  string
		Digest  string
		Entry   int
		Segment *int
		Hash    string
	}{ref.SourceIdentity, ref.DocumentDigest, ref.EntryOrdinal, ref.SegmentOrdinal, ref.ContentHash})
	if err != nil {
		return domain.EvidenceRef{}, errors.New("fingerprint evidence reference")
	}
	ref.ID = platform.DerivedID("eref_", fingerprint)
	return ref, nil
}

// ValidateSessionsReference requires a stored reference to match the canonical
// Sessions coordinate exactly. Pointer fields are compared by value so a
// decoded reference does not need to share memory with the source document.
func ValidateSessionsReference(document domain.EvidenceDocument, ref domain.EvidenceRef) error {
	expected, err := SessionsReference(document, ref.EntryOrdinal, ref.SegmentOrdinal)
	if err != nil {
		return err
	}
	if ref.Excerpt != "" {
		return errors.New("evidence reference contains an excerpt")
	}
	if !sameReference(expected, ref) {
		return errors.New("evidence reference does not match the canonical coordinate")
	}
	return nil
}

func sameReference(left, right domain.EvidenceRef) bool {
	return left.ID == right.ID &&
		left.SourceKind == right.SourceKind &&
		left.SourceIdentity == right.SourceIdentity &&
		left.DocumentDigestScheme == right.DocumentDigestScheme &&
		left.DocumentDigest == right.DocumentDigest &&
		left.EntryOrdinal == right.EntryOrdinal &&
		sameOptionalInt(left.SegmentOrdinal, right.SegmentOrdinal) &&
		left.EntryKind == right.EntryKind &&
		left.Actor == right.Actor &&
		left.Origin == right.Origin &&
		left.OriginConfidence == right.OriginConfidence &&
		sameOptionalInt(left.RelatedEntryOrdinal, right.RelatedEntryOrdinal) &&
		left.ToolCallID == right.ToolCallID &&
		left.ToolName == right.ToolName &&
		left.ToolNamespace == right.ToolNamespace &&
		left.ContentHashScheme == right.ContentHashScheme &&
		left.ContentHash == right.ContentHash &&
		left.Excerpt == right.Excerpt
}

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
