package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func TestV1RuntimeLoadsExactJobKnowledge(t *testing.T) {
	fixture := newV1RuntimeFixture(t, true)
	ctx := context.Background()

	input, found, err := fixture.store.LoadV1JobKnowledge(ctx, fixture.job.ID)
	if err != nil || !found {
		t.Fatalf("load V1 job knowledge = %#v, %v, %v", input, found, err)
	}
	if input.Job.ID != fixture.job.ID ||
		input.TriggerEvent.ID != fixture.job.EventID ||
		!reflect.DeepEqual(input.Analysis.Claims, fixture.record.Analysis.Claims) ||
		len(input.Facts) != 1 ||
		input.Facts[0].ID != fixture.record.Analysis.Run.InputFactIDs[0] {
		t.Fatalf("V1 job knowledge = %#v", input)
	}

	if _, err := fixture.database.ExecContext(
		ctx,
		"DELETE FROM facts WHERE id = ?",
		input.Facts[0].ID,
	); err != nil {
		t.Fatalf("delete supporting fact: %v", err)
	}
	if _, found, err := fixture.store.LoadV1JobKnowledge(ctx, fixture.job.ID); !errors.Is(err, ErrAgentRuntimeDataInvalid) || found {
		t.Fatalf("load missing supporting fact = %v, %v", found, err)
	}
}

func TestV1RuntimeCompletesInspectsAndReusesGenericArtifacts(t *testing.T) {
	fixture := newV1RuntimeFixture(t, true)
	ctx := context.Background()
	job := claimV1RuntimeJob(t, fixture)
	input, found, err := fixture.store.LoadV1JobKnowledge(ctx, job.ID)
	if err != nil || !found {
		t.Fatalf("load claimed V1 input = %#v, %v, %v", input, found, err)
	}
	completion := successfulV1Completion(t, input, fixture.now.Add(2*time.Minute))

	completed, err := fixture.store.CompleteV1Job(ctx, completion)
	if err != nil || !completed {
		t.Fatalf("complete V1 job = %v, %v", completed, err)
	}
	completed, err = fixture.store.CompleteV1Job(ctx, completion)
	if err != nil || completed {
		t.Fatalf("reuse V1 completion = %v, %v", completed, err)
	}

	inspection, found, err := fixture.store.InspectV1Job(ctx, job.ID)
	if err != nil || !found || inspection.Run == nil {
		t.Fatalf("inspect completed V1 job = %#v, %v, %v", inspection, found, err)
	}
	if inspection.Job.Status != domain.JobSucceeded ||
		!reflect.DeepEqual(*inspection.Run, completion.Run) ||
		!reflect.DeepEqual(inspection.Artifacts, completion.Artifacts) {
		t.Fatalf("completed V1 inspection = %#v", inspection)
	}

	ideas, err := fixture.store.ListV1ContentIdeaArtifacts(ctx)
	if err != nil || !reflect.DeepEqual(ideas, completion.Artifacts) {
		t.Fatalf("list authoritative V1 ideas = %#v, %v", ideas, err)
	}
	insertFoundationIdeaProjection(t, ctx, fixture.database, completion)
	ideas, err = fixture.store.ListV1ContentIdeaArtifacts(ctx)
	if err != nil || !reflect.DeepEqual(ideas, completion.Artifacts) {
		t.Fatalf("list V1 ideas with disposable projection = %#v, %v", ideas, err)
	}

	changed := completion
	changed.Artifacts = append([]domain.Artifact{}, completion.Artifacts...)
	changed.Artifacts[0].CreatedAt = changed.Artifacts[0].CreatedAt.Add(time.Second)
	if completed, err := fixture.store.CompleteV1Job(ctx, changed); !errors.Is(err, ErrAgentRuntimeDataInvalid) || completed {
		t.Fatalf("reuse changed completion = %v, %v", completed, err)
	}
}

func TestV1RuntimePersistsSafeFailureWithoutArtifacts(t *testing.T) {
	fixture := newV1RuntimeFixture(t, true)
	ctx := context.Background()
	job := claimV1RuntimeJob(t, fixture)
	run := failedV1Run(job, fixture.now.Add(2*time.Minute))

	failed, err := fixture.store.FailV1Job(ctx, job, run)
	if err != nil || !failed {
		t.Fatalf("fail V1 job = %v, %v", failed, err)
	}
	beforeReuse, found, inspectErr := fixture.store.InspectV1Job(ctx, job.ID)
	if inspectErr != nil || !found || beforeReuse.Run == nil {
		t.Fatalf("inspect before failure reuse = %#v, %v, %v", beforeReuse, found, inspectErr)
	}
	if !reflect.DeepEqual(*beforeReuse.Run, run) {
		t.Fatalf("stored failure before reuse differs:\nstored: %#v\nwant:   %#v", *beforeReuse.Run, run)
	}
	failed, err = fixture.store.FailV1Job(ctx, job, run)
	if err != nil || failed {
		t.Fatalf("reuse V1 failure = %v, %v", failed, err)
	}
	inspection, found, err := fixture.store.InspectV1Job(ctx, job.ID)
	if err != nil || !found || inspection.Run == nil {
		t.Fatalf("inspect failed V1 job = %#v, %v, %v", inspection, found, err)
	}
	if inspection.Job.Status != domain.JobFailed ||
		inspection.Run.Result.Failure == nil ||
		inspection.Run.Result.Failure.Category != domain.AgentFailureCategoryInputInvalid ||
		len(inspection.Artifacts) != 0 {
		t.Fatalf("failed V1 inspection = %#v", inspection)
	}
	var jobError, runError string
	if err := fixture.database.QueryRowContext(
		ctx,
		"SELECT jobs.error, agent_runs.error FROM jobs JOIN agent_runs ON agent_runs.job_id = jobs.id WHERE jobs.id = ?",
		job.ID,
	).Scan(&jobError, &runError); err != nil {
		t.Fatalf("read safe failures: %v", err)
	}
	if jobError != domain.AgentFailureCategoryInputInvalid ||
		runError != domain.AgentFailureCategoryInputInvalid {
		t.Fatalf("safe failures = %q / %q", jobError, runError)
	}
}

func TestV1RuntimeCompletesZeroClaimJobLocally(t *testing.T) {
	fixture := newV1RuntimeFixture(t, false)
	ctx := context.Background()
	job := claimV1RuntimeJob(t, fixture)
	input, found, err := fixture.store.LoadV1JobKnowledge(ctx, job.ID)
	if err != nil || !found || len(input.Analysis.Claims) != 0 || len(input.Facts) != 0 {
		t.Fatalf("load zero-claim input = %#v, %v, %v", input, found, err)
	}
	run := application.V1AgentRunRecord{
		ID: platform.DerivedID("run_", job.ID), JobID: job.ID, Agent: job.Agent,
		Result: domain.AgentRunResultV1{
			SchemaVersion: domain.AgentRunResultSchemaVersion,
			Outcome:       domain.AgentRunOutcomeSucceeded,
			Disposition:   domain.AgentExecutionDispositionSkipped,
			ArtifactIDs:   []string{},
		},
		StartedAt: *job.StartedAt, FinishedAt: fixture.now.Add(2 * time.Minute),
	}
	completion := application.V1JobCompletion{
		Job: job, Run: run, Artifacts: []domain.Artifact{},
	}
	if completed, err := fixture.store.CompleteV1Job(ctx, completion); err != nil || !completed {
		t.Fatalf("complete zero-claim V1 job = %v, %v", completed, err)
	}
	inspection, found, err := fixture.store.InspectV1Job(ctx, job.ID)
	if err != nil || !found || inspection.Run == nil ||
		inspection.Run.Result.Disposition != domain.AgentExecutionDispositionSkipped ||
		len(inspection.Artifacts) != 0 {
		t.Fatalf("inspect zero-claim V1 job = %#v, %v, %v", inspection, found, err)
	}
}

func TestV1RuntimeCompletionRollsBackAtomically(t *testing.T) {
	fixture := newV1RuntimeFixture(t, true)
	ctx := context.Background()
	job := claimV1RuntimeJob(t, fixture)
	input, found, err := fixture.store.LoadV1JobKnowledge(ctx, job.ID)
	if err != nil || !found {
		t.Fatalf("load claimed V1 input = %#v, %v, %v", input, found, err)
	}
	completion := successfulV1Completion(t, input, fixture.now.Add(2*time.Minute))
	if _, err := fixture.database.ExecContext(ctx, `
		CREATE TRIGGER reject_v1_artifact
		BEFORE INSERT ON artifacts
		BEGIN
			SELECT RAISE(ABORT, 'reject artifact');
		END
	`); err != nil {
		t.Fatalf("create artifact rejection trigger: %v", err)
	}
	if completed, err := fixture.store.CompleteV1Job(ctx, completion); err == nil || completed {
		t.Fatalf("complete rejected V1 artifact = %v, %v", completed, err)
	}
	var status string
	if err := fixture.database.QueryRowContext(
		ctx, "SELECT status FROM jobs WHERE id = ?", job.ID,
	).Scan(&status); err != nil || status != domain.JobRunning {
		t.Fatalf("job status after rollback = %q, %v", status, err)
	}
	for _, table := range []string{"agent_runs", "artifacts", "content_ideas"} {
		var count int
		if err := fixture.database.QueryRowContext(
			ctx, "SELECT COUNT(*) FROM "+table,
		).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count after rollback = %d, %v", table, count, err)
		}
	}
}

func TestV1RuntimeInspectionIgnoresRowsWithoutV1Sidecar(t *testing.T) {
	fixture := newV1RuntimeFixture(t, false)
	ctx := context.Background()
	if _, err := fixture.database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_json, created_at
		) VALUES ('foundation-runtime-job', 'foundation-runtime-fingerprint', ?,
		          'content-scout', 'v0', 'pending', '{}', ?)
	`, fixture.job.EventID, formatTime(fixture.now.Add(-time.Hour))); err != nil {
		t.Fatalf("insert foundation runtime job: %v", err)
	}
	if _, found, err := fixture.store.InspectV1Job(ctx, "foundation-runtime-job"); err != nil || found {
		t.Fatalf("inspect foundation runtime row = %v, %v", found, err)
	}
	if _, found, err := fixture.store.LoadV1JobKnowledge(ctx, "foundation-runtime-job"); err != nil || found {
		t.Fatalf("load foundation runtime knowledge = %v, %v", found, err)
	}
	if _, err := fixture.database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_json, created_at
		) VALUES ('future-runtime-job', ?, ?, 'content-scout',
		          'content-scout-v2', 'pending', '{"schemaVersion":2}', ?);
		INSERT INTO agent_job_details (
			job_id, payload_schema_version, configuration_digest
		) VALUES ('future-runtime-job', 2, ?)
	`,
		strings.Repeat("9", 64),
		fixture.job.EventID,
		formatTime(fixture.now.Add(-30*time.Minute)),
		strings.Repeat("8", 64),
	); err != nil {
		t.Fatalf("insert future runtime job: %v", err)
	}
	if _, found, err := fixture.store.InspectV1Job(ctx, "future-runtime-job"); err != nil || found {
		t.Fatalf("inspect future runtime row = %v, %v", found, err)
	}
	if _, found, err := fixture.store.LoadV1JobKnowledge(ctx, "future-runtime-job"); err != nil || found {
		t.Fatalf("load future runtime knowledge = %v, %v", found, err)
	}
}

type v1RuntimeFixture struct {
	database *sql.DB
	store    *Store
	record   application.SemanticAnalysisRecord
	job      application.AgentJobRecordV1
	now      time.Time
}

func newV1RuntimeFixture(t *testing.T, withClaim bool) v1RuntimeFixture {
	t.Helper()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open V1 runtime database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := NewStore(database)
	now := time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC)
	suffix := strings.ToLower(t.Name())
	record := semanticStoreRecord(now, suffix, withClaim)
	commitRuntimeFactAnalysis(t, ctx, store, record, suffix, now)
	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin semantic analysis: %v", err)
	}
	if err := attempt.Commit(ctx, record); err != nil {
		t.Fatalf("commit semantic analysis: %v", err)
	}
	event := record.Events[len(record.Events)-1]
	job := v1RuntimeJob(t, event.ID, record, now)
	if created, err := store.CreateOrReuseV1Job(ctx, job); err != nil || !created {
		t.Fatalf("create V1 runtime job = %v, %v", created, err)
	}
	return v1RuntimeFixture{
		database: database, store: store, record: record, job: job, now: now,
	}
}

func commitRuntimeFactAnalysis(
	t *testing.T,
	ctx context.Context,
	store *Store,
	record application.SemanticAnalysisRecord,
	suffix string,
	now time.Time,
) {
	t.Helper()
	factID := record.Analysis.Run.InputFactIDs[0]
	revision := *record.Analysis.Run.Revision
	selection := *record.Analysis.Run.Selection
	evidence := []domain.EvidenceRef{semanticStoreEvidenceRef(revision)}
	fact := domain.Fact{
		ID: factID, Fingerprint: strings.Repeat("f", 64),
		AnalysisRunID: "fact-analysis-" + suffix,
		Kind:          "tool", SchemaVersion: 1,
		Value: domain.FactValue{
			Tool: &domain.ToolFactValue{Kind: "shell"},
		},
		Outcome: domain.FactOutcomeNotApplicable, ExtractorName: "session-facts",
		ExtractorVersion: "1", ParseRule: "tool-call", Evidence: evidence,
		CreatedAt: now,
	}
	analysis := domain.FactAnalysis{
		Run: domain.AnalysisRun{
			ID: fact.AnalysisRunID, ProcessingKey: "fact-processing-" + suffix,
			Stage: domain.AnalysisStageFacts, RequestedSourceIdentity: revision.CanonicalID,
			Revision: &revision, Selection: &selection, ExtractorName: "session-facts",
			ExtractorVersion: "1", SchemaVersion: 1, FactIDs: []string{fact.ID},
			Status: domain.AnalysisCompleted, StartedAt: now, FinishedAt: now,
		},
		Facts: []domain.Fact{fact},
	}
	if created, err := store.CommitFactAnalysis(ctx, analysis); err != nil || !created {
		t.Fatalf("commit runtime fact analysis = %v, %v", created, err)
	}
}

func v1RuntimeJob(
	t *testing.T,
	eventID string,
	record application.SemanticAnalysisRecord,
	now time.Time,
) application.AgentJobRecordV1 {
	t.Helper()
	job := v1JobForTest(t, eventID, now, []string{}, []string{})
	job.EventID = eventID
	job.Payload.Inputs = domain.KnowledgeInputRefsV1{
		AnalysisRunID: record.Analysis.Run.ID,
		ClaimIDs:      append([]string{}, record.Analysis.Run.ClaimIDs...),
	}
	fingerprint, err := domain.AgentJobFingerprint(job.EventID, job.Agent, job.Payload)
	if err != nil {
		t.Fatalf("fingerprint runtime job: %v", err)
	}
	job.Fingerprint = fingerprint
	job.ID = platform.DerivedID("job_", fingerprint)
	return job
}

func claimV1RuntimeJob(t *testing.T, fixture v1RuntimeFixture) application.AgentJobRecordV1 {
	t.Helper()
	ctx := context.Background()
	pending, found, err := fixture.store.InspectOldestPendingV1Job(ctx)
	if err != nil || !found || pending.ID != fixture.job.ID {
		t.Fatalf("inspect pending V1 job = %#v, %v, %v", pending, found, err)
	}
	startedAt := fixture.now.Add(time.Minute)
	claimed, err := fixture.store.ClaimPendingV1Job(ctx, pending.PendingV1JobIdentity, startedAt)
	if err != nil || !claimed {
		t.Fatalf("claim pending V1 job = %v, %v", claimed, err)
	}
	job, found, err := fixture.store.LoadV1Job(ctx, fixture.job.ID)
	if err != nil || !found || job.Status != domain.JobRunning {
		t.Fatalf("load running V1 job = %#v, %v, %v", job, found, err)
	}
	return job
}

func successfulV1Completion(
	t *testing.T,
	input application.V1JobKnowledgeInput,
	finishedAt time.Time,
) application.V1JobCompletion {
	t.Helper()
	job := input.Job
	claim := input.Analysis.Claims[0]
	idea := domain.ContentIdeaV1{
		Rank: 1, Concept: "Keep runtime boundaries small.",
		CoreLesson:      "Store evidence and interpretation separately.",
		AudienceBenefit: "Readers can trace an idea to admitted knowledge.",
		Hook:            "A useful agent output starts with exact lineage.",
		Resonance:       "It makes model output reviewable.",
		Confidence:      0.8,
		ShortPost: domain.ContentFormatAngleV1{
			Suitable: true, Angle: "Explain the boundary in one example.",
		},
		Thread: domain.ContentFormatAngleV1{
			Suitable: true, Angle: "Walk through evidence, claims, and artifacts.",
		},
		Article: domain.ContentFormatAngleV1{
			Suitable: true, Angle: "Explore the complete architecture.",
		},
		ClaimIDs: []string{claim.ID},
	}
	payload, err := json.Marshal(idea)
	if err != nil {
		t.Fatalf("encode content idea: %v", err)
	}
	factIDs := []string{input.Facts[0].ID}
	fingerprint, err := domain.ContentIdeaArtifactFingerprint(
		job.Fingerprint,
		idea,
		factIDs,
		claim.SupportingEvidence,
		claim.ContradictingEvidence,
		domain.ArtifactSafetyReviewRequired,
	)
	if err != nil {
		t.Fatalf("fingerprint content idea: %v", err)
	}
	runID := platform.DerivedID("run_", job.ID)
	artifact := domain.Artifact{
		ID: platform.DerivedID("artifact_", fingerprint), Fingerprint: fingerprint,
		Kind: domain.ArtifactKindContentIdea, SchemaVersion: domain.ContentIdeaSchemaVersion,
		PayloadJSON: payload, RunID: runID, TriggerEventID: job.EventID,
		JobFingerprint: job.Fingerprint, Inputs: job.Payload.Inputs,
		ClaimIDs: []string{claim.ID}, FactIDs: factIDs,
		SupportingEvidence:    append([]domain.EvidenceRef{}, claim.SupportingEvidence...),
		ContradictingEvidence: append([]domain.EvidenceRef{}, claim.ContradictingEvidence...),
		ProposalStatus:        domain.ArtifactProposalReviewRequired,
		SafetyStatus:          domain.ArtifactSafetyReviewRequired,
		CreatedAt:             finishedAt,
	}
	route := job.Payload.Configuration.Route
	latency := int64(1200)
	run := application.V1AgentRunRecord{
		ID: runID, JobID: job.ID, Agent: job.Agent,
		Result: domain.AgentRunResultV1{
			SchemaVersion: domain.AgentRunResultSchemaVersion,
			Outcome:       domain.AgentRunOutcomeSucceeded,
			Disposition:   domain.AgentExecutionDispositionInvoked,
			Execution: &domain.AgentExecutionIdentity{
				ExecutorKind:          application.ContentScoutExecutorKind,
				ExecutorVersion:       application.ContentScoutExecutorVersion,
				AgentDefinitionDigest: strings.Repeat("a", 64),
				ContractVersion:       domain.AgentExecutionContractVersion,
				RecoveryPolicyVersion: application.ContentScoutRecoveryPolicyVersion,
			},
			Receipt: &domain.AgentExecutionReceiptV1{
				ExecutorKind:    application.ContentScoutExecutorKind,
				ExecutorVersion: application.ContentScoutExecutorVersion,
				SessionID:       "eve-session", TurnID: "eve-turn", CompletedModelSteps: 1,
				RequestedRoute: &route, GatewayGenerationID: "generation-one",
				Usage:   &domain.AgentUsageV1{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
				CostUSD: "0.001", LatencyMilliseconds: &latency,
			},
			Privacy: domain.AgentPrivacyOutcomeV1{
				CompletedStages: []domain.AgentPolicyStageV1{{
					Name: "postflight", PolicyVersion: application.PrivacyPolicyVersion,
					Outcome:    domain.AgentPolicyOutcomePassed,
					Categories: []domain.AgentPolicyCategoryCountV1{},
				}},
			},
			ArtifactIDs: []string{artifact.ID},
		},
		StartedAt: *job.StartedAt, FinishedAt: finishedAt,
	}
	return application.V1JobCompletion{
		Job: job, Run: run, Artifacts: []domain.Artifact{artifact},
	}
}

func failedV1Run(
	job application.AgentJobRecordV1,
	finishedAt time.Time,
) application.V1AgentRunRecord {
	return application.V1AgentRunRecord{
		ID: platform.DerivedID("run_", job.ID), JobID: job.ID, Agent: job.Agent,
		Result: domain.AgentRunResultV1{
			SchemaVersion: domain.AgentRunResultSchemaVersion,
			Outcome:       domain.AgentRunOutcomeFailed,
			Disposition:   domain.AgentExecutionDispositionNone,
			Privacy:       domain.AgentPrivacyOutcomeV1{},
			Failure: &domain.AgentFailureV1{
				Stage:    domain.AgentFailureStagePreparation,
				Category: domain.AgentFailureCategoryInputInvalid,
			},
			ArtifactIDs: []string{},
		},
		StartedAt: *job.StartedAt, FinishedAt: finishedAt,
	}
}

func insertFoundationIdeaProjection(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	completion application.V1JobCompletion,
) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO content_ideas (
			id, fingerprint, run_id, rank, concept, core_lesson,
			audience_benefit, hook, resonance, confidence, formats_json,
			evidence_json, created_at
		) VALUES (
			'foundation-projection', 'foundation-projection-fingerprint', ?,
			1, 'old concept', 'old lesson', 'old benefit', 'old hook',
			'old resonance', 0.5, '{}', '[]', ?
		)
	`, completion.Run.ID, formatTime(completion.Run.FinishedAt)); err != nil {
		t.Fatalf("insert foundation idea projection: %v", err)
	}
}
