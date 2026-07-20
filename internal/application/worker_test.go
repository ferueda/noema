package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

func TestWorkerRejectsInvalidIdeasAndRecordsTerminalFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		change  func([]ContentIdeaDraft) []ContentIdeaDraft
		wantErr string
	}{
		{
			name: "missing hook",
			change: func(drafts []ContentIdeaDraft) []ContentIdeaDraft {
				drafts[0].Hook = ""
				return drafts
			},
			wantErr: "hook",
		},
		{
			name: "missing resonance",
			change: func(drafts []ContentIdeaDraft) []ContentIdeaDraft {
				drafts[0].Resonance = ""
				return drafts
			},
			wantErr: "resonance",
		},
		{
			name: "suitable format without angle",
			change: func(drafts []ContentIdeaDraft) []ContentIdeaDraft {
				drafts[0].Thread.Angle = ""
				return drafts
			},
			wantErr: "thread angle",
		},
		{
			name: "duplicate idea",
			change: func(drafts []ContentIdeaDraft) []ContentIdeaDraft {
				duplicate := drafts[0]
				duplicate.Rank = 2
				return append(drafts, duplicate)
			},
			wantErr: "duplicate idea",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
			store := newWorkerTestStore(now)
			worker := Worker{
				Store:    store,
				Registry: NewStaticAgentRegistry(draftAgent{drafts: test.change(validIdeaDrafts())}),
				Now:      func() time.Time { return now.Add(time.Minute) },
			}
			result, err := worker.RunOnce(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("worker error = %v, want %q", err, test.wantErr)
			}
			if !result.FoundJob || result.JobID != store.job.ID {
				t.Fatalf("worker result = %#v, want failed job %s", result, store.job.ID)
			}
			if store.failedRun == nil {
				t.Fatal("worker did not record a failed run")
			}
			if store.failedRun.Status != domain.JobFailed || store.failedRun.ID == "" {
				t.Fatalf("failed run = %#v, want terminal run with stable id", store.failedRun)
			}
		})
	}
}

type workerTestStore struct {
	job          domain.Job
	observations []domain.Observation
	claimed      bool
	failedRun    *domain.AgentRun
}

func newWorkerTestStore(now time.Time) *workerTestStore {
	evidence := domain.EvidenceRef{
		ID:             "ev_test",
		SourceKind:     "sessions",
		SourceIdentity: "session-example",
		ContentHash:    "content-hash",
		Excerpt:        "A bounded generic example.",
	}
	return &workerTestStore{
		job: domain.Job{
			ID:           "job_test",
			Fingerprint:  "job-fingerprint",
			AgentName:    "content-scout",
			AgentVersion: "v0",
			Status:       domain.JobPending,
			Payload: domain.JobPayload{
				ObservationIDs: []string{"ob_test"},
			},
			CreatedAt: now,
		},
		observations: []domain.Observation{{
			ID:         "ob_test",
			Kind:       "insight",
			Summary:    "A useful observation.",
			Confidence: 0.9,
			Evidence:   []domain.EvidenceRef{evidence},
			CreatedAt:  now,
		}},
	}
}

func (store *workerTestStore) FindCompletedScan(
	context.Context,
	string,
) (domain.Scan, bool, error) {
	panic("unexpected FindCompletedScan call")
}

func (store *workerTestStore) FindCompletedKnowledge(
	context.Context,
	string,
) ([]domain.Observation, bool, error) {
	panic("unexpected FindCompletedKnowledge call")
}

func (store *workerTestStore) CommitScan(
	context.Context,
	domain.ScanCommit,
) (bool, error) {
	panic("unexpected CommitScan call")
}

func (store *workerTestStore) ClaimPendingJob(context.Context) (domain.Job, bool, error) {
	if store.claimed {
		return domain.Job{}, false, nil
	}
	store.claimed = true
	store.job.Status = domain.JobRunning
	return store.job, true, nil
}

func (store *workerTestStore) LoadObservations(
	context.Context,
	[]string,
) ([]domain.Observation, error) {
	return store.observations, nil
}

func (store *workerTestStore) CompleteJob(
	context.Context,
	domain.JobCompletion,
) error {
	panic("unexpected CompleteJob call")
}

func (store *workerTestStore) FailJob(_ context.Context, run domain.AgentRun) error {
	store.failedRun = &run
	return nil
}

func (store *workerTestStore) ListJobs(context.Context) ([]domain.Job, error) {
	panic("unexpected ListJobs call")
}

func (store *workerTestStore) ListIdeas(context.Context) ([]domain.ContentIdea, error) {
	panic("unexpected ListIdeas call")
}

type draftAgent struct {
	drafts []ContentIdeaDraft
}

func (draftAgent) Name() string {
	return "content-scout"
}

func (draftAgent) Version() string {
	return "v0"
}

func (agent draftAgent) Run(context.Context, AgentRequest) ([]ContentIdeaDraft, error) {
	return agent.drafts, nil
}

func validIdeaDrafts() []ContentIdeaDraft {
	return []ContentIdeaDraft{{
		Rank:            1,
		Concept:         "A focused concept",
		CoreLesson:      "A useful lesson",
		AudienceBenefit: "A clear benefit",
		Hook:            "A concrete hook",
		Resonance:       "A reason it may resonate",
		Confidence:      0.9,
		ShortPost: domain.FormatAngle{
			Suitable: true,
			Angle:    "A short angle",
		},
		Thread: domain.FormatAngle{
			Suitable: true,
			Angle:    "A thread angle",
		},
		Article: domain.FormatAngle{
			Suitable: true,
			Angle:    "An article angle",
		},
		EvidenceIDs: []string{"ev_test"},
	}}
}
