package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/platform"
)

func TestAgentConfigurationAndJobFingerprintsAreStableAndConfigurationSensitive(t *testing.T) {
	configuration := validAgentConfiguration(t)
	payload := AgentJobPayloadV1{
		SchemaVersion: AgentJobPayloadSchemaVersion,
		Inputs: KnowledgeInputRefsV1{
			AnalysisRunID: "analysis_one",
			ClaimIDs:      []string{"claim_one", "claim_two"},
		},
		Configuration: configuration,
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("validate payload: %v", err)
	}
	first, err := AgentJobFingerprint(
		"event_one",
		AgentIdentity{Name: "content-scout", Version: "content-scout-v1"},
		payload,
	)
	if err != nil {
		t.Fatalf("fingerprint job: %v", err)
	}
	second, err := AgentJobFingerprint(
		"event_one",
		AgentIdentity{Name: "content-scout", Version: "content-scout-v1"},
		payload,
	)
	if err != nil || second != first {
		t.Fatalf("repeat fingerprint = %q, %v; want %q", second, err, first)
	}

	changed := configuration
	changed.HandlerConfigurationJSON = json.RawMessage(`{"approvedPublicTerms":["Go","SQLite"]}`)
	changed.Digest, err = AgentConfigurationDigest(changed)
	if err != nil {
		t.Fatalf("digest changed configuration: %v", err)
	}
	payload.Configuration = changed
	changedFingerprint, err := AgentJobFingerprint(
		"event_one",
		AgentIdentity{Name: "content-scout", Version: "content-scout-v1"},
		payload,
	)
	if err != nil {
		t.Fatalf("fingerprint changed job: %v", err)
	}
	if changedFingerprint == first {
		t.Fatal("changed configuration reused the original job fingerprint")
	}
}

func TestAgentConfigurationCanonicalizesHandlerConfigurationForIdentity(t *testing.T) {
	configuration := validAgentConfiguration(t)
	originalDigest := configuration.Digest
	configuration.HandlerConfigurationJSON = json.RawMessage(`{
		"limits": {"maximumIdeas": 5, "maximumClaims": 128},
		"approvedPublicTerms": ["Go"]
	}`)
	configuration.Digest = ""
	first, err := AgentConfigurationDigest(configuration)
	if err != nil {
		t.Fatalf("digest formatted configuration: %v", err)
	}
	configuration.HandlerConfigurationJSON = json.RawMessage(
		`{"approvedPublicTerms":["Go"],"limits":{"maximumClaims":128,"maximumIdeas":5}}`,
	)
	second, err := AgentConfigurationDigest(configuration)
	if err != nil {
		t.Fatalf("digest reordered configuration: %v", err)
	}
	if first != second {
		t.Fatalf("configuration digests differ: %q != %q", first, second)
	}

	configuration = validAgentConfiguration(t)
	configuration.Digest = strings.Repeat("f", 64)
	if err := configuration.Validate(); err == nil {
		t.Fatal("mismatched configuration digest was accepted")
	}
	if originalDigest == configuration.Digest {
		t.Fatal("test did not change the configuration digest")
	}
}

func TestAgentRunResultEnforcesOutcomeAndInvocationMatrix(t *testing.T) {
	execution := validExecutionIdentity()
	receipt := validExecutionReceipt()
	tests := []struct {
		name    string
		result  AgentRunResultV1
		wantErr bool
	}{
		{
			name: "skipped no claims",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeSucceeded,
				Disposition:   AgentExecutionDispositionSkipped,
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				ArtifactIDs:   []string{},
			},
		},
		{
			name: "successful invocation",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeSucceeded,
				Disposition:   AgentExecutionDispositionInvoked,
				Execution:     &execution,
				Receipt:       &receipt,
				Privacy: AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{{
					Name: "postflight", PolicyVersion: "deterministic-privacy-v1",
					Outcome: AgentPolicyOutcomePassed, Categories: []AgentPolicyCategoryCountV1{},
				}}},
				ArtifactIDs: []string{"artifact_one"},
			},
		},
		{
			name: "pre-executor failure",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeFailed,
				Disposition:   AgentExecutionDispositionNone,
				Execution:     &execution,
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				Failure: &AgentFailureV1{
					Stage: AgentFailureStagePreparation, Category: "input-invalid",
				},
				ArtifactIDs: []string{},
			},
		},
		{
			name: "invoked failure without receipt",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeFailed,
				Disposition:   AgentExecutionDispositionInvoked,
				Execution:     &execution,
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				Failure: &AgentFailureV1{
					Stage: AgentFailureStageExecution, Category: "executor-unavailable",
				},
				ArtifactIDs: []string{},
			},
		},
		{
			name: "post-executor admission mislabeled not invoked",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeFailed,
				Disposition:   AgentExecutionDispositionNone,
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				Failure: &AgentFailureV1{
					Stage: AgentFailureStageAdmission, Category: "candidate-invalid",
				},
				ArtifactIDs: []string{},
			},
			wantErr: true,
		},
		{
			name: "skipped with receipt",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeSucceeded,
				Disposition:   AgentExecutionDispositionSkipped,
				Receipt:       &receipt,
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				ArtifactIDs:   []string{},
			},
			wantErr: true,
		},
		{
			name: "failed with artifact",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeFailed,
				Disposition:   AgentExecutionDispositionInvoked,
				Execution:     &execution,
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				Failure: &AgentFailureV1{
					Stage: AgentFailureStageAdmission, Category: "candidate-invalid",
				},
				ArtifactIDs: []string{"artifact_one"},
			},
			wantErr: true,
		},
		{
			name: "invoked failure with empty observed receipt",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeFailed,
				Disposition:   AgentExecutionDispositionInvoked,
				Execution:     &execution,
				Receipt:       &AgentExecutionReceiptV1{},
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				Failure: &AgentFailureV1{
					Stage: AgentFailureStageExecution, Category: AgentFailureCategoryExecutorFailed,
				},
				ArtifactIDs: []string{},
			},
			wantErr: true,
		},
		{
			name: "successful invocation with blocked policy",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeSucceeded,
				Disposition:   AgentExecutionDispositionInvoked,
				Execution:     &execution,
				Receipt:       &receipt,
				Privacy: AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{{
					Name: "postflight", PolicyVersion: "deterministic-privacy-v1",
					Outcome: AgentPolicyOutcomeBlocked, Categories: []AgentPolicyCategoryCountV1{},
				}}},
				ArtifactIDs: []string{"artifact_one"},
			},
			wantErr: true,
		},
		{
			name: "failed with unregistered category",
			result: AgentRunResultV1{
				SchemaVersion: AgentRunResultSchemaVersion,
				Outcome:       AgentRunOutcomeFailed,
				Disposition:   AgentExecutionDispositionNone,
				Privacy:       AgentPrivacyOutcomeV1{CompletedStages: []AgentPolicyStageV1{}},
				Failure: &AgentFailureV1{
					Stage: AgentFailureStagePreparation, Category: "made-up-category",
				},
				ArtifactIDs: []string{},
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.result.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestAgentExecutionRequestAndResponseValidatePortableEnvelope(t *testing.T) {
	configuration := validAgentConfiguration(t)
	request := AgentExecutionRequestV1{
		ContractVersion: AgentExecutionContractVersion,
		JobID:           "job_one",
		JobFingerprint:  strings.Repeat("1", 64),
		TriggerEventID:  "event_one",
		Agent:           AgentIdentity{Name: "content-scout", Version: "content-scout-v1"},
		Configuration:   configuration,
		Execution:       validExecutionIdentity(),
		Input: AgentExecutionInputV1{
			SchemaName: "content-scout-input", SchemaVersion: 1,
			SchemaDigest:  strings.Repeat("2", 64),
			CanonicalJSON: json.RawMessage(`{"analysisRunId":"analysis_one","schemaVersion":1}`),
		},
		RequiredOutputSchema: configuration.OutputSchema,
		DeadlineAt:           time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC),
		Authority:            AgentExecutionAuthorityV1{AllowRemote: true},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate execution request: %v", err)
	}
	response := AgentExecutionResponseV1{
		ContractVersion: AgentExecutionContractVersion,
		CandidateJSON:   json.RawMessage(`{"ideas":[]}`),
		Receipt:         validExecutionReceipt(),
	}
	if err := response.Validate(); err != nil {
		t.Fatalf("validate execution response: %v", err)
	}
	response.CandidateJSON = json.RawMessage(`{ "ideas": [] }`)
	if err := response.Validate(); err != nil {
		t.Fatalf("validate formatted candidate JSON: %v", err)
	}
}

func TestContentIdeaArtifactFingerprintIgnoresRankButCommitsToContentAndLineage(t *testing.T) {
	idea := validContentIdea()
	evidence := []EvidenceRef{{ID: "evidence_one"}}
	first, err := ContentIdeaArtifactFingerprint(
		strings.Repeat("1", 64),
		idea,
		[]string{"fact_one"},
		evidence,
		[]EvidenceRef{},
		ArtifactSafetyReviewRequired,
	)
	if err != nil {
		t.Fatalf("fingerprint artifact: %v", err)
	}
	idea.Rank = 2
	reordered, err := ContentIdeaArtifactFingerprint(
		strings.Repeat("1", 64),
		idea,
		[]string{"fact_one"},
		evidence,
		[]EvidenceRef{},
		ArtifactSafetyReviewRequired,
	)
	if err != nil || reordered != first {
		t.Fatalf("rank-only fingerprint = %q, %v; want %q", reordered, err, first)
	}
	idea.Hook = "A different hook"
	changed, err := ContentIdeaArtifactFingerprint(
		strings.Repeat("1", 64),
		idea,
		[]string{"fact_one"},
		evidence,
		[]EvidenceRef{},
		ArtifactSafetyReviewRequired,
	)
	if err != nil {
		t.Fatalf("fingerprint changed artifact: %v", err)
	}
	if changed == first {
		t.Fatal("changed content reused the original artifact fingerprint")
	}
}

func TestContentScoutCandidatesRejectExactDuplicates(t *testing.T) {
	idea := validContentIdea()
	candidate := ContentIdeaCandidateV1{
		Concept: idea.Concept, CoreLesson: idea.CoreLesson,
		AudienceBenefit: idea.AudienceBenefit, Hook: idea.Hook,
		Resonance: idea.Resonance, Confidence: idea.Confidence,
		ShortPost: idea.ShortPost, Thread: idea.Thread, Article: idea.Article,
		ClaimIDs: idea.ClaimIDs,
	}
	if err := (ContentScoutCandidatesV1{
		Ideas: []ContentIdeaCandidateV1{candidate, candidate},
	}).Validate(); err == nil {
		t.Fatal("duplicate content candidates were accepted")
	}
}

func validAgentConfiguration(t *testing.T) AgentConfigurationIdentity {
	t.Helper()
	value := AgentConfigurationIdentity{
		PromptVersion: "content-scout-prompt-v1",
		OutputSchema: StructuredOutputSchemaIdentity{
			Name: "content-scout-candidates", Version: 1,
			Disposition: StructuredOutputDispositionStrict,
			Digest:      strings.Repeat("a", 64),
		},
		Route: AgentRouteIdentity{
			Alias: "content-scout-v1", Gateway: "vercel-ai-gateway",
			Model: "openai/gpt-5.4-mini", Provider: "azure",
			RouteVersion: "content-scout-route-v1", ServiceTier: "flex",
		},
		PrivacyPolicyVersion:     "deterministic-privacy-v1",
		DisclosurePolicyVersion:  "content-disclosure-v1",
		SafetyPolicyVersion:      "content-safety-v1",
		RetrievalPolicyVersion:   "content-scout-knowledge-v1",
		HandlerConfigurationJSON: json.RawMessage(`{"approvedPublicTerms":["Go"]}`),
	}
	var err error
	value.Digest, err = AgentConfigurationDigest(value)
	if err != nil {
		t.Fatalf("digest configuration: %v", err)
	}
	return value
}

func validExecutionIdentity() AgentExecutionIdentity {
	return AgentExecutionIdentity{
		ExecutorKind: "eve", ExecutorVersion: "0.27.8",
		AgentDefinitionDigest: strings.Repeat("b", 64),
		ContractVersion:       AgentExecutionContractVersion,
		RecoveryPolicyVersion: "eve-0.27.8-default-recovery-v1",
	}
}

func validExecutionReceipt() AgentExecutionReceiptV1 {
	latency := int64(850)
	return AgentExecutionReceiptV1{
		ExecutorKind: "eve", ExecutorVersion: "0.27.8",
		SessionID: "session_one", TurnID: "turn_one", CompletedModelSteps: 1,
		RequestedRoute: &AgentRouteIdentity{
			Alias: "content-scout-v1", Gateway: "vercel-ai-gateway",
			Model: "openai/gpt-5.4-mini", Provider: "azure",
			RouteVersion: "content-scout-route-v1", ServiceTier: "flex",
		},
		GatewayGenerationID: "generation_one",
		Usage: &AgentUsageV1{
			InputTokens: 300, OutputTokens: 140, TotalTokens: 440,
		},
		CostUSD: "0.00042", LatencyMilliseconds: &latency,
	}
}

func validContentIdea() ContentIdeaV1 {
	return ContentIdeaV1{
		Rank: 1, Concept: "Portable agent boundaries",
		CoreLesson:      "Keep durable domain authority outside the executor.",
		AudienceBenefit: "Developers can replace agent runtimes safely.",
		Hook:            "Your agent framework should be replaceable.",
		Resonance:       "Early experiments often leak framework choices into the core.",
		Confidence:      0.9,
		ShortPost: ContentFormatAngleV1{
			Suitable: true, Angle: "State the boundary and its benefit.",
		},
		Thread: ContentFormatAngleV1{
			Suitable: true, Angle: "Walk through event, job, and artifact ownership.",
		},
		Article: ContentFormatAngleV1{
			Suitable: true, Angle: "Compare portable and framework-owned architectures.",
		},
		ClaimIDs: []string{"claim_one"},
	}
}

func TestArtifactEnvelopeValidatesCanonicalPayloadAndDerivedIdentity(t *testing.T) {
	idea := validContentIdea()
	marshalIdea := func(value ContentIdeaV1) json.RawMessage {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encode idea: %v", err)
		}
		return payload
	}
	payload := marshalIdea(idea)
	jobFingerprint := strings.Repeat("1", 64)
	evidence := []EvidenceRef{{ID: "evidence_one"}}
	fingerprint, err := ContentIdeaArtifactFingerprint(
		jobFingerprint, idea, []string{"fact_one"}, evidence,
		[]EvidenceRef{}, ArtifactSafetyReviewRequired,
	)
	if err != nil {
		t.Fatalf("fingerprint artifact: %v", err)
	}
	artifact := Artifact{
		ID: platform.DerivedID("artifact_", fingerprint), Fingerprint: fingerprint,
		Kind: ArtifactKindContentIdea, SchemaVersion: ContentIdeaSchemaVersion,
		PayloadJSON: payload, RunID: "run_one", TriggerEventID: "event_one",
		JobFingerprint: jobFingerprint,
		Inputs: KnowledgeInputRefsV1{
			AnalysisRunID: "analysis_one", ClaimIDs: []string{"claim_one"},
		},
		ClaimIDs: []string{"claim_one"}, FactIDs: []string{"fact_one"},
		SupportingEvidence: evidence, ContradictingEvidence: []EvidenceRef{},
		ProposalStatus: ArtifactProposalReviewRequired,
		SafetyStatus:   ArtifactSafetyReviewRequired,
		CreatedAt:      time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC),
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("validate artifact: %v", err)
	}
	reranked := artifact
	rerankedIdea := idea
	rerankedIdea.Rank = 2
	reranked.PayloadJSON = marshalIdea(rerankedIdea)
	if err := reranked.Validate(); err != nil {
		t.Fatalf("validate rank-only artifact change: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Artifact)
	}{
		{"mismatched id", func(value *Artifact) { value.ID = "artifact_wrong" }},
		{"unknown payload field", func(value *Artifact) {
			value.PayloadJSON = json.RawMessage(`{"unexpected":true}`)
		}},
		{"unsafe proposal status", func(value *Artifact) { value.ProposalStatus = "published" }},
		{"unsafe safety status", func(value *Artifact) { value.SafetyStatus = "safe" }},
		{"payload claim mismatch", func(value *Artifact) {
			changed := idea
			changed.ClaimIDs = []string{"claim_other"}
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale concept fingerprint", func(value *Artifact) {
			changed := idea
			changed.Concept = "A changed concept"
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale core lesson fingerprint", func(value *Artifact) {
			changed := idea
			changed.CoreLesson = "A changed lesson."
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale audience benefit fingerprint", func(value *Artifact) {
			changed := idea
			changed.AudienceBenefit = "A changed benefit."
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale hook fingerprint", func(value *Artifact) {
			changed := idea
			changed.Hook = "A changed hook."
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale resonance fingerprint", func(value *Artifact) {
			changed := idea
			changed.Resonance = "A changed reason."
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale confidence fingerprint", func(value *Artifact) {
			changed := idea
			changed.Confidence = 0.8
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale short post fingerprint", func(value *Artifact) {
			changed := idea
			changed.ShortPost.Angle = "A changed short-post angle."
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale thread fingerprint", func(value *Artifact) {
			changed := idea
			changed.Thread.Angle = "A changed thread angle."
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale article fingerprint", func(value *Artifact) {
			changed := idea
			changed.Article.Angle = "A changed article angle."
			value.PayloadJSON = marshalIdea(changed)
		}},
		{"stale claim lineage fingerprint", func(value *Artifact) {
			changed := idea
			changed.ClaimIDs = []string{"claim_other"}
			value.PayloadJSON = marshalIdea(changed)
			value.ClaimIDs = []string{"claim_other"}
			value.Inputs.ClaimIDs = []string{"claim_other"}
		}},
		{"stale fact lineage fingerprint", func(value *Artifact) {
			value.FactIDs = []string{"fact_other"}
		}},
		{"stale supporting evidence fingerprint", func(value *Artifact) {
			value.SupportingEvidence = []EvidenceRef{{ID: "evidence_other"}}
		}},
		{"stale contradicting evidence fingerprint", func(value *Artifact) {
			value.ContradictingEvidence = []EvidenceRef{{ID: "evidence_other"}}
		}},
		{"stale job fingerprint", func(value *Artifact) {
			value.JobFingerprint = strings.Repeat("2", 64)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := artifact
			test.mutate(&changed)
			if err := changed.Validate(); err == nil {
				t.Fatal("invalid artifact was accepted")
			}
		})
	}
}
