package application

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

type ContentScoutAdmissionV1 struct {
	Artifacts []domain.Artifact
	Privacy   domain.AgentPrivacyOutcomeV1
}

func (prepared PreparedContentScoutV1) AdmitCandidates(
	candidateJSON json.RawMessage,
	runID string,
	createdAt time.Time,
) (ContentScoutAdmissionV1, error) {
	if !prepared.prepared || prepared.SkipNoClaims ||
		runID == "" || createdAt.IsZero() ||
		len(prepared.CanonicalJSON) == 0 ||
		prepared.Input.Validate() != nil ||
		prepared.job.Fingerprint == "" {
		return ContentScoutAdmissionV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, prepared.Privacy,
		)
	}
	var candidates domain.ContentScoutCandidatesV1
	if _, err := decodeStrictBoundedJSON(
		bytes.NewReader(candidateJSON), maxContentScoutCandidateBytes, &candidates,
	); err != nil {
		return ContentScoutAdmissionV1{}, contentScoutFailure(
			domain.AgentFailureCategoryResponseInvalid, prepared.Privacy,
		)
	}
	if candidates.Validate() != nil {
		return ContentScoutAdmissionV1{}, contentScoutFailure(
			domain.AgentFailureCategoryCandidateInvalid, prepared.Privacy,
		)
	}
	for _, candidate := range candidates.Ideas {
		for _, claimID := range candidate.ClaimIDs {
			if _, exists := prepared.claimsByID[claimID]; !exists {
				return ContentScoutAdmissionV1{}, contentScoutFailure(
					domain.AgentFailureCategoryCandidateInvalid, prepared.Privacy,
				)
			}
		}
	}

	generatedText := contentScoutCandidateFreeText(candidates)
	privacyReport, err := (PrivacyPolicy{}).Postflight(generatedText...)
	if err != nil {
		privacy := appendPolicyStage(
			prepared.Privacy,
			privacyPolicyStage(
				contentScoutPrivacyPostflight, domain.AgentPolicyOutcomeBlocked, privacyReport,
			),
		)
		return ContentScoutAdmissionV1{}, contentScoutFailure(
			domain.AgentFailureCategoryPrivacyBlocked, privacy,
		)
	}
	privacy := appendPolicyStage(
		prepared.Privacy,
		privacyPolicyStage(
			contentScoutPrivacyPostflight, domain.AgentPolicyOutcomePassed, privacyReport,
		),
	)
	disclosureReport, err := prepared.disclosure.Postflight(generatedText...)
	if err != nil {
		privacy = appendPolicyStage(
			privacy,
			disclosurePolicyStage(
				contentScoutDisclosurePostflight, domain.AgentPolicyOutcomeBlocked,
				disclosureReport,
			),
		)
		return ContentScoutAdmissionV1{}, contentScoutFailure(
			domain.AgentFailureCategoryDisclosureBlocked, privacy,
		)
	}
	privacy = appendPolicyStage(
		privacy,
		disclosurePolicyStage(
			contentScoutDisclosurePostflight, domain.AgentPolicyOutcomePassed,
			disclosureReport,
		),
	)

	artifacts := make([]domain.Artifact, 0, len(candidates.Ideas))
	seenFingerprints := make(map[string]struct{}, len(candidates.Ideas))
	for _, candidate := range candidates.Ideas {
		factIDs, supporting, contradicting := contentScoutCandidateLineage(
			candidatesClaimRecords(candidate, prepared.claimsByID),
		)
		if len(factIDs) == 0 {
			continue
		}
		idea := domain.ContentIdeaV1{
			Rank:    len(artifacts) + 1,
			Concept: candidate.Concept, CoreLesson: candidate.CoreLesson,
			AudienceBenefit: candidate.AudienceBenefit, Hook: candidate.Hook,
			Resonance: candidate.Resonance, Confidence: candidate.Confidence,
			ShortPost: candidate.ShortPost, Thread: candidate.Thread,
			Article: candidate.Article, ClaimIDs: append([]string{}, candidate.ClaimIDs...),
		}
		payload, err := json.Marshal(idea)
		if err != nil {
			return ContentScoutAdmissionV1{}, contentScoutFailure(
				domain.AgentFailureCategoryCandidateInvalid, privacy,
			)
		}
		fingerprint, err := domain.ContentIdeaArtifactFingerprint(
			prepared.job.Fingerprint, idea, factIDs, supporting, contradicting,
			domain.ArtifactSafetyReviewRequired,
		)
		if err != nil {
			return ContentScoutAdmissionV1{}, contentScoutFailure(
				domain.AgentFailureCategoryCandidateInvalid, privacy,
			)
		}
		if _, duplicate := seenFingerprints[fingerprint]; duplicate {
			return ContentScoutAdmissionV1{}, contentScoutFailure(
				domain.AgentFailureCategoryCandidateInvalid, privacy,
			)
		}
		seenFingerprints[fingerprint] = struct{}{}
		artifact := domain.Artifact{
			ID: platform.DerivedID("artifact_", fingerprint), Fingerprint: fingerprint,
			Kind: domain.ArtifactKindContentIdea, SchemaVersion: domain.ContentIdeaSchemaVersion,
			PayloadJSON: payload, RunID: runID, TriggerEventID: prepared.job.EventID,
			JobFingerprint: prepared.job.Fingerprint, Inputs: prepared.job.Payload.Inputs,
			ClaimIDs: append([]string{}, candidate.ClaimIDs...), FactIDs: factIDs,
			SupportingEvidence: supporting, ContradictingEvidence: contradicting,
			ProposalStatus: domain.ArtifactProposalReviewRequired,
			SafetyStatus:   domain.ArtifactSafetyReviewRequired, CreatedAt: createdAt.UTC(),
		}
		if artifact.Validate() != nil {
			return ContentScoutAdmissionV1{}, contentScoutFailure(
				domain.AgentFailureCategoryCandidateInvalid, privacy,
			)
		}
		artifacts = append(artifacts, artifact)
	}
	return ContentScoutAdmissionV1{Artifacts: artifacts, Privacy: privacy}, nil
}

func contentScoutCandidateFreeText(
	candidates domain.ContentScoutCandidatesV1,
) []string {
	result := make([]string, 0, len(candidates.Ideas)*8)
	for _, candidate := range candidates.Ideas {
		result = append(
			result,
			candidate.Concept,
			candidate.CoreLesson,
			candidate.AudienceBenefit,
			candidate.Hook,
			candidate.Resonance,
			candidate.ShortPost.Angle,
			candidate.Thread.Angle,
			candidate.Article.Angle,
		)
	}
	return result
}

func candidatesClaimRecords(
	candidate domain.ContentIdeaCandidateV1,
	claimsByID map[string]domain.Claim,
) []domain.Claim {
	result := make([]domain.Claim, len(candidate.ClaimIDs))
	for index, claimID := range candidate.ClaimIDs {
		result[index] = claimsByID[claimID]
	}
	return result
}

func contentScoutCandidateLineage(
	claims []domain.Claim,
) ([]string, []domain.EvidenceRef, []domain.EvidenceRef) {
	factIDs := make([]string, 0)
	supporting := make([]domain.EvidenceRef, 0)
	contradicting := make([]domain.EvidenceRef, 0)
	seenFacts := map[string]struct{}{}
	seenSupporting := map[string]struct{}{}
	seenContradicting := map[string]struct{}{}
	for _, claim := range claims {
		for _, factID := range claim.SupportingFactIDs {
			if _, duplicate := seenFacts[factID]; duplicate {
				continue
			}
			seenFacts[factID] = struct{}{}
			factIDs = append(factIDs, factID)
		}
		for _, ref := range claim.SupportingEvidence {
			if _, duplicate := seenSupporting[ref.ID]; duplicate {
				continue
			}
			seenSupporting[ref.ID] = struct{}{}
			supporting = append(supporting, ref)
		}
		for _, ref := range claim.ContradictingEvidence {
			if _, duplicate := seenContradicting[ref.ID]; duplicate {
				continue
			}
			seenContradicting[ref.ID] = struct{}{}
			contradicting = append(contradicting, ref)
		}
	}
	return factIDs, supporting, contradicting
}
