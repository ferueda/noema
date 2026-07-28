package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

type contentScoutKnowledgeReader struct {
	record       SemanticAnalysisRecord
	facts        map[string]domain.Fact
	requestedIDs []string
	loadErr      error
}

func (reader *contentScoutKnowledgeReader) LoadSemanticAnalysis(
	context.Context,
	string,
) (SemanticAnalysisRecord, error) {
	if reader.loadErr != nil {
		return SemanticAnalysisRecord{}, reader.loadErr
	}
	return reader.record, nil
}

func (reader *contentScoutKnowledgeReader) LoadFactsByID(
	_ context.Context,
	ids []string,
) ([]domain.Fact, error) {
	if reader.loadErr != nil {
		return nil, reader.loadErr
	}
	reader.requestedIDs = append([]string{}, ids...)
	result := make([]domain.Fact, 0, len(ids))
	for _, id := range ids {
		if fact, exists := reader.facts[id]; exists {
			result = append(result, fact)
		}
	}
	return result, nil
}

func TestContentScoutPrepareLoadsOnlyFirstReferencedFactsAndGeneralizesInput(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, []string{"Cerebras"})
	prepared, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
		context.Background(), fixture.job,
	)
	if err != nil {
		t.Fatalf("prepare Content Scout: %v", err)
	}
	if prepared.SkipNoClaims ||
		!slices.Equal(fixture.reader.requestedIDs, []string{"fact-one", "fact-two"}) ||
		len(prepared.Input.Claims) != 3 ||
		len(prepared.Input.Facts) != 2 {
		t.Fatalf("prepared input = %#v, requested = %#v", prepared.Input, fixture.reader.requestedIDs)
	}
	if strings.Contains(string(prepared.CanonicalJSON), "SecretProject") ||
		!strings.Contains(string(prepared.CanonicalJSON), disclosurePrivateIdentifier) ||
		!strings.Contains(string(prepared.CanonicalJSON), "Cerebras") {
		t.Fatalf("canonical input disclosure = %s", prepared.CanonicalJSON)
	}
	if len(prepared.Privacy.CompletedStages) != 2 ||
		prepared.Privacy.CompletedStages[0].Name != contentScoutPrivacyPreflight ||
		prepared.Privacy.CompletedStages[1].Name != contentScoutDisclosurePreflight ||
		prepared.Privacy.Validate() != nil {
		t.Fatalf("preflight policy outcome = %#v", prepared.Privacy)
	}
	if strings.Contains(string(prepared.CanonicalJSON), fixture.record.Analysis.Run.RequestedSourceIdentity) ||
		strings.Contains(string(prepared.CanonicalJSON), fixture.evidence.DocumentDigest) {
		t.Fatal("canonical input contains source identity")
	}
}

func TestContentScoutPrepareReturnsLocalZeroClaimResult(t *testing.T) {
	fixture := newContentScoutFixture(t, nil)
	fixture.record = contentScoutZeroClaimRecord(t)
	fixture.reader.record = fixture.record
	fixture.job = contentScoutJobForRecord(t, fixture.configuration, fixture.record)

	prepared, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
		context.Background(), fixture.job,
	)
	if err != nil {
		t.Fatalf("prepare zero-claim Content Scout: %v", err)
	}
	if !prepared.SkipNoClaims || len(prepared.Input.Claims) != 0 ||
		len(prepared.Input.Facts) != 0 ||
		len(prepared.Privacy.CompletedStages) != 0 ||
		len(fixture.reader.requestedIDs) != 0 {
		t.Fatalf("zero-claim preparation = %#v", prepared)
	}
}

func TestContentScoutPrepareFailsClosedOnMissingOrChangedKnowledge(t *testing.T) {
	tests := map[string]func(*contentScoutFixture){
		"missing fact": func(fixture *contentScoutFixture) {
			delete(fixture.reader.facts, "fact-two")
		},
		"reordered fact": func(fixture *contentScoutFixture) {
			first := fixture.reader.facts["fact-one"]
			second := fixture.reader.facts["fact-two"]
			fixture.reader.facts = map[string]domain.Fact{
				"fact-one": second,
				"fact-two": first,
			}
		},
		"changed event": func(fixture *contentScoutFixture) {
			fixture.job.EventID = "event-other"
		},
		"changed evidence": func(fixture *contentScoutFixture) {
			fact := fixture.reader.facts["fact-one"]
			fact.Evidence[0].DocumentDigest = strings.Repeat("e", 64)
			fixture.reader.facts["fact-one"] = fact
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newContentScoutFixture(t, nil)
			mutate(&fixture)
			_, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
				context.Background(), fixture.job,
			)
			var failure ContentScoutApplicationFailure
			if !errors.As(err, &failure) ||
				failure.Category != domain.AgentFailureCategoryInputInvalid {
				t.Fatalf("failure = %#v, %v", failure, err)
			}
		})
	}
}

func TestContentScoutPrepareAppliesPrivacyBeforeDisclosure(t *testing.T) {
	fixture := newContentScoutFixture(t, nil)
	fixture.record.Analysis.Claims[0].Statement =
		"Read /Users/person/private/file.go before the fix"
	fixture.reader.record = fixture.record
	prepared, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
		context.Background(), fixture.job,
	)
	if err != nil {
		t.Fatalf("prepare privacy-filtered input: %v", err)
	}
	if strings.Contains(string(prepared.CanonicalJSON), "/Users/person/private") ||
		!strings.Contains(string(prepared.CanonicalJSON), "redacted:local-path") ||
		prepared.Privacy.CompletedStages[0].Categories[0].Category != privacyLocalPath {
		t.Fatalf("privacy-filtered preparation = %s / %#v", prepared.CanonicalJSON, prepared.Privacy)
	}

	fixture = newContentScoutFixture(t, nil)
	fixture.record.Analysis.Claims[0].Statement =
		"Use sk-proj-abcdefghijklmnopqrstuvwxyz before the fix"
	fixture.reader.record = fixture.record
	_, err = (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
		context.Background(), fixture.job,
	)
	var failure ContentScoutApplicationFailure
	if !errors.As(err, &failure) ||
		failure.Category != domain.AgentFailureCategoryPrivacyBlocked ||
		len(failure.Privacy.CompletedStages) != 1 ||
		failure.Privacy.CompletedStages[0].Outcome != domain.AgentPolicyOutcomeBlocked ||
		strings.Contains(err.Error(), "sk-proj") {
		t.Fatalf("privacy-blocked preparation = %#v, %v", failure, err)
	}
}

func TestContentScoutAdmitFiltersUnsupportedCandidatesAndDerivesLineage(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	prepared := prepareContentScoutFixture(t, fixture)
	candidates := domain.ContentScoutCandidatesV1{Ideas: []domain.ContentIdeaCandidateV1{
		contentScoutCandidate("No supporting facts", []string{"claim-two"}),
		contentScoutCandidate("First useful idea", []string{"claim-one"}),
		contentScoutCandidate("Still no supporting facts", []string{"claim-two"}),
		contentScoutCandidate("Second useful idea", []string{"claim-three", "claim-one"}),
	}}
	admission, err := prepared.AdmitCandidates(
		marshalCandidates(t, candidates), "run-one",
		time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("admit Content Scout candidates: %v", err)
	}
	if len(admission.Artifacts) != 2 ||
		len(admission.Privacy.CompletedStages) != 4 {
		t.Fatalf("admission = %#v", admission)
	}
	first := admission.Artifacts[0]
	second := admission.Artifacts[1]
	if !slices.Equal(first.ClaimIDs, []string{"claim-one"}) ||
		!slices.Equal(first.FactIDs, []string{"fact-one"}) ||
		!slices.Equal(evidenceIDs(first.SupportingEvidence), []string{"evidence-one"}) ||
		!slices.Equal(second.ClaimIDs, []string{"claim-three", "claim-one"}) ||
		!slices.Equal(second.FactIDs, []string{"fact-two", "fact-one"}) ||
		!slices.Equal(
			evidenceIDs(second.SupportingEvidence),
			[]string{"evidence-two", "evidence-one"},
		) {
		t.Fatalf("artifact lineage = %#v / %#v", first, second)
	}
	var firstIdea, secondIdea domain.ContentIdeaV1
	if err := json.Unmarshal(first.PayloadJSON, &firstIdea); err != nil {
		t.Fatalf("decode first idea: %v", err)
	}
	if err := json.Unmarshal(second.PayloadJSON, &secondIdea); err != nil {
		t.Fatalf("decode second idea: %v", err)
	}
	if firstIdea.Rank != 1 || secondIdea.Rank != 2 ||
		first.ProposalStatus != domain.ArtifactProposalReviewRequired ||
		first.SafetyStatus != domain.ArtifactSafetyReviewRequired {
		t.Fatalf("idea ranks and safety = %#v / %#v", firstIdea, secondIdea)
	}
}

func TestContentScoutAdmitRejectsUnknownAndUnsafeOutputWithoutEchoingIt(
	t *testing.T,
) {
	for name, test := range map[string]struct {
		candidate domain.ContentIdeaCandidateV1
		category  string
	}{
		"unknown claim": {
			candidate: contentScoutCandidate("A useful idea", []string{"claim-unknown"}),
			category:  domain.AgentFailureCategoryCandidateInvalid,
		},
		"protected term": {
			candidate: contentScoutCandidate("SecretProject lesson", []string{"claim-one"}),
			category:  domain.AgentFailureCategoryDisclosureBlocked,
		},
		"novel identifier": {
			candidate: contentScoutCandidate("Avoid ABC-123 failures", []string{"claim-one"}),
			category:  domain.AgentFailureCategoryDisclosureBlocked,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newContentScoutFixture(t, nil)
			prepared := prepareContentScoutFixture(t, fixture)
			_, err := prepared.AdmitCandidates(
				marshalCandidates(t, domain.ContentScoutCandidatesV1{
					Ideas: []domain.ContentIdeaCandidateV1{test.candidate},
				}),
				"run-one",
				time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC),
			)
			var failure ContentScoutApplicationFailure
			if !errors.As(err, &failure) || failure.Category != test.category {
				t.Fatalf("failure = %#v, %v", failure, err)
			}
			if strings.Contains(err.Error(), test.candidate.Concept) {
				t.Fatalf("failure exposed generated text: %v", err)
			}
		})
	}
}

func TestContentScoutAdmitUsesStrictCandidateDecodingAndDuplicateChecks(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	prepared := prepareContentScoutFixture(t, fixture)
	valid := contentScoutCandidate("A practical idea", []string{"claim-one"})
	tests := map[string]json.RawMessage{
		"unknown field": json.RawMessage(`{"ideas":[],"extra":true}`),
		"duplicate claim": marshalCandidates(
			t,
			domain.ContentScoutCandidatesV1{Ideas: []domain.ContentIdeaCandidateV1{
				func() domain.ContentIdeaCandidateV1 {
					value := valid
					value.ClaimIDs = []string{"claim-one", "claim-one"}
					return value
				}(),
			}},
		),
		"duplicate output": marshalCandidates(
			t,
			domain.ContentScoutCandidatesV1{
				Ideas: []domain.ContentIdeaCandidateV1{valid, valid},
			},
		),
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := prepared.AdmitCandidates(
				document, "run-one",
				time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC),
			); err == nil {
				t.Fatal("invalid candidate document was accepted")
			}
		})
	}
}

func TestContentScoutAdmitAllowsExactApprovedPublicTerm(t *testing.T) {
	fixture := newContentScoutFixture(t, []string{"Cerebras"})
	prepared := prepareContentScoutFixture(t, fixture)
	candidate := contentScoutCandidate("A Cerebras lesson for developers", []string{"claim-three"})
	admission, err := prepared.AdmitCandidates(
		marshalCandidates(
			t,
			domain.ContentScoutCandidatesV1{
				Ideas: []domain.ContentIdeaCandidateV1{candidate},
			},
		),
		"run-one",
		time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("admit approved term: %v", err)
	}
	if len(admission.Artifacts) != 1 {
		t.Fatalf("approved-term artifacts = %#v", admission.Artifacts)
	}
}

func TestContentScoutAdmitAcceptsEmptyCandidateResult(t *testing.T) {
	fixture := newContentScoutFixture(t, nil)
	prepared := prepareContentScoutFixture(t, fixture)
	admission, err := prepared.AdmitCandidates(
		marshalCandidates(t, domain.ContentScoutCandidatesV1{Ideas: []domain.ContentIdeaCandidateV1{}}),
		"run-one",
		time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("admit empty candidates: %v", err)
	}
	if len(admission.Artifacts) != 0 ||
		len(admission.Privacy.CompletedStages) != 4 {
		t.Fatalf("empty admission = %#v", admission)
	}
}

func TestContentScoutArtifactIdentityDoesNotDependOnAssignedRank(t *testing.T) {
	fixture := newContentScoutFixture(t, nil)
	prepared := prepareContentScoutFixture(t, fixture)
	target := contentScoutCandidate("Stable target idea", []string{"claim-one"})
	first, err := prepared.AdmitCandidates(
		marshalCandidates(t, domain.ContentScoutCandidatesV1{Ideas: []domain.ContentIdeaCandidateV1{
			target,
		}}),
		"run-one",
		time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("admit target: %v", err)
	}
	second, err := prepared.AdmitCandidates(
		marshalCandidates(t, domain.ContentScoutCandidatesV1{Ideas: []domain.ContentIdeaCandidateV1{
			contentScoutCandidate("Stronger sibling", []string{"claim-three"}),
			target,
		}}),
		"run-two",
		time.Date(2026, 7, 28, 21, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("admit ranked target: %v", err)
	}
	if first.Artifacts[0].ID != second.Artifacts[1].ID {
		t.Fatalf(
			"rank changed artifact identity: %s / %s",
			first.Artifacts[0].ID, second.Artifacts[1].ID,
		)
	}
}

type contentScoutFixture struct {
	record        SemanticAnalysisRecord
	evidence      domain.EvidenceRef
	reader        *contentScoutKnowledgeReader
	job           AgentJobRecordV1
	configuration ContentScoutConfiguration
}

func newContentScoutFixture(
	t *testing.T,
	approvedPublicTerms []string,
) contentScoutFixture {
	t.Helper()
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	evidenceOne := contentScoutEvidence("evidence-one", 1)
	evidenceTwo := contentScoutEvidence("evidence-two", 2)
	claims := []domain.Claim{
		{
			ID: "claim-one", AnalysisRunID: "analysis-one",
			Type: domain.ClaimTypeLesson, Statement: "SecretProject showed a useful Go lesson",
			Status: domain.ClaimStatusInferred, Confidence: 0.9,
			SupportingEvidence: []domain.EvidenceRef{evidenceOne},
			SupportingFactIDs:  []string{"fact-one"},
		},
		{
			ID: "claim-two", AnalysisRunID: "analysis-one",
			Type: domain.ClaimTypeProblem, Statement: "A coding problem was investigated",
			Status: domain.ClaimStatusObserved, Confidence: 0.8,
			SupportingEvidence: []domain.EvidenceRef{evidenceOne},
		},
		{
			ID: "claim-three", AnalysisRunID: "analysis-one",
			Type: domain.ClaimTypeVerification, Statement: "Cerebras helped verify the result",
			Status: domain.ClaimStatusObserved, Confidence: 0.95,
			Outcome:               domain.FactOutcomeSuccess,
			SupportingEvidence:    []domain.EvidenceRef{evidenceTwo},
			ContradictingEvidence: []domain.EvidenceRef{},
			SupportingFactIDs:     []string{"fact-two", "fact-one"},
		},
	}
	claimIDs := []string{"claim-one", "claim-two", "claim-three"}
	inputFactIDs := []string{"fact-one", "fact-two", "fact-unused"}
	first, last := 0, 4
	record := contentScoutCompletionForTest(t, now, claimIDs)
	record.Analysis.Run.RequestedSourceIdentity = "private-source-identity"
	record.Analysis.Run.Selection = &domain.EvidenceSelection{
		Mode: "range",
		Entries: domain.EntrySelection{
			Selected: 5, Total: 10, Truncated: true,
			FirstOrdinal: &first, LastOrdinal: &last,
		},
		Segments: domain.CountSelection{Truncated: true},
		Coverage: semanticCoveragePartial,
	}
	record.Analysis.Run.InputFactIDs = append([]string{}, inputFactIDs...)
	record.Analysis.Run.Omissions = domain.AnalysisOmissions{
		CanonicalSegments: 1, OmittedTextFactCount: 1,
		OmittedTextOriginalUTF8Bytes: 20,
	}
	record.Analysis.Claims = claims
	record.Details.InputFactIDs = pointerToStrings(inputFactIDs)
	record.Details.Selection = &SemanticSelection{
		Mode: "range", SelectedEntries: 5, TotalEntries: 10,
		FirstOrdinal: &first, LastOrdinal: &last,
		CanonicalOmittedSegments: 0, Coverage: semanticCoveragePartial,
	}
	facts := map[string]domain.Fact{
		"fact-one": {
			ID: "fact-one", AnalysisRunID: "fact-analysis-one",
			Kind: "tool", SchemaVersion: 1, Outcome: domain.FactOutcomeNotApplicable,
			Value: domain.FactValue{Tool: &domain.ToolFactValue{
				Kind: "call", Name: "SecretTool", Namespace: "Go",
			}},
			Evidence: []domain.EvidenceRef{evidenceOne},
		},
		"fact-two": {
			ID: "fact-two", AnalysisRunID: "fact-analysis-one",
			Kind: "test-result", SchemaVersion: 1, Outcome: domain.FactOutcomeSuccess,
			Value:    domain.FactValue{ExitCode: pointerToInt(0)},
			Evidence: []domain.EvidenceRef{evidenceTwo},
		},
	}
	configuration := loadContentScoutConfigurationForTest(
		t,
		contentScoutAgentJSON(ContentScoutInstructionsDigest),
		marshalDisclosureConfig(t, approvedPublicTerms),
	)
	reader := &contentScoutKnowledgeReader{record: record, facts: facts}
	return contentScoutFixture{
		record: record, evidence: evidenceOne, reader: reader,
		job:           contentScoutJobForRecord(t, configuration, record),
		configuration: configuration,
	}
}

func contentScoutZeroClaimRecord(t *testing.T) SemanticAnalysisRecord {
	t.Helper()
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	record := contentScoutCompletionForTest(t, now, nil)
	first, last := 0, 0
	record.Analysis.Run.Selection = &domain.EvidenceSelection{
		Mode: "complete",
		Entries: domain.EntrySelection{
			Selected: 1, Total: 1, FirstOrdinal: &first, LastOrdinal: &last,
		},
		Coverage: domain.CoverageCompleteRetainedSnapshot,
	}
	record.Analysis.Run.InputFactIDs = []string{}
	record.Details.InputFactIDs = pointerToStrings(nil)
	record.Details.Selection = &SemanticSelection{
		Mode: "complete", SelectedEntries: 1, TotalEntries: 1,
		FirstOrdinal: &first, LastOrdinal: &last,
		Coverage: domain.CoverageCompleteRetainedSnapshot,
	}
	return record
}

func contentScoutJobForRecord(
	t *testing.T,
	configuration ContentScoutConfiguration,
	record SemanticAnalysisRecord,
) AgentJobRecordV1 {
	t.Helper()
	payload := domain.AgentJobPayloadV1{
		SchemaVersion: domain.AgentJobPayloadSchemaVersion,
		Inputs: domain.KnowledgeInputRefsV1{
			AnalysisRunID: record.Analysis.Run.ID,
			ClaimIDs:      append([]string{}, record.Analysis.Run.ClaimIDs...),
		},
		Configuration: configuration.identity,
	}
	eventID := record.Events[len(record.Events)-1].ID
	fingerprint, err := domain.AgentJobFingerprint(
		eventID, configuration.agent, payload,
	)
	if err != nil {
		t.Fatalf("fingerprint Content Scout job: %v", err)
	}
	return AgentJobRecordV1{
		ID: platform.DerivedID("job_", fingerprint), Fingerprint: fingerprint,
		EventID: eventID, Agent: configuration.agent, Status: domain.JobPending,
		Payload:   payload,
		CreatedAt: record.Analysis.Run.FinishedAt.Add(time.Minute),
	}
}

func contentScoutEvidence(id string, entry int) domain.EvidenceRef {
	segment := 0
	return domain.EvidenceRef{
		ID: id, SourceKind: domain.EvidenceSourceSessions,
		SourceIdentity:       "synthetic@local:content-scout",
		DocumentDigestScheme: "sha256-sessions-document-jcs-v1",
		DocumentDigest:       strings.Repeat("d", 64),
		EntryOrdinal:         entry, SegmentOrdinal: &segment,
		EntryKind: "message", Actor: "model", Origin: "model",
		OriginConfidence: "high", ContentHashScheme: "sha256-utf8-v1",
		ContentHash: strings.Repeat("a", 64),
	}
}

func contentScoutCandidate(
	concept string,
	claimIDs []string,
) domain.ContentIdeaCandidateV1 {
	return domain.ContentIdeaCandidateV1{
		Concept:         concept,
		CoreLesson:      "Verify AI coding results with direct tool evidence",
		AudienceBenefit: "Developers can make AI coding work more reliable",
		Hook:            "The useful lesson appears after the first failed approach",
		Resonance:       "The pattern is common in everyday software development",
		Confidence:      0.85,
		ShortPost: domain.ContentFormatAngleV1{
			Suitable: true, Angle: "Share one practical verification tip",
		},
		Thread: domain.ContentFormatAngleV1{
			Suitable: true, Angle: "Explain the problem, attempt, and lesson",
		},
		Article: domain.ContentFormatAngleV1{
			Suitable: true, Angle: "Explore a repeatable AI coding workflow",
		},
		ClaimIDs: append([]string{}, claimIDs...),
	}
}

func prepareContentScoutFixture(
	t *testing.T,
	fixture contentScoutFixture,
) PreparedContentScoutV1 {
	t.Helper()
	prepared, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
		context.Background(), fixture.job,
	)
	if err != nil {
		t.Fatalf("prepare Content Scout fixture: %v", err)
	}
	return prepared
}

func marshalCandidates(
	t *testing.T,
	candidates domain.ContentScoutCandidatesV1,
) json.RawMessage {
	t.Helper()
	document, err := json.Marshal(candidates)
	if err != nil {
		t.Fatalf("marshal candidates: %v", err)
	}
	return document
}

func marshalDisclosureConfig(t *testing.T, terms []string) string {
	t.Helper()
	document, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "approvedPublicTerms": terms,
	})
	if err != nil {
		t.Fatalf("marshal disclosure config: %v", err)
	}
	return string(document)
}

func pointerToStrings(values []string) *[]string {
	copy := append([]string{}, values...)
	return &copy
}

func pointerToInt(value int) *int {
	return &value
}
