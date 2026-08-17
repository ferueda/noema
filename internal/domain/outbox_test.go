package domain

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOutboxRecordAcknowledgementAndRetryInvariants(t *testing.T) {
	pending, err := NewOutboxRecord("evt_123")
	if err != nil {
		t.Fatalf("new outbox record: %v", err)
	}
	failed, err := pending.WithFailure(OutboxFailureTimeout)
	if err != nil {
		t.Fatalf("record failed attempt: %v", err)
	}
	if failed.Status != OutboxStatusPending || failed.EventID != pending.EventID ||
		failed.AttemptCount != 1 || failed.LastFailureCategory != OutboxFailureTimeout {
		t.Fatalf("failed outbox record = %#v", failed)
	}

	deliveredAt := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.FixedZone("offset", -7*60*60))
	delivered, err := failed.WithAcknowledgement("transport-ack-123", deliveredAt)
	if err != nil {
		t.Fatalf("record acknowledgement: %v", err)
	}
	if delivered.Status != OutboxStatusDelivered || delivered.EventID != pending.EventID ||
		delivered.AttemptCount != 2 || delivered.LastFailureCategory != "" ||
		delivered.AcknowledgementID != "transport-ack-123" ||
		delivered.DeliveredAt == nil || delivered.DeliveredAt.Location() != time.UTC {
		t.Fatalf("delivered outbox record = %#v", delivered)
	}
	if _, err := delivered.WithAcknowledgement("another-ack", time.Now()); err == nil {
		t.Fatal("delivered record accepted another publication attempt")
	}

	local, err := pending.WithAcknowledgement("", deliveredAt)
	if err != nil {
		t.Fatalf("record local acknowledgement without an ID: %v", err)
	}
	if local.AcknowledgementID != "" || local.Status != OutboxStatusDelivered {
		t.Fatalf("local acknowledgement = %#v", local)
	}
}

func TestOutboxRecordModelsAtLeastOnceAcknowledgement(t *testing.T) {
	pending, err := NewOutboxRecord("evt_stable")
	if err != nil {
		t.Fatalf("new outbox record: %v", err)
	}
	acceptedButUncommitted, err := pending.WithAcknowledgement("ack_1", time.Now())
	if err != nil {
		t.Fatalf("build acknowledged state: %v", err)
	}

	// If acceptedButUncommitted is lost before its local commit, the durable
	// value remains pending and the same stable EventID is published again.
	if acceptedButUncommitted.EventID != pending.EventID ||
		pending.Status != OutboxStatusPending || pending.AttemptCount != 0 {
		t.Fatalf("at-least-once identity changed: pending=%#v delivered=%#v", pending, acceptedButUncommitted)
	}
}

func TestOutboxRecordValidationRejectsInvalidStateCombinations(t *testing.T) {
	now := time.Now()
	tests := []OutboxRecord{
		{},
		{EventID: "evt_1", Status: "failed"},
		{EventID: "evt_1", Status: OutboxStatusPending, AttemptCount: -1},
		{EventID: "evt_1", Status: OutboxStatusPending, LastFailureCategory: OutboxFailureTransport},
		{EventID: "evt_1", Status: OutboxStatusPending, AttemptCount: 1},
		{EventID: "evt_1", Status: OutboxStatusPending, AttemptCount: 1, LastFailureCategory: "remote said secret"},
		{EventID: "evt_1", Status: OutboxStatusPending, DeliveredAt: &now, AcknowledgementID: "ack"},
		{EventID: "evt_1", Status: OutboxStatusDelivered, AttemptCount: 1},
		{
			EventID: "evt_1", Status: OutboxStatusDelivered, AttemptCount: 1,
			DeliveredAt: &now, AcknowledgementID: "ack", LastFailureCategory: OutboxFailureTimeout,
		},
	}
	for index, record := range tests {
		if err := record.Validate(); err == nil {
			t.Fatalf("invalid outbox record %d was accepted: %#v", index, record)
		}
	}
}

func TestOutboxAcknowledgementIsBoundedAndContainsNoConsumerState(t *testing.T) {
	pending, err := NewOutboxRecord("evt_123")
	if err != nil {
		t.Fatalf("new outbox record: %v", err)
	}
	for _, acknowledgementID := range []string{" ack ", "ack\nsecret", strings.Repeat("x", maxOutboxAcknowledgementIDBytes+1)} {
		if _, err := pending.WithAcknowledgement(acknowledgementID, time.Now()); err == nil {
			t.Fatalf("invalid acknowledgement %q was accepted", acknowledgementID)
		}
	}

	outboxType := reflect.TypeOf(OutboxRecord{})
	wantFields := []string{
		"EventID",
		"Status",
		"AttemptCount",
		"LastFailureCategory",
		"DeliveredAt",
		"AcknowledgementID",
	}
	if outboxType.NumField() != len(wantFields) {
		t.Fatalf("OutboxRecord fields = %d, want %d", outboxType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		if got := outboxType.Field(index).Name; got != want {
			t.Fatalf("OutboxRecord field %d = %q, want %q", index, got, want)
		}
	}
}
