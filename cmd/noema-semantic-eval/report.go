package main

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

const (
	reportSchemaVersion = 1
	reviewSchemaVersion = 1
	scoreSchemaVersion  = 1
	maxReportBytes      = 8 << 20
	maxReviewBytes      = 4 << 20
)

type evaluationReport struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Corpus        corpusIdentity           `json:"corpus"`
	Contract      semanticContractIdentity `json:"contract"`
	Route         routeIdentity            `json:"route"`
	StartedAt     time.Time                `json:"startedAt"`
	FinishedAt    time.Time                `json:"finishedAt"`
	Complete      bool                     `json:"complete"`
	StopCategory  string                   `json:"stopCategory,omitempty"`
	Cases         []evaluationCaseResult   `json:"cases"`
	Aggregates    evaluationAggregates     `json:"aggregates"`
}

type corpusIdentity struct {
	SchemaVersion int               `json:"schemaVersion"`
	Digest        string            `json:"digest"`
	CaseOrder     []string          `json:"caseOrder"`
	HumanCriteria []caseCriteriaSet `json:"humanCriteria"`
}

type caseCriteriaSet struct {
	CaseID   string           `json:"caseId"`
	Criteria []humanCriterion `json:"criteria"`
}

type semanticContractIdentity struct {
	ExtractorName    string                                `json:"extractorName"`
	ExtractorVersion string                                `json:"extractorVersion"`
	ClaimSchema      domain.StructuredOutputSchemaIdentity `json:"claimSchema"`
	PromptVersion    string                                `json:"promptVersion"`
	PrivacyVersion   string                                `json:"privacyVersion"`
}

type routeIdentity struct {
	ConfigDigest    string                     `json:"configDigest"`
	SanitizedConfig json.RawMessage            `json:"sanitizedConfig"`
	Requested       domain.RequestedModelRoute `json:"requested"`
}

type evaluationCaseResult struct {
	ID                  string                         `json:"id"`
	Intent              string                         `json:"intent"`
	Status              string                         `json:"status"`
	FailureStage        string                         `json:"failureStage,omitempty"`
	FailureCategory     string                         `json:"failureCategory,omitempty"`
	CandidateCount      int                            `json:"candidateCount"`
	AdmittedCount       int                            `json:"admittedCount"`
	Claims              []domain.Claim                 `json:"claims"`
	Evidence            []reviewEvidence               `json:"evidence"`
	Model               *domain.ModelExecutionMetadata `json:"model,omitempty"`
	MachineExpectations []machineExpectationResult     `json:"machineExpectations"`
}

type reviewEvidence struct {
	ID             string `json:"id"`
	EntryOrdinal   int    `json:"entryOrdinal"`
	SegmentOrdinal *int   `json:"segmentOrdinal,omitempty"`
	Text           string `json:"text,omitempty"`
}

type machineExpectationResult struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Value     string `json:"value,omitempty"`
	Evaluated bool   `json:"evaluated"`
	Passed    bool   `json:"passed"`
}

type evaluationAggregates struct {
	CaseCount               int            `json:"caseCount"`
	CompletedCaseCount      int            `json:"completedCaseCount"`
	ValidBatchCount         int            `json:"validBatchCount"`
	EmptyBatchCount         int            `json:"emptyBatchCount"`
	FailureCounts           map[string]int `json:"failureCounts"`
	ExpectationCount        int            `json:"expectationCount"`
	EvaluatedExpectations   int            `json:"evaluatedExpectations"`
	PassedExpectations      int            `json:"passedExpectations"`
	TokenMetadataCount      int            `json:"tokenMetadataCount"`
	TotalTokens             int            `json:"totalTokens"`
	LatencyMetadataCount    int            `json:"latencyMetadataCount"`
	MeanLatencyMilliseconds *int64         `json:"meanLatencyMilliseconds,omitempty"`
	P50LatencyMilliseconds  *int64         `json:"p50LatencyMilliseconds,omitempty"`
	P95LatencyMilliseconds  *int64         `json:"p95LatencyMilliseconds,omitempty"`
	CostMetadataCount       int            `json:"costMetadataCount"`
	TotalCostUSD            *string        `json:"totalCostUsd,omitempty"`
	AverageCostUSD          *string        `json:"averageCostUsd,omitempty"`
	ValidBatchRate          countRatio     `json:"validBatchRate"`
	ExpectationCoverage     countRatio     `json:"expectationCoverage"`
	TokenMetadataCoverage   countRatio     `json:"tokenMetadataCoverage"`
	LatencyMetadataCoverage countRatio     `json:"latencyMetadataCoverage"`
	CostMetadataCoverage    countRatio     `json:"costMetadataCoverage"`
}

type countRatio struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator"`
}

type reviewTemplate struct {
	SchemaVersion int                   `json:"schemaVersion"`
	CorpusDigest  string                `json:"corpusDigest"`
	ReportDigest  string                `json:"reportDigest"`
	ClaimReviews  []claimReview         `json:"claimReviews"`
	CaseCriteria  []caseCriterionReview `json:"caseCriteria"`
}

type claimReview struct {
	CaseID           string `json:"caseId"`
	ClaimFingerprint string `json:"claimFingerprint"`
	EvidenceSupport  string `json:"evidenceSupport"`
	Usefulness       string `json:"usefulness"`
	Note             string `json:"note"`
}

type caseCriterionReview struct {
	CaseID      string `json:"caseId"`
	CriterionID string `json:"criterionId"`
	Verdict     string `json:"verdict"`
	Note        string `json:"note"`
}

func newEvaluationReport(
	corpus evaluationCorpus,
	route domain.ValidatedModelRoute,
	startedAt time.Time,
) evaluationReport {
	order := make([]string, len(corpus.Cases))
	criteria := make([]caseCriteriaSet, len(corpus.Cases))
	for index := range corpus.Cases {
		order[index] = corpus.Cases[index].Definition.ID
		criteria[index] = caseCriteriaSet{
			CaseID:   order[index],
			Criteria: append([]humanCriterion{}, corpus.Cases[index].Definition.HumanCriteria...),
		}
	}
	return evaluationReport{
		SchemaVersion: reportSchemaVersion,
		Corpus: corpusIdentity{
			SchemaVersion: corpusSchemaVersion, Digest: corpus.Digest,
			CaseOrder: order, HumanCriteria: criteria,
		},
		Contract: semanticContractIdentity{
			ExtractorName:    application.SemanticExtractorName,
			ExtractorVersion: application.SemanticExtractorVersion,
			PromptVersion:    application.SemanticPromptVersion,
			PrivacyVersion:   application.PrivacyPolicyVersion,
		},
		Route: routeIdentity{
			ConfigDigest:    route.ConfigDigest,
			SanitizedConfig: append(json.RawMessage(nil), route.SanitizedConfig...),
			Requested:       route.Requested,
		},
		StartedAt:  startedAt.UTC(),
		Cases:      []evaluationCaseResult{},
		Aggregates: evaluationAggregates{FailureCounts: map[string]int{}},
	}
}

func finalizeReport(report *evaluationReport, finishedAt time.Time) {
	report.FinishedAt = finishedAt.UTC()
	caseCount := len(report.Corpus.CaseOrder)
	report.Complete = report.StopCategory == "" && len(report.Cases) == caseCount
	aggregates := evaluationAggregates{
		CaseCount: caseCount, FailureCounts: make(map[string]int),
	}
	latencies := []int64{}
	totalCost := new(big.Rat)
	for _, result := range report.Cases {
		switch result.Status {
		case "completed":
			aggregates.CompletedCaseCount++
			aggregates.ValidBatchCount++
			if result.AdmittedCount == 0 {
				aggregates.EmptyBatchCount++
			}
		case "failed":
			aggregates.FailureCounts[result.FailureCategory]++
		}
		for _, expectation := range result.MachineExpectations {
			aggregates.ExpectationCount++
			if expectation.Evaluated {
				aggregates.EvaluatedExpectations++
				if expectation.Passed {
					aggregates.PassedExpectations++
				}
			}
		}
		if result.Model == nil {
			continue
		}
		if result.Model.TotalTokens != nil {
			aggregates.TokenMetadataCount++
			aggregates.TotalTokens += *result.Model.TotalTokens
		}
		if result.Model.LatencyMilliseconds != nil {
			aggregates.LatencyMetadataCount++
			latencies = append(latencies, *result.Model.LatencyMilliseconds)
		}
		if result.Model.CostUSD != nil {
			cost, ok := new(big.Rat).SetString(*result.Model.CostUSD)
			if ok {
				aggregates.CostMetadataCount++
				totalCost.Add(totalCost, cost)
			}
		}
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
		var total int64
		for _, latency := range latencies {
			total += latency
		}
		mean := total / int64(len(latencies))
		p50 := latencies[(len(latencies)-1)/2]
		p95 := latencies[(95*len(latencies)+99)/100-1]
		aggregates.MeanLatencyMilliseconds = &mean
		aggregates.P50LatencyMilliseconds = &p50
		aggregates.P95LatencyMilliseconds = &p95
	}
	if aggregates.CostMetadataCount > 0 {
		total := decimalString(totalCost, 12)
		average := decimalString(new(big.Rat).Quo(totalCost, big.NewRat(int64(aggregates.CostMetadataCount), 1)), 12)
		aggregates.TotalCostUSD = &total
		aggregates.AverageCostUSD = &average
	}
	aggregates.ValidBatchRate = countRatio{
		Numerator: aggregates.ValidBatchCount, Denominator: len(report.Cases),
	}
	aggregates.ExpectationCoverage = countRatio{
		Numerator: aggregates.EvaluatedExpectations, Denominator: aggregates.ExpectationCount,
	}
	aggregates.TokenMetadataCoverage = countRatio{
		Numerator: aggregates.TokenMetadataCount, Denominator: len(report.Cases),
	}
	aggregates.LatencyMetadataCoverage = countRatio{
		Numerator: aggregates.LatencyMetadataCount, Denominator: len(report.Cases),
	}
	aggregates.CostMetadataCoverage = countRatio{
		Numerator: aggregates.CostMetadataCount, Denominator: len(report.Cases),
	}
	report.Aggregates = aggregates
}

func expectationResults(
	definitions []machineExpectation,
	claims []domain.Claim,
	evaluated bool,
) []machineExpectationResult {
	results := make([]machineExpectationResult, len(definitions))
	for index, expectation := range definitions {
		result := machineExpectationResult{
			ID: expectation.ID, Kind: expectation.Kind, Value: expectation.Value, Evaluated: evaluated,
		}
		if evaluated {
			switch expectation.Kind {
			case "must-be-empty":
				result.Passed = len(claims) == 0
			case "must-include-claim-type":
				for _, claim := range claims {
					result.Passed = result.Passed || string(claim.Type) == expectation.Value
				}
			case "must-not-include-outcome":
				result.Passed = true
				for _, claim := range claims {
					if claim.Outcome == expectation.Value {
						result.Passed = false
					}
				}
			}
		}
		results[index] = result
	}
	return results
}

func citedEvidence(fixture evaluationCase, claims []domain.Claim) []reviewEvidence {
	seen := make(map[string]bool)
	result := []reviewEvidence{}
	for _, claim := range claims {
		refs := append([]domain.EvidenceRef{}, claim.SupportingEvidence...)
		refs = append(refs, claim.ContradictingEvidence...)
		for _, ref := range refs {
			if seen[ref.ID] {
				continue
			}
			seen[ref.ID] = true
			value := reviewEvidence{
				ID: ref.ID, EntryOrdinal: ref.EntryOrdinal, SegmentOrdinal: cloneInt(ref.SegmentOrdinal),
			}
			if ref.SegmentOrdinal != nil {
				segment := fixture.Document.Entries[ref.EntryOrdinal].Content[*ref.SegmentOrdinal]
				if segment.Text != nil {
					value.Text = segment.Text.Text
				}
			}
			result = append(result, value)
		}
	}
	return result
}

func buildReviewTemplate(report evaluationReport, corpus evaluationCorpus) (reviewTemplate, error) {
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return reviewTemplate{}, err
	}
	template := reviewTemplate{
		SchemaVersion: reviewSchemaVersion, CorpusDigest: corpus.Digest,
		ReportDigest: sha256Hex(reportJSON), ClaimReviews: []claimReview{}, CaseCriteria: []caseCriterionReview{},
	}
	for _, result := range report.Cases {
		for _, claim := range result.Claims {
			template.ClaimReviews = append(template.ClaimReviews, claimReview{
				CaseID: result.ID, ClaimFingerprint: claim.Fingerprint,
				EvidenceSupport: "unreviewed", Usefulness: "unreviewed", Note: "",
			})
		}
	}
	for _, fixture := range corpus.Cases {
		for _, criterion := range fixture.Definition.HumanCriteria {
			template.CaseCriteria = append(template.CaseCriteria, caseCriterionReview{
				CaseID: fixture.Definition.ID, CriterionID: criterion.ID, Verdict: "unreviewed", Note: "",
			})
		}
	}
	return template, nil
}

func writeJSONExclusive(path string, value any) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path is required")
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("encode evaluation output")
	}
	content = append(content, '\n')
	if len(content) > maxReportBytes {
		return errors.New("evaluation output is too large")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.New("create evaluation output directory")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("evaluation output already exists or cannot be created")
	}
	remove := true
	defer func() {
		file.Close()
		if remove {
			os.Remove(path)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return errors.New("write evaluation output")
	}
	if err := file.Sync(); err != nil {
		return errors.New("sync evaluation output")
	}
	if err := file.Close(); err != nil {
		return errors.New("close evaluation output")
	}
	remove = false
	return nil
}

func validateOutputPaths(paths ...string) error {
	seen := make(map[string]bool)
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			return errors.New("output path is required")
		}
		absolute, err := filepath.Abs(path)
		if err != nil || seen[absolute] {
			return errors.New("output paths must be distinct")
		}
		seen[absolute] = true
		if _, err := os.Lstat(absolute); err == nil || !errors.Is(err, os.ErrNotExist) {
			return errors.New("evaluation output already exists or cannot be inspected")
		}
	}
	return nil
}

func decimalString(value *big.Rat, precision int) string {
	text := value.FloatString(precision)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "" {
		return "0"
	}
	return text
}
