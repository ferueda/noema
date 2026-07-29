package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func TestV1JobStoreCreatesReusesAndInspectsJobs(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	eventID := insertV1JobTestEvent(t, ctx, database, now)

	first := v1JobForTest(t, eventID, now, []string{"claim-one"}, []string{})
	created, err := store.CreateOrReuseV1Job(ctx, first)
	if err != nil || !created {
		t.Fatalf("create V1 job = %v, %v", created, err)
	}
	repeated := first
	repeated.CreatedAt = now.Add(time.Hour)
	created, err = store.CreateOrReuseV1Job(ctx, repeated)
	if err != nil || created {
		t.Fatalf("reuse V1 job = %v, %v", created, err)
	}

	jobs, err := store.ListV1Jobs(ctx)
	if err != nil {
		t.Fatalf("list V1 jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != first.ID ||
		!jobs[0].CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("V1 jobs = %#v", jobs)
	}
	loaded, found, err := store.LoadV1Job(ctx, first.ID)
	if err != nil || !found || loaded.ID != first.ID {
		t.Fatalf("load V1 job = %#v, %v, %v", loaded, found, err)
	}
	changed := v1JobForTest(t, eventID, now.Add(2*time.Hour), []string{"claim-one"}, []string{"Go"})
	created, err = store.CreateOrReuseV1Job(ctx, changed)
	if err != nil || !created {
		t.Fatalf("create changed configuration job = %v, %v", created, err)
	}
	jobs, err = store.ListV1Jobs(ctx)
	if err != nil || len(jobs) != 2 || jobs[0].ID != first.ID || jobs[1].ID != changed.ID {
		t.Fatalf("changed V1 jobs = %#v, %v", jobs, err)
	}
}

func TestV1JobStoreRejectsCorruptDeclaredPayload(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	eventID := insertV1JobTestEvent(t, ctx, database, now)
	job := v1JobForTest(t, eventID, now, []string{"claim-one"}, []string{})
	if _, err := NewStore(database).CreateOrReuseV1Job(ctx, job); err != nil {
		t.Fatalf("create V1 job: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		"UPDATE jobs SET payload_json = ? WHERE id = ?",
		`{"schemaVersion":1,"unknown":true}`,
		job.ID,
	); err != nil {
		t.Fatalf("corrupt V1 job: %v", err)
	}
	if _, err := NewStore(database).ListV1Jobs(ctx); !errors.Is(err, ErrAgentRuntimeDataInvalid) {
		t.Fatalf("list corrupt V1 job error = %v", err)
	}
}

func insertV1JobTestEvent(
	t *testing.T,
	ctx context.Context,
	database interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
	now time.Time,
) string {
	t.Helper()
	const eventID = "event-v1-job"
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_type, subject_id, payload_json,
			evidence_json, created_at
		) VALUES (?, ?, 'analysis.completed', 'analysis', 'analysis-v1-job',
		          '{}', '[]', ?)
	`, eventID, strings.Repeat("e", 64), formatTime(now)); err != nil {
		t.Fatalf("insert V1 job event: %v", err)
	}
	return eventID
}

func v1JobForTest(
	t *testing.T,
	eventID string,
	now time.Time,
	claimIDs []string,
	approvedTerms []string,
) application.AgentJobRecordV1 {
	t.Helper()
	handler, err := json.Marshal(struct {
		AgentFileDigest               string   `json:"agentFileDigest"`
		DisclosureConfigurationDigest string   `json:"disclosureConfigurationDigest"`
		ApprovedPublicTerms           []string `json:"approvedPublicTerms"`
	}{
		AgentFileDigest:               strings.Repeat("a", 64),
		DisclosureConfigurationDigest: strings.Repeat("b", 64),
		ApprovedPublicTerms:           approvedTerms,
	})
	if err != nil {
		t.Fatalf("encode handler configuration: %v", err)
	}
	configuration := domain.AgentConfigurationIdentity{
		PromptVersion: application.ContentScoutInstructionsVersion,
		OutputSchema: domain.StructuredOutputSchemaIdentity{
			Name:        application.ContentScoutCandidatesSchemaName,
			Version:     domain.ContentIdeaSchemaVersion,
			Disposition: domain.StructuredOutputDispositionStrict,
			Digest:      application.ContentScoutCandidatesSchemaDigest,
		},
		Route: domain.AgentRouteIdentity{
			Alias:        application.ContentScoutRouteAlias,
			Gateway:      application.ContentScoutGateway,
			Model:        application.ContentScoutModel,
			Provider:     application.ContentScoutProvider,
			RouteVersion: application.ContentScoutRouteVersion,
			ServiceTier:  application.ContentScoutServiceTier,
		},
		PrivacyPolicyVersion:     application.PrivacyPolicyVersion,
		DisclosurePolicyVersion:  application.ContentScoutDisclosurePolicyVersion,
		SafetyPolicyVersion:      application.ContentScoutSafetyPolicyVersion,
		RetrievalPolicyVersion:   application.ContentScoutRetrievalPolicyVersion,
		HandlerConfigurationJSON: handler,
	}
	configuration.Digest, err = domain.AgentConfigurationDigest(configuration)
	if err != nil {
		t.Fatalf("digest configuration: %v", err)
	}
	agent := domain.AgentIdentity{
		Name:    application.ContentScoutAgentName,
		Version: application.ContentScoutAgentVersion,
	}
	payload := domain.AgentJobPayloadV1{
		SchemaVersion: domain.AgentJobPayloadSchemaVersion,
		Inputs: domain.KnowledgeInputRefsV1{
			AnalysisRunID: "analysis-v1-job",
			ClaimIDs:      append([]string{}, claimIDs...),
		},
		Configuration: configuration,
	}
	fingerprint, err := domain.AgentJobFingerprint(eventID, agent, payload)
	if err != nil {
		t.Fatalf("fingerprint V1 job: %v", err)
	}
	return application.AgentJobRecordV1{
		ID:          platform.DerivedID("job_", fingerprint),
		Fingerprint: fingerprint,
		EventID:     eventID,
		Agent:       agent,
		Status:      domain.JobPending,
		Payload:     payload,
		CreatedAt:   now,
	}
}
