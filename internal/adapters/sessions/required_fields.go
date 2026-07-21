package sessions

import (
	"encoding/json"
	"errors"
)

func validateRequiredRecordFields(raw json.RawMessage, recordType string) error {
	if err := rejectUnexpectedNull(raw, ""); err != nil {
		return err
	}
	record, err := object(raw)
	if err != nil || !hasFields(record, "schemaVersion", "command", "type", "disposition") {
		return errors.New("invalid record")
	}
	switch recordType {
	case "session":
		if !hasFields(record, "snapshot") {
			return errors.New("snapshot is absent")
		}
		snapshot, err := object(record["snapshot"])
		if err != nil || !hasFields(snapshot,
			"session", "documentDigest", "capturedAt", "sourceState", "sourceObservedAt",
			"adapterVersion", "freshness", "lineageCoverage", "selection",
		) || validateSessionRefFields(snapshot["session"]) != nil || validateDigestFields(snapshot["documentDigest"]) != nil {
			return errors.New("snapshot field is absent")
		}
		if title, ok := snapshot["title"]; ok && validateSelectedTextFields(title) != nil {
			return errors.New("title field is malformed")
		}
		return validateSelectionFields(snapshot["selection"])
	case "relation":
		if !hasFields(record, "session", "documentDigest", "relation") ||
			validateSessionRefFields(record["session"]) != nil || validateDigestFields(record["documentDigest"]) != nil {
			return errors.New("relation envelope field is absent")
		}
		relation, err := object(record["relation"])
		if err != nil || !hasFields(relation, "ordinal", "kind", "target", "confidence") {
			return errors.New("relation field is absent")
		}
		return validateSessionRefFields(relation["target"])
	case "entry":
		if !hasFields(record, "session", "documentDigest", "entry") ||
			validateSessionRefFields(record["session"]) != nil || validateDigestFields(record["documentDigest"]) != nil {
			return errors.New("entry envelope field is absent")
		}
		entry, err := object(record["entry"])
		if err != nil || !hasFields(entry, "ordinal", "kind", "actor", "content", "omittedSegmentCount") {
			return errors.New("entry field is absent")
		}
		var segments []json.RawMessage
		if err := json.Unmarshal(entry["content"], &segments); err != nil {
			return errors.New("entry content is malformed")
		}
		for _, segment := range segments {
			if err := validateSegmentFields(segment); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("unsupported record")
	}
}

func rejectUnexpectedNull(raw json.RawMessage, field string) error {
	if string(raw) == "null" {
		if field == "firstOrdinal" || field == "lastOrdinal" {
			return nil
		}
		return errors.New("null is not allowed")
	}
	var objectValue map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objectValue); err == nil && objectValue != nil {
		for name, value := range objectValue {
			if err := rejectUnexpectedNull(value, name); err != nil {
				return err
			}
		}
		return nil
	}
	var arrayValue []json.RawMessage
	if err := json.Unmarshal(raw, &arrayValue); err == nil && arrayValue != nil {
		for _, value := range arrayValue {
			if err := rejectUnexpectedNull(value, field); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSelectionFields(raw json.RawMessage) error {
	selection, err := object(raw)
	if err != nil || !hasFields(selection,
		"mode", "relations", "entries", "segments", "segmentText",
		"canonicalOmittedSegments", "truncatedTextSegments",
	) {
		return errors.New("selection field is absent")
	}
	for _, name := range []string{"relations", "entries", "segments"} {
		count, err := object(selection[name])
		if err != nil || !hasFields(count, "selected", "total", "truncated") {
			return errors.New("selection count field is absent")
		}
	}
	entries, _ := object(selection["entries"])
	if !hasFields(entries, "firstOrdinal", "lastOrdinal") {
		return errors.New("entry selection bound is absent")
	}
	text, err := object(selection["segmentText"])
	if err != nil || !hasFields(text, "emittedUtf8Bytes", "originalUtf8Bytes", "truncated") {
		return errors.New("text selection field is absent")
	}
	return nil
}

func validateSegmentFields(raw json.RawMessage) error {
	segment, err := object(raw)
	if err != nil || !hasFields(segment, "ordinal", "kind", "origin", "originConfidence") {
		return errors.New("segment coordinate is absent")
	}
	var kind string
	if err := json.Unmarshal(segment["kind"], &kind); err != nil {
		return errors.New("segment kind is malformed")
	}
	switch kind {
	case "text":
		if !hasFields(segment, "text", "contentHash") || validateSelectedTextFields(segment["text"]) != nil {
			return errors.New("text segment field is absent")
		}
		return validateDigestFields(segment["contentHash"])
	case "omitted":
		if !hasFields(segment, "contentClass", "sourceType") {
			return errors.New("omitted segment field is absent")
		}
		return nil
	default:
		return errors.New("unsupported segment kind")
	}
}

func validateSessionRefFields(raw json.RawMessage) error {
	ref, err := object(raw)
	if err != nil || !hasFields(ref, "canonicalId", "source", "nativeId") {
		return errors.New("session identity field is absent")
	}
	source, err := object(ref["source"])
	if err != nil || !hasFields(source, "kind", "instanceId") {
		return errors.New("source identity field is absent")
	}
	return nil
}

func validateDigestFields(raw json.RawMessage) error {
	digest, err := object(raw)
	if err != nil || !hasFields(digest, "scheme", "digest") {
		return errors.New("digest field is absent")
	}
	return nil
}

func validateSelectedTextFields(raw json.RawMessage) error {
	text, err := object(raw)
	if err != nil || !hasFields(text, "text", "truncated", "originalUtf8Bytes", "emittedUtf8Bytes") {
		return errors.New("selected text field is absent")
	}
	return nil
}

func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, errors.New("value is not an object")
	}
	return value, nil
}

func hasFields(value map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if _, ok := value[name]; !ok {
			return false
		}
	}
	return true
}
