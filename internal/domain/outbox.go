package domain

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	OutboxStatusPending   = "pending"
	OutboxStatusDelivered = "delivered"

	OutboxFailureTransport              = "transport-failed"
	OutboxFailureTimeout                = "transport-timeout"
	OutboxFailureInvalidAcknowledgement = "acknowledgement-invalid"

	maxOutboxAcknowledgementIDBytes = 256
)

// OutboxRecord is the durable publication state for one domain event. EventID
// is also the stable publication identity used for external deduplication.
type OutboxRecord struct {
	EventID             string     `json:"eventId"`
	Status              string     `json:"status"`
	AttemptCount        int        `json:"attemptCount"`
	LastFailureCategory string     `json:"lastFailureCategory,omitempty"`
	DeliveredAt         *time.Time `json:"deliveredAt,omitempty"`
	AcknowledgementID   string     `json:"acknowledgementId,omitempty"`
}

func NewOutboxRecord(eventID string) (OutboxRecord, error) {
	record := OutboxRecord{EventID: eventID, Status: OutboxStatusPending}
	if err := record.Validate(); err != nil {
		return OutboxRecord{}, err
	}
	return record, nil
}

func (record OutboxRecord) Validate() error {
	if !boundedEventValue(record.EventID, maxEventIDBytes) {
		return errors.New("outbox event ID is invalid")
	}
	if record.AttemptCount < 0 {
		return errors.New("outbox attempt count is invalid")
	}
	switch record.Status {
	case OutboxStatusPending:
		if record.DeliveredAt != nil || record.AcknowledgementID != "" {
			return errors.New("pending outbox record cannot contain an acknowledgement")
		}
		if record.AttemptCount == 0 && record.LastFailureCategory != "" {
			return errors.New("unattempted outbox record cannot contain a failure")
		}
		if record.AttemptCount > 0 && !validOutboxFailureCategory(record.LastFailureCategory) {
			return errors.New("attempted pending outbox record requires a safe failure category")
		}
	case OutboxStatusDelivered:
		if record.AttemptCount < 1 {
			return errors.New("delivered outbox record requires an attempt")
		}
		if record.DeliveredAt == nil || record.DeliveredAt.IsZero() {
			return errors.New("delivered outbox record requires a delivery time")
		}
		if record.AcknowledgementID != "" && !validAcknowledgementID(record.AcknowledgementID) {
			return errors.New("delivered outbox record acknowledgement is invalid")
		}
		if record.LastFailureCategory != "" {
			return errors.New("delivered outbox record cannot retain a failure category")
		}
	default:
		return errors.New("outbox status is invalid")
	}
	return nil
}

// WithFailure records one failed publication attempt and leaves the stable
// event eligible for a later explicit attempt.
func (record OutboxRecord) WithFailure(category string) (OutboxRecord, error) {
	if err := record.Validate(); err != nil {
		return OutboxRecord{}, err
	}
	if record.Status != OutboxStatusPending {
		return OutboxRecord{}, errors.New("delivered outbox record cannot be attempted again")
	}
	if !validOutboxFailureCategory(category) {
		return OutboxRecord{}, errors.New("outbox failure category is invalid")
	}
	record.AttemptCount++
	record.LastFailureCategory = category
	return record, record.Validate()
}

// WithAcknowledgement records one transport acknowledgement. If the transport
// accepted the event but this updated value is not committed, the prior record
// remains pending and a later attempt may repeat the same EventID.
func (record OutboxRecord) WithAcknowledgement(
	acknowledgementID string,
	deliveredAt time.Time,
) (OutboxRecord, error) {
	if err := record.Validate(); err != nil {
		return OutboxRecord{}, err
	}
	if record.Status != OutboxStatusPending {
		return OutboxRecord{}, errors.New("delivered outbox record cannot be attempted again")
	}
	if (acknowledgementID != "" && !validAcknowledgementID(acknowledgementID)) || deliveredAt.IsZero() {
		return OutboxRecord{}, errors.New("outbox acknowledgement is invalid")
	}
	deliveredAt = deliveredAt.UTC()
	record.Status = OutboxStatusDelivered
	record.AttemptCount++
	record.LastFailureCategory = ""
	record.DeliveredAt = &deliveredAt
	record.AcknowledgementID = acknowledgementID
	return record, record.Validate()
}

func validOutboxFailureCategory(value string) bool {
	switch value {
	case OutboxFailureTransport, OutboxFailureTimeout, OutboxFailureInvalidAcknowledgement:
		return true
	default:
		return false
	}
}

func validAcknowledgementID(value string) bool {
	if value != strings.TrimSpace(value) ||
		!utf8.ValidString(value) || len([]byte(value)) > maxOutboxAcknowledgementIDBytes {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
