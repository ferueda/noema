package domain

import (
	"encoding/hex"
	"encoding/json"
	"errors"
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

	EventTypeClaimAdmitted     = "claim.admitted"
	EventTypeAnalysisCompleted = "analysis.completed"

	maxEventIDBytes        = 128
	maxEventTypeBytes      = 128
	maxEventSubjectIDBytes = 256
	maxEventPayloadBytes   = 16 * 1024
	maxEventReferences     = 256
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
	if !boundedEventValue(event.Type, maxEventTypeBytes) {
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
	if err := validateEventContract(event); err != nil {
		return err
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

func validateEventContract(event DomainEvent) error {
	if event.Payload == nil {
		return errors.New("domain event payload is required")
	}
	encoded, err := json.Marshal(event.Payload)
	if err != nil {
		return errors.New("domain event payload is not valid JSON")
	}
	if len(encoded) > maxEventPayloadBytes {
		return errors.New("domain event payload is too large")
	}
	switch event.Type {
	case EventTypeClaimAdmitted:
		return validateClaimAdmittedEvent(event)
	case EventTypeAnalysisCompleted:
		return validateAnalysisCompletedEvent(event)
	default:
		return errors.New("domain event type is unsupported")
	}
}

func validateClaimAdmittedEvent(event DomainEvent) error {
	if event.SubjectType != EventReferenceClaim || len(event.Payload) != 2 {
		return errors.New("claim event envelope is invalid")
	}
	claimID, claimOK := eventPayloadString(event.Payload, "claimId")
	analysisID, analysisOK := eventPayloadString(event.Payload, "analysisId")
	if !claimOK || !analysisOK || claimID != event.SubjectID ||
		len(event.References) == 0 ||
		event.References[0] != (EventReference{
			RecordType: EventReferenceAnalysis,
			RecordID:   analysisID,
		}) {
		return errors.New("claim event payload or analysis reference is invalid")
	}
	factIDs := make(map[string]struct{}, len(event.References)-1)
	for _, reference := range event.References[1:] {
		if reference.RecordType != EventReferenceFact {
			return errors.New("claim event contains an invalid record reference")
		}
		if _, duplicate := factIDs[reference.RecordID]; duplicate {
			return errors.New("claim event contains a duplicate fact reference")
		}
		factIDs[reference.RecordID] = struct{}{}
	}
	return nil
}

func validateAnalysisCompletedEvent(event DomainEvent) error {
	if event.SubjectType != EventReferenceAnalysis || len(event.Payload) != 2 {
		return errors.New("analysis event envelope is invalid")
	}
	analysisID, analysisOK := eventPayloadString(event.Payload, "analysisId")
	claimIDs, claimsOK := eventPayloadStrings(event.Payload, "claimIds")
	if !analysisOK || !claimsOK || analysisID != event.SubjectID ||
		len(claimIDs) != len(event.References) {
		return errors.New("analysis event payload or references are invalid")
	}
	seen := make(map[string]struct{}, len(claimIDs))
	for index, claimID := range claimIDs {
		if event.References[index] != (EventReference{
			RecordType: EventReferenceClaim,
			RecordID:   claimID,
		}) {
			return errors.New("analysis event claim reference is invalid")
		}
		if _, duplicate := seen[claimID]; duplicate {
			return errors.New("analysis event contains a duplicate claim reference")
		}
		seen[claimID] = struct{}{}
	}
	return nil
}

func eventPayloadString(payload map[string]any, key string) (string, bool) {
	value, ok := payload[key].(string)
	return value, ok && boundedEventValue(value, maxEventSubjectIDBytes)
}

func eventPayloadStrings(payload map[string]any, key string) ([]string, bool) {
	switch values := payload[key].(type) {
	case []string:
		result := append([]string{}, values...)
		for _, value := range result {
			if !boundedEventValue(value, maxEventSubjectIDBytes) {
				return nil, false
			}
		}
		return result, true
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			value, ok := item.(string)
			if !ok || !boundedEventValue(value, maxEventSubjectIDBytes) {
				return nil, false
			}
			result = append(result, value)
		}
		return result, true
	default:
		return nil, false
	}
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
