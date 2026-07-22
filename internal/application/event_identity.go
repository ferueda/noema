package application

import "github.com/ferueda/noema/internal/platform"

// EventFingerprint identifies an event from its stable semantic content.
// Event IDs remain the responsibility of each event producer.
func EventFingerprint(
	eventType string,
	subjectType string,
	subjectID string,
	payload map[string]any,
) (string, error) {
	return platform.Fingerprint(struct {
		Type        string
		SubjectType string
		SubjectID   string
		Payload     map[string]any
	}{
		Type:        eventType,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Payload:     payload,
	})
}
