package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ferueda/noema/internal/adapters/aigateway"
	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func TestRemoteSemanticAnalysisEndToEndThroughCLI(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	databasePath, exportPath := prepareFakeSessions(t, temp)
	writeExportFixture(t, exportPath, strings.Repeat("d", 64))
	fact := runScanForTest(t, ctx, databasePath)
	routePath := writeSemanticRouteConfig(t, temp)
	t.Setenv("AI_GATEWAY_API_KEY", "test-gateway-key")

	var gatewayCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gatewayCalls.Add(1)
		if err := serveSemanticGateway(writer, request); err != nil {
			t.Error(err)
			http.Error(writer, "invalid test request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	dependencies := gatewayTestDependencies(t, server.URL)

	first := runRemoteClaimsForTest(t, ctx, dependencies, fact.AnalysisID, routePath, databasePath)
	if first.Reused || first.ClaimCount != 2 || first.Coverage != domain.CoverageCompleteRetainedSnapshot ||
		first.Model != "openai/gpt-oss-120b" || first.Provider != "cerebras" {
		t.Fatalf("first semantic run = %#v", first)
	}
	shown, _ := showSemanticForCLI(t, ctx, databasePath, first.AnalysisID, false)
	if shown.Details.Model == nil || shown.Details.Model.CostUSD == nil || *shown.Details.Model.CostUSD != "0.00042" ||
		len(shown.Analysis.Claims) != 2 || shown.Analysis.Claims[0].Type != domain.ClaimTypeProblem ||
		shown.Analysis.Claims[1].Type != domain.ClaimTypeLesson {
		t.Fatalf("stored semantic analysis = %#v", shown)
	}
	resolved, _ := showSemanticForCLI(t, ctx, databasePath, first.AnalysisID, true)
	if len(resolved.Analysis.Claims) != 2 {
		t.Fatalf("resolved semantic analysis = %#v", resolved.Analysis)
	}

	second := runRemoteClaimsForTest(t, ctx, dependencies, fact.AnalysisID, routePath, databasePath)
	if !second.Reused || second.AnalysisID != first.AnalysisID || gatewayCalls.Load() != 1 {
		t.Fatalf("second semantic run = %#v, gateway calls = %d", second, gatewayCalls.Load())
	}
	assertRemoteSemanticRows(t, ctx, databasePath)

	writeExportFixture(t, exportPath, strings.Repeat("e", 64))
	var stdout, stderr bytes.Buffer
	err := runWithDependencies(ctx, remoteClaimsArgs(fact.AnalysisID, routePath, databasePath), &stdout, &stderr, dependencies)
	var analysisError application.AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Category != "source-revision-unavailable" || gatewayCalls.Load() != 1 {
		t.Fatalf("changed digest error = %v, gateway calls = %d", err, gatewayCalls.Load())
	}
}

func TestAnalyzeClaimsRejectsRemoteGatesBeforeConstruction(t *testing.T) {
	temp := t.TempDir()
	routePath := writeSemanticRouteConfig(t, temp)
	databasePath := filepath.Join(temp, "noema.db")
	for _, test := range []struct {
		name string
		key  string
		args []string
	}{
		{name: "missing approval", key: "key", args: []string{
			"analyze", "claims", "fact-id", "--route-config", routePath, "--database", databasePath,
		}},
		{name: "missing key", args: []string{
			"analyze", "claims", "fact-id", "--allow-remote", "--route-config", routePath, "--database", databasePath,
		}},
		{name: "missing route", key: "key", args: []string{
			"analyze", "claims", "fact-id", "--allow-remote", "--database", databasePath,
		}},
		{name: "unpaired bounds", key: "key", args: []string{
			"analyze", "claims", "fact-id", "--allow-remote", "--route-config", routePath,
			"--first-entry", "0", "--database", databasePath,
		}},
		{name: "invalid bounds", key: "key", args: []string{
			"analyze", "claims", "fact-id", "--allow-remote", "--route-config", routePath,
			"--first-entry", "2", "--last-entry", "1", "--database", databasePath,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AI_GATEWAY_API_KEY", test.key)
			constructed := false
			dependencies := commandDependencies{newSemanticGenerator: func(aigateway.Route, string) (application.SemanticGenerator, error) {
				constructed = true
				return nil, errors.New("unexpected construction")
			}}
			err := runWithDependencies(context.Background(), test.args, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
			if err == nil || constructed {
				t.Fatalf("error = %v, constructed = %v", err, constructed)
			}
		})
	}
}

func TestAnalyzeClaimsRejectsImplicitOversizeBeforeGateway(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	databasePath, exportPath := prepareFakeSessions(t, temp)
	writeCommandOnlyFixture(t, exportPath, strings.Repeat("d", 64), commandFixtures(51, 16))
	fact := runScanForTest(t, ctx, databasePath)
	routePath := writeSemanticRouteConfig(t, temp)
	t.Setenv("AI_GATEWAY_API_KEY", "test-gateway-key")

	var gatewayCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gatewayCalls.Add(1)
		if err := serveSemanticGateway(writer, request); err != nil {
			t.Error(err)
			http.Error(writer, "invalid test request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	dependencies := gatewayTestDependencies(t, server.URL)
	var stdout, stderr bytes.Buffer
	err := runWithDependencies(ctx, remoteClaimsArgs(fact.AnalysisID, routePath, databasePath), &stdout, &stderr, dependencies)
	var analysisError application.AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Category != "semantic-input-too-large" || gatewayCalls.Load() != 0 {
		t.Fatalf("implicit oversize error = %v, gateway calls = %d", err, gatewayCalls.Load())
	}

	args := remoteClaimsArgs(fact.AnalysisID, routePath, databasePath)
	args = append(args, "--first-entry", "0", "--last-entry", "49")
	stdout.Reset()
	stderr.Reset()
	if err := runWithDependencies(ctx, args, &stdout, &stderr, dependencies); err != nil {
		t.Fatalf("explicit bounded analysis: %v; stderr: %s", err, stderr.String())
	}
	var output remoteClaimsOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode bounded output: %v", err)
	}
	if output.Coverage != "partial" || gatewayCalls.Load() != 1 {
		t.Fatalf("bounded output = %#v, gateway calls = %d", output, gatewayCalls.Load())
	}
}

type remoteClaimsOutput struct {
	AnalysisID string `json:"analysisId"`
	Reused     bool   `json:"reused"`
	Coverage   string `json:"coverage"`
	ClaimCount int    `json:"claimCount"`
	Model      string `json:"model"`
	Provider   string `json:"provider"`
}

func runRemoteClaimsForTest(
	t *testing.T,
	ctx context.Context,
	dependencies commandDependencies,
	factAnalysisID, routePath, databasePath string,
) remoteClaimsOutput {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := runWithDependencies(
		ctx, remoteClaimsArgs(factAnalysisID, routePath, databasePath), &stdout, &stderr, dependencies,
	); err != nil {
		t.Fatalf("run remote claims: %v; stderr: %s", err, stderr.String())
	}
	var output remoteClaimsOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode remote claims output: %v", err)
	}
	return output
}

func remoteClaimsArgs(factAnalysisID, routePath, databasePath string) []string {
	return []string{
		"analyze", "claims", factAnalysisID, "--allow-remote",
		"--route-config", routePath, "--database", databasePath,
	}
}

func prepareFakeSessions(t *testing.T, temp string) (databasePath, exportPath string) {
	t.Helper()
	databasePath = filepath.Join(temp, "noema.db")
	exportPath = filepath.Join(temp, "export.jsonl")
	executable := filepath.Join(temp, "sessions")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec /bin/cat \"$NOEMA_FAKE_EXPORT\"\n"), 0o700); err != nil {
		t.Fatalf("write fake Sessions executable: %v", err)
	}
	t.Setenv("NOEMA_SESSIONS_COMMAND", executable)
	t.Setenv("NOEMA_FAKE_EXPORT", exportPath)
	return databasePath, exportPath
}

func gatewayTestDependencies(t *testing.T, serverURL string) commandDependencies {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse gateway server URL: %v", err)
	}
	client := &http.Client{Transport: gatewayRewriteTransport{
		target: target, next: http.DefaultTransport,
	}}
	return commandDependencies{
		newSemanticGenerator: func(route aigateway.Route, apiKey string) (application.SemanticGenerator, error) {
			return aigateway.NewGenerator(route, apiKey, client)
		},
	}
}

type gatewayRewriteTransport struct {
	target *url.URL
	next   http.RoundTripper
}

func (transport gatewayRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "ai-gateway.vercel.sh" ||
		request.URL.Path != "/v1/chat/completions" {
		return nil, errors.New("request did not target the locked Gateway URL")
	}
	copy := request.Clone(request.Context())
	urlCopy := *request.URL
	urlCopy.Scheme = transport.target.Scheme
	urlCopy.Host = transport.target.Host
	copy.URL = &urlCopy
	return transport.next.RoundTrip(copy)
}

func serveSemanticGateway(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer test-gateway-key" {
		return errors.New("unexpected gateway method or authorization")
	}
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Messages) != 2 {
		return errors.New("invalid gateway request messages")
	}
	var input application.SemanticModelInput
	if err := json.Unmarshal([]byte(body.Messages[1].Content), &input); err != nil || len(input.Entries) == 0 {
		return errors.New("invalid semantic input")
	}
	first := input.Entries[0].Segments[0].EvidenceID
	second := first
	if len(input.Entries) > 1 {
		second = input.Entries[1].Segments[0].EvidenceID
	}
	candidates := []map[string]any{
		semanticCandidate("problem", "A test command required a durable record.", "observed", first),
		semanticCandidate("lesson", "Passing test output can support a reusable lesson.", "inferred", second),
	}
	content, err := json.Marshal(map[string]any{"claims": candidates})
	if err != nil {
		return errors.New("encode candidate content")
	}
	response := map[string]any{
		"id": "chat_cli_test", "object": "chat.completion", "created": 1, "model": "openai/gpt-oss-120b",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop", "logprobs": nil,
			"message": map[string]any{"role": "assistant", "content": string(content), "refusal": nil},
		}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150},
		"provider_metadata": map[string]any{"gateway": map[string]any{
			"routing": map[string]any{
				"originalModelId": "openai/gpt-oss-120b", "resolvedProvider": "cerebras",
				"canonicalSlug": "openai/gpt-oss-120b",
			},
			"generationId": "gen_cli_test", "cost": "0.00042",
		}},
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		return errors.New("encode gateway response")
	}
	return nil
}

func semanticCandidate(claimType, statement, status, evidenceID string) map[string]any {
	return map[string]any{
		"type": claimType, "statement": statement, "status": status, "confidence": 0.9,
		"supportingEvidenceIds": []string{evidenceID}, "contradictingEvidenceIds": []string{},
		"supportingFactIds": nil, "outcome": nil, "actor": nil, "origin": nil,
		"subject": nil, "scope": nil, "attribution": "unknown",
	}
}

func writeSemanticRouteConfig(t *testing.T, directory string) string {
	t.Helper()
	config := map[string]any{"routes": map[string]any{"semantic-v1": map[string]any{
		"gateway": "vercel-ai-gateway", "baseUrl": "https://ai-gateway.vercel.sh/v1",
		"model": "openai/gpt-oss-120b", "providerAllowlist": []string{"cerebras"},
		"providerOrder": []string{"cerebras"}, "requiredCapabilities": []string{"strict-json-schema"},
		"zeroDataRetention": true, "disallowPromptTraining": true, "timeoutMilliseconds": 60000,
		"maxOutputTokens": 4096, "maxRetries": 0, "routeVersion": "route-v1",
		"privacyPolicyVersion": "deterministic-privacy-v1",
	}}}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal route config: %v", err)
	}
	path := filepath.Join(directory, "semantic-route.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write route config: %v", err)
	}
	return path
}

func assertRemoteSemanticRows(t *testing.T, ctx context.Context, databasePath string) {
	t.Helper()
	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open semantic database: %v", err)
	}
	defer database.Close()
	want := map[string]int{"claims": 2, "events": 3, "observations": 0, "jobs": 0, "agent_runs": 0, "content_ideas": 0}
	for table, wantCount := range want {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != wantCount {
			t.Fatalf("%s count = %d, want %d", table, count, wantCount)
		}
	}
}
