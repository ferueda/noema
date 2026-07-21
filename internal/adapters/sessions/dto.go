package sessions

import "encoding/json"

type headerDTO struct {
	SchemaVersion int    `json:"schemaVersion"`
	Command       string `json:"command"`
	Type          string `json:"type"`
	Disposition   string `json:"disposition"`
}

type sessionRefDTO struct {
	CanonicalID string `json:"canonicalId"`
	Source      struct {
		Kind       string `json:"kind"`
		InstanceID string `json:"instanceId"`
	} `json:"source"`
	NativeID string `json:"nativeId"`
}

type digestDTO struct {
	Scheme string `json:"scheme"`
	Digest string `json:"digest"`
}

type selectedTextDTO struct {
	Text              string `json:"text"`
	Truncated         bool   `json:"truncated"`
	OriginalUTF8Bytes int    `json:"originalUtf8Bytes"`
	EmittedUTF8Bytes  int    `json:"emittedUtf8Bytes"`
}

type countSelectionDTO struct {
	Selected  int  `json:"selected"`
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
}

type entrySelectionDTO struct {
	Selected     int  `json:"selected"`
	Total        int  `json:"total"`
	Truncated    bool `json:"truncated"`
	FirstOrdinal *int `json:"firstOrdinal"`
	LastOrdinal  *int `json:"lastOrdinal"`
}

type byteSelectionDTO struct {
	EmittedUTF8Bytes  int  `json:"emittedUtf8Bytes"`
	OriginalUTF8Bytes int  `json:"originalUtf8Bytes"`
	Truncated         bool `json:"truncated"`
}

type selectionDTO struct {
	Mode                     string            `json:"mode"`
	Relations                countSelectionDTO `json:"relations"`
	Entries                  entrySelectionDTO `json:"entries"`
	Segments                 countSelectionDTO `json:"segments"`
	SegmentText              byteSelectionDTO  `json:"segmentText"`
	CanonicalOmittedSegments int               `json:"canonicalOmittedSegments"`
	TruncatedTextSegments    int               `json:"truncatedTextSegments"`
}

type snapshotDTO struct {
	Session          sessionRefDTO    `json:"session"`
	DocumentDigest   digestDTO        `json:"documentDigest"`
	Title            *selectedTextDTO `json:"title,omitempty"`
	CreatedAt        string           `json:"createdAt,omitempty"`
	UpdatedAt        string           `json:"updatedAt,omitempty"`
	CapturedAt       string           `json:"capturedAt"`
	SourceState      string           `json:"sourceState"`
	SourceObservedAt string           `json:"sourceObservedAt"`
	AdapterVersion   string           `json:"adapterVersion"`
	Freshness        string           `json:"freshness"`
	LineageCoverage  string           `json:"lineageCoverage"`
	Selection        selectionDTO     `json:"selection"`
}

type sessionRecordDTO struct {
	headerDTO
	Snapshot snapshotDTO `json:"snapshot"`
}

type relationDTO struct {
	Ordinal    int           `json:"ordinal"`
	Kind       string        `json:"kind"`
	Target     sessionRefDTO `json:"target"`
	Confidence string        `json:"confidence"`
}

type relationRecordDTO struct {
	headerDTO
	Session        sessionRefDTO `json:"session"`
	DocumentDigest digestDTO     `json:"documentDigest"`
	Relation       relationDTO   `json:"relation"`
}

type entryDTO struct {
	Ordinal             int               `json:"ordinal"`
	Kind                string            `json:"kind"`
	Actor               string            `json:"actor"`
	Timestamp           string            `json:"timestamp,omitempty"`
	RelatedEntryOrdinal *int              `json:"relatedEntryOrdinal,omitempty"`
	ToolCallID          string            `json:"toolCallId,omitempty"`
	ToolName            string            `json:"toolName,omitempty"`
	ToolNamespace       string            `json:"toolNamespace,omitempty"`
	Content             []json.RawMessage `json:"content"`
	OmittedSegmentCount int               `json:"omittedSegmentCount"`
}

type entryRecordDTO struct {
	headerDTO
	Session        sessionRefDTO `json:"session"`
	DocumentDigest digestDTO     `json:"documentDigest"`
	Entry          entryDTO      `json:"entry"`
}

type segmentHeaderDTO struct {
	Ordinal          int    `json:"ordinal"`
	Kind             string `json:"kind"`
	Origin           string `json:"origin"`
	OriginConfidence string `json:"originConfidence"`
}

type textSegmentDTO struct {
	segmentHeaderDTO
	Text        selectedTextDTO `json:"text"`
	ContentHash digestDTO       `json:"contentHash"`
}

type omittedSegmentDTO struct {
	segmentHeaderDTO
	ContentClass string `json:"contentClass"`
	SourceType   string `json:"sourceType"`
}
