package domain

import "time"

const (
	EvidenceSourceSessions           = "sessions"
	CoverageCompleteRetainedSnapshot = "complete-retained-snapshot"
)

type Digest struct {
	Scheme string `json:"scheme"`
	Digest string `json:"digest"`
}

type EvidenceRevision struct {
	SourceKind       string     `json:"sourceKind"`
	CanonicalID      string     `json:"canonicalId"`
	NativeSourceKind string     `json:"nativeSourceKind"`
	SourceInstanceID string     `json:"sourceInstanceId"`
	NativeID         string     `json:"nativeId"`
	SchemaVersion    int        `json:"schemaVersion"`
	Disposition      string     `json:"disposition"`
	DocumentDigest   Digest     `json:"documentDigest"`
	AdapterVersion   string     `json:"adapterVersion"`
	SourceState      string     `json:"sourceState"`
	Freshness        string     `json:"freshness"`
	CapturedAt       time.Time  `json:"capturedAt"`
	SourceObservedAt time.Time  `json:"sourceObservedAt"`
	CreatedAt        *time.Time `json:"createdAt,omitempty"`
	UpdatedAt        *time.Time `json:"updatedAt,omitempty"`
	LineageCoverage  string     `json:"lineageCoverage"`
}

type EvidenceRevisionIdentity struct {
	SourceKind       string `json:"sourceKind"`
	CanonicalID      string `json:"canonicalId"`
	NativeSourceKind string `json:"nativeSourceKind"`
	SourceInstanceID string `json:"sourceInstanceId"`
	NativeID         string `json:"nativeId"`
	DocumentDigest   Digest `json:"documentDigest"`
}

func (revision EvidenceRevision) Identity() EvidenceRevisionIdentity {
	return EvidenceRevisionIdentity{
		SourceKind: revision.SourceKind, CanonicalID: revision.CanonicalID,
		NativeSourceKind: revision.NativeSourceKind, SourceInstanceID: revision.SourceInstanceID,
		NativeID: revision.NativeID, DocumentDigest: revision.DocumentDigest,
	}
}

type CountSelection struct {
	Selected  int  `json:"selected"`
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
}

type EntrySelection struct {
	Selected     int  `json:"selected"`
	Total        int  `json:"total"`
	Truncated    bool `json:"truncated"`
	FirstOrdinal *int `json:"firstOrdinal"`
	LastOrdinal  *int `json:"lastOrdinal"`
}

type ByteSelection struct {
	EmittedUTF8Bytes  int  `json:"emittedUtf8Bytes"`
	OriginalUTF8Bytes int  `json:"originalUtf8Bytes"`
	Truncated         bool `json:"truncated"`
}

type EvidenceSelection struct {
	Mode                     string         `json:"mode"`
	Relations                CountSelection `json:"relations"`
	Entries                  EntrySelection `json:"entries"`
	Segments                 CountSelection `json:"segments"`
	SegmentText              ByteSelection  `json:"segmentText"`
	CanonicalOmittedSegments int            `json:"canonicalOmittedSegments"`
	TruncatedTextSegments    int            `json:"truncatedTextSegments"`
	Coverage                 string         `json:"coverage"`
}

type SelectedText struct {
	Text              string `json:"text"`
	Truncated         bool   `json:"truncated"`
	OriginalUTF8Bytes int    `json:"originalUtf8Bytes"`
	EmittedUTF8Bytes  int    `json:"emittedUtf8Bytes"`
	ContentHash       Digest `json:"contentHash"`
}

type EvidenceRelation struct {
	Ordinal    int    `json:"ordinal"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Confidence string `json:"confidence"`
}

type EvidenceEntry struct {
	Ordinal             int               `json:"ordinal"`
	Kind                string            `json:"kind"`
	Actor               string            `json:"actor"`
	Timestamp           *time.Time        `json:"timestamp,omitempty"`
	RelatedEntryOrdinal *int              `json:"relatedEntryOrdinal,omitempty"`
	ToolCallID          string            `json:"toolCallId,omitempty"`
	ToolName            string            `json:"toolName,omitempty"`
	ToolNamespace       string            `json:"toolNamespace,omitempty"`
	Content             []EvidenceSegment `json:"content"`
}

type EvidenceSegment struct {
	Ordinal          int           `json:"ordinal"`
	Kind             string        `json:"kind"`
	Origin           string        `json:"origin"`
	OriginConfidence string        `json:"originConfidence"`
	Text             *SelectedText `json:"text,omitempty"`
	ContentClass     string        `json:"contentClass,omitempty"`
	SourceType       string        `json:"sourceType,omitempty"`
}

type EvidenceDocument struct {
	Revision  EvidenceRevision   `json:"revision"`
	Selection EvidenceSelection  `json:"selection"`
	Relations []EvidenceRelation `json:"relations"`
	Entries   []EvidenceEntry    `json:"entries"`
}

type EvidenceRef struct {
	ID                   string `json:"id"`
	SourceKind           string `json:"sourceKind"`
	SourceIdentity       string `json:"sourceIdentity"`
	DocumentDigestScheme string `json:"documentDigestScheme,omitempty"`
	DocumentDigest       string `json:"documentDigest"`
	EntryOrdinal         int    `json:"entryOrdinal"`
	SegmentOrdinal       *int   `json:"segmentOrdinal,omitempty"`
	EntryKind            string `json:"entryKind,omitempty"`
	Actor                string `json:"actor,omitempty"`
	Origin               string `json:"origin,omitempty"`
	OriginConfidence     string `json:"originConfidence,omitempty"`
	RelatedEntryOrdinal  *int   `json:"relatedEntryOrdinal,omitempty"`
	ToolCallID           string `json:"toolCallId,omitempty"`
	ToolName             string `json:"toolName,omitempty"`
	ToolNamespace        string `json:"toolNamespace,omitempty"`
	ContentHashScheme    string `json:"contentHashScheme,omitempty"`
	ContentHash          string `json:"contentHash"`
	Excerpt              string `json:"excerpt"`
}
