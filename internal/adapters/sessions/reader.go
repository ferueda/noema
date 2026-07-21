package sessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ferueda/noema/internal/domain"
)

const (
	documentDigestScheme = "sha256-sessions-document-jcs-v1"
	contentDigestScheme  = "sha256-utf8-v1"
)

var canonicalTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

type Reader struct {
	Executable string
	Runner     CommandRunner
}

func (reader Reader) Read(ctx context.Context, canonicalID string) (domain.EvidenceDocument, error) {
	if strings.TrimSpace(canonicalID) == "" {
		return domain.EvidenceDocument{}, errors.New("read Sessions export: canonical identity is required")
	}
	executable := reader.Executable
	if executable == "" {
		executable = "sessions"
	}
	runner := reader.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	output, err := runner.Run(ctx, executable, "export", canonicalID, "--format", "jsonl", "--full")
	if err != nil {
		var commandError CommandError
		if errors.As(err, &commandError) {
			return domain.EvidenceDocument{}, fmt.Errorf("read Sessions export: %w", commandError)
		}
		return domain.EvidenceDocument{}, errors.New("read Sessions export: Sessions command unavailable")
	}
	return decodeExport(output, canonicalID)
}

func decodeExport(output []byte, requestedID string) (domain.EvidenceDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	recordIndex := 0
	seenEntry := false
	var document domain.EvidenceDocument
	var expectedSession sessionRefDTO
	var expectedDigest digestDTO
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return domain.EvidenceDocument{}, contractError(recordIndex, "invalid JSON")
		}
		var header headerDTO
		if err := json.Unmarshal(raw, &header); err != nil {
			return domain.EvidenceDocument{}, contractError(recordIndex, "invalid record header")
		}
		if err := validateHeader(header); err != nil {
			return domain.EvidenceDocument{}, contractError(recordIndex, err.Error())
		}
		if err := validateRequiredRecordFields(raw, header.Type); err != nil {
			return domain.EvidenceDocument{}, contractError(recordIndex, "required field is absent or malformed")
		}
		switch header.Type {
		case "session":
			if recordIndex != 0 {
				return domain.EvidenceDocument{}, contractError(recordIndex, "session record must be first and unique")
			}
			var record sessionRecordDTO
			if err := strictDecode(raw, &record); err != nil {
				return domain.EvidenceDocument{}, contractError(recordIndex, "invalid session record")
			}
			converted, err := validateSession(record, requestedID)
			if err != nil {
				return domain.EvidenceDocument{}, contractError(recordIndex, err.Error())
			}
			document = converted
			expectedSession = record.Snapshot.Session
			expectedDigest = record.Snapshot.DocumentDigest
		case "relation":
			if recordIndex == 0 || seenEntry {
				return domain.EvidenceDocument{}, contractError(recordIndex, "relation record is out of order")
			}
			var record relationRecordDTO
			if err := strictDecode(raw, &record); err != nil {
				return domain.EvidenceDocument{}, contractError(recordIndex, "invalid relation record")
			}
			if !sameSession(record.Session, expectedSession) || record.DocumentDigest != expectedDigest {
				return domain.EvidenceDocument{}, contractError(recordIndex, "session identity or document digest drift")
			}
			relation, err := validateRelation(record.Relation, len(document.Relations))
			if err != nil {
				return domain.EvidenceDocument{}, contractError(recordIndex, err.Error())
			}
			document.Relations = append(document.Relations, relation)
		case "entry":
			if recordIndex == 0 {
				return domain.EvidenceDocument{}, contractError(recordIndex, "entry record cannot precede session")
			}
			seenEntry = true
			var record entryRecordDTO
			if err := strictDecode(raw, &record); err != nil {
				return domain.EvidenceDocument{}, contractError(recordIndex, "invalid entry record")
			}
			if !sameSession(record.Session, expectedSession) || record.DocumentDigest != expectedDigest {
				return domain.EvidenceDocument{}, contractError(recordIndex, "session identity or document digest drift")
			}
			entry, err := validateEntry(record.Entry, len(document.Entries))
			if err != nil {
				return domain.EvidenceDocument{}, contractError(recordIndex, err.Error())
			}
			document.Entries = append(document.Entries, entry)
		default:
			return domain.EvidenceDocument{}, contractError(recordIndex, "unsupported record type")
		}
		recordIndex++
	}
	if recordIndex == 0 {
		return domain.EvidenceDocument{}, errors.New("read Sessions export: session record is absent")
	}
	if err := validateDocumentCounts(document); err != nil {
		return domain.EvidenceDocument{}, fmt.Errorf("read Sessions export: %w", err)
	}
	return document, nil
}

func validateHeader(header headerDTO) error {
	if header.SchemaVersion != 1 {
		return errors.New("unsupported schema version")
	}
	if header.Command != "export" {
		return errors.New("unexpected command")
	}
	if header.Disposition != "untrusted-history" {
		return errors.New("unsupported trust disposition")
	}
	return nil
}

func validateSession(record sessionRecordDTO, requestedID string) (domain.EvidenceDocument, error) {
	snapshot := record.Snapshot
	if err := validateSessionRef(snapshot.Session); err != nil {
		return domain.EvidenceDocument{}, err
	}
	if snapshot.Session.CanonicalID != requestedID {
		return domain.EvidenceDocument{}, errors.New("requested session identity does not match export")
	}
	if err := validateDigest(snapshot.DocumentDigest, documentDigestScheme); err != nil {
		return domain.EvidenceDocument{}, err
	}
	if snapshot.AdapterVersion == "" {
		return domain.EvidenceDocument{}, errors.New("adapter version is absent")
	}
	if !oneOf(snapshot.SourceState, "present", "missing", "unknown") ||
		!oneOf(snapshot.Freshness, "current", "stale") ||
		!oneOf(snapshot.LineageCoverage, "complete", "unknown") {
		return domain.EvidenceDocument{}, errors.New("invalid source metadata")
	}
	capturedAt, err := parseTimestamp(snapshot.CapturedAt)
	if err != nil {
		return domain.EvidenceDocument{}, errors.New("invalid captured timestamp")
	}
	observedAt, err := parseTimestamp(snapshot.SourceObservedAt)
	if err != nil {
		return domain.EvidenceDocument{}, errors.New("invalid source-observed timestamp")
	}
	createdAt, err := parseOptionalTimestamp(snapshot.CreatedAt)
	if err != nil {
		return domain.EvidenceDocument{}, errors.New("invalid created timestamp")
	}
	updatedAt, err := parseOptionalTimestamp(snapshot.UpdatedAt)
	if err != nil {
		return domain.EvidenceDocument{}, errors.New("invalid updated timestamp")
	}
	if snapshot.Title != nil {
		if err := validateSelectedText(*snapshot.Title, true); err != nil {
			return domain.EvidenceDocument{}, errors.New("invalid title selection")
		}
	}
	selection, err := validateSelection(snapshot.Selection)
	if err != nil {
		return domain.EvidenceDocument{}, err
	}
	return domain.EvidenceDocument{
		Revision: domain.EvidenceRevision{
			SourceKind:       domain.EvidenceSourceSessions,
			CanonicalID:      snapshot.Session.CanonicalID,
			NativeSourceKind: snapshot.Session.Source.Kind,
			SourceInstanceID: snapshot.Session.Source.InstanceID,
			NativeID:         snapshot.Session.NativeID,
			SchemaVersion:    record.SchemaVersion,
			Disposition:      record.Disposition,
			DocumentDigest: domain.Digest{
				Scheme: snapshot.DocumentDigest.Scheme,
				Digest: snapshot.DocumentDigest.Digest,
			},
			AdapterVersion:   snapshot.AdapterVersion,
			SourceState:      snapshot.SourceState,
			Freshness:        snapshot.Freshness,
			CapturedAt:       capturedAt,
			SourceObservedAt: observedAt,
			CreatedAt:        createdAt,
			UpdatedAt:        updatedAt,
			LineageCoverage:  snapshot.LineageCoverage,
		},
		Selection: selection,
		Relations: make([]domain.EvidenceRelation, 0, snapshot.Selection.Relations.Selected),
		Entries:   make([]domain.EvidenceEntry, 0, snapshot.Selection.Entries.Selected),
	}, nil
}

func validateSelection(input selectionDTO) (domain.EvidenceSelection, error) {
	if input.Mode != "full" {
		return domain.EvidenceSelection{}, errors.New("export selection is not full")
	}
	counts := []countSelectionDTO{input.Relations, input.Segments}
	for _, count := range counts {
		if count.Selected < 0 || count.Total < 0 || count.Selected != count.Total || count.Truncated {
			return domain.EvidenceSelection{}, errors.New("export selection is truncated or inconsistent")
		}
	}
	if input.Entries.Selected < 0 || input.Entries.Total < 0 ||
		input.Entries.Selected != input.Entries.Total || input.Entries.Truncated {
		return domain.EvidenceSelection{}, errors.New("entry selection is truncated or inconsistent")
	}
	if input.SegmentText.EmittedUTF8Bytes < 0 || input.SegmentText.OriginalUTF8Bytes < 0 ||
		input.SegmentText.EmittedUTF8Bytes != input.SegmentText.OriginalUTF8Bytes || input.SegmentText.Truncated ||
		input.CanonicalOmittedSegments < 0 || input.TruncatedTextSegments != 0 {
		return domain.EvidenceSelection{}, errors.New("text selection is truncated or inconsistent")
	}
	if input.Entries.Selected == 0 {
		if input.Entries.FirstOrdinal != nil || input.Entries.LastOrdinal != nil {
			return domain.EvidenceSelection{}, errors.New("empty entry selection has ordinal bounds")
		}
	} else if input.Entries.FirstOrdinal == nil || input.Entries.LastOrdinal == nil ||
		*input.Entries.FirstOrdinal != 0 || *input.Entries.LastOrdinal != input.Entries.Selected-1 {
		return domain.EvidenceSelection{}, errors.New("entry selection has invalid ordinal bounds")
	}
	return domain.EvidenceSelection{
		Mode:                     input.Mode,
		Relations:                domain.CountSelection(input.Relations),
		Entries:                  domain.EntrySelection(input.Entries),
		Segments:                 domain.CountSelection(input.Segments),
		SegmentText:              domain.ByteSelection(input.SegmentText),
		CanonicalOmittedSegments: input.CanonicalOmittedSegments,
		TruncatedTextSegments:    input.TruncatedTextSegments,
		Coverage:                 domain.CoverageCompleteRetainedSnapshot,
	}, nil
}

func validateRelation(input relationDTO, expectedOrdinal int) (domain.EvidenceRelation, error) {
	if input.Ordinal != expectedOrdinal || !oneOf(input.Kind, "parent", "child", "fork", "continuation", "unknown") ||
		!oneOf(input.Confidence, "high", "medium", "low", "unknown") {
		return domain.EvidenceRelation{}, errors.New("invalid relation")
	}
	if err := validateSessionRef(input.Target); err != nil {
		return domain.EvidenceRelation{}, errors.New("invalid relation target")
	}
	return domain.EvidenceRelation{
		Ordinal: input.Ordinal, Kind: input.Kind, Target: input.Target.CanonicalID, Confidence: input.Confidence,
	}, nil
}

func validateEntry(input entryDTO, expectedOrdinal int) (domain.EvidenceEntry, error) {
	if input.Ordinal != expectedOrdinal || input.Kind == "" ||
		!oneOf(input.Actor, "human", "model", "tool", "system", "unknown") || input.Content == nil {
		return domain.EvidenceEntry{}, errors.New("invalid entry coordinate")
	}
	timestamp, err := parseOptionalTimestamp(input.Timestamp)
	if err != nil {
		return domain.EvidenceEntry{}, errors.New("invalid entry timestamp")
	}
	if input.RelatedEntryOrdinal != nil && (*input.RelatedEntryOrdinal < 0 || *input.RelatedEntryOrdinal == input.Ordinal) {
		return domain.EvidenceEntry{}, errors.New("invalid related entry ordinal")
	}
	entry := domain.EvidenceEntry{
		Ordinal: input.Ordinal, Kind: input.Kind, Actor: input.Actor, Timestamp: timestamp,
		RelatedEntryOrdinal: input.RelatedEntryOrdinal, ToolCallID: input.ToolCallID,
		ToolName: input.ToolName, ToolNamespace: input.ToolNamespace,
		Content: make([]domain.EvidenceSegment, 0, len(input.Content)),
	}
	for index, raw := range input.Content {
		segment, err := validateSegment(raw, index)
		if err != nil {
			return domain.EvidenceEntry{}, err
		}
		entry.Content = append(entry.Content, segment)
	}
	// In a full export this count only covers presentation omissions, not canonical omitted segments.
	if input.OmittedSegmentCount != 0 {
		return domain.EvidenceEntry{}, errors.New("full export omitted segment records")
	}
	return entry, nil
}

func validateSegment(raw json.RawMessage, expectedOrdinal int) (domain.EvidenceSegment, error) {
	var header segmentHeaderDTO
	if err := json.Unmarshal(raw, &header); err != nil || header.Ordinal != expectedOrdinal ||
		!oneOf(header.Origin, "human", "injected", "delegated", "replayed-copied", "model", "tool", "system", "unknown") ||
		!oneOf(header.OriginConfidence, "high", "medium", "low", "unknown") {
		return domain.EvidenceSegment{}, errors.New("invalid segment coordinate")
	}
	segment := domain.EvidenceSegment{
		Ordinal: header.Ordinal, Kind: header.Kind, Origin: header.Origin, OriginConfidence: header.OriginConfidence,
	}
	switch header.Kind {
	case "text":
		var text textSegmentDTO
		if err := strictDecode(raw, &text); err != nil {
			return domain.EvidenceSegment{}, errors.New("invalid text segment")
		}
		if err := validateSelectedText(text.Text, true); err != nil {
			return domain.EvidenceSegment{}, errors.New("invalid selected text")
		}
		if err := validateDigest(text.ContentHash, contentDigestScheme); err != nil {
			return domain.EvidenceSegment{}, errors.New("invalid content hash")
		}
		hash := sha256.Sum256([]byte(text.Text.Text))
		if hex.EncodeToString(hash[:]) != text.ContentHash.Digest {
			return domain.EvidenceSegment{}, errors.New("content hash mismatch")
		}
		segment.Text = &domain.SelectedText{
			Text: text.Text.Text, Truncated: text.Text.Truncated,
			OriginalUTF8Bytes: text.Text.OriginalUTF8Bytes, EmittedUTF8Bytes: text.Text.EmittedUTF8Bytes,
			ContentHash: domain.Digest{Scheme: text.ContentHash.Scheme, Digest: text.ContentHash.Digest},
		}
	case "omitted":
		var omitted omittedSegmentDTO
		if err := strictDecode(raw, &omitted); err != nil ||
			!oneOf(omitted.ContentClass, "image", "resource", "structured", "unknown") || omitted.SourceType == "" {
			return domain.EvidenceSegment{}, errors.New("invalid omitted segment")
		}
		segment.ContentClass = omitted.ContentClass
		segment.SourceType = omitted.SourceType
	default:
		return domain.EvidenceSegment{}, errors.New("unsupported segment kind")
	}
	return segment, nil
}

func validateDocumentCounts(document domain.EvidenceDocument) error {
	selection := document.Selection
	if len(document.Relations) != selection.Relations.Selected || len(document.Entries) != selection.Entries.Selected {
		return errors.New("record counts do not match selection")
	}
	segments := 0
	omitted := 0
	textBytes := 0
	for _, entry := range document.Entries {
		if entry.RelatedEntryOrdinal != nil && *entry.RelatedEntryOrdinal >= len(document.Entries) {
			return errors.New("related entry ordinal is out of range")
		}
		segments += len(entry.Content)
		for _, segment := range entry.Content {
			if segment.Kind == "omitted" {
				omitted++
			} else if segment.Text != nil {
				textBytes += segment.Text.EmittedUTF8Bytes
			}
		}
	}
	if segments != selection.Segments.Selected || omitted != selection.CanonicalOmittedSegments ||
		textBytes != selection.SegmentText.EmittedUTF8Bytes {
		return errors.New("segment counts do not match selection")
	}
	return nil
}

func validateSessionRef(ref sessionRefDTO) error {
	if ref.CanonicalID == "" || ref.Source.Kind == "" || ref.Source.InstanceID == "" || ref.NativeID == "" {
		return errors.New("invalid session identity")
	}
	return nil
}

func validateDigest(digest digestDTO, scheme string) error {
	if digest.Scheme != scheme || len(digest.Digest) != sha256.Size*2 {
		return errors.New("invalid digest")
	}
	decoded, err := hex.DecodeString(digest.Digest)
	if err != nil || hex.EncodeToString(decoded) != digest.Digest {
		return errors.New("invalid digest")
	}
	return nil
}

func validateSelectedText(text selectedTextDTO, requireComplete bool) error {
	if !utf8.ValidString(text.Text) || text.OriginalUTF8Bytes < 0 || text.EmittedUTF8Bytes < 0 ||
		text.EmittedUTF8Bytes != len([]byte(text.Text)) || text.EmittedUTF8Bytes > text.OriginalUTF8Bytes {
		return errors.New("invalid selected text")
	}
	if requireComplete && (text.Truncated || text.EmittedUTF8Bytes != text.OriginalUTF8Bytes) {
		return errors.New("selected text is truncated")
	}
	return nil
}

func sameSession(left, right sessionRefDTO) bool {
	return left.CanonicalID == right.CanonicalID && left.NativeID == right.NativeID &&
		left.Source.Kind == right.Source.Kind && left.Source.InstanceID == right.Source.InstanceID
}

func parseTimestamp(value string) (time.Time, error) {
	if !canonicalTimestampPattern.MatchString(value) {
		return time.Time{}, errors.New("timestamp is absent")
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func parseOptionalTimestamp(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseTimestamp(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func contractError(record int, category string) error {
	return fmt.Errorf("read Sessions export: record %d: %s", record, category)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
