package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type evaluationScore struct {
	SchemaVersion       int                `json:"schemaVersion"`
	CorpusDigest        string             `json:"corpusDigest"`
	ReportDigest        string             `json:"reportDigest"`
	ReportComplete      bool               `json:"reportComplete"`
	Claims              claimScore         `json:"claims"`
	CaseCriteria        criterionScore     `json:"caseCriteria"`
	MachineExpectations []caseMachineScore `json:"machineExpectations"`
}

type claimScore struct {
	Total           int            `json:"total"`
	Reviewed        int            `json:"reviewed"`
	EvidenceSupport map[string]int `json:"evidenceSupport"`
	Usefulness      map[string]int `json:"usefulness"`
	UsefulCaseCount int            `json:"usefulCaseCount"`
}

type criterionScore struct {
	Total    int            `json:"total"`
	Reviewed int            `json:"reviewed"`
	Verdicts map[string]int `json:"verdicts"`
}

type caseMachineScore struct {
	CaseID       string                     `json:"caseId"`
	Expectations []machineExpectationResult `json:"expectations"`
}

func runScore(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("score", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reportPath := flags.String("report", "", "immutable evaluation report")
	reviewPath := flags.String("reviews", "", "edited human review sidecar")
	outputPath := flags.String("output", "", "new path for the offline score")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("evaluation score received unexpected arguments")
	}
	if err := validateOutputPaths(*outputPath); err != nil {
		return err
	}
	reportContent, err := readBoundedFile(*reportPath, maxReportBytes)
	if err != nil {
		return errors.New("evaluation report is unavailable")
	}
	reviewContent, err := readBoundedFile(*reviewPath, maxReviewBytes)
	if err != nil {
		return errors.New("evaluation review is unavailable")
	}
	var report evaluationReport
	var reviews reviewTemplate
	if decodeStrictJSON(reportContent, &report) != nil || decodeStrictJSON(reviewContent, &reviews) != nil {
		return errors.New("evaluation score input is invalid")
	}
	score, err := scoreReviews(report, reviews)
	if err != nil {
		return err
	}
	if err := writeJSONExclusive(*outputPath, score); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "evaluation score: %s\n", *outputPath)
	return nil
}

func scoreReviews(report evaluationReport, reviews reviewTemplate) (evaluationScore, error) {
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return evaluationScore{}, errors.New("evaluation report is invalid")
	}
	reportDigest := sha256Hex(reportJSON)
	profile, reviewed := reviewedCorpora[report.Corpus.Digest]
	if report.SchemaVersion != reportSchemaVersion || report.Corpus.SchemaVersion != corpusSchemaVersion ||
		!reviewed || !reportMatchesCorpusProfile(report, profile) ||
		reviews.SchemaVersion != reviewSchemaVersion ||
		reviews.CorpusDigest != report.Corpus.Digest || reviews.ReportDigest != reportDigest {
		return evaluationScore{}, errors.New("evaluation review does not match the report")
	}

	knownCases := make(map[string]bool)
	knownClaims := make(map[string]string)
	knownCriteria := make(map[string]bool)
	score := evaluationScore{
		SchemaVersion: scoreSchemaVersion, CorpusDigest: report.Corpus.Digest,
		ReportDigest: reportDigest, ReportComplete: report.Complete,
		Claims: claimScore{
			EvidenceSupport: labelCounts("unreviewed", "supported", "partly-supported", "unsupported"),
			Usefulness:      labelCounts("unreviewed", "useful", "weak", "not-useful"),
		},
		CaseCriteria: criterionScore{
			Verdicts: labelCounts("unreviewed", "pass", "partial", "fail"),
		},
		MachineExpectations: []caseMachineScore{},
	}
	for _, result := range report.Cases {
		if result.ID == "" || knownCases[result.ID] {
			return evaluationScore{}, errors.New("evaluation report is invalid")
		}
		knownCases[result.ID] = true
		score.MachineExpectations = append(score.MachineExpectations, caseMachineScore{
			CaseID: result.ID, Expectations: append([]machineExpectationResult{}, result.MachineExpectations...),
		})
		for _, claim := range result.Claims {
			if claim.Fingerprint == "" {
				return evaluationScore{}, errors.New("evaluation report is invalid")
			}
			key := result.ID + "\x00" + claim.Fingerprint
			if _, exists := knownClaims[key]; exists {
				return evaluationScore{}, errors.New("evaluation report is invalid")
			}
			knownClaims[key] = result.ID
		}
	}
	for _, criteria := range report.Corpus.HumanCriteria {
		if !containsString(report.Corpus.CaseOrder, criteria.CaseID) {
			return evaluationScore{}, errors.New("evaluation report is invalid")
		}
		for _, criterion := range criteria.Criteria {
			key := criteria.CaseID + "\x00" + criterion.ID
			if criterion.ID == "" || knownCriteria[key] {
				return evaluationScore{}, errors.New("evaluation report is invalid")
			}
			knownCriteria[key] = true
		}
	}
	score.Claims.Total = len(knownClaims)
	score.CaseCriteria.Total = len(knownCriteria)

	claimDecisions := make(map[string]claimReview)
	usefulCases := make(map[string]bool)
	for _, review := range reviews.ClaimReviews {
		key := review.CaseID + "\x00" + review.ClaimFingerprint
		if _, exists := knownClaims[key]; !exists || claimDecisions[key].ClaimFingerprint != "" ||
			!oneOf(review.EvidenceSupport, "unreviewed", "supported", "partly-supported", "unsupported") ||
			!oneOf(review.Usefulness, "unreviewed", "useful", "weak", "not-useful") ||
			!validReviewNote(review.Note) {
			return evaluationScore{}, errors.New("evaluation claim review is invalid or stale")
		}
		claimDecisions[key] = review
	}
	for key, caseID := range knownClaims {
		review, exists := claimDecisions[key]
		if !exists {
			score.Claims.EvidenceSupport["unreviewed"]++
			score.Claims.Usefulness["unreviewed"]++
			continue
		}
		score.Claims.EvidenceSupport[review.EvidenceSupport]++
		score.Claims.Usefulness[review.Usefulness]++
		if review.EvidenceSupport != "unreviewed" && review.Usefulness != "unreviewed" {
			score.Claims.Reviewed++
		}
		if review.Usefulness == "useful" {
			usefulCases[caseID] = true
		}
	}
	score.Claims.UsefulCaseCount = len(usefulCases)

	criterionDecisions := make(map[string]caseCriterionReview)
	for _, review := range reviews.CaseCriteria {
		key := review.CaseID + "\x00" + review.CriterionID
		if !knownCriteria[key] || criterionDecisions[key].CriterionID != "" ||
			!oneOf(review.Verdict, "unreviewed", "pass", "partial", "fail") ||
			!validReviewNote(review.Note) {
			return evaluationScore{}, errors.New("evaluation case review is invalid or stale")
		}
		criterionDecisions[key] = review
	}
	for key := range knownCriteria {
		review, exists := criterionDecisions[key]
		if !exists {
			score.CaseCriteria.Verdicts["unreviewed"]++
			continue
		}
		score.CaseCriteria.Verdicts[review.Verdict]++
		if review.Verdict != "unreviewed" {
			score.CaseCriteria.Reviewed++
		}
	}
	return score, nil
}

func reportMatchesCorpusProfile(report evaluationReport, profile reviewedCorpus) bool {
	if len(report.Corpus.CaseOrder) != len(profile.CaseIDs) ||
		len(report.Corpus.HumanCriteria) != len(profile.CaseIDs) ||
		len(report.Cases) != len(profile.CaseIDs) {
		return false
	}
	for index, caseID := range profile.CaseIDs {
		criteria := report.Corpus.HumanCriteria[index]
		if report.Corpus.CaseOrder[index] != caseID ||
			report.Cases[index].ID != caseID ||
			criteria.CaseID != caseID ||
			len(criteria.Criteria) != 1 ||
			criteria.Criteria[0].ID != profile.CriterionIDs[index] {
			return false
		}
	}
	return true
}

func labelCounts(labels ...string) map[string]int {
	counts := make(map[string]int, len(labels))
	for _, label := range labels {
		counts[label] = 0
	}
	return counts
}

func validReviewNote(value string) bool {
	return utf8.ValidString(value) && len([]byte(value)) <= 1024 &&
		!strings.ContainsAny(value, "\x00")
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
