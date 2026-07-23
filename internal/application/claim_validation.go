package application

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const (
	maxClaimCandidates     = 64
	maxClaimStatementBytes = 2 * 1024
	maxClaimSubjectBytes   = 512
	maxClaimScopeBytes     = 512
	maxClaimEvidenceRefs   = 512
	maxClaimFactRefs       = 256
)

var ErrClaimCandidateInvalid = errors.New("claim-candidate-invalid")

// V0 claim prose stays actor-neutral until causal attribution has explicit
// evidence rules. The structured Actor field remains available for supported
// source-role metadata.
var unsupportedAttributionActorPattern = regexp.MustCompile(`(?i)\b(?:users?|humans?|agents?|assistants?|models?|environments?|developers?|operators?|you|your|yours|we|us|our|ours|i|me|my|mine)\b`)

type ClaimAdmissionConfig struct {
	AnalysisRunID    string
	ProcessingKey    string
	ExtractorName    string
	ExtractorVersion string
	SchemaVersion    int
	PromptVersion    string
	Model            domain.ModelExecutionMetadata
	CreatedAt        time.Time
}

// AdmitClaimCandidates resolves untrusted identifiers against the exact
// outbound input and rejects the complete batch when any candidate is unsafe.
func AdmitClaimCandidates(
	input PreparedSemanticInput,
	candidates []domain.ClaimCandidate,
	config ClaimAdmissionConfig,
) ([]domain.Claim, error) {
	if err := validateClaimAdmissionConfig(config); err != nil {
		return nil, err
	}
	if len(candidates) > maxClaimCandidates {
		return nil, fmt.Errorf("%w: candidate count", ErrClaimCandidateInvalid)
	}
	claims := make([]domain.Claim, 0, len(candidates))
	seenFingerprints := make(map[string]bool, len(candidates))
	for index, candidate := range candidates {
		claim, err := admitClaimCandidate(input, candidate, config)
		if err != nil {
			return nil, fmt.Errorf("%w: candidate %d: %s", ErrClaimCandidateInvalid, index, err.Error())
		}
		if seenFingerprints[claim.Fingerprint] {
			return nil, fmt.Errorf("%w: candidate %d: duplicate claim", ErrClaimCandidateInvalid, index)
		}
		seenFingerprints[claim.Fingerprint] = true
		claims = append(claims, claim)
	}
	return claims, nil
}

func admitClaimCandidate(
	input PreparedSemanticInput,
	candidate domain.ClaimCandidate,
	config ClaimAdmissionConfig,
) (domain.Claim, error) {
	statement := strings.TrimSpace(candidate.Statement)
	subject := strings.TrimSpace(candidate.Subject)
	scope := strings.TrimSpace(candidate.Scope)
	if !candidate.Type.Valid() || !candidate.Status.Valid() {
		return domain.Claim{}, errors.New("invalid type or status")
	}
	if !boundedUTF8(statement, maxClaimStatementBytes) {
		return domain.Claim{}, errors.New("invalid statement")
	}
	if (subject != "" && !boundedUTF8(subject, maxClaimSubjectBytes)) ||
		(scope != "" && !boundedUTF8(scope, maxClaimScopeBytes)) {
		return domain.Claim{}, errors.New("invalid subject or scope")
	}
	if math.IsNaN(candidate.Confidence) || math.IsInf(candidate.Confidence, 0) ||
		candidate.Confidence < 0 || candidate.Confidence > 1 {
		return domain.Claim{}, errors.New("invalid confidence")
	}
	if candidate.Attribution != "" && candidate.Attribution != domain.ClaimAttributionUnknown {
		return domain.Claim{}, errors.New("unsupported attribution")
	}
	if unsupportedAttributionActorPattern.MatchString(statement) ||
		unsupportedAttributionActorPattern.MatchString(subject) ||
		unsupportedAttributionActorPattern.MatchString(scope) {
		return domain.Claim{}, errors.New("unsupported attribution actor in free text")
	}

	supporting, supportIDs, err := resolveClaimEvidence(input, candidate.SupportingEvidenceIDs, true)
	if err != nil {
		return domain.Claim{}, err
	}
	contradicting, contradictionIDs, err := resolveClaimEvidence(input, candidate.ContradictingEvidenceIDs, false)
	if err != nil {
		return domain.Claim{}, err
	}
	for id := range supportIDs {
		if contradictionIDs[id] {
			return domain.Claim{}, errors.New("supporting and contradicting evidence overlap")
		}
	}
	supportingFacts, err := resolveSupportingFacts(input, candidate.SupportingFactIDs)
	if err != nil {
		return domain.Claim{}, err
	}
	if err := validateSupportedActor(candidate.Actor, supporting); err != nil {
		return domain.Claim{}, err
	}
	if err := validateSupportedOrigin(candidate.Origin, supporting); err != nil {
		return domain.Claim{}, err
	}
	if err := validateClaimOutcome(input, candidate, supporting, contradicting, supportingFacts); err != nil {
		return domain.Claim{}, err
	}

	claim := domain.Claim{
		AnalysisRunID: config.AnalysisRunID, Type: candidate.Type, Statement: statement,
		Status: candidate.Status, Confidence: candidate.Confidence,
		SupportingEvidence: supporting, ContradictingEvidence: contradicting,
		SupportingFactIDs: append([]string(nil), candidate.SupportingFactIDs...), Outcome: candidate.Outcome,
		Actor: candidate.Actor, Origin: candidate.Origin, Subject: subject, Scope: scope,
		Attribution: candidate.Attribution, ExtractorName: config.ExtractorName,
		ExtractorVersion: config.ExtractorVersion, SchemaVersion: config.SchemaVersion,
		PromptVersion: config.PromptVersion, RequestedRoute: config.Model.RequestedRoute,
		ResolvedProvider: config.Model.ResolvedProvider, ResolvedModel: config.Model.ResolvedModel,
		CreatedAt: config.CreatedAt.UTC(),
	}
	fingerprint, err := ClaimFingerprint(config.ProcessingKey, claim)
	if err != nil {
		return domain.Claim{}, errors.New("fingerprint claim")
	}
	claim.ID = platform.DerivedID("claim_", fingerprint)
	claim.Fingerprint = fingerprint
	return claim, nil
}

func resolveClaimEvidence(
	input PreparedSemanticInput,
	ids []string,
	required bool,
) ([]domain.EvidenceRef, map[string]bool, error) {
	if required && len(ids) == 0 {
		return nil, nil, errors.New("supporting evidence is required")
	}
	if len(ids) > maxClaimEvidenceRefs {
		return nil, nil, errors.New("too many evidence ids")
	}
	resolved := make([]domain.EvidenceRef, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, nil, errors.New("duplicate or empty evidence id")
		}
		ref, ok := input.EvidenceByID[id]
		if !ok {
			return nil, nil, errors.New("unknown evidence id")
		}
		seen[id] = true
		resolved = append(resolved, ref)
	}
	return resolved, seen, nil
}

func resolveSupportingFacts(input PreparedSemanticInput, ids []string) ([]domain.Fact, error) {
	if len(ids) > maxClaimFactRefs {
		return nil, errors.New("too many supporting fact ids")
	}
	resolved := make([]domain.Fact, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, errors.New("duplicate or empty supporting fact id")
		}
		fact, ok := input.FactsByID[id]
		if !ok {
			return nil, errors.New("unknown supporting fact id")
		}
		for _, ref := range fact.Evidence {
			selected, ok := input.EvidenceByID[ref.ID]
			if !ok || !sameEvidenceReference(selected, ref) {
				return nil, errors.New("supporting fact falls outside selection")
			}
		}
		seen[id] = true
		resolved = append(resolved, fact)
	}
	return resolved, nil
}

func validateSupportedActor(asserted string, supporting []domain.EvidenceRef) error {
	if asserted == "" {
		return nil
	}
	if !domain.ValidClaimActor(asserted) {
		return errors.New("invalid actor")
	}
	matched := false
	for _, ref := range supporting {
		if ref.Actor == "" || ref.Actor == "unknown" {
			continue
		}
		if ref.Actor != asserted {
			return errors.New("actor conflicts with supporting evidence")
		}
		matched = true
	}
	if !matched {
		return errors.New("actor is unsupported")
	}
	return nil
}

func validateSupportedOrigin(asserted string, supporting []domain.EvidenceRef) error {
	if asserted == "" {
		return nil
	}
	if !domain.ValidClaimOrigin(asserted) {
		return errors.New("invalid origin")
	}
	matched := false
	for _, ref := range supporting {
		if ref.Origin == "" || ref.Origin == "unknown" {
			continue
		}
		if ref.Origin != asserted {
			return errors.New("origin conflicts with supporting evidence")
		}
		matched = true
	}
	if !matched {
		return errors.New("origin is unsupported")
	}
	return nil
}

func validateClaimOutcome(
	input PreparedSemanticInput,
	candidate domain.ClaimCandidate,
	supporting, contradicting []domain.EvidenceRef,
	supportingFacts []domain.Fact,
) error {
	switch candidate.Type {
	case domain.ClaimTypeFailedAttempt:
		if candidate.Outcome != domain.FactOutcomeFailure {
			return errors.New("failed attempt requires failure outcome")
		}
	case domain.ClaimTypeVerification:
		if candidate.Outcome != domain.FactOutcomeSuccess && candidate.Outcome != domain.FactOutcomeFailure &&
			candidate.Outcome != domain.FactOutcomeUnknown {
			return errors.New("verification requires a supported outcome")
		}
	default:
		if candidate.Outcome != "" {
			return errors.New("outcome is not allowed for claim type")
		}
		return nil
	}

	if candidate.Status == domain.ClaimStatusObserved {
		if !observedOutcomeEstablished(candidate.Outcome, supportingFacts) {
			return errors.New("observed outcome is not established by a result fact")
		}
		for _, fact := range linkedResultFacts(input.OrderedFacts, supporting, supportingFacts) {
			if outcomeConflicts(candidate.Outcome, fact.Outcome) {
				return errors.New("observed outcome conflicts with linked result facts")
			}
		}
		return nil
	}

	contradictingIDs := make(map[string]bool, len(contradicting))
	for _, ref := range contradicting {
		contradictingIDs[ref.ID] = true
	}
	for _, fact := range linkedResultFacts(input.OrderedFacts, supporting, supportingFacts) {
		if !outcomeConflicts(candidate.Outcome, fact.Outcome) {
			continue
		}
		cited := false
		for _, ref := range fact.Evidence {
			if contradictingIDs[ref.ID] {
				cited = true
				break
			}
		}
		if !cited {
			return errors.New("conflicting result evidence is not cited")
		}
	}
	return nil
}

func observedOutcomeEstablished(outcome string, facts []domain.Fact) bool {
	hasSuccess, hasFailure, hasUnknown := false, false, false
	for _, fact := range facts {
		if !isResultFact(fact) || fact.Outcome == domain.FactOutcomeNotApplicable {
			continue
		}
		switch fact.Outcome {
		case domain.FactOutcomeSuccess:
			hasSuccess = true
		case domain.FactOutcomeFailure:
			hasFailure = true
		case domain.FactOutcomeUnknown:
			hasUnknown = true
		}
	}
	switch outcome {
	case domain.FactOutcomeSuccess:
		return hasSuccess
	case domain.FactOutcomeFailure:
		return hasFailure
	case domain.FactOutcomeUnknown:
		return hasUnknown || (hasSuccess && hasFailure)
	default:
		return false
	}
}

func linkedResultFacts(all []domain.Fact, supporting []domain.EvidenceRef, supportingFacts []domain.Fact) []domain.Fact {
	anchors := append([]domain.EvidenceRef(nil), supporting...)
	for _, fact := range supportingFacts {
		anchors = append(anchors, fact.Evidence...)
	}
	linked := make([]domain.Fact, 0)
	for _, fact := range all {
		if !isResultFact(fact) {
			continue
		}
		if anyEvidenceLinked(fact.Evidence, anchors) {
			linked = append(linked, fact)
		}
	}
	return linked
}

func anyEvidenceLinked(left, right []domain.EvidenceRef) bool {
	for _, leftRef := range left {
		for _, rightRef := range right {
			if evidenceLinked(leftRef, rightRef) {
				return true
			}
		}
	}
	return false
}

func evidenceLinked(left, right domain.EvidenceRef) bool {
	if left.EntryOrdinal == right.EntryOrdinal {
		return true
	}
	if left.ToolCallID != "" && left.ToolCallID == right.ToolCallID {
		return true
	}
	if left.RelatedEntryOrdinal != nil && *left.RelatedEntryOrdinal == right.EntryOrdinal {
		return true
	}
	if right.RelatedEntryOrdinal != nil && *right.RelatedEntryOrdinal == left.EntryOrdinal {
		return true
	}
	return left.RelatedEntryOrdinal != nil && right.RelatedEntryOrdinal != nil &&
		*left.RelatedEntryOrdinal == *right.RelatedEntryOrdinal
}

func isResultFact(fact domain.Fact) bool {
	return fact.Kind == "exit-code" || fact.Kind == "test-result"
}

func outcomeConflicts(claimed, observed string) bool {
	if claimed == domain.FactOutcomeUnknown {
		return false
	}
	return observed == domain.FactOutcomeUnknown ||
		(claimed == domain.FactOutcomeSuccess && observed == domain.FactOutcomeFailure) ||
		(claimed == domain.FactOutcomeFailure && observed == domain.FactOutcomeSuccess)
}

func validateClaimAdmissionConfig(config ClaimAdmissionConfig) error {
	route := config.Model.RequestedRoute
	if config.AnalysisRunID == "" || config.ProcessingKey == "" || config.ExtractorName == "" || config.ExtractorVersion == "" ||
		config.SchemaVersion <= 0 || config.PromptVersion == "" || config.Model.PromptVersion != config.PromptVersion ||
		route.Alias == "" || route.Gateway == "" || route.Model == "" || route.Provider == "" ||
		route.RouteVersion == "" || route.PrivacyPolicyVersion == "" || config.CreatedAt.IsZero() {
		return fmt.Errorf("%w: admission configuration", ErrClaimCandidateInvalid)
	}
	return nil
}

func boundedUTF8(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && len([]byte(value)) <= limit
}
