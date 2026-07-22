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
  "type": "object",
  "additionalProperties": false,
  "required": ["claims"],
  "properties": {
    "claims": {
      "type": "array",
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
          "statement": {"type": "string"},
          "status": {"type": "string", "enum": ["observed", "inferred", "uncertain"]},
          "confidence": {"type": "number", "minimum": 0, "maximum": 1},
          "supportingEvidenceIds": {
            "type": "array", "items": {"type": "string"}
          },
          "contradictingEvidenceIds": {
            "type": "array", "items": {"type": "string"}
          },
          "supportingFactIds": {
            "type": ["array", "null"], "items": {"type": "string"}
          },
          "outcome": {
            "type": ["string", "null"],
            "enum": ["success", "failure", "unknown", null]
          },
          "actor": {
            "type": "null"
          },
          "origin": {
            "type": "null"
          },
          "subject": {"type": ["string", "null"]},
          "scope": {"type": ["string", "null"]},
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
