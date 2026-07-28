package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/ferueda/noema/internal/platform"
)

const (
	ContentScoutInputSchemaVersion = 1
	ContentIdeaSchemaVersion       = 1
	ArtifactKindContentIdea        = "content-idea"
	ArtifactProposalReviewRequired = "review-required"
	ArtifactSafetyReviewRequired   = "review-required"
	ContentScoutCoveragePartial    = "partial"

	maxContentScoutFacts      = 256
	maxContentTextBytes       = 4_096
	maxContentIdeaTextBytes   = 1_024
	maxContentTechnicalBytes  = 512
	maxContentEvidencePerItem = 256
)

type ContentScoutCoverageV1 struct {
	Scope              string `json:"scope"`
	SelectedClaimCount int    `json:"selectedClaimCount"`
	SelectedFactCount  int    `json:"selectedFactCount"`
}

type ContentScoutClaimV1 struct {
	ID                       string      `json:"id"`
	Type                     ClaimType   `json:"type"`
	Statement                string      `json:"statement"`
	Status                   ClaimStatus `json:"status"`
	Confidence               float64     `json:"confidence"`
	Outcome                  *string     `json:"outcome"`
	SupportingFactIDs        []string    `json:"supportingFactIds"`
	SupportingEvidenceIDs    []string    `json:"supportingEvidenceIds"`
	ContradictingEvidenceIDs []string    `json:"contradictingEvidenceIds"`
}

type ContentScoutToolValueV1 struct {
	Kind      string  `json:"kind"`
	Name      *string `json:"name"`
	Namespace *string `json:"namespace"`
}

type ContentScoutSelectedTextV1 struct {
	Text              string `json:"text"`
	OriginalUTF8Bytes int    `json:"originalUtf8Bytes"`
	Truncated         bool   `json:"truncated"`
}

type ContentScoutTestValueV1 struct {
	Framework string                      `json:"framework"`
	Command   *ContentScoutSelectedTextV1 `json:"command"`
	Passed    *int                        `json:"passed"`
	Failed    *int                        `json:"failed"`
	Skipped   *int                        `json:"skipped"`
}

type ContentScoutFactValueV1 struct {
	Tool     *ContentScoutToolValueV1    `json:"tool"`
	Command  *ContentScoutSelectedTextV1 `json:"command"`
	Test     *ContentScoutTestValueV1    `json:"test"`
	ExitCode *int                        `json:"exitCode"`
	Error    *ContentScoutSelectedTextV1 `json:"error"`
}

type ContentScoutFactV1 struct {
	ID          string                  `json:"id"`
	Kind        string                  `json:"kind"`
	Outcome     string                  `json:"outcome"`
	Value       ContentScoutFactValueV1 `json:"value"`
	EvidenceIDs []string                `json:"evidenceIds"`
}

type ContentScoutInputV1 struct {
	SchemaVersion int                    `json:"schemaVersion"`
	AnalysisRunID string                 `json:"analysisRunId"`
	Coverage      ContentScoutCoverageV1 `json:"coverage"`
	Claims        []ContentScoutClaimV1  `json:"claims"`
	Facts         []ContentScoutFactV1   `json:"facts"`
	Omissions     AnalysisOmissions      `json:"omissions"`
}

type ContentFormatAngleV1 struct {
	Suitable bool   `json:"suitable"`
	Angle    string `json:"angle"`
}

type ContentIdeaCandidateV1 struct {
	Concept         string               `json:"concept"`
	CoreLesson      string               `json:"coreLesson"`
	AudienceBenefit string               `json:"audienceBenefit"`
	Hook            string               `json:"hook"`
	Resonance       string               `json:"resonance"`
	Confidence      float64              `json:"confidence"`
	ShortPost       ContentFormatAngleV1 `json:"shortPost"`
	Thread          ContentFormatAngleV1 `json:"thread"`
	Article         ContentFormatAngleV1 `json:"article"`
	ClaimIDs        []string             `json:"claimIds"`
}

type ContentScoutCandidatesV1 struct {
	Ideas []ContentIdeaCandidateV1 `json:"ideas"`
}

type ContentIdeaV1 struct {
	Rank            int                  `json:"rank"`
	Concept         string               `json:"concept"`
	CoreLesson      string               `json:"coreLesson"`
	AudienceBenefit string               `json:"audienceBenefit"`
	Hook            string               `json:"hook"`
	Resonance       string               `json:"resonance"`
	Confidence      float64              `json:"confidence"`
	ShortPost       ContentFormatAngleV1 `json:"shortPost"`
	Thread          ContentFormatAngleV1 `json:"thread"`
	Article         ContentFormatAngleV1 `json:"article"`
	ClaimIDs        []string             `json:"claimIds"`
}

type Artifact struct {
	ID                    string               `json:"id"`
	Fingerprint           string               `json:"fingerprint"`
	Kind                  string               `json:"kind"`
	SchemaVersion         int                  `json:"schemaVersion"`
	PayloadJSON           json.RawMessage      `json:"payloadJson"`
	RunID                 string               `json:"runId"`
	TriggerEventID        string               `json:"triggerEventId"`
	Inputs                KnowledgeInputRefsV1 `json:"inputs"`
	ClaimIDs              []string             `json:"claimIds"`
	FactIDs               []string             `json:"factIds"`
	SupportingEvidence    []EvidenceRef        `json:"supportingEvidence"`
	ContradictingEvidence []EvidenceRef        `json:"contradictingEvidence"`
	ProposalStatus        string               `json:"proposalStatus"`
	SafetyStatus          string               `json:"safetyStatus"`
	CreatedAt             time.Time            `json:"createdAt"`
}

func (value ContentScoutInputV1) Validate() error {
	if value.SchemaVersion != ContentScoutInputSchemaVersion ||
		!validAgentID(value.AnalysisRunID) ||
		len(value.Claims) > maxAgentKnowledgeClaims ||
		len(value.Facts) > maxContentScoutFacts ||
		value.Coverage.SelectedClaimCount != len(value.Claims) ||
		value.Coverage.SelectedFactCount != len(value.Facts) ||
		(value.Coverage.Scope != CoverageCompleteRetainedSnapshot &&
			value.Coverage.Scope != ContentScoutCoveragePartial) ||
		value.Omissions.CanonicalSegments < 0 ||
		value.Omissions.OmittedTextFactCount < 0 ||
		value.Omissions.OmittedTextOriginalUTF8Bytes < 0 {
		return errors.New("content scout input is invalid")
	}
	claimIDs := make([]string, 0, len(value.Claims))
	for _, claim := range value.Claims {
		if claim.Validate() != nil {
			return errors.New("content scout claim input is invalid")
		}
		claimIDs = append(claimIDs, claim.ID)
	}
	if !validUniqueAgentIDs(claimIDs) {
		return errors.New("content scout claim inputs are duplicated")
	}
	factIDs := make([]string, 0, len(value.Facts))
	for _, fact := range value.Facts {
		if fact.Validate() != nil {
			return errors.New("content scout fact input is invalid")
		}
		factIDs = append(factIDs, fact.ID)
	}
	if !validUniqueAgentIDs(factIDs) {
		return errors.New("content scout fact inputs are duplicated")
	}
	return nil
}

func (value ContentScoutClaimV1) Validate() error {
	if !validAgentID(value.ID) ||
		!value.Type.Valid() ||
		len(value.Statement) == 0 ||
		len(value.Statement) > maxContentTextBytes ||
		!value.Status.Valid() ||
		!validConfidence(value.Confidence) ||
		len(value.SupportingFactIDs) > maxContentEvidencePerItem ||
		len(value.SupportingEvidenceIDs) > maxContentEvidencePerItem ||
		len(value.ContradictingEvidenceIDs) > maxContentEvidencePerItem ||
		!validUniqueAgentIDs(value.SupportingFactIDs) ||
		!validUniqueAgentIDs(value.SupportingEvidenceIDs) ||
		!validUniqueAgentIDs(value.ContradictingEvidenceIDs) {
		return errors.New("content scout claim is invalid")
	}
	if value.Outcome != nil &&
		*value.Outcome != FactOutcomeSuccess &&
		*value.Outcome != FactOutcomeFailure &&
		*value.Outcome != FactOutcomeUnknown {
		return errors.New("content scout claim outcome is invalid")
	}
	return nil
}

func (value ContentScoutFactV1) Validate() error {
	if !validAgentID(value.ID) ||
		len(value.Kind) == 0 ||
		len(value.Kind) > maxAgentNameBytes ||
		(value.Outcome != FactOutcomeNotApplicable &&
			value.Outcome != FactOutcomeSuccess &&
			value.Outcome != FactOutcomeFailure &&
			value.Outcome != FactOutcomeUnknown) ||
		len(value.EvidenceIDs) > maxContentEvidencePerItem ||
		!validUniqueAgentIDs(value.EvidenceIDs) ||
		value.Value.Validate() != nil {
		return errors.New("content scout fact is invalid")
	}
	return nil
}

func (value ContentScoutFactValueV1) Validate() error {
	if value.Tool != nil {
		if len(value.Tool.Kind) == 0 || len(value.Tool.Kind) > maxAgentNameBytes ||
			!validOptionalContentText(value.Tool.Name, maxContentTechnicalBytes) ||
			!validOptionalContentText(value.Tool.Namespace, maxContentTechnicalBytes) {
			return errors.New("content scout tool value is invalid")
		}
	}
	if !validSelectedText(value.Command) || !validSelectedText(value.Error) {
		return errors.New("content scout selected text is invalid")
	}
	if value.Test != nil {
		if len(value.Test.Framework) == 0 ||
			len(value.Test.Framework) > maxAgentNameBytes ||
			!validSelectedText(value.Test.Command) ||
			!validOptionalNonNegative(value.Test.Passed) ||
			!validOptionalNonNegative(value.Test.Failed) ||
			!validOptionalNonNegative(value.Test.Skipped) {
			return errors.New("content scout test value is invalid")
		}
	}
	return nil
}

func (value ContentIdeaCandidateV1) Validate() error {
	for _, field := range []string{
		value.Concept,
		value.CoreLesson,
		value.AudienceBenefit,
		value.Hook,
		value.Resonance,
	} {
		if len(field) == 0 || len(field) > maxContentIdeaTextBytes {
			return errors.New("content idea text is invalid")
		}
	}
	if !validConfidence(value.Confidence) ||
		len(value.ClaimIDs) == 0 ||
		len(value.ClaimIDs) > maxAgentKnowledgeClaims ||
		!validUniqueAgentIDs(value.ClaimIDs) {
		return errors.New("content idea candidate is invalid")
	}
	for _, angle := range []ContentFormatAngleV1{value.ShortPost, value.Thread, value.Article} {
		if len(angle.Angle) > maxContentIdeaTextBytes ||
			angle.Suitable && len(angle.Angle) == 0 {
			return errors.New("content idea format angle is invalid")
		}
	}
	return nil
}

func (value ContentScoutCandidatesV1) Validate() error {
	if len(value.Ideas) > maxAgentArtifacts {
		return errors.New("content scout candidate count is invalid")
	}
	seen := make(map[string]struct{}, len(value.Ideas))
	for _, idea := range value.Ideas {
		if idea.Validate() != nil {
			return errors.New("content scout candidate is invalid")
		}
		fingerprint, err := platform.Fingerprint(idea)
		if err != nil {
			return errors.New("content scout candidate is invalid")
		}
		if _, duplicate := seen[fingerprint]; duplicate {
			return errors.New("content scout candidate is duplicated")
		}
		seen[fingerprint] = struct{}{}
	}
	return nil
}

func (value ContentIdeaV1) Validate() error {
	if value.Rank < 1 || value.Rank > maxAgentArtifacts {
		return errors.New("content idea rank is invalid")
	}
	return ContentIdeaCandidateV1{
		Concept: value.Concept, CoreLesson: value.CoreLesson,
		AudienceBenefit: value.AudienceBenefit, Hook: value.Hook,
		Resonance: value.Resonance, Confidence: value.Confidence,
		ShortPost: value.ShortPost, Thread: value.Thread, Article: value.Article,
		ClaimIDs: value.ClaimIDs,
	}.Validate()
}

func (value Artifact) Validate() error {
	if !validAgentID(value.ID) ||
		!agentDigestPattern.MatchString(value.Fingerprint) ||
		value.ID != platform.DerivedID("artifact_", value.Fingerprint) ||
		!validSafeAgentValue(value.Kind) ||
		value.SchemaVersion < 1 ||
		!validAgentID(value.RunID) ||
		!validAgentID(value.TriggerEventID) ||
		value.Inputs.Validate() != nil ||
		!validUniqueAgentIDs(value.ClaimIDs) ||
		!validUniqueAgentIDs(value.FactIDs) ||
		!validEvidenceIDs(value.SupportingEvidence) ||
		!validEvidenceIDs(value.ContradictingEvidence) ||
		!validSafeAgentValue(value.ProposalStatus) ||
		!validSafeAgentValue(value.SafetyStatus) ||
		value.CreatedAt.IsZero() {
		return errors.New("artifact is invalid")
	}
	canonical, err := compactJSONObject(value.PayloadJSON, maxAgentPayloadBytes, 64)
	if err != nil || !bytes.Equal(canonical, value.PayloadJSON) {
		return errors.New("artifact payload is invalid")
	}
	if value.Kind == ArtifactKindContentIdea {
		if value.SchemaVersion != ContentIdeaSchemaVersion ||
			value.ProposalStatus != ArtifactProposalReviewRequired ||
			value.SafetyStatus != ArtifactSafetyReviewRequired ||
			len(value.ClaimIDs) == 0 ||
			len(value.FactIDs) == 0 ||
			len(value.SupportingEvidence) == 0 {
			return errors.New("content idea artifact is invalid")
		}
		idea, err := decodeStrictJSON[ContentIdeaV1](value.PayloadJSON)
		if err != nil || idea.Validate() != nil || !slices.Equal(idea.ClaimIDs, value.ClaimIDs) {
			return errors.New("content idea artifact payload is invalid")
		}
		for _, claimID := range value.ClaimIDs {
			if !slices.Contains(value.Inputs.ClaimIDs, claimID) {
				return errors.New("content idea artifact claim is outside the job inputs")
			}
		}
	}
	return nil
}

func ContentIdeaArtifactFingerprint(
	jobFingerprint string,
	idea ContentIdeaV1,
	factIDs []string,
	supportingEvidence []EvidenceRef,
	contradictingEvidence []EvidenceRef,
	safetyStatus string,
) (string, error) {
	if !agentDigestPattern.MatchString(jobFingerprint) ||
		idea.Validate() != nil ||
		len(factIDs) == 0 ||
		!validUniqueAgentIDs(factIDs) ||
		!validEvidenceIDs(supportingEvidence) ||
		!validEvidenceIDs(contradictingEvidence) ||
		safetyStatus != ArtifactSafetyReviewRequired {
		return "", errors.New("content idea artifact fingerprint input is invalid")
	}
	return platform.Fingerprint(struct {
		Kind                  string
		SchemaVersion         int
		JobFingerprint        string
		Concept               string
		CoreLesson            string
		AudienceBenefit       string
		Hook                  string
		Resonance             string
		Confidence            float64
		ShortPost             ContentFormatAngleV1
		Thread                ContentFormatAngleV1
		Article               ContentFormatAngleV1
		ClaimIDs              []string
		FactIDs               []string
		SupportingEvidence    []EvidenceRef
		ContradictingEvidence []EvidenceRef
		SafetyStatus          string
	}{
		Kind: ArtifactKindContentIdea, SchemaVersion: ContentIdeaSchemaVersion,
		JobFingerprint: jobFingerprint, Concept: idea.Concept,
		CoreLesson: idea.CoreLesson, AudienceBenefit: idea.AudienceBenefit,
		Hook: idea.Hook, Resonance: idea.Resonance, Confidence: idea.Confidence,
		ShortPost: idea.ShortPost, Thread: idea.Thread, Article: idea.Article,
		ClaimIDs: idea.ClaimIDs, FactIDs: factIDs,
		SupportingEvidence:    supportingEvidence,
		ContradictingEvidence: contradictingEvidence,
		SafetyStatus:          safetyStatus,
	})
}

func validSelectedText(value *ContentScoutSelectedTextV1) bool {
	return value == nil ||
		len(value.Text) > 0 &&
			len(value.Text) <= maxContentTextBytes &&
			value.OriginalUTF8Bytes >= len([]byte(value.Text))
}

func validOptionalContentText(value *string, maximum int) bool {
	return value == nil || len(*value) <= maximum
}

func validOptionalNonNegative(value *int) bool {
	return value == nil || *value >= 0
}

func validEvidenceIDs(values []EvidenceRef) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validAgentID(value.ID) {
			return false
		}
		if _, duplicate := seen[value.ID]; duplicate {
			return false
		}
		seen[value.ID] = struct{}{}
	}
	return true
}

func decodeStrictJSON[T any](value []byte) (T, error) {
	var decoded T
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return decoded, err
	}
	if err := decoder.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return decoded, errors.New("json contains trailing data")
	}
	return decoded, nil
}
