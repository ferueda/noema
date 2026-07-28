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
	factAnalysis domain.FactAnalysis
	facts        map[string]domain.Fact
	requestedIDs []string
	loadErr      error
}

func (reader *contentScoutKnowledgeReader) LoadFactAnalysis(
	_ context.Context,
	id string,
) (domain.FactAnalysis, error) {
	if reader.loadErr != nil {
		return domain.FactAnalysis{}, reader.loadErr
	}
	if id != reader.factAnalysis.Run.ID {
		return domain.FactAnalysis{}, errors.New("fact analysis not found")
	}
	return reader.factAnalysis, nil
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
		!slices.Equal(fixture.reader.requestedIDs, []string{
			fixture.factOneID, fixture.factTwoID,
		}) ||
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

func TestContentScoutPreparePreservesSupportingFactFirstReferenceOrder(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	fixture.record.Analysis.Claims[0].SupportingFactIDs = []string{
		fixture.factTwoID, fixture.factOneID,
	}
	fixture.record.Analysis.Claims[0].SupportingEvidence = append(
		fixture.record.Analysis.Claims[0].SupportingEvidence,
		contentScoutEvidence("evidence-two", 2),
	)
	fixture.reader.record = fixture.record

	prepared, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
		context.Background(), fixture.job,
	)
	if err != nil {
		t.Fatalf("prepare first-reference fact order: %v", err)
	}
	got := make([]string, len(prepared.Input.Facts))
	for index, fact := range prepared.Input.Facts {
		got[index] = fact.ID
	}
	want := []string{fixture.factTwoID, fixture.factOneID}
	if !slices.Equal(fixture.reader.requestedIDs, want) ||
		!slices.Equal(got, want) {
		t.Fatalf(
			"first-reference fact order = requested %#v / input %#v, want %#v",
			fixture.reader.requestedIDs, got, want,
		)
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
			delete(fixture.reader.facts, fixture.factTwoID)
		},
		"reordered fact": func(fixture *contentScoutFixture) {
			first := fixture.reader.facts[fixture.factOneID]
			second := fixture.reader.facts[fixture.factTwoID]
			fixture.reader.facts = map[string]domain.Fact{
				fixture.factOneID: second,
				fixture.factTwoID: first,
			}
		},
		"cross-analysis fact": func(fixture *contentScoutFixture) {
			fact := fixture.reader.facts[fixture.factTwoID]
			fact.AnalysisRunID = "other-fact-analysis"
			fixture.reader.facts[fixture.factTwoID] = fact
		},
		"changed fact value": func(fixture *contentScoutFixture) {
			fact := fixture.reader.facts[fixture.factOneID]
			fact.Value.Tool = &domain.ToolFactValue{
				Kind: "call", Name: "ChangedTool", Namespace: "Go",
			}
			fixture.reader.facts[fixture.factOneID] = fact
		},
		"changed event": func(fixture *contentScoutFixture) {
			fixture.job.EventID = "event-other"
		},
		"changed evidence": func(fixture *contentScoutFixture) {
			fact := fixture.reader.facts[fixture.factOneID]
			fact.Evidence[0].DocumentDigest = strings.Repeat("e", 64)
			fixture.reader.facts[fixture.factOneID] = fact
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

func TestContentScoutPrepareTracksOutboundBytesAfterSelectedTextGeneralization(
	t *testing.T,
) {
	tests := map[string]struct {
		source      string
		assign      func(*domain.FactValue, *domain.SelectedText)
		selectValue func(domain.ContentScoutFactValueV1) *domain.ContentScoutSelectedTextV1
	}{
		"command local path": {
			source: "/x/y",
			assign: func(value *domain.FactValue, selected *domain.SelectedText) {
				value.Command = selected
			},
			selectValue: func(value domain.ContentScoutFactValueV1) *domain.ContentScoutSelectedTextV1 {
				return value.Command
			},
		},
		"test command private identifier": {
			source: "XZQ",
			assign: func(value *domain.FactValue, selected *domain.SelectedText) {
				value.Test = &domain.TestFactValue{
					Framework: "go",
					Command:   selected,
				}
			},
			selectValue: func(value domain.ContentScoutFactValueV1) *domain.ContentScoutSelectedTextV1 {
				return value.Test.Command
			},
		},
		"error private identifier": {
			source: "XZQ",
			assign: func(value *domain.FactValue, selected *domain.SelectedText) {
				value.Error = selected
			},
			selectValue: func(value domain.ContentScoutFactValueV1) *domain.ContentScoutSelectedTextV1 {
				return value.Error
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newContentScoutFixture(t, nil)
			replaceContentScoutFixtureFact(
				t, &fixture, fixture.factOneID,
				func(fact *domain.Fact) {
					fact.Value = domain.FactValue{}
					test.assign(&fact.Value, &domain.SelectedText{
						Text: test.source, EmittedUTF8Bytes: len([]byte(test.source)),
						OriginalUTF8Bytes: len([]byte(test.source)),
						ContentHash: domain.Digest{
							Scheme: "sha256-utf8-v1",
							Digest: strings.Repeat("b", 64),
						},
					})
				},
			)

			prepared, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
				context.Background(), fixture.job,
			)
			if err != nil {
				t.Fatalf("prepare generalized selected text: %v", err)
			}
			selected := test.selectValue(prepared.Input.Facts[0].Value)
			if selected == nil ||
				selected.Text == test.source ||
				selected.OriginalUTF8Bytes != len([]byte(test.source)) ||
				selected.EmittedUTF8Bytes != len([]byte(selected.Text)) ||
				selected.EmittedUTF8Bytes <= selected.OriginalUTF8Bytes ||
				prepared.Input.Validate() != nil {
				t.Fatalf("generalized selected text = %#v", selected)
			}
		})
	}
}

func TestContentScoutPrepareOmitsValidZeroBudgetFactText(t *testing.T) {
	fixture := newContentScoutFixture(t, nil)
	replaceContentScoutFixtureFact(
		t, &fixture, fixture.factOneID,
		func(fact *domain.Fact) {
			fact.Value = domain.FactValue{Command: &domain.SelectedText{
				Text: "", EmittedUTF8Bytes: 0,
				OriginalUTF8Bytes: 24, Truncated: true,
				ContentHash: domain.Digest{
					Scheme: "sha256-utf8-v1",
					Digest: strings.Repeat("b", 64),
				},
			}}
		},
	)

	prepared, err := (ContentScoutHandlerV1{Knowledge: fixture.reader}).Prepare(
		context.Background(), fixture.job,
	)
	if err != nil {
		t.Fatalf("prepare zero-budget fact text: %v", err)
	}
	if prepared.Input.Facts[0].Value.Command != nil ||
		prepared.Input.Omissions.OmittedTextFactCount == 0 ||
		prepared.Input.Omissions.OmittedTextOriginalUTF8Bytes == 0 ||
		prepared.Input.Validate() != nil {
		t.Fatalf("zero-budget fact input = %#v", prepared.Input)
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
		!slices.Equal(first.FactIDs, []string{fixture.factOneID}) ||
		!slices.Equal(evidenceIDs(first.SupportingEvidence), []string{"evidence-one"}) ||
		!slices.Equal(second.ClaimIDs, []string{"claim-three", "claim-one"}) ||
		!slices.Equal(second.FactIDs, []string{
			fixture.factTwoID, fixture.factOneID,
		}) ||
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

func TestContentScoutAdmitRejectsCompleteBatchForNovelAbsolutePath(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	prepared := prepareContentScoutFixture(t, fixture)
	candidates := domain.ContentScoutCandidatesV1{Ideas: []domain.ContentIdeaCandidateV1{
		contentScoutCandidate("A useful lesson about verification", []string{"claim-one"}),
		contentScoutCandidate("Do not expose /secret", []string{"claim-one"}),
	}}

	admission, err := prepared.AdmitCandidates(
		marshalCandidates(t, candidates),
		"run-one",
		time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC),
	)
	var failure ContentScoutApplicationFailure
	if !errors.As(err, &failure) ||
		failure.Category != domain.AgentFailureCategoryDisclosureBlocked ||
		len(admission.Artifacts) != 0 {
		t.Fatalf("unsafe batch admission = %#v, failure = %#v, %v", admission, failure, err)
	}
	if strings.Contains(err.Error(), "/secret") {
		t.Fatalf("failure exposed generated path: %v", err)
	}

	safeAdmission, err := prepared.AdmitCandidates(
		marshalCandidates(t, domain.ContentScoutCandidatesV1{
			Ideas: []domain.ContentIdeaCandidateV1{
				contentScoutCandidate("A useful lesson about verification", []string{"claim-one"}),
			},
		}),
		"run-two",
		time.Date(2026, 7, 28, 20, 1, 0, 0, time.UTC),
	)
	if err != nil || len(safeAdmission.Artifacts) != 1 {
		t.Fatalf("ordinary prose admission = %#v, %v", safeAdmission, err)
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
	factOneID     string
	factTwoID     string
}

func newContentScoutFixture(
	t *testing.T,
	approvedPublicTerms []string,
) contentScoutFixture {
	t.Helper()
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	evidenceOne := contentScoutEvidence("evidence-one", 1)
	evidenceTwo := contentScoutEvidence("evidence-two", 2)
	revision := domain.EvidenceRevision{
		SourceKind:  domain.EvidenceSourceSessions,
		CanonicalID: evidenceOne.SourceIdentity,
		DocumentDigest: domain.Digest{
			Scheme: evidenceOne.DocumentDigestScheme,
			Digest: evidenceOne.DocumentDigest,
		},
	}
	factRunID := "fact-analysis-one"
	factOne := contentScoutStoredFact(t, revision, domain.Fact{
		AnalysisRunID: factRunID,
		Kind:          "tool", SchemaVersion: 1, Outcome: domain.FactOutcomeNotApplicable,
		Value: domain.FactValue{Tool: &domain.ToolFactValue{
			Kind: "call", Name: "SecretTool", Namespace: "Go",
		}},
		ExtractorName: "content-scout-test", ExtractorVersion: "v1",
		ParseRule: "tool-call", Evidence: []domain.EvidenceRef{evidenceOne},
		CreatedAt: now,
	})
	factTwo := contentScoutStoredFact(t, revision, domain.Fact{
		AnalysisRunID: factRunID,
		Kind:          "test-result", SchemaVersion: 1, Outcome: domain.FactOutcomeSuccess,
		Value:         domain.FactValue{ExitCode: pointerToInt(0)},
		ExtractorName: "content-scout-test", ExtractorVersion: "v1",
		ParseRule: "test-result", Evidence: []domain.EvidenceRef{evidenceTwo},
		CreatedAt: now,
	})
	factUnused := contentScoutStoredFact(t, revision, domain.Fact{
		AnalysisRunID: factRunID,
		Kind:          "exit-code", SchemaVersion: 1, Outcome: domain.FactOutcomeFailure,
		Value:         domain.FactValue{ExitCode: pointerToInt(1)},
		ExtractorName: "content-scout-test", ExtractorVersion: "v1",
		ParseRule: "exit-code", Evidence: []domain.EvidenceRef{evidenceTwo},
		CreatedAt: now,
	})
	claims := []domain.Claim{
		{
			ID: "claim-one", AnalysisRunID: "analysis-one",
			Type: domain.ClaimTypeLesson, Statement: "SecretProject showed a useful Go lesson",
			Status: domain.ClaimStatusInferred, Confidence: 0.9,
			SupportingEvidence: []domain.EvidenceRef{evidenceOne},
			SupportingFactIDs:  []string{factOne.ID},
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
			SupportingFactIDs:     []string{factTwo.ID, factOne.ID},
		},
	}
	claimIDs := []string{"claim-one", "claim-two", "claim-three"}
	inputFactIDs := []string{factOne.ID, factTwo.ID, factUnused.ID}
	first, last := 0, 4
	record := contentScoutCompletionForTest(t, now, claimIDs)
	record.Analysis.Run.RequestedSourceIdentity = "private-source-identity"
	record.Analysis.Run.Revision = &revision
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
	factAnalysis := domain.FactAnalysis{
		Run: domain.AnalysisRun{
			ID: factRunID, Stage: domain.AnalysisStageFacts,
			RequestedSourceIdentity: revision.CanonicalID,
			Revision:                &revision,
			ExtractorName:           "content-scout-test",
			ExtractorVersion:        "v1",
			SchemaVersion:           1,
			FactIDs:                 append([]string{}, inputFactIDs...),
			Status:                  domain.AnalysisCompleted,
			StartedAt:               now.Add(-time.Minute),
			FinishedAt:              now,
		},
		Facts: []domain.Fact{factOne, factTwo, factUnused},
	}
	facts := map[string]domain.Fact{
		factOne.ID: factOne,
		factTwo.ID: factTwo,
	}
	configuration := loadContentScoutConfigurationForTest(
		t,
		contentScoutAgentJSON(ContentScoutInstructionsDigest),
		marshalDisclosureConfig(t, approvedPublicTerms),
	)
	reader := &contentScoutKnowledgeReader{
		record: record, factAnalysis: factAnalysis, facts: facts,
	}
	return contentScoutFixture{
		record: record, evidence: evidenceOne, reader: reader,
		job:           contentScoutJobForRecord(t, configuration, record),
		configuration: configuration,
		factOneID:     factOne.ID,
		factTwoID:     factTwo.ID,
	}
}

func contentScoutStoredFact(
	t *testing.T,
	revision domain.EvidenceRevision,
	fact domain.Fact,
) domain.Fact {
	t.Helper()
	fingerprint, err := factFingerprint(revision.Identity(), fact)
	if err != nil {
		t.Fatalf("fingerprint content scout fact: %v", err)
	}
	fact.Fingerprint = fingerprint
	fact.ID = platform.DerivedID("fact_", fingerprint)
	return fact
}

func replaceContentScoutFixtureFact(
	t *testing.T,
	fixture *contentScoutFixture,
	oldID string,
	mutate func(*domain.Fact),
) {
	t.Helper()
	index := slices.Index(fixture.reader.factAnalysis.Run.FactIDs, oldID)
	if index < 0 {
		t.Fatalf("replace unknown content scout fact %s", oldID)
	}
	fact := fixture.reader.factAnalysis.Facts[index]
	mutate(&fact)
	fact.ID = ""
	fact.Fingerprint = ""
	fact = contentScoutStoredFact(
		t, *fixture.reader.factAnalysis.Run.Revision, fact,
	)
	newID := fact.ID
	fixture.reader.factAnalysis.Facts[index] = fact
	fixture.reader.factAnalysis.Run.FactIDs[index] = newID
	delete(fixture.reader.facts, oldID)
	fixture.reader.facts[newID] = fact

	replaceID := func(values []string) {
		for index := range values {
			if values[index] == oldID {
				values[index] = newID
			}
		}
	}
	replaceID(fixture.record.Analysis.Run.InputFactIDs)
	for index := range fixture.record.Analysis.Claims {
		replaceID(fixture.record.Analysis.Claims[index].SupportingFactIDs)
	}
	if fixture.record.Details.InputFactIDs != nil {
		replaceID(*fixture.record.Details.InputFactIDs)
	}
	fixture.reader.record = fixture.record
	if fixture.factOneID == oldID {
		fixture.factOneID = newID
	}
	if fixture.factTwoID == oldID {
		fixture.factTwoID = newID
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
