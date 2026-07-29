package application

import (
	"reflect"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

func TestEventIdentityUsesCompleteStableEnvelope(t *testing.T) {
	now := time.Date(2026, 7, 21, 16, 0, 0, 0, time.UTC)

	references := []domain.EventReference{
		{RecordType: domain.EventReferenceAnalysis, RecordID: "semantic-stable"},
		{RecordType: domain.EventReferenceFact, RecordID: "fact-stable"},
	}
	event, err := newSemanticEvent(
		"claim.admitted",
		domain.EventReferenceClaim,
		"claim-stable",
		map[string]any{
			"claimId":    "claim-stable",
			"analysisId": "semantic-stable",
		},
		references,
		now,
	)
	if err != nil {
		t.Fatalf("build semantic event: %v", err)
	}
	fingerprint, err := domain.DomainEventFingerprint(
		event.SchemaVersion,
		event.Type,
		event.SubjectType,
		event.SubjectID,
		event.Payload,
		event.References,
	)
	if err != nil {
		t.Fatalf("fingerprint semantic event: %v", err)
	}
	if event.SchemaVersion != domain.DomainEventSchemaVersionV1 ||
		event.Fingerprint != fingerprint || event.ID == "" {
		t.Fatalf("semantic event = %#v", event)
	}

	repeated, err := newSemanticEvent(
		event.Type,
		event.SubjectType,
		event.SubjectID,
		event.Payload,
		references,
		now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("repeat semantic event: %v", err)
	}
	if repeated.Fingerprint != event.Fingerprint || repeated.ID != event.ID {
		t.Fatal("creation time changed stable semantic event identity")
	}

	changedReferences := append([]domain.EventReference{}, references...)
	changedReferences[1].RecordID = "fact-other"
	changed, err := newSemanticEvent(
		event.Type,
		event.SubjectType,
		event.SubjectID,
		event.Payload,
		changedReferences,
		now,
	)
	if err != nil {
		t.Fatalf("build changed semantic event: %v", err)
	}
	if changed.Fingerprint == event.Fingerprint || changed.ID == event.ID {
		t.Fatal("changed references did not change semantic event identity")
	}
}

func TestSemanticEventsReferenceOnlyNoemaRecords(t *testing.T) {
	now := time.Date(2026, 7, 21, 16, 0, 0, 0, time.UTC)
	analysis := domain.SemanticAnalysis{
		Run: domain.AnalysisRun{
			ID:         "analysis_123",
			ClaimIDs:   []string{"claim_1", "claim_2"},
			FinishedAt: now,
		},
		Claims: []domain.Claim{
			{
				ID:                "claim_1",
				SupportingFactIDs: []string{"fact_1", "fact_2"},
				CreatedAt:         now,
			},
			{
				ID:        "claim_2",
				CreatedAt: now,
			},
		},
	}
	events, err := buildSemanticEvents(analysis)
	if err != nil {
		t.Fatalf("build semantic events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	wantClaimReferences := []domain.EventReference{
		{RecordType: domain.EventReferenceAnalysis, RecordID: "analysis_123"},
		{RecordType: domain.EventReferenceFact, RecordID: "fact_1"},
		{RecordType: domain.EventReferenceFact, RecordID: "fact_2"},
	}
	if !reflect.DeepEqual(events[0].References, wantClaimReferences) {
		t.Fatalf("claim references = %#v, want %#v", events[0].References, wantClaimReferences)
	}
	if !reflect.DeepEqual(
		events[1].References,
		[]domain.EventReference{{RecordType: domain.EventReferenceAnalysis, RecordID: "analysis_123"}},
	) {
		t.Fatalf("claim without facts references = %#v", events[1].References)
	}
	wantAnalysisReferences := []domain.EventReference{
		{RecordType: domain.EventReferenceClaim, RecordID: "claim_1"},
		{RecordType: domain.EventReferenceClaim, RecordID: "claim_2"},
	}
	if !reflect.DeepEqual(events[2].References, wantAnalysisReferences) {
		t.Fatalf("analysis references = %#v, want %#v", events[2].References, wantAnalysisReferences)
	}
	wantClaimPayload := map[string]any{"claimId": "claim_1", "analysisId": "analysis_123"}
	if !reflect.DeepEqual(events[0].Payload, wantClaimPayload) {
		t.Fatalf("claim payload = %#v, want %#v", events[0].Payload, wantClaimPayload)
	}
	wantAnalysisPayload := map[string]any{
		"analysisId": "analysis_123",
		"claimIds":   []string{"claim_1", "claim_2"},
	}
	if !reflect.DeepEqual(events[2].Payload, wantAnalysisPayload) {
		t.Fatalf("analysis payload = %#v, want %#v", events[2].Payload, wantAnalysisPayload)
	}
	for _, event := range events {
		if event.SchemaVersion != domain.DomainEventSchemaVersionV1 {
			t.Fatalf("event schema version = %d", event.SchemaVersion)
		}
		if _, duplicated := event.Payload["schemaVersion"]; duplicated {
			t.Fatalf("event payload duplicates top-level schema: %#v", event.Payload)
		}
	}
}
