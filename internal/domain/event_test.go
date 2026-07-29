package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDomainEventFingerprintCoversStableEnvelope(t *testing.T) {
	payload := map[string]any{
		"analysisId": "analysis_123",
		"claimIds":   []string{"claim_1", "claim_2"},
	}
	event, err := NewDomainEvent(
		"analysis.completed",
		EventReferenceAnalysis,
		"analysis_123",
		payload,
		[]EventReference{
			{RecordType: EventReferenceClaim, RecordID: "claim_1"},
			{RecordType: EventReferenceClaim, RecordID: "claim_2"},
		},
		time.Date(2026, time.July, 28, 12, 0, 0, 0, time.FixedZone("offset", -7*60*60)),
	)
	if err != nil {
		t.Fatalf("new domain event: %v", err)
	}
	if event.SchemaVersion != DomainEventSchemaVersionV1 ||
		event.CreatedAt.Location() != time.UTC ||
		event.ID == "" || event.Fingerprint == "" {
		t.Fatalf("domain event = %#v", event)
	}

	fingerprint, err := DomainEventFingerprint(
		event.SchemaVersion,
		event.Type,
		event.SubjectType,
		event.SubjectID,
		event.Payload,
		event.References,
	)
	if err != nil {
		t.Fatalf("fingerprint event: %v", err)
	}
	if event.Fingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want %q", event.Fingerprint, fingerprint)
	}

	changes := []func(*DomainEvent){
		func(changed *DomainEvent) { changed.SchemaVersion++ },
		func(changed *DomainEvent) { changed.Type = "analysis.updated" },
		func(changed *DomainEvent) { changed.SubjectType = EventReferenceClaim },
		func(changed *DomainEvent) { changed.SubjectID = "analysis_456" },
		func(changed *DomainEvent) { changed.Payload["analysisId"] = "analysis_456" },
		func(changed *DomainEvent) {
			changed.References = []EventReference{{RecordType: EventReferenceClaim, RecordID: "claim_2"}}
		},
	}
	for index, change := range changes {
		changed := event
		changed.Payload = clonePayload(t, event.Payload)
		changed.References = append([]EventReference{}, event.References...)
		change(&changed)
		changedFingerprint, err := DomainEventFingerprint(
			changed.SchemaVersion,
			changed.Type,
			changed.SubjectType,
			changed.SubjectID,
			changed.Payload,
			changed.References,
		)
		if err != nil {
			t.Fatalf("fingerprint changed event %d: %v", index, err)
		}
		if changedFingerprint == event.Fingerprint {
			t.Fatalf("stable envelope change %d did not change fingerprint", index)
		}
	}

	changedTime := event
	changedTime.CreatedAt = event.CreatedAt.Add(time.Hour)
	if err := changedTime.Validate(); err != nil {
		t.Fatalf("validate event with changed creation time: %v", err)
	}
}

func TestDomainEventValidationRejectsInvalidIdentityAndBounds(t *testing.T) {
	valid := validDomainEvent(t)
	tests := []struct {
		name   string
		change func(*DomainEvent)
	}{
		{name: "schema", change: func(event *DomainEvent) { event.SchemaVersion = 2 }},
		{name: "ID", change: func(event *DomainEvent) { event.ID = "evt_other" }},
		{name: "fingerprint", change: func(event *DomainEvent) { event.Fingerprint = strings.Repeat("0", 64) }},
		{name: "type", change: func(event *DomainEvent) { event.Type = "completed" }},
		{name: "subject type", change: func(event *DomainEvent) { event.SubjectType = "consumer" }},
		{name: "subject ID", change: func(event *DomainEvent) { event.SubjectID = "" }},
		{name: "creation time", change: func(event *DomainEvent) { event.CreatedAt = time.Time{} }},
		{name: "nil payload", change: func(event *DomainEvent) { event.Payload = nil }},
		{name: "redundant payload schema", change: func(event *DomainEvent) {
			event.Payload["schemaVersion"] = DomainEventSchemaVersionV1
		}},
		{name: "reference", change: func(event *DomainEvent) {
			event.References = []EventReference{{RecordType: "agent-run", RecordID: "run_1"}}
		}},
		{name: "oversized payload", change: func(event *DomainEvent) {
			event.Payload["detail"] = strings.Repeat("x", maxEventPayloadBytes)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			event.Payload = clonePayload(t, valid.Payload)
			event.References = append([]EventReference{}, valid.References...)
			test.change(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("invalid domain event was accepted")
			}
		})
	}
}

func TestDomainEventPayloadAcceptsBoundedDomainMetadata(t *testing.T) {
	event, err := NewDomainEvent(
		"fact.observed",
		EventReferenceFact,
		"fact_123",
		map[string]any{
			"factId":    "fact_123",
			"factKind":  "tool-call",
			"toolCount": 1,
		},
		nil,
		time.Now(),
	)
	if err != nil {
		t.Fatalf("bounded domain metadata was rejected: %v", err)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("validate event: %v", err)
	}
}

func TestDomainEventContractContainsNoConsumerOrEvidenceBodyFields(t *testing.T) {
	eventType := reflect.TypeOf(DomainEvent{})
	wantFields := []string{
		"ID",
		"Fingerprint",
		"SchemaVersion",
		"Type",
		"SubjectType",
		"SubjectID",
		"Payload",
		"References",
		"CreatedAt",
	}
	if eventType.NumField() != len(wantFields) {
		t.Fatalf("DomainEvent fields = %d, want %d", eventType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		if got := eventType.Field(index).Name; got != want {
			t.Fatalf("DomainEvent field %d = %q, want %q", index, got, want)
		}
	}
	referenceType := reflect.TypeOf(EventReference{})
	if referenceType.NumField() != 2 {
		t.Fatalf("EventReference fields = %d, want 2", referenceType.NumField())
	}
}

func validDomainEvent(t *testing.T) DomainEvent {
	t.Helper()
	event, err := NewDomainEvent(
		"claim.admitted",
		EventReferenceClaim,
		"claim_123",
		map[string]any{
			"claimId":    "claim_123",
			"analysisId": "analysis_123",
		},
		[]EventReference{{RecordType: EventReferenceAnalysis, RecordID: "analysis_123"}},
		time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("new valid event: %v", err)
	}
	return event
}

func clonePayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return result
}
