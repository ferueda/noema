package application

import (
	"context"
	"fmt"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

type AgentRequest struct {
	Job          domain.Job
	Observations []domain.Observation
}

type ContentIdeaDraft struct {
	Rank            int
	Concept         string
	CoreLesson      string
	AudienceBenefit string
	Hook            string
	Resonance       string
	Confidence      float64
	ShortPost       domain.FormatAngle
	Thread          domain.FormatAngle
	Article         domain.FormatAngle
	EvidenceIDs     []string
}

type WorkerResult struct {
	FoundJob bool
	JobID    string
	IdeaIDs  []string
}

type Worker struct {
	Store    Store
	Registry AgentRegistry
	Now      func() time.Time
}

func (worker Worker) RunOnce(ctx context.Context) (WorkerResult, error) {
	job, found, err := worker.Store.ClaimPendingJob(ctx)
	if err != nil {
		return WorkerResult{}, fmt.Errorf("claim pending job: %w", err)
	}
	if !found {
		return WorkerResult{}, nil
	}

	startedAt := worker.now()
	observations, err := worker.Store.LoadObservations(ctx, job.Payload.ObservationIDs)
	if err != nil {
		return worker.fail(ctx, job, startedAt, nil, fmt.Errorf("load observations: %w", err))
	}
	evidence := observationEvidence(observations)
	agent, ok := worker.Registry.Find(job.AgentName, job.AgentVersion)
	if !ok {
		cause := fmt.Errorf("agent %s@%s is not registered", job.AgentName, job.AgentVersion)
		return worker.fail(ctx, job, startedAt, evidence, cause)
	}
	drafts, err := agent.Run(ctx, AgentRequest{Job: job, Observations: observations})
	if err != nil {
		return worker.fail(ctx, job, startedAt, evidence, fmt.Errorf("run agent: %w", err))
	}
	ideas, err := worker.buildIdeas(drafts, observations, job.Fingerprint)
	if err != nil {
		return worker.fail(ctx, job, startedAt, evidence, err)
	}

	runID := platform.DerivedID("run_", job.ID)
	finishedAt := worker.now()
	run := domain.AgentRun{
		ID:           runID,
		JobID:        job.ID,
		AgentName:    job.AgentName,
		AgentVersion: job.AgentVersion,
		Status:       domain.JobSucceeded,
		Evidence:     evidence,
		Output:       ideas,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
	}
	for index := range ideas {
		ideas[index].RunID = runID
	}
	run.Output = ideas
	if err := worker.Store.CompleteJob(ctx, domain.JobCompletion{
		JobID: job.ID,
		Run:   run,
		Ideas: ideas,
	}); err != nil {
		return WorkerResult{}, fmt.Errorf("complete job: %w", err)
	}
	return WorkerResult{
		FoundJob: true,
		JobID:    job.ID,
		IdeaIDs:  ideaIDs(ideas),
	}, nil
}

func (worker Worker) buildIdeas(
	drafts []ContentIdeaDraft,
	observations []domain.Observation,
	jobFingerprint string,
) ([]domain.ContentIdea, error) {
	if len(drafts) > 5 {
		return nil, fmt.Errorf("validate agent output: at most five ideas are allowed")
	}
	evidenceByID := make(map[string]domain.EvidenceRef)
	for _, observation := range observations {
		for _, reference := range observation.Evidence {
			evidenceByID[reference.ID] = reference
		}
	}

	ideas := make([]domain.ContentIdea, 0, len(drafts))
	seen := make(map[string]struct{}, len(drafts))
	for index, draft := range drafts {
		if draft.Rank != index+1 {
			return nil, fmt.Errorf("validate idea %d: rank must match ordered position", index)
		}
		if draft.Concept == "" || draft.CoreLesson == "" || draft.AudienceBenefit == "" ||
			draft.Hook == "" || draft.Resonance == "" {
			return nil, fmt.Errorf(
				"validate idea %d: concept, lesson, audience benefit, hook, and resonance are required",
				index,
			)
		}
		if draft.Confidence < 0 || draft.Confidence > 1 {
			return nil, fmt.Errorf("validate idea %d: confidence must be between 0 and 1", index)
		}
		if len(draft.EvidenceIDs) == 0 {
			return nil, fmt.Errorf("validate idea %d: evidence is required", index)
		}
		for name, format := range map[string]domain.FormatAngle{
			"short post": draft.ShortPost,
			"thread":     draft.Thread,
			"article":    draft.Article,
		} {
			if format.Suitable && format.Angle == "" {
				return nil, fmt.Errorf(
					"validate idea %d: %s angle is required when suitable",
					index,
					name,
				)
			}
		}
		evidence := make([]domain.EvidenceRef, 0, len(draft.EvidenceIDs))
		for _, evidenceID := range draft.EvidenceIDs {
			reference, ok := evidenceByID[evidenceID]
			if !ok {
				return nil, fmt.Errorf("validate idea %d: unknown evidence id %q", index, evidenceID)
			}
			evidence = append(evidence, reference)
		}
		canonicalFingerprint, err := platform.Fingerprint(struct {
			Concept     string
			CoreLesson  string
			EvidenceIDs []string
		}{
			Concept:     draft.Concept,
			CoreLesson:  draft.CoreLesson,
			EvidenceIDs: draft.EvidenceIDs,
		})
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[canonicalFingerprint]; duplicate {
			return nil, fmt.Errorf("validate idea %d: duplicate idea", index)
		}
		seen[canonicalFingerprint] = struct{}{}
		fingerprint, err := platform.Fingerprint(struct {
			Job  string
			Idea string
		}{
			Job:  jobFingerprint,
			Idea: canonicalFingerprint,
		})
		if err != nil {
			return nil, err
		}
		ideas = append(ideas, domain.ContentIdea{
			ID:              "idea_" + fingerprint[:32],
			Fingerprint:     fingerprint,
			Rank:            draft.Rank,
			Concept:         draft.Concept,
			CoreLesson:      draft.CoreLesson,
			AudienceBenefit: draft.AudienceBenefit,
			Hook:            draft.Hook,
			Resonance:       draft.Resonance,
			Confidence:      draft.Confidence,
			ShortPost:       draft.ShortPost,
			Thread:          draft.Thread,
			Article:         draft.Article,
			Evidence:        evidence,
			CreatedAt:       worker.now(),
		})
	}
	return ideas, nil
}

func (worker Worker) fail(
	ctx context.Context,
	job domain.Job,
	startedAt time.Time,
	evidence []domain.EvidenceRef,
	cause error,
) (WorkerResult, error) {
	run := worker.failedRun(job, startedAt, evidence, cause.Error())
	if err := worker.Store.FailJob(ctx, run); err != nil {
		return WorkerResult{}, fmt.Errorf("%v; record failure: %w", cause, err)
	}
	return WorkerResult{FoundJob: true, JobID: job.ID}, cause
}

func (worker Worker) failedRun(
	job domain.Job,
	startedAt time.Time,
	evidence []domain.EvidenceRef,
	message string,
) domain.AgentRun {
	return domain.AgentRun{
		ID:           platform.DerivedID("run_", job.ID),
		JobID:        job.ID,
		AgentName:    job.AgentName,
		AgentVersion: job.AgentVersion,
		Status:       domain.JobFailed,
		Evidence:     evidence,
		Error:        message,
		StartedAt:    startedAt,
		FinishedAt:   worker.now(),
	}
}

func (worker Worker) now() time.Time {
	if worker.Now == nil {
		return time.Now().UTC()
	}
	return worker.Now().UTC()
}

func observationEvidence(observations []domain.Observation) []domain.EvidenceRef {
	seen := make(map[string]struct{})
	var result []domain.EvidenceRef
	for _, observation := range observations {
		for _, reference := range observation.Evidence {
			if _, ok := seen[reference.ID]; ok {
				continue
			}
			seen[reference.ID] = struct{}{}
			result = append(result, reference)
		}
	}
	return result
}

func ideaIDs(ideas []domain.ContentIdea) []string {
	values := make([]string, 0, len(ideas))
	for _, idea := range ideas {
		values = append(values, idea.ID)
	}
	return values
}
