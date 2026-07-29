package domain

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ferueda/noema/internal/platform"
)

const (
	DomainEventSchemaVersionV1 = 1

	EventReferenceAnalysis = "analysis"
	EventReferenceClaim    = "claim"
	EventReferenceFact     = "fact"
	EventReferenceSummary  = "summary"
	EventReferenceEpisode  = "episode"

	maxEventIDBytes        = 128
	maxEventTypeBytes      = 128
	maxEventSubjectIDBytes = 256
	maxEventPayloadBytes   = 16 * 1024
	maxEventReferences     = 256
	maxEventPayloadDepth   = 16
)

// EventReference points to one Noema-owned record. It never points to a
// consumer execution or carries source evidence content.
type EventReference struct {
	RecordType string `json:"recordType"`
	RecordID   string `json:"recordId"`
}

// DomainEvent reports one consumer-neutral change to Noema-owned state.
type DomainEvent struct {
	ID            string           `json:"id"`
	Fingerprint   string           `json:"fingerprint"`
	SchemaVersion int              `json:"schemaVersion"`
	Type          string           `json:"type"`
	SubjectType   string           `json:"subjectType"`
	SubjectID     string           `json:"subjectId"`
	Payload       map[string]any   `json:"payload"`
	References    []EventReference `json:"references"`
	CreatedAt     time.Time        `json:"createdAt"`
}

// NewDomainEvent constructs a valid consumer-neutral V1 event.
func NewDomainEvent(
	eventType string,
	subjectType string,
	subjectID string,
	payload map[string]any,
	references []EventReference,
	createdAt time.Time,
) (DomainEvent, error) {
	references = append([]EventReference{}, references...)
	fingerprint, err := DomainEventFingerprint(
		DomainEventSchemaVersionV1,
		eventType,
		subjectType,
		subjectID,
		payload,
		references,
	)
	if err != nil {
		return DomainEvent{}, err
	}
	event := DomainEvent{
		ID:            platform.DerivedID("evt_", fingerprint),
		Fingerprint:   fingerprint,
		SchemaVersion: DomainEventSchemaVersionV1,
		Type:          eventType,
		SubjectType:   subjectType,
		SubjectID:     subjectID,
		Payload:       payload,
		References:    references,
		CreatedAt:     createdAt.UTC(),
	}
	if err := event.Validate(); err != nil {
		return DomainEvent{}, err
	}
	return event, nil
}

// DomainEventFingerprint identifies an event from its complete stable envelope.
// Creation time and publication state do not participate.
func DomainEventFingerprint(
	schemaVersion int,
	eventType string,
	subjectType string,
	subjectID string,
	payload map[string]any,
	references []EventReference,
) (string, error) {
	return platform.Fingerprint(struct {
		SchemaVersion int
		Type          string
		SubjectType   string
		SubjectID     string
		Payload       map[string]any
		References    []EventReference
	}{
		SchemaVersion: schemaVersion,
		Type:          eventType,
		SubjectType:   subjectType,
		SubjectID:     subjectID,
		Payload:       payload,
		References:    references,
	})
}

func (event DomainEvent) Validate() error {
	if event.SchemaVersion != DomainEventSchemaVersionV1 {
		return errors.New("domain event schema version is unsupported")
	}
	if !boundedEventValue(event.ID, maxEventIDBytes) {
		return errors.New("domain event ID is invalid")
	}
	if !validFingerprint(event.Fingerprint) {
		return errors.New("domain event fingerprint is invalid")
	}
	if !boundedEventValue(event.Type, maxEventTypeBytes) || !validEventType(event.Type) {
		return errors.New("domain event type is invalid")
	}
	if !validEventReferenceType(event.SubjectType) {
		return errors.New("domain event subject type is invalid")
	}
	if !boundedEventValue(event.SubjectID, maxEventSubjectIDBytes) {
		return errors.New("domain event subject ID is invalid")
	}
	if event.CreatedAt.IsZero() {
		return errors.New("domain event creation time is required")
	}
	if err := validateEventPayload(event.Payload); err != nil {
		return err
	}
	if len(event.References) > maxEventReferences {
		return errors.New("domain event has too many references")
	}
	if event.References == nil {
		return errors.New("domain event references are required")
	}
	for _, reference := range event.References {
		if err := reference.Validate(); err != nil {
			return err
		}
	}
	fingerprint, err := DomainEventFingerprint(
		event.SchemaVersion,
		event.Type,
		event.SubjectType,
		event.SubjectID,
		event.Payload,
		event.References,
	)
	if err != nil || fingerprint != event.Fingerprint {
		return errors.New("domain event fingerprint does not match its content")
	}
	if event.ID != platform.DerivedID("evt_", fingerprint) {
		return errors.New("domain event ID does not match its fingerprint")
	}
	return nil
}

func (reference EventReference) Validate() error {
	if !validEventReferenceType(reference.RecordType) {
		return errors.New("domain event reference type is invalid")
	}
	if !boundedEventValue(reference.RecordID, maxEventSubjectIDBytes) {
		return errors.New("domain event reference ID is invalid")
	}
	return nil
}

func validEventReferenceType(value string) bool {
	switch value {
	case EventReferenceAnalysis, EventReferenceClaim, EventReferenceFact,
		EventReferenceSummary, EventReferenceEpisode:
		return true
	default:
		return false
	}
}

func validateEventPayload(payload map[string]any) error {
	if payload == nil {
		return errors.New("domain event payload is required")
	}
	if _, duplicated := payload["schemaVersion"]; duplicated {
		return errors.New("domain event payload duplicates the envelope schema version")
	}
	if err := validateEventPayloadValue(reflect.ValueOf(payload), 0); err != nil {
		return err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return errors.New("domain event payload is not valid JSON")
	}
	if len(encoded) > maxEventPayloadBytes {
		return errors.New("domain event payload is too large")
	}
	return nil
}

func validateEventPayloadValue(value reflect.Value, depth int) error {
	if depth > maxEventPayloadDepth {
		return errors.New("domain event payload is too deeply nested")
	}
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return validateEventPayloadValue(value.Elem(), depth)
	}
	switch value.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	case reflect.Float32, reflect.Float64:
		if math.IsInf(value.Float(), 0) || math.IsNaN(value.Float()) {
			return errors.New("domain event payload contains a non-finite number")
		}
		return nil
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return errors.New("domain event payload contains invalid UTF-8")
		}
		return nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return errors.New("domain event payload contains a non-string key")
		}
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			if !boundedEventValue(key, maxEventSubjectIDBytes) {
				return errors.New("domain event payload key is invalid")
			}
			if err := validateEventPayloadValue(iterator.Value(), depth+1); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			if err := validateEventPayloadValue(value.Index(index), depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("domain event payload contains an unsupported value")
	}
}

func validEventType(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for index, character := range part {
			if character >= 'a' && character <= 'z' {
				continue
			}
			if index > 0 && (character == '-' || character >= '0' && character <= '9') {
				continue
			}
			return false
		}
	}
	return true
}

func validFingerprint(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func boundedEventValue(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.ValidString(value) && len([]byte(value)) <= limit
}
