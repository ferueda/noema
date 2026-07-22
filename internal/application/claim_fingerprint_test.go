package application

import (
	"math"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func TestClaimFingerprintMatchesAdmissionAndIgnoresRunMetadata(t *testing.T) {
	input, refs := validationInput()
	candidate := validationCandidate(domain.ClaimTypeLesson, domain.ClaimStatusInferred, refs["human"].ID)
	candidate.Statement = "  A reusable lesson  "
	config := validationConfig()
	claim := admitOneClaim(t, input, candidate, config)

	fingerprint, err := ClaimFingerprint(config.ProcessingKey, claim)
	if err != nil {
		t.Fatalf("fingerprint admitted claim: %v", err)
	}
	if fingerprint != claim.Fingerprint || claim.ID != platform.DerivedID("claim_", fingerprint) {
		t.Fatalf("admitted identity = %s/%s, recalculated %s", claim.ID, claim.Fingerprint, fingerprint)
	}
	if claim.Statement != "A reusable lesson" {
		t.Fatalf("claim statement was not normalized: %q", claim.Statement)
	}

	replayed := cloneFingerprintClaim(claim)
	replayed.ID = "ignored-id"
	replayed.Fingerprint = "ignored-fingerprint"
	replayed.AnalysisRunID = "another-run"
	replayed.CreatedAt = claim.CreatedAt.Add(24 * time.Hour)
	replayedFingerprint, err := ClaimFingerprint(config.ProcessingKey, replayed)
	if err != nil {
		t.Fatalf("fingerprint replayed claim: %v", err)
	}
	if replayedFingerprint != fingerprint {
		t.Fatalf("run metadata changed claim identity: %s != %s", replayedFingerprint, fingerprint)
	}
}

func TestClaimFingerprintIncludesEveryNormalizedIdentityField(t *testing.T) {
	input, refs := validationInput()
	claim := admitOneClaim(
		t, input,
		validationCandidate(domain.ClaimTypeLesson, domain.ClaimStatusInferred, refs["human"].ID),
		validationConfig(),
	)
	processingKey := validationConfig().ProcessingKey
	base, err := ClaimFingerprint(processingKey, claim)
	if err != nil {
		t.Fatalf("fingerprint base claim: %v", err)
	}

	tests := []struct {
		name   string
		change func(*string, *domain.Claim)
	}{
		{name: "processing key", change: func(key *string, _ *domain.Claim) { *key = "another-processing-key" }},
		{name: "type", change: func(_ *string, value *domain.Claim) { value.Type = domain.ClaimTypeProblem }},
		{name: "statement", change: func(_ *string, value *domain.Claim) { value.Statement = "Another statement" }},
		{name: "status", change: func(_ *string, value *domain.Claim) { value.Status = domain.ClaimStatusUncertain }},
		{name: "confidence", change: func(_ *string, value *domain.Claim) { value.Confidence = 0.5 }},
		{name: "supporting evidence", change: func(_ *string, value *domain.Claim) {
			value.SupportingEvidence[0].ContentHash = "another-content-hash"
		}},
		{name: "contradicting evidence", change: func(_ *string, value *domain.Claim) {
			value.ContradictingEvidence = append(value.ContradictingEvidence, refs["model"])
		}},
		{name: "supporting fact IDs", change: func(_ *string, value *domain.Claim) {
			value.SupportingFactIDs = append(value.SupportingFactIDs, "fact-one")
		}},
		{name: "outcome", change: func(_ *string, value *domain.Claim) { value.Outcome = domain.FactOutcomeUnknown }},
		{name: "actor", change: func(_ *string, value *domain.Claim) { value.Actor = "human" }},
		{name: "origin", change: func(_ *string, value *domain.Claim) { value.Origin = "human" }},
		{name: "subject", change: func(_ *string, value *domain.Claim) { value.Subject = "subject" }},
		{name: "scope", change: func(_ *string, value *domain.Claim) { value.Scope = "scope" }},
		{name: "attribution", change: func(_ *string, value *domain.Claim) {
			value.Attribution = domain.ClaimAttributionUnknown
		}},
		{name: "extractor name", change: func(_ *string, value *domain.Claim) { value.ExtractorName = "another-extractor" }},
		{name: "extractor version", change: func(_ *string, value *domain.Claim) { value.ExtractorVersion = "2" }},
		{name: "schema version", change: func(_ *string, value *domain.Claim) { value.SchemaVersion = 2 }},
		{name: "prompt version", change: func(_ *string, value *domain.Claim) { value.PromptVersion = "prompt-v2" }},
		{name: "route alias", change: func(_ *string, value *domain.Claim) { value.RequestedRoute.Alias = "another-route" }},
		{name: "route gateway", change: func(_ *string, value *domain.Claim) { value.RequestedRoute.Gateway = "another-gateway" }},
		{name: "route model", change: func(_ *string, value *domain.Claim) { value.RequestedRoute.Model = "another/model" }},
		{name: "route provider", change: func(_ *string, value *domain.Claim) { value.RequestedRoute.Provider = "another-provider" }},
		{name: "route version", change: func(_ *string, value *domain.Claim) { value.RequestedRoute.RouteVersion = "route-v2" }},
		{name: "privacy policy version", change: func(_ *string, value *domain.Claim) {
			value.RequestedRoute.PrivacyPolicyVersion = "privacy-v2"
		}},
		{name: "resolved provider", change: func(_ *string, value *domain.Claim) { value.ResolvedProvider = "another-provider" }},
		{name: "resolved model", change: func(_ *string, value *domain.Claim) { value.ResolvedModel = "another-model" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := processingKey
			changed := cloneFingerprintClaim(claim)
			test.change(&key, &changed)
			fingerprint, err := ClaimFingerprint(key, changed)
			if err != nil {
				t.Fatalf("fingerprint changed claim: %v", err)
			}
			if fingerprint == base {
				t.Fatalf("%s did not change claim identity", test.name)
			}
		})
	}
}

func TestClaimFingerprintRejectsIncompleteOrUnencodableInput(t *testing.T) {
	input, refs := validationInput()
	claim := admitOneClaim(
		t, input,
		validationCandidate(domain.ClaimTypeLesson, domain.ClaimStatusInferred, refs["human"].ID),
		validationConfig(),
	)
	if _, err := ClaimFingerprint("", claim); err == nil {
		t.Fatal("empty processing key was accepted")
	}
	claim.Confidence = math.NaN()
	if _, err := ClaimFingerprint(validationConfig().ProcessingKey, claim); err == nil {
		t.Fatal("unencodable claim was accepted")
	}
}

func cloneFingerprintClaim(claim domain.Claim) domain.Claim {
	clone := claim
	clone.SupportingEvidence = cloneFingerprintEvidence(claim.SupportingEvidence)
	clone.ContradictingEvidence = cloneFingerprintEvidence(claim.ContradictingEvidence)
	if claim.SupportingFactIDs != nil {
		clone.SupportingFactIDs = append([]string{}, claim.SupportingFactIDs...)
	}
	return clone
}

func cloneFingerprintEvidence(evidence []domain.EvidenceRef) []domain.EvidenceRef {
	if evidence == nil {
		return nil
	}
	return append([]domain.EvidenceRef{}, evidence...)
}
