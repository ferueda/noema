package application

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func TestSemanticClaimOutputSchemaHasStableStrictIdentity(t *testing.T) {
	first, err := semanticClaimOutputSchema()
	if err != nil {
		t.Fatalf("build first schema: %v", err)
	}
	second, err := semanticClaimOutputSchema()
	if err != nil {
		t.Fatalf("build second schema: %v", err)
	}
	digest, err := platform.Fingerprint(json.RawMessage(first.CanonicalJSON))
	if err != nil {
		t.Fatalf("fingerprint schema: %v", err)
	}
	if first.Identity.Name != SemanticClaimSchemaName || first.Identity.Version != SemanticClaimSchemaVersion ||
		first.Identity.Disposition != domain.StructuredOutputDispositionStrict ||
		first.Identity.Digest != digest || first.Identity != second.Identity ||
		string(first.CanonicalJSON) != string(second.CanonicalJSON) || !json.Valid(first.CanonicalJSON) {
		t.Fatalf("schema identity is not stable: %#v / %#v", first.Identity, second.Identity)
	}
}

func TestSemanticClaimOutputSchemaUsesCerebrasStrictSubset(t *testing.T) {
	schema, err := semanticClaimOutputSchema()
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}
	for _, unsupported := range []string{
		`"$schema"`, `"minItems"`, `"maxItems"`, `"uniqueItems"`,
		`"minLength"`, `"maxLength"`,
	} {
		if bytes.Contains(schema.CanonicalJSON, []byte(unsupported)) {
			t.Fatalf("schema contains provider-unsupported keyword %s", unsupported)
		}
	}
}

func TestSemanticClaimOutputSchemaKeepsV0ActorAndOriginNull(t *testing.T) {
	schema, err := semanticClaimOutputSchema()
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(schema.CanonicalJSON, &document); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	properties := schemaObject(t, document, "properties")
	claims := schemaObject(t, properties, "claims")
	items := schemaObject(t, claims, "items")
	candidateProperties := schemaObject(t, items, "properties")

	actor := schemaObject(t, candidateProperties, "actor")
	origin := schemaObject(t, candidateProperties, "origin")
	if actor["type"] != "null" || origin["type"] != "null" {
		t.Fatalf("V0 provenance schema = actor %#v / origin %#v", actor, origin)
	}
	// The domain keeps this vocabulary for later extractors that can establish
	// provenance without asking the semantic model to infer it.
	for _, value := range []string{"human", "model", "tool", "system"} {
		if !domain.ValidClaimActor(value) {
			t.Fatalf("schema actor %q is not admitted", value)
		}
	}
	for _, value := range []string{"human", "injected", "delegated", "replayed-copied", "model", "tool", "system"} {
		if !domain.ValidClaimOrigin(value) {
			t.Fatalf("schema origin %q is not admitted", value)
		}
	}
}

func schemaObject(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("schema field %q is not an object", key)
	}
	return value
}
