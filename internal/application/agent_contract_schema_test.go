package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/domain"
)

const maxAgentContractDocumentBytes = 512 * 1024

func TestAgentContractSchemasHaveStableStrictIdentities(t *testing.T) {
	tests := []struct {
		path   string
		id     string
		title  string
		digest string
	}{
		{"agent-execution/v1/request.schema.json", "urn:noema:agent-execution:request:v1", "AgentExecutionRequestV1", AgentExecutionRequestSchemaDigest},
		{"agent-execution/v1/response.schema.json", "urn:noema:agent-execution:response:v1", "AgentExecutionResponseV1", AgentExecutionResponseSchemaDigest},
		{"agents/content-scout/v1/input.schema.json", "urn:noema:agent:content-scout:input:v1", "ContentScoutInputV1", ContentScoutInputSchemaDigest},
		{"agents/content-scout/v1/candidates.schema.json", "urn:noema:agent:content-scout:candidates:v1", "ContentScoutCandidatesV1", ContentScoutCandidatesSchemaDigest},
	}

	for _, test := range tests {
		t.Run(test.title, func(t *testing.T) {
			document := readContractFile(t, test.path)
			if len(document) > maxAgentContractDocumentBytes {
				t.Fatalf("schema bytes = %d, want at most %d", len(document), maxAgentContractDocumentBytes)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, document); err != nil {
				t.Fatalf("compact schema: %v", err)
			}
			sum := sha256.Sum256(compact.Bytes())
			if digest := hex.EncodeToString(sum[:]); digest != test.digest {
				t.Fatalf("schema digest = %s, want %s", digest, test.digest)
			}
			var identity struct {
				Schema string `json:"$schema"`
				ID     string `json:"$id"`
				Title  string `json:"title"`
				Type   string `json:"type"`
			}
			if err := json.Unmarshal(document, &identity); err != nil {
				t.Fatalf("decode schema identity: %v", err)
			}
			if identity.Schema != "https://json-schema.org/draft/2020-12/schema" ||
				identity.ID != test.id || identity.Title != test.title || identity.Type != "object" {
				t.Fatalf("schema identity = %#v", identity)
			}
			var schema map[string]any
			if err := json.Unmarshal(document, &schema); err != nil {
				t.Fatalf("decode schema: %v", err)
			}
			assertFixedSchemaObjectsAreStrict(t, schema, "")
		})
	}
}

func TestContentScoutOutputSchemaLoadsFromFrozenContract(t *testing.T) {
	schema, err := ContentScoutOutputSchema()
	if err != nil {
		t.Fatalf("load output schema: %v", err)
	}
	if schema.Identity.Name != ContentScoutCandidatesSchemaName ||
		schema.Identity.Version != domain.ContentIdeaSchemaVersion ||
		schema.Identity.Digest != ContentScoutCandidatesSchemaDigest ||
		schema.Identity.Disposition != domain.StructuredOutputDispositionStrict ||
		!json.Valid(schema.CanonicalJSON) {
		t.Fatalf("output schema = %#v", schema)
	}
}

func TestAgentExecutionContractFixturesUseAuthoritativeGoTypes(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		decode  func([]byte) error
		wantErr bool
	}{
		{"valid request", "agent-execution/v1/fixtures/request.valid.json", decodeExecutionRequestFixture, false},
		{"request unknown field", "agent-execution/v1/fixtures/request.invalid-unknown-field.json", decodeExecutionRequestFixture, true},
		{"request wrong version", "agent-execution/v1/fixtures/request.invalid-wrong-version.json", decodeExecutionRequestFixture, true},
		{"request oversized value", "agent-execution/v1/fixtures/request.invalid-oversized.json", decodeExecutionRequestFixture, true},
		{"valid response", "agent-execution/v1/fixtures/response.valid.json", decodeExecutionResponseFixture, false},
		{"response unknown field", "agent-execution/v1/fixtures/response.invalid-unknown-field.json", decodeExecutionResponseFixture, true},
		{"response malformed", "agent-execution/v1/fixtures/response.invalid-malformed.json", decodeExecutionResponseFixture, true},
		{"valid input", "agents/content-scout/v1/fixtures/input.valid.json", decodeContentScoutInputFixture, false},
		{"input unknown field", "agents/content-scout/v1/fixtures/input.invalid-unknown-field.json", decodeContentScoutInputFixture, true},
		{"input wrong version", "agents/content-scout/v1/fixtures/input.invalid-wrong-version.json", decodeContentScoutInputFixture, true},
		{"input oversized value", "agents/content-scout/v1/fixtures/input.invalid-oversized.json", decodeContentScoutInputFixture, true},
		{"valid candidates", "agents/content-scout/v1/fixtures/candidates.valid.json", decodeContentScoutCandidatesFixture, false},
		{"valid empty candidates", "agents/content-scout/v1/fixtures/candidates.valid-empty.json", decodeContentScoutCandidatesFixture, false},
		{"candidate unknown field", "agents/content-scout/v1/fixtures/candidates.invalid-unknown-field.json", decodeContentScoutCandidatesFixture, true},
		{"too many candidates", "agents/content-scout/v1/fixtures/candidates.invalid-too-many.json", decodeContentScoutCandidatesFixture, true},
		{"oversized candidate", "agents/content-scout/v1/fixtures/candidates.invalid-oversized.json", decodeContentScoutCandidatesFixture, true},
		{"duplicate candidate", "agents/content-scout/v1/fixtures/candidates.invalid-duplicate.json", decodeContentScoutCandidatesFixture, true},
		{"empty suitable angle", "agents/content-scout/v1/fixtures/candidates.invalid-empty-suitable-angle.json", decodeContentScoutCandidatesFixture, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.decode(readContractFile(t, test.path))
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func decodeExecutionRequestFixture(document []byte) error {
	request, err := decodeContract[domain.AgentExecutionRequestV1](document)
	if err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if request.Input.SchemaName != ContentScoutInputSchemaName ||
		request.Input.SchemaVersion != domain.ContentScoutInputSchemaVersion ||
		request.Input.SchemaDigest != ContentScoutInputSchemaDigest ||
		request.RequiredOutputSchema.Name != ContentScoutCandidatesSchemaName ||
		request.RequiredOutputSchema.Version != domain.ContentIdeaSchemaVersion ||
		request.RequiredOutputSchema.Digest != ContentScoutCandidatesSchemaDigest {
		return errors.New("execution schema identity is invalid")
	}
	return decodeContentScoutInputFixture(request.Input.CanonicalJSON)
}

func decodeExecutionResponseFixture(document []byte) error {
	response, err := decodeContract[domain.AgentExecutionResponseV1](document)
	if err != nil {
		return err
	}
	if err := response.Validate(); err != nil {
		return err
	}
	return decodeContentScoutCandidatesFixture(response.CandidateJSON)
}

func decodeContentScoutInputFixture(document []byte) error {
	value, err := decodeContract[domain.ContentScoutInputV1](document)
	if err != nil {
		return err
	}
	return value.Validate()
}

func decodeContentScoutCandidatesFixture(document []byte) error {
	value, err := decodeContract[domain.ContentScoutCandidatesV1](document)
	if err != nil {
		return err
	}
	return value.Validate()
}

func decodeContract[T any](document []byte) (T, error) {
	var value T
	if len(document) == 0 || len(document) > maxAgentContractDocumentBytes {
		return value, errors.New("contract document size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return value, errors.New("contract document contains trailing JSON")
	}
	return value, nil
}

func readContractFile(t *testing.T, relativePath string) []byte {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve contract fixture path")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "contracts", relativePath)
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return document
}

func assertFixedSchemaObjectsAreStrict(t *testing.T, value any, path string) {
	t.Helper()
	object, isObject := value.(map[string]any)
	if isObject {
		if object["type"] == "object" && !opaqueSchemaObject(path) {
			if additional, exists := object["additionalProperties"]; !exists || additional != false {
				t.Fatalf("fixed schema object %s does not reject unknown fields", path)
			}
		}
		for key, child := range object {
			assertFixedSchemaObjectsAreStrict(t, child, path+"/"+key)
		}
		return
	}
	if array, isArray := value.([]any); isArray {
		for index, child := range array {
			assertFixedSchemaObjectsAreStrict(t, child, fmt.Sprintf("%s/%d", path, index))
		}
	}
}

func opaqueSchemaObject(path string) bool {
	return strings.HasSuffix(path, "/properties/handlerConfigurationJson") ||
		strings.HasSuffix(path, "/properties/canonicalJson") ||
		strings.HasSuffix(path, "/properties/candidateJson")
}
