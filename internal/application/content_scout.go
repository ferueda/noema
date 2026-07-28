package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const (
	maxContentScoutInputBytes        = 256 * 1024
	maxContentScoutCandidateBytes    = 256 * 1024
	maxContentScoutEvidenceRefs      = 1_024
	contentScoutPrivacyPreflight     = "privacy-preflight"
	contentScoutDisclosurePreflight  = "disclosure-preflight"
	contentScoutPrivacyPostflight    = "privacy-postflight"
	contentScoutDisclosurePostflight = "disclosure-postflight"
)

// ContentScoutKnowledgeReader supplies only the admitted semantic analysis and
// exact fact identities selected by Content Scout. It exposes no source body.
type ContentScoutKnowledgeReader interface {
	LoadSemanticAnalysis(context.Context, string) (SemanticAnalysisRecord, error)
	LoadFactsByID(context.Context, []string) ([]domain.Fact, error)
	LoadFactAnalysis(context.Context, string) (domain.FactAnalysis, error)
}

// ContentScoutHandlerV1 owns Content Scout's local preparation and admission.
// Queue lifecycle and executor invocation remain outside this type.
type ContentScoutHandlerV1 struct {
	Knowledge ContentScoutKnowledgeReader
}

// PreparedContentScoutV1 contains the bounded request input and transient
// disclosure state needed to admit the corresponding result.
type PreparedContentScoutV1 struct {
	Input         domain.ContentScoutInputV1
	CanonicalJSON json.RawMessage
	Privacy       domain.AgentPrivacyOutcomeV1
	SkipNoClaims  bool

	job        AgentJobRecordV1
	claimsByID map[string]domain.Claim
	disclosure ContentDisclosurePolicyV1
	prepared   bool
}

// ContentScoutApplicationFailure contains only a fixed category and safe
// policy metadata. It never includes private or generated text.
type ContentScoutApplicationFailure struct {
	Category string
	Privacy  domain.AgentPrivacyOutcomeV1
}

func (failure ContentScoutApplicationFailure) Error() string {
	return "Content Scout " + failure.Category
}

func (handler ContentScoutHandlerV1) Prepare(
	ctx context.Context,
	job AgentJobRecordV1,
) (PreparedContentScoutV1, error) {
	if handler.Knowledge == nil || ValidateAgentJobRecordV1(job) != nil ||
		(job.Status != domain.JobPending && job.Status != domain.JobRunning) {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}
	configuration, err := contentScoutConfigurationFromJob(job)
	if err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryConfigurationInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}
	record, err := handler.Knowledge.LoadSemanticAnalysis(
		ctx, job.Payload.Inputs.AnalysisRunID,
	)
	if err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}
	event, claimIDs, err := validateContentScoutCompletion(
		record, job.Payload.Inputs.AnalysisRunID,
	)
	if err != nil || event.ID != job.EventID ||
		!slices.Equal(claimIDs, job.Payload.Inputs.ClaimIDs) {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}
	if err := validateContentScoutAnalysisInput(record); err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}

	factIDs, err := contentScoutSupportingFactIDs(record.Analysis)
	if err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}
	facts := []domain.Fact{}
	var factAnalysis domain.FactAnalysis
	if len(factIDs) > 0 {
		facts, err = handler.Knowledge.LoadFactsByID(ctx, factIDs)
		if err != nil {
			return PreparedContentScoutV1{}, contentScoutFailure(
				domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
			)
		}
		if len(facts) > 0 {
			factAnalysis, err = handler.Knowledge.LoadFactAnalysis(
				ctx, facts[0].AnalysisRunID,
			)
			if err != nil {
				return PreparedContentScoutV1{}, contentScoutFailure(
					domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
				)
			}
		}
	}
	if err := validateContentScoutFacts(
		factIDs, facts, record.Analysis, factAnalysis,
	); err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}

	input, claimsByID, err := buildContentScoutInput(
		record.Analysis, facts,
	)
	if err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
		)
	}
	prepared := PreparedContentScoutV1{
		Input: input, SkipNoClaims: len(input.Claims) == 0,
		job: job, claimsByID: claimsByID, prepared: true,
	}
	if prepared.SkipNoClaims {
		canonical, err := canonicalContentScoutInput(input)
		if err != nil {
			return PreparedContentScoutV1{}, contentScoutFailure(
				domain.AgentFailureCategoryInputInvalid, domain.AgentPrivacyOutcomeV1{},
			)
		}
		prepared.CanonicalJSON = canonical
		return prepared, nil
	}

	privateValues := contentScoutFreeText(input)
	sanitizedValues, privacyReport, err := (PrivacyPolicy{}).PreflightBatch(privateValues)
	if err != nil {
		privacy := domain.AgentPrivacyOutcomeV1{CompletedStages: []domain.AgentPolicyStageV1{
			privacyPolicyStage(contentScoutPrivacyPreflight, domain.AgentPolicyOutcomeBlocked, privacyReport),
		}}
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryPrivacyBlocked, privacy,
		)
	}
	prepared.Privacy.CompletedStages = append(
		prepared.Privacy.CompletedStages,
		privacyPolicyStage(
			contentScoutPrivacyPreflight, domain.AgentPolicyOutcomePassed, privacyReport,
		),
	)
	disclosure, generalized, disclosureReport, err := compileContentDisclosurePolicyV1(
		privateValues, sanitizedValues, configuration.approvedTerms,
	)
	if err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryDisclosureBlocked, prepared.Privacy,
		)
	}
	prepared.Privacy.CompletedStages = append(
		prepared.Privacy.CompletedStages,
		disclosurePolicyStage(
			contentScoutDisclosurePreflight, domain.AgentPolicyOutcomePassed, disclosureReport,
		),
	)
	if err := applyContentScoutFreeText(&prepared.Input, generalized); err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, prepared.Privacy,
		)
	}
	canonical, err := canonicalContentScoutInput(prepared.Input)
	if err != nil {
		return PreparedContentScoutV1{}, contentScoutFailure(
			domain.AgentFailureCategoryInputInvalid, prepared.Privacy,
		)
	}
	prepared.CanonicalJSON = canonical
	prepared.disclosure = disclosure
	return prepared, nil
}

func contentScoutConfigurationFromJob(
	job AgentJobRecordV1,
) (ContentScoutConfiguration, error) {
	var handler contentScoutHandlerConfiguration
	if _, err := decodeStrictBoundedJSON(
		bytes.NewReader(job.Payload.Configuration.HandlerConfigurationJSON),
		maxContentScoutConfigurationBytes,
		&handler,
	); err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout configuration is invalid")
	}
	configuration := ContentScoutConfiguration{
		agent: job.Agent, identity: job.Payload.Configuration,
		agentFileDigest: handler.AgentFileDigest,
		approvedTerms:   append([]string{}, handler.ApprovedPublicTerms...),
	}
	if configuration.validate() != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout configuration is invalid")
	}
	return configuration, nil
}

func validateContentScoutAnalysisInput(record SemanticAnalysisRecord) error {
	run := record.Analysis.Run
	if run.Selection == nil || run.Omissions.CanonicalSegments < 0 ||
		run.Omissions.OmittedTextFactCount < 0 ||
		run.Omissions.OmittedTextOriginalUTF8Bytes < 0 ||
		record.Details.Selection == nil ||
		record.Details.InputFactIDs == nil ||
		!slices.Equal(run.InputFactIDs, *record.Details.InputFactIDs) {
		return errors.New("Content Scout analysis input is invalid")
	}
	if err := ValidateSemanticSelectionProjection(
		*record.Details.Selection, *run.Selection,
	); err != nil {
		return errors.New("Content Scout analysis selection is invalid")
	}
	if run.Selection.Coverage != domain.CoverageCompleteRetainedSnapshot &&
		run.Selection.Coverage != semanticCoveragePartial {
		return errors.New("Content Scout analysis coverage is invalid")
	}
	inputFactIDs := make(map[string]struct{}, len(run.InputFactIDs))
	for _, factID := range run.InputFactIDs {
		if factID == "" {
			return errors.New("Content Scout analysis fact identity is invalid")
		}
		if _, duplicate := inputFactIDs[factID]; duplicate {
			return errors.New("Content Scout analysis fact identity is duplicated")
		}
		inputFactIDs[factID] = struct{}{}
	}
	evidenceByID := map[string]domain.EvidenceRef{}
	totalEvidence := 0
	for _, claim := range record.Analysis.Claims {
		if !claim.Type.Valid() || !claim.Status.Valid() ||
			claim.Statement == "" || math.IsNaN(claim.Confidence) ||
			math.IsInf(claim.Confidence, 0) ||
			claim.Confidence < 0 || claim.Confidence > 1 ||
			len(claim.SupportingEvidence) == 0 {
			return errors.New("Content Scout claim is invalid")
		}
		seenFacts := map[string]struct{}{}
		for _, factID := range claim.SupportingFactIDs {
			if _, exists := inputFactIDs[factID]; !exists {
				return errors.New("Content Scout claim fact is outside analysis")
			}
			if _, duplicate := seenFacts[factID]; duplicate {
				return errors.New("Content Scout claim fact is duplicated")
			}
			seenFacts[factID] = struct{}{}
		}
		seenSupporting := map[string]struct{}{}
		for _, ref := range claim.SupportingEvidence {
			if err := addContentScoutEvidence(evidenceByID, ref); err != nil {
				return err
			}
			seenSupporting[ref.ID] = struct{}{}
			totalEvidence++
		}
		seenContradicting := map[string]struct{}{}
		for _, ref := range claim.ContradictingEvidence {
			if _, overlap := seenSupporting[ref.ID]; overlap {
				return errors.New("Content Scout claim evidence overlaps")
			}
			if _, duplicate := seenContradicting[ref.ID]; duplicate {
				return errors.New("Content Scout claim evidence is duplicated")
			}
			if err := addContentScoutEvidence(evidenceByID, ref); err != nil {
				return err
			}
			seenContradicting[ref.ID] = struct{}{}
			totalEvidence++
		}
	}
	if totalEvidence > maxContentScoutEvidenceRefs {
		return errors.New("Content Scout evidence limit exceeded")
	}
	return nil
}

func contentScoutSupportingFactIDs(
	analysis domain.SemanticAnalysis,
) ([]string, error) {
	inputFacts := make(map[string]struct{}, len(analysis.Run.InputFactIDs))
	for _, factID := range analysis.Run.InputFactIDs {
		inputFacts[factID] = struct{}{}
	}
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for _, claim := range analysis.Claims {
		for _, factID := range claim.SupportingFactIDs {
			if _, available := inputFacts[factID]; !available {
				return nil, errors.New("Content Scout supporting fact is unavailable")
			}
			if _, duplicate := seen[factID]; duplicate {
				continue
			}
			seen[factID] = struct{}{}
			result = append(result, factID)
		}
	}
	return result, nil
}

func validateContentScoutFacts(
	factIDs []string,
	facts []domain.Fact,
	analysis domain.SemanticAnalysis,
	factAnalysis domain.FactAnalysis,
) error {
	if len(factIDs) != len(facts) {
		return errors.New("Content Scout facts are incomplete")
	}
	if len(facts) == 0 {
		return nil
	}
	if analysis.Run.Revision == nil ||
		factAnalysis.Run.ID == "" ||
		factAnalysis.Run.Stage != domain.AnalysisStageFacts ||
		factAnalysis.Run.Status != domain.AnalysisCompleted ||
		factAnalysis.Run.Revision == nil ||
		factAnalysis.Run.Revision.Identity() != analysis.Run.Revision.Identity() ||
		len(factAnalysis.Run.FactIDs) != len(factAnalysis.Facts) {
		return errors.New("Content Scout fact analysis is invalid")
	}
	originByID := make(map[string]int, len(factAnalysis.Facts))
	for index, fact := range factAnalysis.Facts {
		if fact.ID != factAnalysis.Run.FactIDs[index] ||
			fact.AnalysisRunID != factAnalysis.Run.ID ||
			fact.ExtractorName != factAnalysis.Run.ExtractorName ||
			fact.ExtractorVersion != factAnalysis.Run.ExtractorVersion ||
			fact.SchemaVersion != factAnalysis.Run.SchemaVersion {
			return errors.New("Content Scout fact analysis is inconsistent")
		}
		if _, duplicate := originByID[fact.ID]; duplicate {
			return errors.New("Content Scout fact analysis is duplicated")
		}
		originByID[fact.ID] = index
	}
	if !orderedContentScoutFactSubset(
		analysis.Run.InputFactIDs, originByID,
	) {
		return errors.New("Content Scout semantic facts are outside fact analysis")
	}
	evidenceByID := map[string]domain.EvidenceRef{}
	totalEvidence := 0
	for _, claim := range analysis.Claims {
		for _, ref := range claim.SupportingEvidence {
			if err := addContentScoutEvidence(evidenceByID, ref); err != nil {
				return err
			}
		}
		for _, ref := range claim.ContradictingEvidence {
			if err := addContentScoutEvidence(evidenceByID, ref); err != nil {
				return err
			}
		}
	}
	for index, fact := range facts {
		originIndex, exists := originByID[fact.ID]
		if fact.ID != factIDs[index] || fact.ID == "" || fact.Kind == "" ||
			fact.SchemaVersion < 1 || fact.AnalysisRunID == "" ||
			!validContentScoutFactOutcome(fact.Outcome) ||
			len(fact.Evidence) == 0 ||
			!exists ||
			!sameSemanticIdentityValue(
				fact, factAnalysis.Facts[originIndex],
			) {
			return errors.New("Content Scout fact is invalid")
		}
		fingerprint, err := factFingerprint(
			factAnalysis.Run.Revision.Identity(), fact,
		)
		if err != nil ||
			fingerprint != fact.Fingerprint ||
			fact.ID != platform.DerivedID("fact_", fingerprint) {
			return errors.New("Content Scout fact identity is invalid")
		}
		seenEvidence := map[string]struct{}{}
		for _, ref := range fact.Evidence {
			if _, duplicate := seenEvidence[ref.ID]; duplicate {
				return errors.New("Content Scout fact evidence is duplicated")
			}
			seenEvidence[ref.ID] = struct{}{}
			if err := addContentScoutEvidence(evidenceByID, ref); err != nil {
				return err
			}
			totalEvidence++
		}
	}
	if totalEvidence > maxContentScoutEvidenceRefs {
		return errors.New("Content Scout fact evidence limit exceeded")
	}
	return nil
}

func orderedContentScoutFactSubset(
	factIDs []string,
	originByID map[string]int,
) bool {
	lastIndex := -1
	for _, factID := range factIDs {
		index, exists := originByID[factID]
		if !exists || index <= lastIndex {
			return false
		}
		lastIndex = index
	}
	return true
}

func addContentScoutEvidence(
	known map[string]domain.EvidenceRef,
	ref domain.EvidenceRef,
) error {
	if ref.ID == "" || ref.SourceKind == "" || ref.SourceIdentity == "" ||
		ref.DocumentDigest == "" || ref.EntryOrdinal < 0 ||
		ref.Excerpt != "" ||
		ref.SegmentOrdinal != nil && *ref.SegmentOrdinal < 0 {
		return errors.New("Content Scout evidence is invalid")
	}
	if existing, duplicate := known[ref.ID]; duplicate {
		if !sameEvidenceReference(existing, ref) {
			return errors.New("Content Scout evidence identity is inconsistent")
		}
		return nil
	}
	known[ref.ID] = ref
	return nil
}

func buildContentScoutInput(
	analysis domain.SemanticAnalysis,
	facts []domain.Fact,
) (
	domain.ContentScoutInputV1,
	map[string]domain.Claim,
	error,
) {
	claims := make([]domain.ContentScoutClaimV1, 0, len(analysis.Claims))
	claimsByID := make(map[string]domain.Claim, len(analysis.Claims))
	for _, claim := range analysis.Claims {
		supporting := evidenceIDs(claim.SupportingEvidence)
		contradicting := evidenceIDs(claim.ContradictingEvidence)
		var outcome *string
		if claim.Outcome != "" {
			copy := claim.Outcome
			outcome = &copy
		}
		value := domain.ContentScoutClaimV1{
			ID: claim.ID, Type: claim.Type, Statement: claim.Statement,
			Status: claim.Status, Confidence: claim.Confidence, Outcome: outcome,
			SupportingFactIDs:        append([]string{}, claim.SupportingFactIDs...),
			SupportingEvidenceIDs:    supporting,
			ContradictingEvidenceIDs: contradicting,
		}
		if value.Validate() != nil {
			return domain.ContentScoutInputV1{}, nil,
				errors.New("Content Scout claim input is invalid")
		}
		claims = append(claims, value)
		claimsByID[claim.ID] = claim
	}
	contentFacts := make([]domain.ContentScoutFactV1, 0, len(facts))
	for _, fact := range facts {
		value := contentScoutFact(fact)
		if value.Validate() != nil {
			return domain.ContentScoutInputV1{}, nil,
				errors.New("Content Scout fact input is invalid")
		}
		contentFacts = append(contentFacts, value)
	}
	input := domain.ContentScoutInputV1{
		SchemaVersion: domain.ContentScoutInputSchemaVersion,
		AnalysisRunID: analysis.Run.ID,
		Coverage: domain.ContentScoutCoverageV1{
			Scope:              analysis.Run.Selection.Coverage,
			SelectedClaimCount: len(claims), SelectedFactCount: len(contentFacts),
		},
		Claims: claims, Facts: contentFacts, Omissions: analysis.Run.Omissions,
	}
	if input.Validate() != nil {
		return domain.ContentScoutInputV1{}, nil,
			errors.New("Content Scout input is invalid")
	}
	return input, claimsByID, nil
}

func contentScoutFact(fact domain.Fact) domain.ContentScoutFactV1 {
	result := domain.ContentScoutFactV1{
		ID: fact.ID, Kind: fact.Kind, Outcome: fact.Outcome,
		EvidenceIDs: evidenceIDs(fact.Evidence),
	}
	if fact.Value.Tool != nil {
		result.Value.Tool = &domain.ContentScoutToolValueV1{
			Kind:      fact.Value.Tool.Kind,
			Name:      optionalString(fact.Value.Tool.Name),
			Namespace: optionalString(fact.Value.Tool.Namespace),
		}
	}
	result.Value.Command = contentScoutSelectedText(fact.Value.Command)
	if fact.Value.Test != nil {
		result.Value.Test = &domain.ContentScoutTestValueV1{
			Framework: fact.Value.Test.Framework,
			Command:   contentScoutSelectedText(fact.Value.Test.Command),
			Passed:    cloneOptionalInt(fact.Value.Test.Passed),
			Failed:    cloneOptionalInt(fact.Value.Test.Failed),
			Skipped:   cloneOptionalInt(fact.Value.Test.Skipped),
		}
	}
	result.Value.ExitCode = cloneOptionalInt(fact.Value.ExitCode)
	result.Value.Error = contentScoutSelectedText(fact.Value.Error)
	return result
}

func contentScoutSelectedText(value *domain.SelectedText) *domain.ContentScoutSelectedTextV1 {
	if value == nil {
		return nil
	}
	// A bounded extractor can retain an explicitly truncated value after its
	// text budget reaches zero. The aggregate omission metadata remains in the
	// input; there is no text to disclose to the agent.
	if value.Text == "" &&
		value.EmittedUTF8Bytes == 0 &&
		value.OriginalUTF8Bytes > 0 &&
		value.Truncated {
		return nil
	}
	return &domain.ContentScoutSelectedTextV1{
		Text: value.Text, EmittedUTF8Bytes: value.EmittedUTF8Bytes,
		OriginalUTF8Bytes: value.OriginalUTF8Bytes, Truncated: value.Truncated,
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func evidenceIDs(values []domain.EvidenceRef) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.ID
	}
	return result
}

func validContentScoutFactOutcome(value string) bool {
	return value == domain.FactOutcomeNotApplicable ||
		value == domain.FactOutcomeSuccess ||
		value == domain.FactOutcomeFailure ||
		value == domain.FactOutcomeUnknown
}

func canonicalContentScoutInput(
	input domain.ContentScoutInputV1,
) (json.RawMessage, error) {
	if input.Validate() != nil {
		return nil, errors.New("Content Scout input is invalid")
	}
	document, err := json.Marshal(input)
	if err != nil || len(document) == 0 || len(document) > maxContentScoutInputBytes {
		return nil, errors.New("Content Scout input exceeds its bound")
	}
	return json.RawMessage(document), nil
}

func contentScoutFreeText(input domain.ContentScoutInputV1) []string {
	result := make([]string, 0)
	_ = walkContentScoutFreeText(
		&input,
		func(value *string, _ *domain.ContentScoutSelectedTextV1) error {
			result = append(result, *value)
			return nil
		},
	)
	return result
}

func applyContentScoutFreeText(
	input *domain.ContentScoutInputV1,
	values []string,
) error {
	index := 0
	err := walkContentScoutFreeText(
		input,
		func(target *string, selected *domain.ContentScoutSelectedTextV1) error {
			if index >= len(values) {
				return errors.New("Content Scout free-text traversal is incomplete")
			}
			*target = values[index]
			if selected != nil {
				selected.EmittedUTF8Bytes = len([]byte(*target))
			}
			index++
			return nil
		},
	)
	if err != nil {
		return err
	}
	if index != len(values) {
		return errors.New("Content Scout free-text traversal has extra values")
	}
	return nil
}

func walkContentScoutFreeText(
	input *domain.ContentScoutInputV1,
	visit func(*string, *domain.ContentScoutSelectedTextV1) error,
) error {
	for claimIndex := range input.Claims {
		if err := visit(&input.Claims[claimIndex].Statement, nil); err != nil {
			return err
		}
	}
	for factIndex := range input.Facts {
		fact := &input.Facts[factIndex]
		if fact.Value.Tool != nil {
			if fact.Value.Tool.Name != nil {
				if err := visit(fact.Value.Tool.Name, nil); err != nil {
					return err
				}
			}
			if fact.Value.Tool.Namespace != nil {
				if err := visit(fact.Value.Tool.Namespace, nil); err != nil {
					return err
				}
			}
		}
		if fact.Value.Command != nil {
			if err := visit(&fact.Value.Command.Text, fact.Value.Command); err != nil {
				return err
			}
		}
		if fact.Value.Test != nil {
			if err := visit(&fact.Value.Test.Framework, nil); err != nil {
				return err
			}
			if fact.Value.Test.Command != nil {
				if err := visit(
					&fact.Value.Test.Command.Text,
					fact.Value.Test.Command,
				); err != nil {
					return err
				}
			}
		}
		if fact.Value.Error != nil {
			if err := visit(&fact.Value.Error.Text, fact.Value.Error); err != nil {
				return err
			}
		}
	}
	return nil
}

func privacyPolicyStage(
	name string,
	outcome string,
	report PrivacyReport,
) domain.AgentPolicyStageV1 {
	counts := make(map[string]int, len(report.Redactions)+len(report.BlockedCategories))
	for _, value := range report.Redactions {
		counts[value.Category] += value.Count
	}
	for _, category := range report.BlockedCategories {
		if counts[category] == 0 {
			counts[category] = 1
		}
	}
	categories := make([]domain.AgentPolicyCategoryCountV1, 0, len(counts))
	for _, value := range sortedCounts(counts) {
		categories = append(categories, domain.AgentPolicyCategoryCountV1{
			Category: value.Category, Count: value.Count,
		})
	}
	return domain.AgentPolicyStageV1{
		Name: name, PolicyVersion: PrivacyPolicyVersion,
		Outcome: outcome, Categories: categories,
	}
}

func appendPolicyStage(
	outcome domain.AgentPrivacyOutcomeV1,
	stage domain.AgentPolicyStageV1,
) domain.AgentPrivacyOutcomeV1 {
	result := domain.AgentPrivacyOutcomeV1{
		CompletedStages: append([]domain.AgentPolicyStageV1{}, outcome.CompletedStages...),
	}
	result.CompletedStages = append(result.CompletedStages, stage)
	return result
}

func contentScoutFailure(
	category string,
	privacy domain.AgentPrivacyOutcomeV1,
) ContentScoutApplicationFailure {
	return ContentScoutApplicationFailure{Category: category, Privacy: privacy}
}
