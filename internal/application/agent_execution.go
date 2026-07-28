package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	contractassets "github.com/ferueda/noema/contracts"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const (
	AgentExecutionRequestSchemaDigest  = "695bb06e7a7b3698cf9205643e74814b901cccaa916364dc4f4cbe3ee74f37e4"
	AgentExecutionResponseSchemaDigest = "57c279c25b6c1b21d00a95fa09a71b10397ee527595a3bbc9a42a5a86bf1f671"
	ContentScoutInputSchemaName        = "content-scout-input"
	ContentScoutInputSchemaDigest      = "66fcd21212e77af2bad2b80c0b423736c13031fe3597d44776d694275cc1d98d"
	ContentScoutCandidatesSchemaName   = "content-scout-candidates"
	ContentScoutCandidatesSchemaDigest = "6859d77424e02745841a7348a3c88e74874a928c4ef0f0b831257b81e5c8c965"
)

// AgentExecutor is the portable execution boundary. Agent-specific input
// preparation and output admission remain in Noema.
type AgentExecutor interface {
	Execute(
		context.Context,
		domain.AgentExecutionRequestV1,
		domain.StructuredOutputSchema,
	) (domain.AgentExecutionResponseV1, error)
}

func ContentScoutOutputSchema() (domain.StructuredOutputSchema, error) {
	document, err := loadContractSchema(
		"agents/content-scout/v1/candidates.schema.json",
		ContentScoutCandidatesSchemaDigest,
	)
	if err != nil {
		return domain.StructuredOutputSchema{}, err
	}
	return domain.StructuredOutputSchema{
		Identity: domain.StructuredOutputSchemaIdentity{
			Name:        ContentScoutCandidatesSchemaName,
			Version:     domain.ContentIdeaSchemaVersion,
			Disposition: domain.StructuredOutputDispositionStrict,
			Digest:      ContentScoutCandidatesSchemaDigest,
		},
		CanonicalJSON: document,
	}, nil
}

func loadContractSchema(path string, expectedDigest string) (json.RawMessage, error) {
	document, err := contractassets.Files.ReadFile(path)
	if err != nil {
		return nil, errors.New("agent contract schema unavailable")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, document); err != nil {
		return nil, errors.New("agent contract schema invalid")
	}
	canonical := json.RawMessage(compact.Bytes())
	digest, err := platform.Fingerprint(canonical)
	if err != nil || digest != expectedDigest {
		return nil, errors.New("agent contract schema identity mismatch")
	}
	return canonical, nil
}
