package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func TestSemanticStoreCommitsLoadsAndReusesOrderedAnalysis(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), "one", true)

	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin semantic attempt: %v", err)
	}
	defer attempt.Rollback(ctx)
	if existing, found, err := attempt.FindCompleted(ctx, record.Analysis.Run.ProcessingKey); err != nil || found {
		t.Fatalf("find before commit = %#v, %v, %v", existing, found, err)
	}
	if err := attempt.Commit(ctx, record); err != nil {
		t.Fatalf("commit semantic analysis: %v", err)
	}

	loaded, err := store.LoadSemanticAnalysis(ctx, record.Analysis.Run.ID)
	if err != nil {
		t.Fatalf("load semantic analysis: %v", err)
	}
	if !reflect.DeepEqual(loaded.Analysis.Run.ClaimIDs, record.Analysis.Run.ClaimIDs) ||
		len(loaded.Analysis.Claims) != 1 || loaded.Analysis.Claims[0].ID != record.Analysis.Claims[0].ID ||
		len(loaded.Events) != 2 || loaded.Events[0].Type != "claim.admitted" ||
		loaded.Events[1].Type != "analysis.completed" || loaded.Details.InputDigest == nil ||
		*loaded.Details.InputDigest != *record.Details.InputDigest || loaded.Details.Selection == nil ||
		!reflect.DeepEqual(loaded.Details.Selection, record.Details.Selection) ||
		loaded.Details.Selection.ExcludedFactCount != 3 ||
		loaded.Details.Selection.TruncatedFactTexts != 2 {
		t.Fatalf("loaded semantic record = %#v", loaded)
	}

	second, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin reuse attempt: %v", err)
	}
	reused, found, err := second.FindCompleted(ctx, record.Analysis.Run.ProcessingKey)
	if err != nil || !found || reused.Analysis.Run.ID != record.Analysis.Run.ID {
		t.Fatalf("find completed = %#v, %v, %v", reused, found, err)
	}
	if err := second.Rollback(ctx); err != nil {
		t.Fatalf("rollback reuse attempt: %v", err)
	}
	for table, want := range map[string]int{
		"analysis_runs": 1,
		"claims":        1,
		"events":        2,
		"jobs":          0,
		"agent_runs":    0,
		"content_ideas": 0,
	} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count = %d, %v; want %d", table, count, err, want)
		}
	}
}

func TestSemanticStoreRejectsProcessingKeyOutsideDurableLineage(t *testing.T) {
	t.Run("before commit", func(t *testing.T) {
		ctx := context.Background()
		database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
		defer database.Close()
		record := semanticStoreRecord(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), "bad-key", false)
		record.Analysis.Run.ProcessingKey = "not-derived-from-lineage"

		attempt, err := NewStore(database).BeginSemanticAttempt(ctx)
		if err != nil {
			t.Fatalf("begin semantic attempt: %v", err)
		}
		defer attempt.Rollback(ctx)
		if err := attempt.Commit(ctx, record); err == nil || !strings.Contains(err.Error(), "processing identity") {
			t.Fatalf("commit error = %v, want processing identity failure", err)
		}
	})

	t.Run("after load", func(t *testing.T) {
		ctx := context.Background()
		database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
		defer database.Close()
		store := NewStore(database)
		record := semanticStoreRecord(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), "changed-selection", false)
		attempt, err := store.BeginSemanticAttempt(ctx)
		if err != nil {
			t.Fatalf("begin semantic attempt: %v", err)
		}
		if err := attempt.Commit(ctx, record); err != nil {
			t.Fatalf("commit semantic analysis: %v", err)
		}

		selection := *record.Details.Selection
		selection.ExcludedFactCount++
		encoded, err := encodeJSON(selection)
		if err != nil {
			t.Fatalf("encode changed selection: %v", err)
		}
		if _, err := database.ExecContext(ctx, `
			UPDATE semantic_analysis_details
			   SET semantic_selection_json = ?
			 WHERE analysis_run_id = ?
		`, encoded, record.Analysis.Run.ID); err != nil {
			t.Fatalf("tamper semantic selection: %v", err)
		}
		if _, err := store.LoadSemanticAnalysis(ctx, record.Analysis.Run.ID); err == nil ||
			!strings.Contains(err.Error(), "processing identity") {
			t.Fatalf("load error = %v, want processing identity failure", err)
		}
	})
}

func TestSemanticStoreRejectsClaimContentOutsideStoredFingerprint(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), "changed-claim", true)
	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin semantic attempt: %v", err)
	}
	if err := attempt.Commit(ctx, record); err != nil {
		t.Fatalf("commit semantic analysis: %v", err)
	}

	if _, err := database.ExecContext(ctx, `
		UPDATE claims SET statement = ? WHERE analysis_run_id = ?
	`, "A changed statement no longer matches its identity.", record.Analysis.Run.ID); err != nil {
		t.Fatalf("tamper stored claim: %v", err)
	}
	if _, err := store.LoadSemanticAnalysis(ctx, record.Analysis.Run.ID); err == nil ||
		!strings.Contains(err.Error(), "claim fingerprint") {
		t.Fatalf("load error = %v, want claim fingerprint failure", err)
	}
}

func TestSemanticStorePreservesCompletedEmptyClaims(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), "empty", false)
	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin semantic attempt: %v", err)
	}
	defer attempt.Rollback(ctx)
	if err := attempt.Commit(ctx, record); err != nil {
		t.Fatalf("commit empty semantic analysis: %v", err)
	}
	loaded, err := store.LoadSemanticAnalysis(ctx, record.Analysis.Run.ID)
	if err != nil {
		t.Fatalf("load empty semantic analysis: %v", err)
	}
	if loaded.Details.ClaimIDs == nil || *loaded.Details.ClaimIDs == nil ||
		len(*loaded.Details.ClaimIDs) != 0 || loaded.Analysis.Run.ClaimIDs == nil ||
		len(loaded.Analysis.Claims) != 0 || len(loaded.Events) != 1 {
		t.Fatalf("empty semantic analysis lost known-empty state: %#v", loaded)
	}
}

func TestSemanticStoreRecordsFailureWithoutInventingUnavailableDetails(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreFailure(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), "early")
	if err := store.RecordSemanticFailure(ctx, record); err != nil {
		t.Fatalf("record semantic failure: %v", err)
	}
	loaded, err := store.LoadSemanticAnalysis(ctx, record.Analysis.Run.ID)
	if err != nil {
		t.Fatalf("load semantic failure: %v", err)
	}
	if loaded.Analysis.Run.Error != "semantic-input-invalid" ||
		loaded.Details.InputFactIDs != nil || loaded.Details.ClaimIDs != nil ||
		loaded.Details.InputDigest != nil || loaded.Details.Selection != nil ||
		loaded.Details.Privacy != nil || loaded.Details.Model != nil ||
		loaded.Details.AttemptedProcessingKey != nil {
		t.Fatalf("failed semantic details = %#v", loaded.Details)
	}
	if len(loaded.Analysis.Claims) != 0 || len(loaded.Events) != 0 {
		t.Fatalf("failed analysis has output: %#v", loaded)
	}
}

func TestSemanticAttemptRecordsLateFailureAndAllowsRetry(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	completed := semanticStoreRecord(now, "retry", true)
	failure := semanticStoreFailure(now, "failed-retry")
	failure.Analysis.Run.Revision = completed.Analysis.Run.Revision
	failure.Analysis.Run.Selection = completed.Analysis.Run.Selection
	failure.Analysis.Run.Omissions = completed.Analysis.Run.Omissions
	failure.Details.InputFactIDs = completed.Details.InputFactIDs
	failure.Details.InputDigest = completed.Details.InputDigest
	failure.Details.Selection = completed.Details.Selection
	failure.Details.Privacy = completed.Details.Privacy
	failure.Details.Model = completed.Details.Model
	failure.Analysis.Run.InputFactIDs = append([]string{}, (*failure.Details.InputFactIDs)...)
	model := *failure.Details.Model
	failure.Analysis.Run.Model = &model
	attemptedKey := completed.Analysis.Run.ProcessingKey
	failure.Details.AttemptedProcessingKey = &attemptedKey
	tamperedDetails := failure.Details
	tamperedKey := "not-derived-from-failed-lineage"
	tamperedDetails.AttemptedProcessingKey = &tamperedKey
	tampered, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin tampered semantic attempt: %v", err)
	}
	if err := tampered.RecordFailure(ctx, failure.Analysis.Run, tamperedDetails); err == nil ||
		!strings.Contains(err.Error(), "processing identity") {
		t.Fatalf("tampered failure error = %v, want processing identity failure", err)
	}
	if err := tampered.Rollback(ctx); err != nil {
		t.Fatalf("rollback tampered semantic attempt: %v", err)
	}

	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin failed semantic attempt: %v", err)
	}
	if err := attempt.RecordFailure(ctx, failure.Analysis.Run, failure.Details); err != nil {
		t.Fatalf("record late failure: %v", err)
	}
	loaded, err := store.LoadSemanticAnalysis(ctx, failure.Analysis.Run.ID)
	if err != nil {
		t.Fatalf("load late failure: %v", err)
	}
	if loaded.Details.Model == nil || loaded.Details.InputFactIDs == nil ||
		loaded.Details.AttemptedProcessingKey == nil ||
		*loaded.Details.AttemptedProcessingKey != attemptedKey || loaded.Details.ClaimIDs != nil {
		t.Fatalf("late failure details = %#v", loaded.Details)
	}

	retry, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin retry: %v", err)
	}
	defer retry.Rollback(ctx)
	if existing, found, err := retry.FindCompleted(ctx, attemptedKey); err != nil || found {
		t.Fatalf("failed processing key reserved completion = %#v, %v, %v", existing, found, err)
	}
	if err := retry.Commit(ctx, completed); err != nil {
		t.Fatalf("commit successful retry: %v", err)
	}
	var runCount int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM analysis_runs").Scan(&runCount); err != nil || runCount != 2 {
		t.Fatalf("analysis run count after retry = %d, %v", runCount, err)
	}
}

func TestSemanticStoreRollsBackPartialCommit(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), "rollback", true)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES ('preexisting-event', ?, 'analysis.completed', 'other-analysis', '{}', '[]', ?);
		INSERT INTO event_subject_types (event_id, subject_type)
		VALUES ('preexisting-event', 'analysis')
	`, record.Events[1].Fingerprint, formatTime(record.Events[1].CreatedAt)); err != nil {
		t.Fatalf("seed conflicting event: %v", err)
	}
	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin semantic attempt: %v", err)
	}
	if err := attempt.Commit(ctx, record); err == nil {
		t.Fatal("partial semantic commit unexpectedly succeeded")
	}
	if err := attempt.Rollback(ctx); err != nil {
		t.Fatalf("rollback failed attempt: %v", err)
	}
	for table, want := range map[string]int{
		"analysis_runs": 0, "semantic_analysis_details": 0, "claims": 0,
		"events": 1, "event_subject_types": 1,
	} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count after rollback = %d, %v; want %d", table, count, err, want)
		}
	}
}

func TestSemanticStoreLoadersRejectWrongAnalysisStage(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	fact := semanticStoreFactAnalysis(now)
	if inserted, err := store.CommitFactAnalysis(ctx, fact); err != nil || !inserted {
		t.Fatalf("commit fact analysis = %v, %v", inserted, err)
	}
	if _, err := store.LoadSemanticAnalysis(ctx, fact.Run.ID); err == nil || !strings.Contains(err.Error(), "want claims") {
		t.Fatalf("semantic loader fact-stage error = %v", err)
	}

	record := semanticStoreRecord(now, "stage", false)
	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin semantic attempt: %v", err)
	}
	defer attempt.Rollback(ctx)
	if err := attempt.Commit(ctx, record); err != nil {
		t.Fatalf("commit semantic analysis: %v", err)
	}
	if _, err := store.LoadFactAnalysis(ctx, record.Analysis.Run.ID); err == nil || !strings.Contains(err.Error(), "want facts") {
		t.Fatalf("fact loader claim-stage error = %v", err)
	}
}

func TestSemanticAttemptUsesImmediateTransactionAcrossDatabaseHandles(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noema.db")
	firstDatabase, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open first database: %v", err)
	}
	defer firstDatabase.Close()
	secondDatabase, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open second database: %v", err)
	}
	defer secondDatabase.Close()
	first, err := NewStore(firstDatabase).BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin first attempt: %v", err)
	}
	defer first.Rollback(ctx)

	started := make(chan struct{})
	result := make(chan struct {
		attempt application.SemanticAnalysisAttempt
		err     error
	}, 1)
	go func() {
		close(started)
		attempt, beginErr := NewStore(secondDatabase).BeginSemanticAttempt(ctx)
		result <- struct {
			attempt application.SemanticAnalysisAttempt
			err     error
		}{attempt: attempt, err: beginErr}
	}()
	<-started
	select {
	case got := <-result:
		if got.attempt != nil {
			got.attempt.Rollback(ctx)
		}
		t.Fatalf("second immediate attempt did not wait: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := first.Rollback(ctx); err != nil {
		t.Fatalf("release first attempt: %v", err)
	}
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("begin second attempt after release: %v", got.err)
		}
		if err := got.attempt.Rollback(ctx); err != nil {
			t.Fatalf("rollback second attempt: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second immediate attempt did not acquire after release")
	}
}

func semanticStoreRecord(now time.Time, suffix string, withClaim bool) application.SemanticAnalysisRecord {
	revision := semanticStoreRevision()
	runSelection := semanticStoreSelection()
	selection := semanticStoreSemanticSelection()
	route := semanticStoreRoute()
	privacy := application.PrivacyReport{
		PolicyVersion: application.PrivacyPolicyVersion,
		Redactions:    []application.PrivacyCategoryCount{}, BlockedCategories: []string{},
	}
	schema := domain.StructuredOutputSchemaIdentity{
		Name: "semantic-claim-candidates", Version: 1,
		Disposition: domain.StructuredOutputDispositionStrict, Digest: strings.Repeat("s", 64),
	}
	model := domain.ModelExecutionMetadata{
		RequestedRoute: route.Requested, ResolvedProvider: "cerebras",
		ResolvedModel: route.Requested.Model, PromptVersion: "semantic-claims-v1",
		RequestID: "request-" + suffix,
	}
	inputFactIDs := []string{"fact-" + suffix}
	inputDigest := strings.Repeat("i", 64)
	processingKey, err := application.SemanticProcessingKey(
		revision.Identity(), selection, inputFactIDs, inputDigest,
		schema, route, privacy.PolicyVersion,
	)
	if err != nil {
		panic(err)
	}
	claimIDs := []string{}
	claims := []domain.Claim{}
	events := []domain.Event{}
	analysisID := "analysis-" + suffix
	if withClaim {
		claim := domain.Claim{
			AnalysisRunID: analysisID, Type: domain.ClaimTypeLesson,
			Statement: "A focused boundary keeps processing inspectable.",
			Status:    domain.ClaimStatusInferred, Confidence: 0.8,
			SupportingEvidence:    []domain.EvidenceRef{semanticStoreEvidenceRef(revision)},
			ContradictingEvidence: []domain.EvidenceRef{}, SupportingFactIDs: inputFactIDs,
			Attribution: domain.ClaimAttributionUnknown, ExtractorName: "semantic-claims",
			ExtractorVersion: "1", SchemaVersion: 1, PromptVersion: "semantic-claims-v1",
			RequestedRoute: route.Requested, ResolvedProvider: "cerebras",
			ResolvedModel: route.Requested.Model, CreatedAt: now,
		}
		claimFingerprint, err := application.ClaimFingerprint(processingKey, claim)
		if err != nil {
			panic(err)
		}
		claimID := platform.DerivedID("claim_", claimFingerprint)
		claim.ID = claimID
		claim.Fingerprint = claimFingerprint
		claimIDs = append(claimIDs, claimID)
		claims = append(claims, claim)
		events = append(events, semanticStoreEvent(
			"claim.admitted", "claim", claimID,
			map[string]any{"schemaVersion": 1, "claimId": claimID, "analysisId": analysisID},
			claim.SupportingEvidence, now,
		))
	}
	events = append(events, semanticStoreEvent(
		"analysis.completed", "analysis", analysisID,
		map[string]any{"schemaVersion": 1, "analysisId": analysisID, "claimIds": claimIDs},
		[]domain.EvidenceRef{}, now,
	))
	run := domain.AnalysisRun{
		ID: analysisID, ProcessingKey: processingKey,
		Stage: domain.AnalysisStageClaims, RequestedSourceIdentity: revision.CanonicalID,
		Revision: &revision, Selection: &runSelection, ExtractorName: "semantic-claims",
		ExtractorVersion: "1", SchemaVersion: 1, FactIDs: []string{},
		InputFactIDs: append([]string{}, inputFactIDs...), ClaimIDs: append([]string{}, claimIDs...),
		Model: &model, Omissions: domain.AnalysisOmissions{}, Status: domain.AnalysisCompleted,
		StartedAt: now, FinishedAt: now,
	}
	return application.SemanticAnalysisRecord{
		Analysis: domain.SemanticAnalysis{Run: run, Claims: claims},
		Details: application.SemanticAnalysisDetails{
			Schema: schema,
			Route:  route, InputFactIDs: &inputFactIDs, ClaimIDs: &claimIDs,
			InputDigest: &inputDigest, Selection: &selection, Privacy: &privacy, Model: &model,
		},
		Events: events,
	}
}

func semanticStoreFailure(now time.Time, suffix string) application.SemanticAnalysisRecord {
	route := semanticStoreRoute()
	revision := semanticStoreRevision()
	return application.SemanticAnalysisRecord{
		Analysis: domain.SemanticAnalysis{Run: domain.AnalysisRun{
			ID: "analysis-" + suffix, Stage: domain.AnalysisStageClaims,
			RequestedSourceIdentity: "codex@local:session-one", ExtractorName: "semantic-claims",
			Revision:         &revision,
			ExtractorVersion: "1", SchemaVersion: 1, FactIDs: []string{},
			Status: domain.AnalysisFailed, Error: "semantic-input-invalid",
			StartedAt: now, FinishedAt: now,
		}, Claims: []domain.Claim{}},
		Details: application.SemanticAnalysisDetails{
			Schema: domain.StructuredOutputSchemaIdentity{
				Name: "semantic-claim-candidates", Version: 1,
				Disposition: domain.StructuredOutputDispositionStrict, Digest: strings.Repeat("s", 64),
			},
			Route: route,
		},
		Events: []domain.Event{},
	}
}

func semanticStoreRoute() domain.ValidatedModelRoute {
	configuration := json.RawMessage(`{"route":"semantic-v1","maxRetries":0}`)
	digest, err := platform.Fingerprint(configuration)
	if err != nil {
		panic(err)
	}
	return domain.ValidatedModelRoute{
		Requested: domain.RequestedModelRoute{
			Alias: "semantic-v1", Gateway: "vercel-ai-gateway",
			Model: "openai/gpt-oss-120b", Provider: "cerebras",
			RouteVersion: "route-v1", PrivacyPolicyVersion: application.PrivacyPolicyVersion,
		},
		SanitizedConfig: configuration,
		ConfigDigest:    digest,
	}
}

func semanticStoreEvent(
	eventType, subjectType, subjectID string,
	payload map[string]any,
	evidence []domain.EvidenceRef,
	createdAt time.Time,
) domain.Event {
	fingerprint, err := platform.Fingerprint(struct {
		Type        string
		SubjectType string
		SubjectID   string
		Payload     map[string]any
	}{eventType, subjectType, subjectID, payload})
	if err != nil {
		panic(err)
	}
	return domain.Event{
		ID: platform.DerivedID("evt_", fingerprint), Fingerprint: fingerprint,
		Type: eventType, SubjectType: subjectType, SubjectID: subjectID,
		Payload: payload, Evidence: evidence, CreatedAt: createdAt,
	}
}

func semanticStoreRevision() domain.EvidenceRevision {
	return domain.EvidenceRevision{
		SourceKind: domain.EvidenceSourceSessions, CanonicalID: "codex@local:session-one",
		DocumentDigest: domain.Digest{
			Scheme: "sha256-sessions-document-jcs-v1", Digest: strings.Repeat("d", 64),
		},
	}
}

func semanticStoreSelection() domain.EvidenceSelection {
	first, last := 0, 0
	return domain.EvidenceSelection{
		Mode:      "range",
		Relations: domain.CountSelection{},
		Entries: domain.EntrySelection{
			Selected: 1, Total: 2, Truncated: true,
			FirstOrdinal: &first, LastOrdinal: &last,
		},
		Segments: domain.CountSelection{Selected: 1, Total: 2, Truncated: true},
		SegmentText: domain.ByteSelection{
			EmittedUTF8Bytes: 80, OriginalUTF8Bytes: 100, Truncated: true,
		},
		CanonicalOmittedSegments: 1,
		TruncatedTextSegments:    1,
		Coverage:                 "partial",
	}
}

func semanticStoreSemanticSelection() application.SemanticSelection {
	first, last := 0, 0
	return application.SemanticSelection{
		Mode: "range", SelectedEntries: 1, TotalEntries: 2,
		FirstOrdinal: &first, LastOrdinal: &last,
		OriginalTextUTF8Bytes: 100, EmittedTextUTF8Bytes: 80,
		TruncatedTextSegments: 1, TruncatedFactTexts: 2,
		CanonicalOmittedSegments: 1, ExcludedFactCount: 3,
		Coverage: "partial",
	}
}

func semanticStoreEvidenceRef(revision domain.EvidenceRevision) domain.EvidenceRef {
	return domain.EvidenceRef{
		ID: "evidence-one", SourceKind: revision.SourceKind,
		SourceIdentity: revision.CanonicalID, DocumentDigestScheme: revision.DocumentDigest.Scheme,
		DocumentDigest: revision.DocumentDigest.Digest, EntryOrdinal: 0,
	}
}

func semanticStoreFactAnalysis(now time.Time) domain.FactAnalysis {
	revision := semanticStoreRevision()
	selection := semanticStoreSelection()
	return domain.FactAnalysis{Run: domain.AnalysisRun{
		ID: "fact-analysis", ProcessingKey: "fact-processing-key",
		Stage: domain.AnalysisStageFacts, RequestedSourceIdentity: revision.CanonicalID,
		Revision: &revision, Selection: &selection, ExtractorName: "session-facts",
		ExtractorVersion: "1", SchemaVersion: 1, FactIDs: []string{},
		Status: domain.AnalysisCompleted, StartedAt: now, FinishedAt: now,
	}, Facts: []domain.Fact{}}
}
