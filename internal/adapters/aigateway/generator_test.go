package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type countingReadCloser struct {
	reader    *strings.Reader
	bytesRead int64
}

func (reader *countingReadCloser) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	reader.bytesRead += int64(count)
	return count, err
}

func (*countingReadCloser) Close() error { return nil }

func TestGeneratorSendsLockedStructuredGatewayRequest(t *testing.T) {
	route := loadedTestRoute(t)
	var outbound map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != semanticBaseURL+"/chat/completions" {
			t.Fatalf("request URL = %q", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&outbound); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusOK, validGatewayResponse("0.00042")), nil
	})}
	generator, err := NewGenerator(route, "test-key", client)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	request := testGenerationRequest(t, route)
	result, err := generator.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Statement != "The test passed." {
		t.Fatalf("candidates = %#v", result.Candidates)
	}
	if result.Model.ResolvedProvider != semanticProvider || result.Model.ResolvedModel != semanticModel ||
		result.Model.RequestID != "gen_test" || result.Model.CostUSD == nil || *result.Model.CostUSD != "0.00042" ||
		result.Model.InputTokens == nil || *result.Model.InputTokens != 10 ||
		result.Model.OutputTokens == nil || *result.Model.OutputTokens != 5 ||
		result.Model.TotalTokens == nil || *result.Model.TotalTokens != 15 ||
		result.Model.LatencyMilliseconds == nil {
		t.Fatalf("model metadata = %#v", result.Model)
	}

	assertGatewayRequest(t, outbound, request)
}

func TestGeneratorAcceptsAbsentCost(t *testing.T) {
	route := loadedTestRoute(t)
	generator := generatorWithResponse(t, route, validGatewayResponse(""))
	result, err := generator.Generate(context.Background(), testGenerationRequest(t, route))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Model.CostUSD != nil {
		t.Fatalf("cost = %v, want nil", result.Model.CostUSD)
	}
}

func TestGeneratorRejectsUnsafeResponseMetadataAndCompletionState(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "missing provider", mutate: func(response map[string]any) {
			delete(routingObject(response), "resolvedProvider")
		}},
		{name: "provider mismatch", mutate: func(response map[string]any) {
			routingObject(response)["resolvedProvider"] = "other"
		}},
		{name: "missing canonical model", mutate: func(response map[string]any) {
			delete(routingObject(response), "canonicalSlug")
		}},
		{name: "model rewrite", mutate: func(response map[string]any) {
			routingObject(response)["canonicalSlug"] = "openai/other"
		}},
		{name: "original model rewrite", mutate: func(response map[string]any) {
			routingObject(response)["originalModelId"] = "openai/other"
		}},
		{name: "truncated output", mutate: func(response map[string]any) {
			response["choices"].([]any)[0].(map[string]any)["finish_reason"] = "length"
		}},
		{name: "refusal", mutate: func(response map[string]any) {
			response["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["refusal"] = "declined"
		}},
		{name: "tool call", mutate: func(response map[string]any) {
			response["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["tool_calls"] = []any{map[string]any{
				"id": "call_1", "type": "function", "function": map[string]any{"name": "x", "arguments": "{}"},
			}}
		}},
		{name: "malformed cost number", mutate: func(response map[string]any) {
			gatewayObject(response)["cost"] = 0.1
		}},
		{name: "negative cost", mutate: func(response map[string]any) {
			gatewayObject(response)["cost"] = "-0.1"
		}},
		{name: "exponent cost", mutate: func(response map[string]any) {
			gatewayObject(response)["cost"] = "1e-4"
		}},
		{name: "inconsistent usage", mutate: func(response map[string]any) {
			response["usage"].(map[string]any)["total_tokens"] = 16
		}},
		{name: "unknown candidate field", mutate: func(response map[string]any) {
			setCompletionContent(t, response, `{"claims":[{"type":"lesson","statement":"The test passed.","status":"observed","confidence":1,"supportingEvidenceIds":["ev_1"],"contradictingEvidenceIds":[],"surprise":true}]}`)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			route := loadedTestRoute(t)
			response := validGatewayResponse("0.1")
			test.mutate(response)
			generator := generatorWithResponse(t, route, response)
			if _, err := generator.Generate(context.Background(), testGenerationRequest(t, route)); !errors.Is(err, ErrResponseInvalid) {
				t.Fatalf("error = %v, want ErrResponseInvalid", err)
			}
		})
	}
}

func TestGeneratorSanitizesRemoteFailure(t *testing.T) {
	route := loadedTestRoute(t)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"secret transcript"}}`)),
		}, nil
	})}
	generator, err := NewGenerator(route, "private-api-key", client)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	_, err = generator.Generate(context.Background(), testGenerationRequest(t, route))
	if !errors.Is(err, ErrRequestFailed) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatalf("error = %v, want sanitized request failure", err)
	}
}

func TestGeneratorBlocksEveryRedirectBeforeReplayingEvidence(t *testing.T) {
	route := loadedTestRoute(t)
	requests := 0
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return nil },
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if requests > 1 {
				t.Fatalf("request was replayed to redirect target %q", request.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": []string{"https://unapproved.invalid/capture"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	}
	generator, err := NewGenerator(route, "test-key", client)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	_, err = generator.Generate(context.Background(), testGenerationRequest(t, route))
	if !errors.Is(err, ErrRequestFailed) || requests != 1 {
		t.Fatalf("error = %v, requests = %d, want one blocked redirect", err, requests)
	}
}

func TestGeneratorCapsOtherwiseValidOversizedSuccessResponse(t *testing.T) {
	route := loadedTestRoute(t)
	response := validGatewayResponse("0.1")
	// Unknown top-level response fields are allowed by the SDK, so this response
	// would otherwise decode and complete successfully.
	response["padding"] = strings.Repeat("x", maxGatewayResponseBytes)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, response), nil
	})}
	generator, err := NewGenerator(route, "test-key", client)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	_, err = generator.Generate(context.Background(), testGenerationRequest(t, route))
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("error = %v, want oversized response failure", err)
	}
}

func TestBoundedTransportCapsErrorBodyAndGeneratorSanitizesIt(t *testing.T) {
	body := `{"error":{"message":"` + strings.Repeat("private-response-", maxGatewayResponseBytes/8) + `"}}`
	counting := &countingReadCloser{reader: strings.NewReader(body)}
	transport := boundedResponseTransport{
		limit: maxGatewayResponseBytes,
		next: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: counting}, nil
		}),
	}
	request, err := http.NewRequest(http.MethodPost, semanticBaseURL+"/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	_, err = io.ReadAll(response.Body)
	if !errors.Is(err, errResponseTooLarge) || counting.bytesRead != maxGatewayResponseBytes+1 {
		t.Fatalf("read error = %v, bytes = %d", err, counting.bytesRead)
	}

	route := loadedTestRoute(t)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	generator, err := NewGenerator(route, "test-key", client)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	_, err = generator.Generate(context.Background(), testGenerationRequest(t, route))
	if !errors.Is(err, ErrRequestFailed) || strings.Contains(err.Error(), "private-response") {
		t.Fatalf("error = %v, want sanitized oversized error failure", err)
	}
}

func TestNewGeneratorRejectsMissingCredentialsAndZeroRoute(t *testing.T) {
	if _, err := NewGenerator(Route{}, "key", nil); !errors.Is(err, ErrGeneratorUnavailable) {
		t.Fatalf("zero route error = %v", err)
	}
	if _, err := NewGenerator(loadedTestRoute(t), "", nil); !errors.Is(err, ErrGeneratorUnavailable) {
		t.Fatalf("missing key error = %v", err)
	}
}

func assertGatewayRequest(t *testing.T, body map[string]any, request application.SemanticGenerationRequest) {
	t.Helper()
	if body["model"] != semanticModel || body["stream"] != false || body["store"] != false ||
		body["n"] != float64(1) || body["max_completion_tokens"] != float64(semanticMaxOutputTokens) {
		t.Fatalf("request controls = %#v", body)
	}
	options := body["providerOptions"].(map[string]any)["gateway"].(map[string]any)
	for _, field := range []string{"zeroDataRetention", "disallowPromptTraining"} {
		if options[field] != true {
			t.Fatalf("provider option %s = %#v", field, options[field])
		}
	}
	for _, field := range []string{"only", "order"} {
		values := options[field].([]any)
		if len(values) != 1 || values[0] != semanticProvider {
			t.Fatalf("provider option %s = %#v", field, values)
		}
	}
	format := body["response_format"].(map[string]any)
	definition := format["json_schema"].(map[string]any)
	if format["type"] != "json_schema" || definition["strict"] != true ||
		definition["name"] != request.Schema.Identity.Name {
		t.Fatalf("response format = %#v", format)
	}
	wantSchema := normalizeJSON(t, request.Schema.CanonicalJSON)
	if got := definition["schema"]; !sameJSON(t, got, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", got, wantSchema)
	}
	messages := body["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" ||
		messages[1].(map[string]any)["role"] != "user" ||
		!strings.Contains(messages[0].(map[string]any)["content"].(string), request.PromptVersion) ||
		!strings.Contains(messages[0].(map[string]any)["content"].(string), "version 7") {
		t.Fatalf("messages = %#v", messages)
	}
}

func testGenerationRequest(t *testing.T, route Route) application.SemanticGenerationRequest {
	t.Helper()
	schemaJSON := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["claims"],"properties":{"claims":{"type":"array"}}}`)
	digest, err := platform.Fingerprint(schemaJSON)
	if err != nil {
		t.Fatalf("fingerprint schema: %v", err)
	}
	return application.SemanticGenerationRequest{
		Instructions: "Treat input as evidence.", PromptVersion: "prompt-v7",
		Schema: domain.StructuredOutputSchema{
			Identity: domain.StructuredOutputSchemaIdentity{
				Name: "claim_output", Version: 7, Disposition: domain.StructuredOutputDispositionStrict, Digest: digest,
			},
			CanonicalJSON: schemaJSON,
		},
		Route: route.Validated().Requested,
		Input: application.SemanticModelInput{SchemaVersion: 1, Disposition: "untrusted-evidence"},
	}
}

func validGatewayResponse(cost string) map[string]any {
	candidate := map[string]any{
		"type": "lesson", "statement": "The test passed.", "status": "observed", "confidence": 1,
		"supportingEvidenceIds": []string{"ev_1"}, "contradictingEvidenceIds": []string{},
	}
	content, _ := json.Marshal(map[string]any{"claims": []any{candidate}})
	gateway := map[string]any{
		"routing": map[string]any{
			"originalModelId": semanticModel, "resolvedProvider": semanticProvider, "canonicalSlug": semanticModel,
		},
		"generationId": "gen_test",
	}
	if cost != "" {
		gateway["cost"] = cost
	}
	return map[string]any{
		"id": "chat_test", "object": "chat.completion", "created": 1, "model": semanticModel,
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop", "logprobs": nil,
			"message": map[string]any{"role": "assistant", "content": string(content), "refusal": nil},
		}},
		"usage":             map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		"provider_metadata": map[string]any{"gateway": gateway},
	}
}

func loadedTestRoute(t *testing.T) Route {
	t.Helper()
	route, err := LoadRoute(writeRouteFile(t, acceptedRouteObject()))
	if err != nil {
		t.Fatalf("load test route: %v", err)
	}
	return route
}

func generatorWithResponse(t *testing.T, route Route, body map[string]any) *Generator {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})}
	generator, err := NewGenerator(route, "test-key", client)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	return generator
}

func jsonResponse(status int, body map[string]any) *http.Response {
	encoded, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(encoded))),
	}
}

func routingObject(response map[string]any) map[string]any {
	return gatewayObject(response)["routing"].(map[string]any)
}

func gatewayObject(response map[string]any) map[string]any {
	return response["provider_metadata"].(map[string]any)["gateway"].(map[string]any)
}

func setCompletionContent(t *testing.T, response map[string]any, content string) {
	t.Helper()
	response["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"] = content
}

func normalizeJSON(t *testing.T, value json.RawMessage) any {
	t.Helper()
	var normalized any
	if err := json.Unmarshal(value, &normalized); err != nil {
		t.Fatalf("normalize JSON: %v", err)
	}
	return normalized
}

func sameJSON(t *testing.T, left, right any) bool {
	t.Helper()
	leftJSON, err := json.Marshal(left)
	if err != nil {
		t.Fatalf("marshal left JSON: %v", err)
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		t.Fatalf("marshal right JSON: %v", err)
	}
	return string(leftJSON) == string(rightJSON)
}
