package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaimEnumsRejectUnknownValues(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{name: "claim type", valid: ClaimType("unknown-type").Valid()},
		{name: "claim status", valid: ClaimStatus("unknown-status").Valid()},
		{name: "claim attribution", valid: ClaimAttribution("unknown-attribution").Valid()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.valid {
				t.Fatal("unknown enum value was accepted")
			}
		})
	}
	if !ClaimTypeLesson.Valid() || !ClaimStatusObserved.Valid() || !ClaimAttributionUnknown.Valid() {
		t.Fatal("known enum value was rejected")
	}
}

func TestGeneratedClaimActorAndOriginVocabulariesAreClosed(t *testing.T) {
	for _, value := range []string{"", "human", "model", "tool", "system"} {
		if !ValidClaimActor(value) {
			t.Fatalf("known actor %q was rejected", value)
		}
	}
	for _, value := range []string{"unknown", "agent", "Human", " human "} {
		if ValidClaimActor(value) {
			t.Fatalf("unknown actor %q was accepted", value)
		}
	}
	for _, value := range []string{"", "human", "injected", "delegated", "replayed-copied", "model", "tool", "system"} {
		if !ValidClaimOrigin(value) {
			t.Fatalf("known origin %q was rejected", value)
		}
	}
	for _, value := range []string{"unknown", "agent", "Human", " human "} {
		if ValidClaimOrigin(value) {
			t.Fatalf("unknown origin %q was accepted", value)
		}
	}
}

func TestFactAnalysisRunOmitsSemanticMetadata(t *testing.T) {
	encoded, err := json.Marshal(AnalysisRun{Stage: AnalysisStageFacts})
	if err != nil {
		t.Fatalf("marshal analysis run: %v", err)
	}
	for _, field := range []string{"inputFactIds", "claimIds", "model"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Fatalf("fact analysis JSON contains semantic field %q: %s", field, encoded)
		}
	}
}
