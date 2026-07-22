package application

import (
	"errors"
	"fmt"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

// ClaimFingerprint computes the durable identity of a normalized claim.
// Analysis-run identity and creation time are intentionally excluded so an
// exact replay under the same processing identity produces the same claim ID.
func ClaimFingerprint(processingKey string, claim domain.Claim) (string, error) {
	if processingKey == "" {
		return "", errors.New("fingerprint claim: processing key is required")
	}
	fingerprint, err := platform.Fingerprint(struct {
		ProcessingKey         string
		Type                  domain.ClaimType
		Statement             string
		Status                domain.ClaimStatus
		Confidence            float64
		SupportingEvidence    []domain.EvidenceRef
		ContradictingEvidence []domain.EvidenceRef
		SupportingFactIDs     []string
		Outcome               string
		Actor                 string
		Origin                string
		Subject               string
		Scope                 string
		Attribution           domain.ClaimAttribution
		ExtractorName         string
		ExtractorVersion      string
		SchemaVersion         int
		PromptVersion         string
		RequestedRoute        domain.RequestedModelRoute
		ResolvedProvider      string
		ResolvedModel         string
	}{
		ProcessingKey:         processingKey,
		Type:                  claim.Type,
		Statement:             claim.Statement,
		Status:                claim.Status,
		Confidence:            claim.Confidence,
		SupportingEvidence:    claim.SupportingEvidence,
		ContradictingEvidence: claim.ContradictingEvidence,
		SupportingFactIDs:     claim.SupportingFactIDs,
		Outcome:               claim.Outcome,
		Actor:                 claim.Actor,
		Origin:                claim.Origin,
		Subject:               claim.Subject,
		Scope:                 claim.Scope,
		Attribution:           claim.Attribution,
		ExtractorName:         claim.ExtractorName,
		ExtractorVersion:      claim.ExtractorVersion,
		SchemaVersion:         claim.SchemaVersion,
		PromptVersion:         claim.PromptVersion,
		RequestedRoute:        claim.RequestedRoute,
		ResolvedProvider:      claim.ResolvedProvider,
		ResolvedModel:         claim.ResolvedModel,
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint claim: %w", err)
	}
	return fingerprint, nil
}
