package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/adapters/aigateway"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func TestGatewayCheckRejectsRemoteGatesBeforeConstruction(t *testing.T) {
	temp := t.TempDir()
	routePath := writeSemanticRouteConfig(t, temp)
	invalidRoutePath := filepath.Join(temp, "invalid-route.json")
	if err := os.WriteFile(invalidRoutePath, []byte(`{"routes":{}}`), 0o600); err != nil {
		t.Fatalf("write invalid route: %v", err)
	}
	for _, test := range []struct {
		name string
		key  string
		args []string
	}{
		{name: "missing approval", key: "key", args: []string{
			"gateway", "check", "--route-config", routePath,
		}},
		{name: "missing key", args: []string{
			"gateway", "check", "--allow-remote", "--route-config", routePath,
		}},
		{name: "missing route", key: "key", args: []string{
			"gateway", "check", "--allow-remote",
		}},
		{name: "invalid route", key: "key", args: []string{
			"gateway", "check", "--allow-remote", "--route-config", invalidRoutePath,
		}},
		{name: "unexpected argument", key: "key", args: []string{
			"gateway", "check", "--allow-remote", "--route-config", routePath, "extra",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AI_GATEWAY_API_KEY", test.key)
			constructed := false
			dependencies := commandDependencies{
				newSemanticGenerator: func(aigateway.Route, string) (application.SemanticGenerator, error) {
					constructed = true
					return nil, errors.New("unexpected construction")
				},
			}
			err := runWithDependencies(
				context.Background(), test.args, &bytes.Buffer{}, &bytes.Buffer{}, dependencies,
			)
			if err == nil || constructed {
				t.Fatalf("error = %v, constructed = %v", err, constructed)
			}
		})
	}
}

func TestGatewayCheckReportsBoundedMetadataWithoutPersistence(t *testing.T) {
	temp := t.TempDir()
	routePath := writeSemanticRouteConfig(t, temp)
	t.Setenv("AI_GATEWAY_API_KEY", "test-gateway-key")
	inputTokens, outputTokens, totalTokens := 11, 2, 13
	latency := int64(27)
	cost := "0.00001"
	var request application.SemanticGenerationRequest
	dependencies := commandDependencies{
		newSemanticGenerator: func(_ aigateway.Route, key string) (application.SemanticGenerator, error) {
			if key != "test-gateway-key" {
				t.Fatalf("gateway key = %q", key)
			}
			return gatewayCheckGenerator(func(value application.SemanticGenerationRequest) (application.SemanticGenerationResult, error) {
				request = value
				return application.SemanticGenerationResult{
					Candidates: []domain.ClaimCandidate{},
					Model: domain.ModelExecutionMetadata{
						ResolvedProvider: "cerebras", ResolvedModel: "openai/gpt-oss-120b",
						RequestID: "request-check", InputTokens: &inputTokens, OutputTokens: &outputTokens,
						TotalTokens: &totalTokens, LatencyMilliseconds: &latency, CostUSD: &cost,
					},
				}, nil
			}), nil
		},
	}

	var stdout, stderr bytes.Buffer
	if err := runWithDependencies(
		context.Background(),
		[]string{"gateway", "check", "--allow-remote", "--route-config", routePath},
		&stdout, &stderr, dependencies,
	); err != nil {
		t.Fatalf("run gateway check: %v; stderr: %s", err, stderr.String())
	}
	var output gatewayCheckOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode gateway output: %v", err)
	}
	if !output.Success || output.CandidateCount != 0 ||
		output.Schema.Name != "semantic-claim-candidates" ||
		output.ResolvedProvider != "cerebras" || output.ResolvedModel != "openai/gpt-oss-120b" ||
		output.RequestID != "request-check" || output.CostUSD == nil || *output.CostUSD != cost ||
		len(output.RouteDigest) != 64 || !json.Valid(output.RouteConfig) {
		t.Fatalf("gateway output = %#v", output)
	}
	if request.PromptVersion != application.SemanticPromptVersion ||
		len(request.Input.Entries) != 0 || len(request.Input.Facts) != 0 {
		t.Fatalf("gateway request = %#v", request)
	}
	encodedOutput := stdout.String()
	if strings.Contains(encodedOutput, "test-gateway-key") ||
		strings.Contains(encodedOutput, request.Instructions) {
		t.Fatalf("gateway output exposed protected inputs: %s", encodedOutput)
	}
	entries, err := os.ReadDir(temp)
	if err != nil {
		t.Fatalf("read temp directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".db") {
			t.Fatalf("gateway check created database %q", entry.Name())
		}
	}
}

func TestGatewayCheckReturnsOnlySanitizedProviderCategory(t *testing.T) {
	temp := t.TempDir()
	routePath := writeSemanticRouteConfig(t, temp)
	t.Setenv("AI_GATEWAY_API_KEY", "test-gateway-key")
	protected := "private provider detail " + "ghp_" + strings.Repeat("z", 24)
	dependencies := commandDependencies{
		newSemanticGenerator: func(aigateway.Route, string) (application.SemanticGenerator, error) {
			return gatewayCheckGenerator(func(application.SemanticGenerationRequest) (application.SemanticGenerationResult, error) {
				return application.SemanticGenerationResult{}, gatewayCheckTestError{
					category: application.SemanticGenerationFailurePermission,
					detail:   protected,
				}
			}), nil
		},
	}

	var stdout, stderr bytes.Buffer
	err := runWithDependencies(
		context.Background(),
		[]string{"gateway", "check", "--allow-remote", "--route-config", routePath},
		&stdout, &stderr, dependencies,
	)
	if err == nil || err.Error() != application.SemanticGenerationFailurePermission ||
		strings.Contains(err.Error(), protected) || stdout.Len() != 0 {
		t.Fatalf("error = %v; stdout = %q", err, stdout.String())
	}
}

type gatewayCheckGenerator func(
	application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error)

func (generator gatewayCheckGenerator) Generate(
	_ context.Context,
	request application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	return generator(request)
}

type gatewayCheckTestError struct {
	category string
	detail   string
}

func (failure gatewayCheckTestError) Error() string {
	return failure.detail
}

func (failure gatewayCheckTestError) SemanticGenerationFailureCategory() string {
	return failure.category
}
