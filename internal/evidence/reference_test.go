package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/domain"
)

func TestSessionsReferencePreservesStableEntryAndSegmentIdentities(t *testing.T) {
	document := referenceDocument()

	entryRef, err := SessionsReference(document, 0, nil)
	if err != nil {
		t.Fatalf("build entry reference: %v", err)
	}
	if entryRef.ID != "eref_975931f4231050ca2bd22ffadc378ea4" {
		t.Fatalf("entry reference ID = %q", entryRef.ID)
	}
	if entryRef.SegmentOrdinal != nil || entryRef.ContentHash != "" || entryRef.Excerpt != "" {
		t.Fatalf("entry reference = %#v", entryRef)
	}

	segmentOrdinal := 0
	segmentRef, err := SessionsReference(document, 0, &segmentOrdinal)
	if err != nil {
		t.Fatalf("build segment reference: %v", err)
	}
	if segmentRef.ID != "eref_52f8d0da6eb60e0fbd7718b13a7174ca" {
		t.Fatalf("segment reference ID = %q", segmentRef.ID)
	}
	if segmentRef.SegmentOrdinal == nil || *segmentRef.SegmentOrdinal != 0 ||
		segmentRef.ContentHash != document.Entries[0].Content[0].Text.ContentHash.Digest ||
		segmentRef.Excerpt != "" {
		t.Fatalf("segment reference = %#v", segmentRef)
	}
}

func TestSessionsReferenceClonesRelatedEntryCoordinate(t *testing.T) {
	document := referenceDocument()
	related := 7
	document.Entries[0].RelatedEntryOrdinal = &related

	ref, err := SessionsReference(document, 0, nil)
	if err != nil {
		t.Fatalf("build entry reference: %v", err)
	}
	*document.Entries[0].RelatedEntryOrdinal = 9
	if ref.RelatedEntryOrdinal == nil || *ref.RelatedEntryOrdinal != 7 {
		t.Fatalf("related coordinate changed through source alias: %#v", ref.RelatedEntryOrdinal)
	}
}

func TestSessionsReferenceRejectsInvalidCoordinates(t *testing.T) {
	for _, test := range []struct {
		name     string
		document domain.EvidenceDocument
		entry    int
		segment  *int
	}{
		{name: "negative entry", document: referenceDocument(), entry: -1},
		{name: "entry past end", document: referenceDocument(), entry: 1},
		{name: "negative segment", document: referenceDocument(), entry: 0, segment: intPointer(-1)},
		{name: "segment past end", document: referenceDocument(), entry: 0, segment: intPointer(1)},
		{name: "inconsistent entry coordinate", document: mutateEntryOrdinal(referenceDocument(), 1), entry: 0},
		{name: "inconsistent segment coordinate", document: mutateSegmentOrdinal(referenceDocument(), 1), entry: 0, segment: intPointer(0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := SessionsReference(test.document, test.entry, test.segment); err == nil {
				t.Fatal("reference succeeded, want coordinate error")
			}
		})
	}
}

func TestValidateSessionsReferenceAcceptsExactDecodedCoordinates(t *testing.T) {
	document := referenceDocument()
	segment := 0
	ref, err := SessionsReference(document, 0, &segment)
	if err != nil {
		t.Fatalf("build segment reference: %v", err)
	}
	decodedSegment := *ref.SegmentOrdinal
	ref.SegmentOrdinal = &decodedSegment

	if err := ValidateSessionsReference(document, ref); err != nil {
		t.Fatalf("validate exact reference: %v", err)
	}
}

func TestValidateSessionsReferenceRejectsTampering(t *testing.T) {
	document := referenceDocument()
	segment := 0
	entryRef, err := SessionsReference(document, 0, nil)
	if err != nil {
		t.Fatalf("build entry reference: %v", err)
	}
	segmentRef, err := SessionsReference(document, 0, &segment)
	if err != nil {
		t.Fatalf("build segment reference: %v", err)
	}

	for _, test := range []struct {
		name string
		ref  domain.EvidenceRef
	}{
		{name: "identity", ref: mutateReference(entryRef, func(ref *domain.EvidenceRef) { ref.ID = "eref_tampered" })},
		{name: "entry metadata", ref: mutateReference(entryRef, func(ref *domain.EvidenceRef) { ref.ToolName = "other-tool" })},
		{name: "segment metadata", ref: mutateReference(segmentRef, func(ref *domain.EvidenceRef) { ref.Origin = "human" })},
		{name: "content hash", ref: mutateReference(segmentRef, func(ref *domain.EvidenceRef) { ref.ContentHash = strings.Repeat("a", 64) })},
		{name: "document digest", ref: mutateReference(segmentRef, func(ref *domain.EvidenceRef) { ref.DocumentDigest = strings.Repeat("e", 64) })},
		{name: "digest scheme", ref: mutateReference(segmentRef, func(ref *domain.EvidenceRef) { ref.DocumentDigestScheme = "other" })},
		{name: "excerpt", ref: mutateReference(segmentRef, func(ref *domain.EvidenceRef) { ref.Excerpt = "private source text" })},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateSessionsReference(document, test.ref); err == nil {
				t.Fatal("validation succeeded, want tampering error")
			}
		})
	}
}

func referenceDocument() domain.EvidenceDocument {
	text := `{"cmd":"go test ./..."}`
	hash := sha256.Sum256([]byte(text))
	return domain.EvidenceDocument{
		Revision: domain.EvidenceRevision{
			SourceKind:  domain.EvidenceSourceSessions,
			CanonicalID: "synthetic@local:one",
			DocumentDigest: domain.Digest{
				Scheme: "sha256-sessions-document-jcs-v1",
				Digest: strings.Repeat("d", 64),
			},
		},
		Entries: []domain.EvidenceEntry{{
			Ordinal: 0, Kind: "tool-call", Actor: "model", ToolCallID: "call", ToolName: "exec_command",
			Content: []domain.EvidenceSegment{{
				Ordinal: 0, Kind: "text", Origin: "model", OriginConfidence: "high",
				Text: &domain.SelectedText{
					Text: text, OriginalUTF8Bytes: len(text), EmittedUTF8Bytes: len(text),
					ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: hex.EncodeToString(hash[:])},
				},
			}},
		}},
	}
}

func mutateEntryOrdinal(document domain.EvidenceDocument, ordinal int) domain.EvidenceDocument {
	document.Entries[0].Ordinal = ordinal
	return document
}

func mutateSegmentOrdinal(document domain.EvidenceDocument, ordinal int) domain.EvidenceDocument {
	document.Entries[0].Content[0].Ordinal = ordinal
	return document
}

func mutateReference(ref domain.EvidenceRef, mutate func(*domain.EvidenceRef)) domain.EvidenceRef {
	mutate(&ref)
	return ref
}

func intPointer(value int) *int { return &value }
