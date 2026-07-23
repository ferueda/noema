package application

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

func TestAdmitClaimCandidatesAcceptsEmptyObservedAndInferredResults(t *testing.T) {
	input, refs := validationInput()
	config := validationConfig()

	claims, err := AdmitClaimCandidates(input, nil, config)
	if err != nil || len(claims) != 0 {
		t.Fatalf("empty claims = %#v, %v", claims, err)
	}

	observed := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusObserved, refs["human"].ID)
	observed.Statement = "  A bounded problem  "
	observed.Subject = "  request handling  "
	observed.Scope = "  selected session  "
	observed.Actor = "human"
	observed.Origin = "human"
	claims, err = AdmitClaimCandidates(input, []domain.ClaimCandidate{observed}, config)
	if err != nil {
		t.Fatalf("admit observed claim: %v", err)
	}
	if len(claims) != 1 || claims[0].Statement != "A bounded problem" ||
		claims[0].Subject != "request handling" || claims[0].Scope != "selected session" ||
		claims[0].Actor != "human" || claims[0].Origin != "human" {
		t.Fatalf("observed claim = %#v", claims)
	}

	inferred := validationCandidate(domain.ClaimTypeLesson, domain.ClaimStatusInferred, refs["model"].ID)
	claims, err = AdmitClaimCandidates(input, []domain.ClaimCandidate{inferred}, config)
	if err != nil || len(claims) != 1 || claims[0].Status != domain.ClaimStatusInferred {
		t.Fatalf("inferred claim = %#v, %v", claims, err)
	}
}

func TestAdmitClaimCandidatesRejectsInvalidScalarFields(t *testing.T) {
	input, refs := validationInput()
	base := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusObserved, refs["human"].ID)
	tests := []struct {
		name   string
		change func(*domain.ClaimCandidate)
		match  string
	}{
		{name: "blank statement", change: func(value *domain.ClaimCandidate) { value.Statement = " \t" }, match: "invalid statement"},
		{name: "oversized statement", change: func(value *domain.ClaimCandidate) { value.Statement = strings.Repeat("x", maxClaimStatementBytes+1) }, match: "invalid statement"},
		{name: "too many supporting evidence ids", change: func(value *domain.ClaimCandidate) {
			value.SupportingEvidenceIDs = make([]string, maxClaimEvidenceRefs+1)
		}, match: "too many evidence ids"},
		{name: "too many contradicting evidence ids", change: func(value *domain.ClaimCandidate) {
			value.ContradictingEvidenceIDs = make([]string, maxClaimEvidenceRefs+1)
		}, match: "too many evidence ids"},
		{name: "too many supporting fact ids", change: func(value *domain.ClaimCandidate) {
			value.SupportingFactIDs = make([]string, maxClaimFactRefs+1)
		}, match: "too many supporting fact ids"},
		{name: "invalid UTF-8", change: func(value *domain.ClaimCandidate) { value.Statement = string([]byte{0xff}) }, match: "invalid statement"},
		{name: "negative confidence", change: func(value *domain.ClaimCandidate) { value.Confidence = -0.01 }, match: "invalid confidence"},
		{name: "large confidence", change: func(value *domain.ClaimCandidate) { value.Confidence = 1.01 }, match: "invalid confidence"},
		{name: "NaN confidence", change: func(value *domain.ClaimCandidate) { value.Confidence = math.NaN() }, match: "invalid confidence"},
		{name: "infinite confidence", change: func(value *domain.ClaimCandidate) { value.Confidence = math.Inf(1) }, match: "invalid confidence"},
		{name: "unknown type", change: func(value *domain.ClaimCandidate) { value.Type = domain.ClaimType("other") }, match: "invalid type or status"},
		{name: "unknown status", change: func(value *domain.ClaimCandidate) { value.Status = domain.ClaimStatus("other") }, match: "invalid type or status"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			test.change(&candidate)
			assertClaimInvalid(t, input, candidate, validationConfig(), test.match)
		})
	}
}

func TestAdmitClaimCandidatesValidatesEvidenceSets(t *testing.T) {
	input, refs := validationInput()
	base := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusObserved, refs["human"].ID)
	tests := []struct {
		name   string
		change func(*domain.ClaimCandidate)
		match  string
	}{
		{name: "missing support", change: func(value *domain.ClaimCandidate) { value.SupportingEvidenceIDs = nil }, match: "supporting evidence is required"},
		{name: "duplicate support", change: func(value *domain.ClaimCandidate) {
			value.SupportingEvidenceIDs = []string{refs["human"].ID, refs["human"].ID}
		}, match: "duplicate or empty evidence id"},
		{name: "unknown support", change: func(value *domain.ClaimCandidate) { value.SupportingEvidenceIDs = []string{"missing"} }, match: "unknown evidence id"},
		{name: "duplicate contradiction", change: func(value *domain.ClaimCandidate) {
			value.ContradictingEvidenceIDs = []string{refs["model"].ID, refs["model"].ID}
		}, match: "duplicate or empty evidence id"},
		{name: "unknown contradiction", change: func(value *domain.ClaimCandidate) { value.ContradictingEvidenceIDs = []string{"missing"} }, match: "unknown evidence id"},
		{name: "overlap", change: func(value *domain.ClaimCandidate) { value.ContradictingEvidenceIDs = []string{refs["human"].ID} }, match: "supporting and contradicting evidence overlap"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			test.change(&candidate)
			assertClaimInvalid(t, input, candidate, validationConfig(), test.match)
		})
	}
}

func TestAdmitClaimCandidatesValidatesSupportingFacts(t *testing.T) {
	_, refs := validationInput()
	fact := validationFact("fact", "error-output", domain.FactOutcomeFailure, refs["result"])
	input := preparedValidationInput(refs, fact)
	candidate := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusInferred, refs["result"].ID)

	unknown := candidate
	unknown.SupportingFactIDs = []string{"missing"}
	assertClaimInvalid(t, input, unknown, validationConfig(), "unknown supporting fact id")

	duplicate := candidate
	duplicate.SupportingFactIDs = []string{fact.ID, fact.ID}
	assertClaimInvalid(t, input, duplicate, validationConfig(), "duplicate or empty supporting fact id")

	outsideRef := refs["result"]
	outsideRef.ID = "outside"
	outsideFact := validationFact("outside-fact", "error-output", domain.FactOutcomeFailure, outsideRef)
	outsideInput := preparedValidationInput(refs, outsideFact)
	outside := candidate
	outside.SupportingFactIDs = []string{outsideFact.ID}
	assertClaimInvalid(t, outsideInput, outside, validationConfig(), "supporting fact falls outside selection")

	mismatchedFact := fact
	mismatchedFact.Evidence = append([]domain.EvidenceRef(nil), fact.Evidence...)
	mismatchedFact.Evidence[0].Actor = "human"
	mismatchedInput := preparedValidationInput(refs, mismatchedFact)
	mismatched := candidate
	mismatched.SupportingFactIDs = []string{mismatchedFact.ID}
	assertClaimInvalid(t, mismatchedInput, mismatched, validationConfig(), "supporting fact falls outside selection")
}

func TestAdmitClaimCandidatesRequiresSupportedActorAndOrigin(t *testing.T) {
	input, refs := validationInput()
	base := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusInferred, refs["human"].ID)

	valid := base
	valid.SupportingEvidenceIDs = []string{refs["human"].ID, refs["unknown"].ID}
	valid.Actor = "human"
	valid.Origin = "human"
	if _, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{valid}, validationConfig()); err != nil {
		t.Fatalf("supported actor and origin: %v", err)
	}

	tests := []struct {
		name   string
		change func(*domain.ClaimCandidate)
		match  string
	}{
		{name: "unknown actor asserted", change: func(value *domain.ClaimCandidate) { value.Actor = "unknown" }, match: "invalid actor"},
		{name: "invalid actor", change: func(value *domain.ClaimCandidate) { value.Actor = "user" }, match: "invalid actor"},
		{name: "actor unsupported", change: func(value *domain.ClaimCandidate) {
			value.SupportingEvidenceIDs = []string{refs["unknown"].ID}
			value.Actor = "human"
		}, match: "actor is unsupported"},
		{name: "actor conflict", change: func(value *domain.ClaimCandidate) {
			value.SupportingEvidenceIDs = []string{refs["human"].ID, refs["model"].ID}
			value.Actor = "human"
		}, match: "actor conflicts"},
		{name: "unknown origin asserted", change: func(value *domain.ClaimCandidate) { value.Origin = "unknown" }, match: "invalid origin"},
		{name: "invalid origin", change: func(value *domain.ClaimCandidate) { value.Origin = "user" }, match: "invalid origin"},
		{name: "origin unsupported", change: func(value *domain.ClaimCandidate) {
			value.SupportingEvidenceIDs = []string{refs["unknown"].ID}
			value.Origin = "human"
		}, match: "origin is unsupported"},
		{name: "origin conflict", change: func(value *domain.ClaimCandidate) {
			value.SupportingEvidenceIDs = []string{refs["human"].ID, refs["model"].ID}
			value.Origin = "human"
		}, match: "origin conflicts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			test.change(&candidate)
			assertClaimInvalid(t, input, candidate, validationConfig(), test.match)
		})
	}
}

func TestAdmitClaimCandidatesRejectsCausalAttributionInV0(t *testing.T) {
	input, refs := validationInput()
	for _, attribution := range []domain.ClaimAttribution{
		domain.ClaimAttributionUser,
		domain.ClaimAttributionAgent,
		domain.ClaimAttributionEnvironment,
		domain.ClaimAttributionMixed,
		domain.ClaimAttribution("other"),
	} {
		candidate := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusInferred, refs["human"].ID)
		candidate.Attribution = attribution
		assertClaimInvalid(t, input, candidate, validationConfig(), "unsupported attribution")
	}

	unknown := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusInferred, refs["human"].ID)
	unknown.Attribution = domain.ClaimAttributionUnknown
	if _, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{unknown}, validationConfig()); err != nil {
		t.Fatalf("unknown attribution: %v", err)
	}

	for _, test := range []struct {
		name   string
		change func(*domain.ClaimCandidate)
	}{
		{name: "causal statement", change: func(candidate *domain.ClaimCandidate) {
			candidate.Statement = "The failure occurred because of the agent."
		}},
		{name: "decision statement", change: func(candidate *domain.ClaimCandidate) {
			candidate.Statement = "The user selected SQLite."
		}},
		{name: "implementation statement", change: func(candidate *domain.ClaimCandidate) {
			candidate.Statement = "The assistant implemented the workaround."
		}},
		{name: "trigger statement", change: func(candidate *domain.ClaimCandidate) {
			candidate.Statement = "The environment triggered the failure."
		}},
		{name: "subject", change: func(candidate *domain.ClaimCandidate) {
			candidate.Subject = "developer responsibility"
		}},
		{name: "scope", change: func(candidate *domain.ClaimCandidate) {
			candidate.Scope = "our coding behavior"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusInferred, refs["human"].ID)
			test.change(&candidate)
			assertClaimInvalid(t, input, candidate, validationConfig(), "unsupported attribution actor in free text")
		})
	}
}

func TestAdmitClaimCandidatesValidatesOutcomeFieldCombinations(t *testing.T) {
	input, refs := validationInput()
	tests := []struct {
		name      string
		candidate domain.ClaimCandidate
		match     string
	}{
		{
			name: "outcome on problem",
			candidate: func() domain.ClaimCandidate {
				value := validationCandidate(domain.ClaimTypeProblem, domain.ClaimStatusInferred, refs["human"].ID)
				value.Outcome = domain.FactOutcomeFailure
				return value
			}(),
			match: "outcome is not allowed",
		},
		{
			name:      "failed attempt without failure",
			candidate: validationCandidate(domain.ClaimTypeFailedAttempt, domain.ClaimStatusInferred, refs["human"].ID),
			match:     "failed attempt requires failure outcome",
		},
		{
			name: "failed attempt with success",
			candidate: func() domain.ClaimCandidate {
				value := validationCandidate(domain.ClaimTypeFailedAttempt, domain.ClaimStatusInferred, refs["human"].ID)
				value.Outcome = domain.FactOutcomeSuccess
				return value
			}(),
			match: "failed attempt requires failure outcome",
		},
		{
			name:      "verification without outcome",
			candidate: validationCandidate(domain.ClaimTypeVerification, domain.ClaimStatusInferred, refs["human"].ID),
			match:     "verification requires a supported outcome",
		},
		{
			name: "verification not applicable",
			candidate: func() domain.ClaimCandidate {
				value := validationCandidate(domain.ClaimTypeVerification, domain.ClaimStatusInferred, refs["human"].ID)
				value.Outcome = domain.FactOutcomeNotApplicable
				return value
			}(),
			match: "verification requires a supported outcome",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertClaimInvalid(t, input, test.candidate, validationConfig(), test.match)
		})
	}
}

func TestAdmitClaimCandidatesAppliesObservedOutcomeEvidenceRules(t *testing.T) {
	_, refs := validationInput()
	errorFact := validationFact("error", "error-output", domain.FactOutcomeFailure, refs["result"])
	successFact := validationFact("success", "exit-code", domain.FactOutcomeSuccess, refs["result"])
	failureFact := validationFact("failure", "test-result", domain.FactOutcomeFailure, refs["call"], refs["result"])
	unknownFact := validationFact("unknown", "test-result", domain.FactOutcomeUnknown, refs["call"], refs["result"])

	t.Run("error output alone cannot establish failed attempt", func(t *testing.T) {
		input := preparedValidationInput(refs, errorFact)
		candidate := observedOutcomeCandidate(domain.ClaimTypeFailedAttempt, domain.FactOutcomeFailure, refs["result"].ID, errorFact.ID)
		assertClaimInvalid(t, input, candidate, validationConfig(), "observed outcome is not established")
	})

	t.Run("error output plus successful exit cannot establish failure", func(t *testing.T) {
		input := preparedValidationInput(refs, errorFact, successFact)
		candidate := observedOutcomeCandidate(domain.ClaimTypeFailedAttempt, domain.FactOutcomeFailure, refs["result"].ID, errorFact.ID)
		assertClaimInvalid(t, input, candidate, validationConfig(), "observed outcome is not established")
	})

	t.Run("observed verification accepts matching success", func(t *testing.T) {
		input := preparedValidationInput(refs, successFact)
		candidate := observedOutcomeCandidate(domain.ClaimTypeVerification, domain.FactOutcomeSuccess, refs["result"].ID, successFact.ID)
		claims, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{candidate}, validationConfig())
		if err != nil || len(claims) != 1 || claims[0].Outcome != domain.FactOutcomeSuccess {
			t.Fatalf("claims = %#v, %v", claims, err)
		}
	})

	t.Run("linked unknown rejects observed success", func(t *testing.T) {
		input := preparedValidationInput(refs, successFact, unknownFact)
		candidate := observedOutcomeCandidate(domain.ClaimTypeVerification, domain.FactOutcomeSuccess, refs["result"].ID, successFact.ID)
		assertClaimInvalid(t, input, candidate, validationConfig(), "conflicts with linked result facts")
	})

	t.Run("linked failure rejects observed success", func(t *testing.T) {
		input := preparedValidationInput(refs, successFact, failureFact)
		candidate := observedOutcomeCandidate(domain.ClaimTypeVerification, domain.FactOutcomeSuccess, refs["result"].ID, successFact.ID)
		assertClaimInvalid(t, input, candidate, validationConfig(), "conflicts with linked result facts")
	})

	t.Run("observed unknown accepts unknown fact", func(t *testing.T) {
		input := preparedValidationInput(refs, unknownFact)
		candidate := observedOutcomeCandidate(domain.ClaimTypeVerification, domain.FactOutcomeUnknown, refs["result"].ID, unknownFact.ID)
		if _, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{candidate}, validationConfig()); err != nil {
			t.Fatalf("admit unknown verification: %v", err)
		}
	})

	t.Run("observed unknown accepts mixed result facts", func(t *testing.T) {
		input := preparedValidationInput(refs, successFact, failureFact)
		candidate := observedOutcomeCandidate(
			domain.ClaimTypeVerification, domain.FactOutcomeUnknown, refs["result"].ID, successFact.ID, failureFact.ID,
		)
		if _, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{candidate}, validationConfig()); err != nil {
			t.Fatalf("admit mixed verification: %v", err)
		}
	})
}

func TestAdmitClaimCandidatesRequiresLinkedContradictionForInferredOutcome(t *testing.T) {
	_, refs := validationInput()
	errorFact := validationFact("error", "error-output", domain.FactOutcomeFailure, refs["result"])
	successFact := validationFact("success", "exit-code", domain.FactOutcomeSuccess, refs["result"])
	input := preparedValidationInput(refs, errorFact, successFact)
	candidate := validationCandidate(domain.ClaimTypeFailedAttempt, domain.ClaimStatusInferred, refs["call"].ID)
	candidate.Outcome = domain.FactOutcomeFailure
	candidate.SupportingFactIDs = []string{errorFact.ID}

	assertClaimInvalid(t, input, candidate, validationConfig(), "conflicting result evidence is not cited")

	candidate.ContradictingEvidenceIDs = []string{refs["result"].ID}
	claims, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{candidate}, validationConfig())
	if err != nil || len(claims) != 1 || len(claims[0].ContradictingEvidence) != 1 {
		t.Fatalf("claims = %#v, %v", claims, err)
	}
}

func TestAdmitClaimCandidatesProducesStableRouteSensitiveIDs(t *testing.T) {
	input, refs := validationInput()
	candidate := validationCandidate(domain.ClaimTypeLesson, domain.ClaimStatusInferred, refs["human"].ID)
	config := validationConfig()
	first := admitOneClaim(t, input, candidate, config)

	same := config
	same.AnalysisRunID = "another-run"
	same.CreatedAt = same.CreatedAt.Add(24 * time.Hour)
	second := admitOneClaim(t, input, candidate, same)
	if first.ID != second.ID || first.Fingerprint != second.Fingerprint {
		t.Fatalf("stable identity changed: %s/%s vs %s/%s", first.ID, first.Fingerprint, second.ID, second.Fingerprint)
	}

	variants := []struct {
		name   string
		change func(*ClaimAdmissionConfig)
	}{
		{name: "processing key", change: func(value *ClaimAdmissionConfig) { value.ProcessingKey = "processing-two" }},
		{name: "extractor version", change: func(value *ClaimAdmissionConfig) { value.ExtractorVersion = "2" }},
		{name: "schema version", change: func(value *ClaimAdmissionConfig) { value.SchemaVersion = 2 }},
		{name: "prompt version", change: func(value *ClaimAdmissionConfig) {
			value.PromptVersion = "prompt-v2"
			value.Model.PromptVersion = "prompt-v2"
		}},
		{name: "route version", change: func(value *ClaimAdmissionConfig) { value.Model.RequestedRoute.RouteVersion = "route-v2" }},
		{name: "requested model", change: func(value *ClaimAdmissionConfig) { value.Model.RequestedRoute.Model = "openai/another-model" }},
		{name: "requested provider", change: func(value *ClaimAdmissionConfig) { value.Model.RequestedRoute.Provider = "another-provider" }},
		{name: "resolved provider", change: func(value *ClaimAdmissionConfig) { value.Model.ResolvedProvider = "another-provider" }},
		{name: "resolved model", change: func(value *ClaimAdmissionConfig) { value.Model.ResolvedModel = "another-model" }},
	}
	for _, test := range variants {
		t.Run(test.name, func(t *testing.T) {
			changed := config
			test.change(&changed)
			claim := admitOneClaim(t, input, candidate, changed)
			if claim.ID == first.ID || claim.Fingerprint == first.Fingerprint {
				t.Fatalf("identity did not change: %#v", claim)
			}
		})
	}
}

func validationInput() (PreparedSemanticInput, map[string]domain.EvidenceRef) {
	callOrdinal := 0
	refs := map[string]domain.EvidenceRef{
		"call":    validationRef("call-evidence", 0, "model", "model", "call-1", nil),
		"result":  validationRef("result-evidence", 1, "tool", "tool", "call-1", &callOrdinal),
		"human":   validationRef("human-evidence", 2, "human", "human", "", nil),
		"model":   validationRef("model-evidence", 3, "model", "model", "", nil),
		"unknown": validationRef("unknown-evidence", 4, "unknown", "unknown", "", nil),
	}
	return preparedValidationInput(refs), refs
}

func validationRef(id string, entry int, actor, origin, toolCallID string, related *int) domain.EvidenceRef {
	segment := 0
	return domain.EvidenceRef{
		ID: id, SourceKind: domain.EvidenceSourceSessions, SourceIdentity: "synthetic@local:claims",
		DocumentDigestScheme: "sha256-sessions-document-jcs-v1", DocumentDigest: strings.Repeat("d", 64),
		EntryOrdinal: entry, SegmentOrdinal: &segment, EntryKind: "message", Actor: actor,
		Origin: origin, OriginConfidence: "high", RelatedEntryOrdinal: cloneOptionalInt(related),
		ToolCallID: toolCallID, ContentHashScheme: "sha256-utf8-v1", ContentHash: strings.Repeat("a", 64),
	}
}

func validationFact(id, kind, outcome string, evidence ...domain.EvidenceRef) domain.Fact {
	return domain.Fact{ID: id, Kind: kind, Outcome: outcome, Evidence: append([]domain.EvidenceRef(nil), evidence...)}
}

func preparedValidationInput(refs map[string]domain.EvidenceRef, facts ...domain.Fact) PreparedSemanticInput {
	evidenceByID := make(map[string]domain.EvidenceRef, len(refs))
	for _, ref := range refs {
		evidenceByID[ref.ID] = ref
	}
	factsByID := make(map[string]domain.Fact, len(facts))
	for _, fact := range facts {
		factsByID[fact.ID] = fact
	}
	return PreparedSemanticInput{
		EvidenceByID: evidenceByID, FactsByID: factsByID,
		OrderedFacts: append([]domain.Fact(nil), facts...),
	}
}

func validationCandidate(claimType domain.ClaimType, status domain.ClaimStatus, supportID string) domain.ClaimCandidate {
	return domain.ClaimCandidate{
		Type: claimType, Statement: "Supported claim", Status: status, Confidence: 0.75,
		SupportingEvidenceIDs: []string{supportID}, ContradictingEvidenceIDs: []string{},
	}
}

func observedOutcomeCandidate(
	claimType domain.ClaimType,
	outcome, supportID string,
	factIDs ...string,
) domain.ClaimCandidate {
	candidate := validationCandidate(claimType, domain.ClaimStatusObserved, supportID)
	candidate.Outcome = outcome
	candidate.SupportingFactIDs = append([]string(nil), factIDs...)
	return candidate
}

func validationConfig() ClaimAdmissionConfig {
	return ClaimAdmissionConfig{
		AnalysisRunID: "analysis-one", ProcessingKey: "processing-one",
		ExtractorName: "semantic-claims", ExtractorVersion: "1",
		SchemaVersion: 1, PromptVersion: "prompt-v1", CreatedAt: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		Model: domain.ModelExecutionMetadata{
			RequestedRoute: domain.RequestedModelRoute{
				Alias: "semantic-v1", Gateway: "vercel-ai-gateway", Model: "openai/gpt-oss-120b",
				Provider: "cerebras", RouteVersion: "route-v1", PrivacyPolicyVersion: PrivacyPolicyVersion,
			},
			ResolvedProvider: "cerebras", ResolvedModel: "openai/gpt-oss-120b", PromptVersion: "prompt-v1",
		},
	}
}

func assertClaimInvalid(
	t *testing.T,
	input PreparedSemanticInput,
	candidate domain.ClaimCandidate,
	config ClaimAdmissionConfig,
	want string,
) {
	t.Helper()
	claims, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{candidate}, config)
	if !errors.Is(err, ErrClaimCandidateInvalid) || !strings.Contains(err.Error(), want) {
		t.Fatalf("claims/error = %#v / %v, want invalid containing %q", claims, err, want)
	}
	if claims != nil {
		t.Fatalf("invalid candidate returned claims: %#v", claims)
	}
}

func admitOneClaim(
	t *testing.T,
	input PreparedSemanticInput,
	candidate domain.ClaimCandidate,
	config ClaimAdmissionConfig,
) domain.Claim {
	t.Helper()
	claims, err := AdmitClaimCandidates(input, []domain.ClaimCandidate{candidate}, config)
	if err != nil || len(claims) != 1 {
		t.Fatalf("admit claim = %#v, %v", claims, err)
	}
	return claims[0]
}
