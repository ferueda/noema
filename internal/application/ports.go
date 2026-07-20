package application

import (
	"context"

	"github.com/ferueda/noema/internal/domain"
)

type Store interface {
	FindCompletedScan(context.Context, string) (domain.Scan, bool, error)
	FindCompletedKnowledge(context.Context, string) ([]domain.Observation, bool, error)
	CommitScan(context.Context, domain.ScanCommit) (bool, error)
	ClaimPendingJob(context.Context) (domain.Job, bool, error)
	LoadObservations(context.Context, []string) ([]domain.Observation, error)
	CompleteJob(context.Context, domain.JobCompletion) error
	FailJob(context.Context, domain.AgentRun) error
	ListJobs(context.Context) ([]domain.Job, error)
	ListIdeas(context.Context) ([]domain.ContentIdea, error)
}

type Source interface {
	Read(context.Context, ScanRequest) (SourceResult, error)
}

type Distiller interface {
	Distill(context.Context, []domain.EvidenceChunk) ([]ObservationDraft, error)
}

type Agent interface {
	Name() string
	Version() string
	Run(context.Context, AgentRequest) ([]ContentIdeaDraft, error)
}

type AgentRegistry interface {
	Find(name, version string) (Agent, bool)
}

type IDGenerator func() (string, error)
