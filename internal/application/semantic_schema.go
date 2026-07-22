package application

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const SemanticClaimSchemaName = "semantic-claim-candidates"

// The application owns this contract. Adapters transmit it and decode its
// response, but do not redefine which candidate fields Noema accepts.
const semanticClaimSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["claims"],
  "properties": {
    "claims": {
      "type": "array",
      "maxItems": 64,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "type", "statement", "status", "confidence",
          "supportingEvidenceIds", "contradictingEvidenceIds",
          "supportingFactIds", "outcome", "actor", "origin",
          "subject", "scope", "attribution"
        ],
        "properties": {
          "type": {
            "type": "string",
            "enum": [
              "problem", "symptom", "hypothesis", "failed-attempt",
              "root-cause", "decision", "solution", "verification", "lesson"
            ]
          },
          "statement": {"type": "string", "minLength": 1, "maxLength": 2048},
          "status": {"type": "string", "enum": ["observed", "inferred", "uncertain"]},
          "confidence": {"type": "number", "minimum": 0, "maximum": 1},
          "supportingEvidenceIds": {
            "type": "array", "minItems": 1, "maxItems": 512,
            "uniqueItems": true, "items": {"type": "string", "minLength": 1}
          },
          "contradictingEvidenceIds": {
            "type": "array", "maxItems": 512,
            "uniqueItems": true, "items": {"type": "string", "minLength": 1}
          },
          "supportingFactIds": {
            "type": ["array", "null"], "maxItems": 256,
            "uniqueItems": true, "items": {"type": "string", "minLength": 1}
          },
          "outcome": {
            "type": ["string", "null"],
            "enum": ["success", "failure", "unknown", null]
          },
          "actor": {
            "type": ["string", "null"],
            "enum": ["human", "model", "tool", "system", null]
          },
          "origin": {
            "type": ["string", "null"],
            "enum": [
              "human", "injected", "delegated", "replayed-copied",
              "model", "tool", "system", null
            ]
          },
          "subject": {"type": ["string", "null"], "maxLength": 512},
          "scope": {"type": ["string", "null"], "maxLength": 512},
          "attribution": {"type": ["string", "null"], "enum": ["unknown", null]}
        }
      }
    }
  }
}`

func semanticClaimOutputSchema() (domain.StructuredOutputSchema, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(semanticClaimSchemaJSON)); err != nil {
		return domain.StructuredOutputSchema{}, errors.New("semantic output schema is unavailable")
	}
	canonical := json.RawMessage(append([]byte(nil), compact.Bytes()...))
	digest, err := platform.Fingerprint(canonical)
	if err != nil {
		return domain.StructuredOutputSchema{}, errors.New("semantic output schema identity is unavailable")
	}
	return domain.StructuredOutputSchema{
		Identity: domain.StructuredOutputSchemaIdentity{
			Name:        SemanticClaimSchemaName,
			Version:     SemanticClaimSchemaVersion,
			Disposition: domain.StructuredOutputDispositionStrict,
			Digest:      digest,
		},
		CanonicalJSON: canonical,
	}, nil
}
