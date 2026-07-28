package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
)

func TestSubscriptionMatchAndV1JobInspectionThroughCLI(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	databasePath := filepath.Join(temp, "noema.db")
	exportPath := filepath.Join(temp, "export.jsonl")
	executable := filepath.Join(temp, "sessions")
	if err := os.WriteFile(
		executable,
		[]byte("#!/bin/sh\nexec /bin/cat \"$NOEMA_FAKE_EXPORT\"\n"),
		0o700,
	); err != nil {
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
	now := time.Date(2026, time.July, 28, 20, 0, 0, 0, time.UTC)
	semantic := runSemanticForCLI(
		t,
		ctx,
		store,
		fact.AnalysisID,
		semanticCLIRoute(t, "subscription"),
		semanticCLIGenerator{},
		"semantic-subscription",
		now,
	)
	completionEventID := semantic.Record.Events[len(semantic.Record.Events)-1].ID
	var originalEventCount int
	if err := database.QueryRowContext(
		ctx, "SELECT COUNT(*) FROM events",
	).Scan(&originalEventCount); err != nil {
		t.Fatalf("count initial events: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close semantic database: %v", err)
	}

	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("NOEMA_EVE_ROUTE_PASSWORD", "")
	agentConfig := filepath.Join("..", "..", "config", "content-scout-agent.example.json")
	disclosureConfig := filepath.Join("..", "..", "config", "content-scout-disclosure.example.json")
	first := matchSubscriptionForCLI(
		t, ctx, databasePath, semantic.Record.Analysis.Run.ID, agentConfig, disclosureConfig,
	)
	if first.EventID != completionEventID || len(first.JobIDs) != 1 ||
		len(first.CreatedJobIDs) != 1 || len(first.ReusedJobIDs) != 0 {
		t.Fatalf("first subscription match = %#v", first)
	}
	repeated := matchSubscriptionForCLI(
		t, ctx, databasePath, semantic.Record.Analysis.Run.ID, agentConfig, disclosureConfig,
	)
	if len(repeated.JobIDs) != 1 || len(repeated.CreatedJobIDs) != 0 ||
		len(repeated.ReusedJobIDs) != 1 ||
		repeated.JobIDs[0] != first.JobIDs[0] {
		t.Fatalf("repeated subscription match = %#v", repeated)
	}

	changedDisclosure := filepath.Join(temp, "changed-disclosure.json")
	if err := os.WriteFile(
		changedDisclosure,
		[]byte(`{"schemaVersion":1,"approvedPublicTerms":["Go"]}`),
		0o600,
	); err != nil {
		t.Fatalf("write changed disclosure: %v", err)
	}
	changed := matchSubscriptionForCLI(
		t, ctx, databasePath, semantic.Record.Analysis.Run.ID, agentConfig, changedDisclosure,
	)
	if len(changed.CreatedJobIDs) != 1 ||
		changed.JobIDs[0] == first.JobIDs[0] ||
		changed.ConfigurationDigest == first.ConfigurationDigest {
		t.Fatalf("changed subscription match = %#v", changed)
	}

	var listOutput, listError bytes.Buffer
	if err := run(
		ctx,
		[]string{"jobs", "list", "--database", databasePath},
		&listOutput,
		&listError,
	); err != nil {
		t.Fatalf("list V1 jobs: %v; stderr: %s", err, listError.String())
	}
	var jobs []application.AgentJobRecordV1
	if err := json.Unmarshal(listOutput.Bytes(), &jobs); err != nil {
		t.Fatalf("decode V1 jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("V1 jobs = %#v, want two jobs", jobs)
	}
	for _, job := range jobs {
		if job.EventID != completionEventID {
			t.Fatalf("unexpected V1 job = %#v", job)
		}
	}

	var showOutput, showError bytes.Buffer
	if err := run(
		ctx,
		[]string{"jobs", "show", first.JobIDs[0], "--database", databasePath},
		&showOutput,
		&showError,
	); err != nil {
		t.Fatalf("show V1 job: %v; stderr: %s", err, showError.String())
	}
	var shown application.AgentJobRecordV1
	if err := json.Unmarshal(showOutput.Bytes(), &shown); err != nil {
		t.Fatalf("decode shown V1 job: %v", err)
	}
	if shown.ID != first.JobIDs[0] ||
		shown.Payload.Inputs.AnalysisRunID != semantic.Record.Analysis.Run.ID {
		t.Fatalf("shown V1 job = %#v", shown)
	}
	database, err = sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen matched database: %v", err)
	}
	defer database.Close()
	var eventCount, jobCount int
	if err := database.QueryRowContext(
		ctx, "SELECT COUNT(*) FROM events",
	).Scan(&eventCount); err != nil {
		t.Fatalf("count matched events: %v", err)
	}
	if err := database.QueryRowContext(
		ctx, "SELECT COUNT(*) FROM jobs",
	).Scan(&jobCount); err != nil {
		t.Fatalf("count V1 jobs: %v", err)
	}
	if eventCount != originalEventCount || jobCount != 2 {
		t.Fatalf(
			"durable counts = events %d/%d, jobs %d",
			eventCount, originalEventCount, jobCount,
		)
	}
}

func matchSubscriptionForCLI(
	t *testing.T,
	ctx context.Context,
	databasePath, analysisID, agentConfig, disclosureConfig string,
) subscriptionMatchOutput {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := run(
		ctx,
		[]string{
			"subscriptions", "match", analysisID,
			"--agent-config", agentConfig,
			"--disclosure-config", disclosureConfig,
			"--database", databasePath,
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("match subscription: %v; stderr: %s", err, stderr.String())
	}
	var output subscriptionMatchOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode subscription match: %v", err)
	}
	return output
}
