package domain

import "encoding/json"

const StructuredOutputDispositionStrict = "strict"

// StructuredOutputSchemaIdentity is the durable identity of an
// application-owned output contract. The schema body is rebuildable.
type StructuredOutputSchemaIdentity struct {
	Name        string `json:"name"`
	Version     int    `json:"version"`
	Disposition string `json:"disposition"`
	Digest      string `json:"digest"`
}

// StructuredOutputSchema carries the exact provider-neutral contract a model
// adapter must request. CanonicalJSON is bounded with the complete request.
type StructuredOutputSchema struct {
	Identity      StructuredOutputSchemaIdentity `json:"identity"`
	CanonicalJSON json.RawMessage                `json:"canonicalJson"`
}

// ValidatedModelRoute carries a composition-root-approved route without
// exposing provider credentials or making the application parse route files.
// SanitizedConfig is retained for inspection; ConfigDigest is its stable
// identity for processing and reuse.
type ValidatedModelRoute struct {
	Requested       RequestedModelRoute `json:"requested"`
	SanitizedConfig json.RawMessage     `json:"sanitizedConfig"`
	ConfigDigest    string              `json:"configDigest"`
}

// ValidClaimActor reports whether a generated actor can be admitted. Empty is
// allowed because actor metadata is optional; "unknown" is intentionally not.
func ValidClaimActor(value string) bool {
	switch value {
	case "", "human", "model", "tool", "system":
		return true
	default:
		return false
	}
}

// ValidClaimOrigin reports whether a generated origin can be admitted. Empty
// is allowed because origin metadata is optional; "unknown" is intentionally
// not.
func ValidClaimOrigin(value string) bool {
	switch value {
	case "", "human", "injected", "delegated", "replayed-copied", "model", "tool", "system":
		return true
	default:
		return false
	}
}
