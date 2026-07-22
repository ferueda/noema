package application

import (
	"testing"
	"time"
)

func TestEventIdentityCompatibility(t *testing.T) {
	now := time.Date(2026, 7, 21, 16, 0, 0, 0, time.UTC)

	t.Run("foundation event", func(t *testing.T) {
		scanner := Scanner{Now: func() time.Time { return now }}
		event, err := scanner.newEvent(
			"observation.created",
			"observation",
			"observation-stable",
			map[string]any{
				"schemaVersion": 1,
				"observationId": "observation-stable",
				"scanId":        "scan-stable",
			},
			nil,
		)
		if err != nil {
			t.Fatalf("build foundation event: %v", err)
		}
		const fingerprint = "bcfda3dfb83a78cf7ad4043b1d0b0966c339e6d28c490e2c01524b62782162c9"
		const id = "evt_bcfda3dfb83a78cf7ad4043b1d0b0966"
		if event.Fingerprint != fingerprint || event.ID != id {
			t.Fatalf("foundation identity = %q / %q, want %q / %q", event.Fingerprint, event.ID, fingerprint, id)
		}
	})

	t.Run("semantic event", func(t *testing.T) {
		event, err := newSemanticEvent(
			"claim.admitted",
			"claim",
			"claim-stable",
			map[string]any{
				"schemaVersion": 1,
				"claimId":       "claim-stable",
				"analysisId":    "semantic-stable",
			},
			nil,
			now,
		)
		if err != nil {
			t.Fatalf("build semantic event: %v", err)
		}
		const fingerprint = "70f07b2be9d68a83aeaa45ea72c0cc46f7e9e86de8e436051739f4be9fba79a7"
		const id = "evt_1236dda6f2ef7bd91aff507ff473f6a8"
		if event.Fingerprint != fingerprint || event.ID != id {
			t.Fatalf("semantic identity = %q / %q, want %q / %q", event.Fingerprint, event.ID, fingerprint, id)
		}
	})
}
