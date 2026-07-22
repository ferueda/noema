package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func TestSemanticAnalysisDurabilityThroughCLI(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	databasePath := filepath.Join(temp, "noema.db")
	exportPath := filepath.Join(temp, "export.jsonl")
	executable := filepath.Join(temp, "sessions")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec /bin/cat \"$NOEMA_FAKE_EXPORT\"\n"), 0o700); err != nil {
		t.Fatalf("write fake Sessions executable: %v", err)
	}
	t.Setenv("NOEMA_SESSIONS_COMMAND", executable)
	t.Setenv("NOEMA_FAKE_EXPORT", exportPath)
	writeExportFixture(t, exportPath, strings.Repeat("d", 64))

	fact := runScanForTest(t, ctx, databasePath)
	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open semantic database: %v", err)
	}
	store := sqlitestore.NewStore(database)
	now := time.Date(2026, time.July, 21, 19, 0, 0, 0, time.UTC)

	analysis := runSemanticForCLI(t, ctx, store, fact.AnalysisID, semanticCLIRoute(t, "ordered"), semanticCLIGenerator{}, "semantic-cli-ordered", now)
	empty := runSemanticForCLI(t, ctx, store, fact.AnalysisID, semanticCLIRoute(t, "empty"), semanticCLIGenerator{empty: true}, "semantic-cli-empty", now)
	if err := database.Close(); err != nil {
		t.Fatalf("close semantic database: %v", err)
	}

	shown, _ := showSemanticForCLI(t, ctx, databasePath, analysis.Record.Analysis.Run.ID, false)
	if shown.Analysis.Run.Status != domain.AnalysisCompleted || len(shown.Analysis.Claims) != 2 ||
		shown.Analysis.Claims[0].Type != domain.ClaimTypeProblem ||
		shown.Analysis.Claims[1].Type != domain.ClaimTypeLesson {
		t.Fatalf("shown ordered semantic analysis = %#v", shown.Analysis)
	}
	wantClaimIDs := []string{shown.Analysis.Claims[0].ID, shown.Analysis.Claims[1].ID}
	if shown.Details.ClaimIDs == nil || !reflect.DeepEqual(*shown.Details.ClaimIDs, wantClaimIDs) ||
		shown.Details.InputFactIDs == nil || shown.Details.InputDigest == nil ||
		shown.Details.Selection == nil || shown.Details.Privacy == nil || shown.Details.Model == nil {
		t.Fatalf("shown semantic details = %#v", shown.Details)
	}

	resolvedRecord, resolvedJSON := showSemanticForCLI(t, ctx, databasePath, analysis.Record.Analysis.Run.ID, true)
	var resolved application.ResolvedSemanticAnalysis
	if err := json.Unmarshal(resolvedJSON, &resolved); err != nil {
		t.Fatalf("decode resolved semantic analysis: %v", err)
	}
	if !reflect.DeepEqual(resolved.Record, resolvedRecord) || len(resolved.Evidence) != 2 {
		t.Fatalf("resolved semantic analysis = %#v", resolved)
	}
	wantTexts := []string{
		`{"cmd":"go test ./...","yield_time_ms":30000}`,
		"Process exited with code 0\n1 passed\n",
	}
	for index, item := range resolved.Evidence {
		wantReference := shown.Analysis.Claims[index].SupportingEvidence[0]
		if !reflect.DeepEqual(item.Reference, wantReference) || item.Text != wantTexts[index] ||
			item.Reference.ContentHash != textDigest(wantTexts[index]) || item.Truncated {
			t.Fatalf("resolved evidence %d = %#v, want exact text %q and reference %#v", index, item, wantTexts[index], wantReference)
		}
	}

	emptyShown, emptyJSON := showSemanticForCLI(t, ctx, databasePath, empty.Record.Analysis.Run.ID, false)
	var emptyWire struct {
		Details struct {
			ClaimIDs json.RawMessage `json:"claimIds"`
		} `json:"details"`
	}
	if err := json.Unmarshal(emptyJSON, &emptyWire); err != nil {
		t.Fatalf("decode known-empty claim IDs: %v", err)
	}
	if emptyShown.Details.ClaimIDs == nil || len(*emptyShown.Details.ClaimIDs) != 0 ||
		len(emptyShown.Analysis.Claims) != 0 || !bytes.Equal(bytes.TrimSpace(emptyWire.Details.ClaimIDs), []byte("[]")) {
		t.Fatalf("known-empty claims did not survive CLI round trip: %s", emptyJSON)
	}

	writeExportFixture(t, exportPath, strings.Repeat("e", 64))
	var stdout, stderr bytes.Buffer
	err = run(ctx, []string{
		"analyses", "show", analysis.Record.Analysis.Run.ID, "--resolve", "--database", databasePath,
	}, &stdout, &stderr)
	if !errors.Is(err, application.ErrSourceRevisionUnavailable) {
		t.Fatalf("resolve changed semantic revision error = %v", err)
	}

	database, err = sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen semantic database: %v", err)
	}
	defer database.Close()
	for _, table := range []string{"jobs", "agent_runs", "content_ideas"} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
}

type semanticCLIGenerator struct {
	empty bool
}

func (generator semanticCLIGenerator) Generate(
	_ context.Context,
	request application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	candidates := []domain.ClaimCandidate{}
	if !generator.empty {
		candidates = []domain.ClaimCandidate{
			{
				Type: domain.ClaimTypeProblem, Statement: "The test command required a durable record.",
				Status: domain.ClaimStatusObserved, Confidence: 0.9,
				SupportingEvidenceIDs: []string{request.Input.Entries[0].Segments[0].EvidenceID},
				Attribution:           domain.ClaimAttributionUnknown,
			},
			{
				Type: domain.ClaimTypeLesson, Statement: "Passing output can support a reusable lesson.",
				Status: domain.ClaimStatusInferred, Confidence: 0.8,
				SupportingEvidenceIDs: []string{request.Input.Entries[1].Segments[0].EvidenceID},
				Attribution:           domain.ClaimAttributionUnknown,
			},
		}
	}
	return application.SemanticGenerationResult{
		Candidates: candidates,
		Model: domain.ModelExecutionMetadata{
			ResolvedProvider: "cerebras", ResolvedModel: "openai/gpt-oss-120b",
			RequestID: "semantic-cli-request",
		},
	}, nil
}

func runSemanticForCLI(
	t *testing.T,
	ctx context.Context,
	store *sqlitestore.Store,
	factAnalysisID string,
	route domain.ValidatedModelRoute,
	generator application.SemanticGenerator,
	analysisID string,
	now time.Time,
) application.SemanticWorkflowResult {
	t.Helper()
	result, err := (application.SemanticWorkflow{
		Source: newSessionsReader(), Facts: store, Store: store,
		Analyzer: application.SemanticAnalyzer{
			Generator: generator, Privacy: application.PrivacyPolicy{},
			NewID: func() (string, error) { return analysisID, nil },
			Now:   func() time.Time { return now },
		},
	}).Run(ctx, application.SemanticWorkflowRequest{FactAnalysisID: factAnalysisID, Route: route})
	if err != nil {
		t.Fatalf("run semantic workflow %s: %v", analysisID, err)
	}
	if result.Reused || result.Record.Analysis.Run.ID != analysisID {
		t.Fatalf("semantic workflow %s = %#v", analysisID, result)
	}
	return result
}

func showSemanticForCLI(
	t *testing.T,
	ctx context.Context,
	databasePath, analysisID string,
	resolve bool,
) (application.SemanticAnalysisRecord, []byte) {
	t.Helper()
	args := []string{"analyses", "show", analysisID, "--database", databasePath}
	if resolve {
		args = []string{"analyses", "show", analysisID, "--resolve", "--database", databasePath}
	}
	var stdout, stderr bytes.Buffer
	if err := run(ctx, args, &stdout, &stderr); err != nil {
		t.Fatalf("show semantic analysis: %v; stderr: %s", err, stderr.String())
	}
	if resolve {
		var resolved application.ResolvedSemanticAnalysis
		if err := json.Unmarshal(stdout.Bytes(), &resolved); err != nil {
			t.Fatalf("decode resolved semantic analysis: %v", err)
		}
		return resolved.Record, stdout.Bytes()
	}
	var record application.SemanticAnalysisRecord
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("decode semantic analysis: %v", err)
	}
	return record, stdout.Bytes()
}

func semanticCLIRoute(t *testing.T, variant string) domain.ValidatedModelRoute {
	t.Helper()
	configuration, err := json.Marshal(map[string]string{"profile": "semantic-v1", "variant": variant})
	if err != nil {
		t.Fatalf("encode semantic route: %v", err)
	}
	digest, err := platform.Fingerprint(json.RawMessage(configuration))
	if err != nil {
		t.Fatalf("fingerprint semantic route: %v", err)
	}
	return domain.ValidatedModelRoute{
		Requested: domain.RequestedModelRoute{
			Alias: "semantic-v1", Gateway: "vercel-ai-gateway", Model: "openai/gpt-oss-120b",
			Provider: "cerebras", RouteVersion: "route-v1",
			PrivacyPolicyVersion: application.PrivacyPolicyVersion,
		},
		SanitizedConfig: configuration,
		ConfigDigest:    digest,
	}
}
