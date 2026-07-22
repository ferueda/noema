package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

type ScanRequest struct {
	After                 time.Time
	Before                time.Time
	ContentScope          string
	DistillationConfigKey string
	ContentScoutConfigKey string
}

type SourceResult struct {
	SourceKind   string
	Coverage     string
	SkippedCount int
	Chunks       []domain.EvidenceChunk
}

type ObservationDraft struct {
	Kind        string
	Summary     string
	Confidence  float64
	EvidenceIDs []string
}

type ScanResult struct {
	Scan   domain.Scan
	Reused bool
}

type Scanner struct {
	Store     Store
	Source    Source
	Distiller Distiller
	NewID     IDGenerator
	Now       func() time.Time
}

func (scanner Scanner) Run(ctx context.Context, request ScanRequest) (ScanResult, error) {
	if err := validateScanRequest(request); err != nil {
		return ScanResult{}, err
	}
	sourceResult, err := scanner.Source.Read(ctx, request)
	if err != nil {
		return ScanResult{}, fmt.Errorf("read source: %w", err)
	}
	if sourceResult.SourceKind == "" {
		return ScanResult{}, fmt.Errorf("read source: source kind is required")
	}

	chunks, err := scanner.prepareChunks(sourceResult.Chunks, request.DistillationConfigKey)
	if err != nil {
		return ScanResult{}, err
	}
	knowledgeFingerprint, err := platform.Fingerprint(struct {
		SourceKind            string
		After                 string
		Before                string
		ContentScope          string
		DistillationConfigKey string
		Coverage              string
		SkippedCount          int
		Evidence              []string
	}{
		SourceKind:            sourceResult.SourceKind,
		After:                 request.After.UTC().Format(time.RFC3339Nano),
		Before:                request.Before.UTC().Format(time.RFC3339Nano),
		ContentScope:          request.ContentScope,
		DistillationConfigKey: request.DistillationConfigKey,
		Coverage:              sourceResult.Coverage,
		SkippedCount:          sourceResult.SkippedCount,
		Evidence:              chunkFingerprints(chunks),
	})
	if err != nil {
		return ScanResult{}, err
	}
	scanFingerprint, err := platform.Fingerprint(struct {
		Knowledge    string
		ContentScout string
	}{
		Knowledge:    knowledgeFingerprint,
		ContentScout: request.ContentScoutConfigKey,
	})
	if err != nil {
		return ScanResult{}, err
	}

	existing, found, err := scanner.Store.FindCompletedScan(ctx, scanFingerprint)
	if err != nil {
		return ScanResult{}, fmt.Errorf("find completed scan: %w", err)
	}
	if found {
		return ScanResult{Scan: existing, Reused: true}, nil
	}

	observations, knowledgeFound, err := scanner.Store.FindCompletedKnowledge(ctx, knowledgeFingerprint)
	if err != nil {
		return ScanResult{}, fmt.Errorf("find completed knowledge: %w", err)
	}
	newObservations := observations
	if !knowledgeFound {
		drafts, distillErr := scanner.Distiller.Distill(ctx, chunks)
		if distillErr != nil {
			return ScanResult{}, fmt.Errorf("distill evidence: %w", distillErr)
		}
		observations, err = scanner.buildObservations(
			drafts,
			chunks,
			request.DistillationConfigKey,
		)
		if err != nil {
			return ScanResult{}, err
		}
		newObservations = observations
	} else {
		newObservations = nil
	}

	commit, err := scanner.buildCommit(
		request,
		sourceResult,
		scanFingerprint,
		knowledgeFingerprint,
		chunks,
		observations,
		newObservations,
	)
	if err != nil {
		return ScanResult{}, err
	}
	inserted, err := scanner.Store.CommitScan(ctx, commit)
	if err != nil {
		return ScanResult{}, fmt.Errorf("commit scan: %w", err)
	}
	if !inserted {
		existing, found, findErr := scanner.Store.FindCompletedScan(ctx, scanFingerprint)
		if findErr != nil {
			return ScanResult{}, fmt.Errorf("find concurrently completed scan: %w", findErr)
		}
		if !found {
			return ScanResult{}, fmt.Errorf("scan fingerprint conflict without completed scan")
		}
		return ScanResult{Scan: existing, Reused: true}, nil
	}
	return ScanResult{Scan: commit.Scan}, nil
}

func (scanner Scanner) prepareChunks(
	input []domain.EvidenceChunk,
	distillationConfigKey string,
) ([]domain.EvidenceChunk, error) {
	chunks := slices.Clone(input)
	for index := range chunks {
		chunk := &chunks[index]
		if chunk.Fingerprint == "" {
			return nil, fmt.Errorf("prepare evidence chunk %d: fingerprint is required", index)
		}
		if chunk.ID == "" {
			identity, err := platform.Fingerprint(struct {
				Kind        string
				Fingerprint string
			}{Kind: "evidence", Fingerprint: chunk.Fingerprint})
			if err != nil {
				return nil, err
			}
			chunk.ID = "ev_" + identity[:32]
		}
		chunk.Evidence.ID = chunk.ID
		distillationKey, err := platform.Fingerprint(struct {
			Evidence string
			Config   string
		}{Evidence: chunk.Fingerprint, Config: distillationConfigKey})
		if err != nil {
			return nil, err
		}
		chunk.DistillationKey = distillationKey
	}
	return chunks, nil
}

func (scanner Scanner) buildObservations(
	drafts []ObservationDraft,
	chunks []domain.EvidenceChunk,
	distillationConfigKey string,
) ([]domain.Observation, error) {
	evidenceByID := make(map[string]domain.EvidenceRef, len(chunks))
	for _, chunk := range chunks {
		evidenceByID[chunk.ID] = chunk.Evidence
	}

	observations := make([]domain.Observation, 0, len(drafts))
	for index, draft := range drafts {
		if draft.Kind == "" || draft.Summary == "" {
			return nil, fmt.Errorf("validate observation %d: kind and summary are required", index)
		}
		if draft.Confidence < 0 || draft.Confidence > 1 {
			return nil, fmt.Errorf("validate observation %d: confidence must be between 0 and 1", index)
		}
		if len(draft.EvidenceIDs) == 0 {
			return nil, fmt.Errorf("validate observation %d: evidence is required", index)
		}
		evidence := make([]domain.EvidenceRef, 0, len(draft.EvidenceIDs))
		for _, evidenceID := range draft.EvidenceIDs {
			reference, ok := evidenceByID[evidenceID]
			if !ok {
				return nil, fmt.Errorf("validate observation %d: unknown evidence id %q", index, evidenceID)
			}
			evidence = append(evidence, reference)
		}
		fingerprint, err := platform.Fingerprint(struct {
			Kind            string
			Summary         string
			EvidenceIDs     []string
			DistillationKey string
		}{
			Kind:            draft.Kind,
			Summary:         draft.Summary,
			EvidenceIDs:     draft.EvidenceIDs,
			DistillationKey: distillationConfigKey,
		})
		if err != nil {
			return nil, err
		}
		observations = append(observations, domain.Observation{
			ID:              "ob_" + fingerprint[:32],
			Fingerprint:     fingerprint,
			DistillationKey: distillationConfigKey,
			Kind:            draft.Kind,
			Summary:         draft.Summary,
			Confidence:      draft.Confidence,
			Evidence:        evidence,
			CreatedAt:       scanner.now(),
		})
	}
	return observations, nil
}

func (scanner Scanner) buildCommit(
	request ScanRequest,
	source SourceResult,
	scanFingerprint string,
	knowledgeFingerprint string,
	chunks []domain.EvidenceChunk,
	observations []domain.Observation,
	newObservations []domain.Observation,
) (domain.ScanCommit, error) {
	scanID, err := scanner.newID()
	if err != nil {
		return domain.ScanCommit{}, err
	}
	now := scanner.now()
	scan := domain.Scan{
		ID:                   scanID,
		Fingerprint:          scanFingerprint,
		KnowledgeFingerprint: knowledgeFingerprint,
		SourceKind:           source.SourceKind,
		After:                request.After.UTC(),
		Before:               request.Before.UTC(),
		ContentScope:         request.ContentScope,
		Coverage:             source.Coverage,
		Status:               "completed",
		SkippedCount:         source.SkippedCount,
		ObservationIDs:       observationIDs(observations),
		CreatedAt:            now,
		FinishedAt:           now,
	}
	events := make([]domain.Event, 0, len(newObservations)+1)
	for _, observation := range newObservations {
		event, eventErr := scanner.newEvent(
			"observation.created",
			"observation",
			observation.ID,
			map[string]any{"schemaVersion": 1, "observationId": observation.ID, "scanId": scanID},
			observation.Evidence,
		)
		if eventErr != nil {
			return domain.ScanCommit{}, eventErr
		}
		events = append(events, event)
	}

	var job *domain.Job
	if len(observations) > 0 {
		scanEvent, eventErr := scanner.newEvent(
			"scan.completed",
			"scan",
			scanID,
			map[string]any{"schemaVersion": 1, "scanId": scanID, "observationIds": observationIDs(observations)},
			unionEvidence(observations),
		)
		if eventErr != nil {
			return domain.ScanCommit{}, eventErr
		}
		events = append(events, scanEvent)

		jobID, idErr := scanner.newID()
		if idErr != nil {
			return domain.ScanCommit{}, idErr
		}
		jobFingerprint, fingerprintErr := platform.Fingerprint(struct {
			EventID string
			Agent   string
			Version string
			Config  string
		}{
			EventID: scanEvent.ID,
			Agent:   "content-scout",
			Version: "v0",
			Config:  request.ContentScoutConfigKey,
		})
		if fingerprintErr != nil {
			return domain.ScanCommit{}, fingerprintErr
		}
		job = &domain.Job{
			ID:           jobID,
			Fingerprint:  jobFingerprint,
			EventID:      scanEvent.ID,
			AgentName:    "content-scout",
			AgentVersion: "v0",
			Status:       domain.JobPending,
			Payload: domain.JobPayload{
				ScanID:         scanID,
				ObservationIDs: observationIDs(observations),
			},
			CreatedAt: now,
		}
		scan.JobID = jobID
	}
	return domain.ScanCommit{
		Scan:         scan,
		Chunks:       chunks,
		Observations: newObservations,
		Events:       events,
		Job:          job,
	}, nil
}

func (scanner Scanner) newEvent(
	eventType string,
	subjectType string,
	subjectID string,
	payload map[string]any,
	evidence []domain.EvidenceRef,
) (domain.Event, error) {
	fingerprint, err := platform.Fingerprint(struct {
		Type        string
		SubjectType string
		SubjectID   string
		Payload     map[string]any
	}{Type: eventType, SubjectType: subjectType, SubjectID: subjectID, Payload: payload})
	if err != nil {
		return domain.Event{}, err
	}
	return domain.Event{
		ID:          "evt_" + fingerprint[:32],
		Fingerprint: fingerprint,
		Type:        eventType,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Payload:     payload,
		Evidence:    evidence,
		CreatedAt:   scanner.now(),
	}, nil
}

func (scanner Scanner) newID() (string, error) {
	if scanner.NewID == nil {
		return "", fmt.Errorf("id generator is required")
	}
	return scanner.NewID()
}

func (scanner Scanner) now() time.Time {
	if scanner.Now == nil {
		return time.Now().UTC()
	}
	return scanner.Now().UTC()
}

func validateScanRequest(request ScanRequest) error {
	if request.After.IsZero() || request.Before.IsZero() {
		return fmt.Errorf("scan bounds are required")
	}
	if !request.After.Before(request.Before) {
		return fmt.Errorf("scan after must be before scan before")
	}
	if request.ContentScope != "private" && request.ContentScope != "public" {
		return fmt.Errorf("content scope must be private or public")
	}
	if request.DistillationConfigKey == "" || request.ContentScoutConfigKey == "" {
		return fmt.Errorf("distillation and content scout configuration keys are required")
	}
	return nil
}

func chunkFingerprints(chunks []domain.EvidenceChunk) []string {
	values := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		values = append(values, chunk.Fingerprint)
	}
	return values
}

func observationIDs(observations []domain.Observation) []string {
	values := make([]string, 0, len(observations))
	for _, observation := range observations {
		values = append(values, observation.ID)
	}
	return values
}

func unionEvidence(observations []domain.Observation) []domain.EvidenceRef {
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
