package eve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

const (
	testPassword        = "synthetic-route-password"
	testObservedModelID = "gateway/openai/gpt-5.4-mini"
)

func TestNewExecutorRequiresLoopbackPortAndPassword(t *testing.T) {
	t.Parallel()
	tests := []Config{
		{
			BaseURL: "https://example.com:443", RoutePassword: testPassword,
			ObservedModelID: testObservedModelID,
		},
		{
			BaseURL: "http://127.0.0.1", RoutePassword: testPassword,
			ObservedModelID: testObservedModelID,
		},
		{
			BaseURL: "http://127.0.0.1:3000/path", RoutePassword: testPassword,
			ObservedModelID: testObservedModelID,
		},
		{
			BaseURL: "http://127.0.0.1:3000", RoutePassword: "",
			ObservedModelID: testObservedModelID,
		},
		{
			BaseURL: "http://127.0.0.1:3000", RoutePassword: "   ",
			ObservedModelID: testObservedModelID,
		},
		{BaseURL: "http://127.0.0.1:3000", RoutePassword: testPassword},
	}
	for _, test := range tests {
		test := test
		t.Run(test.BaseURL, func(t *testing.T) {
			t.Parallel()
			if _, err := NewExecutor(test); err == nil {
				t.Fatal("NewExecutor() error = nil, want configuration error")
			}
		})
	}
}

func TestPreflightValidatesObservableAgentConfiguration(t *testing.T) {
	t.Parallel()
	instructions := "Use only the supplied generalized evidence."
	expectation := testInfoExpectation(instructions)
	var healthAuthorization string
	var infoUser, infoPassword string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/eve/v1/health":
			healthAuthorization = request.Header.Get("Authorization")
			writeJSON(writer, http.StatusOK, map[string]any{
				"ok": true, "status": "ready", "workflowId": "workflow_synthetic",
			})
		case "/eve/v1/info":
			infoUser, infoPassword, _ = request.BasicAuth()
			writeJSON(writer, http.StatusOK, testInfoDocument(instructions))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	executor := mustExecutor(t, server.URL)
	result, err := executor.Preflight(context.Background(), expectation)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.AgentName != expectation.AgentName || result.ModelID != testObservedModelID {
		t.Fatalf("Preflight() = %#v, want expected identity", result)
	}
	if healthAuthorization != "" {
		t.Fatal("health probe unexpectedly carried authorization")
	}
	if infoUser != routeUsername || infoPassword != testPassword {
		t.Fatal("info probe did not carry route Basic auth")
	}
	if result.ModelID == testExecutionRequest(t).Configuration.Route.Model {
		t.Fatal("preflight conflated observed and authored model identities")
	}
}

func TestPreflightRejectsMismatchWithoutLeakingInfo(t *testing.T) {
	t.Parallel()
	const privateText = "private instructions that must not escape"
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/eve/v1/health":
			writeJSON(writer, http.StatusOK, map[string]any{
				"ok": true, "status": "ready", "workflowId": "workflow_synthetic",
			})
		case "/eve/v1/info":
			writeJSON(writer, http.StatusOK, testInfoDocument(privateText))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	_, err := mustExecutor(t, server.URL).Preflight(
		context.Background(),
		testInfoExpectation("different checked-in instructions"),
	)
	if !errors.Is(err, ErrMismatch) ||
		strings.Contains(err.Error(), privateText) ||
		strings.Contains(err.Error(), testPassword) {
		t.Fatalf("Preflight() error = %q, want sanitized mismatch", err)
	}
}

func TestExecuteConsumesSyntheticPinnedSuccessSequence(t *testing.T) {
	t.Parallel()
	request := testExecutionRequest(t)
	schema := testOutputSchema(t)
	var observedMessage string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		httpRequest *http.Request,
	) {
		requireProtected(t, httpRequest)
		switch httpRequest.URL.Path {
		case "/eve/v1/session":
			var body struct {
				Message      string          `json:"message"`
				OutputSchema json.RawMessage `json:"outputSchema"`
			}
			if err := json.NewDecoder(httpRequest.Body).Decode(&body); err != nil {
				t.Errorf("decode session request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			observedMessage = body.Message
			if !bytes.Equal(compactJSON(t, body.OutputSchema), compactJSON(t, schema.CanonicalJSON)) {
				t.Error("session output schema differs from required schema")
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("x-eve-session-id", "session_synthetic")
			writer.WriteHeader(http.StatusAccepted)
			_, _ = writer.Write([]byte(
				`{"continuationToken":"continuation_synthetic","ok":true,"sessionId":"session_synthetic"}`,
			))
		case "/eve/v1/session/session_synthetic/stream":
			if httpRequest.URL.Query().Get("startIndex") != "0" {
				t.Errorf("startIndex = %q, want 0", httpRequest.URL.Query().Get("startIndex"))
			}
			writeStreamHeaders(writer, "session_synthetic")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(syntheticSuccessStream(t, observedMessage, request))
		default:
			http.NotFound(writer, httpRequest)
		}
	}))
	defer server.Close()

	response, err := mustExecutor(t, server.URL).Execute(context.Background(), request, schema)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(response.CandidateJSON) != `{"ideas":[]}` {
		t.Fatalf("CandidateJSON = %s, want empty synthetic candidates", response.CandidateJSON)
	}
	receipt := response.Receipt
	if receipt.SessionID != "session_synthetic" ||
		receipt.TurnID != "turn_synthetic" ||
		receipt.CompletedModelSteps != 1 ||
		receipt.GatewayGenerationID != "generation_synthetic" ||
		receipt.Usage == nil ||
		receipt.Usage.TotalTokens != 440 ||
		receipt.CostUSD != "0.00042" ||
		receipt.RequestedRoute == nil ||
		*receipt.RequestedRoute != request.Configuration.Route {
		t.Fatalf("receipt = %#v, want complete observed metadata", receipt)
	}
}

func TestExecuteRecognizesSyntheticFailureCascadeAndSanitizesMessage(t *testing.T) {
	t.Parallel()
	const remotePrivateMessage = "customer-private remote failure"
	request := testExecutionRequest(t)
	schema := testOutputSchema(t)
	server := newExecutionServer(t, request, func(t *testing.T, message string) []byte {
		events := syntheticSuccessEvents(message, request)
		events = events[:4]
		events = append(events,
			event("step.failed", map[string]any{
				"code": "model-failed", "message": remotePrivateMessage,
				"sequence": 0, "stepIndex": 0, "turnId": "turn_synthetic",
			}, 4),
			event("turn.failed", map[string]any{
				"code": "turn-failed", "message": remotePrivateMessage,
				"sequence": 0, "turnId": "turn_synthetic",
			}, 5),
			event("session.failed", map[string]any{
				"code": "session-failed", "message": remotePrivateMessage,
				"sessionId": "session_synthetic",
			}, 6),
		)
		return encodeEvents(t, events)
	})
	defer server.Close()

	response, err := mustExecutor(t, server.URL).Execute(context.Background(), request, schema)
	if !errors.Is(err, ErrExecutionFailed) ||
		FailureCategory(err) != domain.AgentFailureCategoryExecutorFailed {
		t.Fatalf("Execute() error = %v, want categorized execution failure", err)
	}
	if strings.Contains(err.Error(), remotePrivateMessage) ||
		strings.Contains(err.Error(), testPassword) {
		t.Fatalf("Execute() leaked sensitive value in %q", err)
	}
	if response.Receipt.SessionID != "session_synthetic" ||
		response.Receipt.TurnID != "turn_synthetic" ||
		response.Receipt.FailureCategory != domain.AgentFailureCategoryExecutorFailed {
		t.Fatalf("partial receipt = %#v, want safely observed fields", response.Receipt)
	}
}

func TestExecuteRejectsForbiddenAndMalformedSyntheticStreams(t *testing.T) {
	t.Parallel()
	request := testExecutionRequest(t)
	schema := testOutputSchema(t)
	tests := map[string]func([]map[string]any) []map[string]any{
		"forbidden action": func(events []map[string]any) []map[string]any {
			events[4] = event("actions.requested", map[string]any{
				"actions": []any{}, "sequence": 0, "stepIndex": 0,
				"turnId": "turn_synthetic",
			}, 4)
			return events
		},
		"wrong turn": func(events []map[string]any) []map[string]any {
			events[3]["data"].(map[string]any)["turnId"] = "different_turn"
			return events
		},
		"duplicate result": func(events []map[string]any) []map[string]any {
			duplicate := cloneEvent(t, events[6])
			return append(events[:7], append([]map[string]any{duplicate}, events[7:]...)...)
		},
		"unknown envelope field": func(events []map[string]any) []map[string]any {
			events[0]["private"] = "must be rejected"
			return events
		},
		"decreasing timestamp": func(events []map[string]any) []map[string]any {
			events[5]["meta"].(map[string]any)["at"] = "2026-07-28T00:00:00Z"
			return events
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := newExecutionServer(t, request, func(t *testing.T, message string) []byte {
				return encodeEvents(t, mutate(syntheticSuccessEvents(message, request)))
			})
			defer server.Close()
			response, err := mustExecutor(t, server.URL).Execute(
				context.Background(),
				request,
				schema,
			)
			if !errors.Is(err, ErrProtocol) ||
				FailureCategory(err) != domain.AgentFailureCategoryExecutorProtocol {
				t.Fatalf("Execute() error = %v, want protocol failure", err)
			}
			if response.Receipt.FailureCategory != domain.AgentFailureCategoryExecutorProtocol {
				t.Fatalf("receipt = %#v, want protocol failure category", response.Receipt)
			}
		})
	}
}

func TestExecuteRejectsObservedRuntimeModelMismatch(t *testing.T) {
	t.Parallel()
	request := testExecutionRequest(t)
	server := newExecutionServer(t, request, func(t *testing.T, message string) []byte {
		events := syntheticSuccessEvents(message, request)
		runtime := events[0]["data"].(map[string]any)["runtime"].(map[string]any)
		runtime["modelId"] = request.Configuration.Route.Model
		return encodeEvents(t, events)
	})
	defer server.Close()

	response, err := mustExecutor(t, server.URL).Execute(
		context.Background(),
		request,
		testOutputSchema(t),
	)
	if !errors.Is(err, ErrMismatch) ||
		FailureCategory(err) != domain.AgentFailureCategoryExecutorMismatch {
		t.Fatalf("Execute() error = %v, want observed-model mismatch", err)
	}
	if response.Receipt.FailureCategory != domain.AgentFailureCategoryExecutorMismatch {
		t.Fatalf("receipt = %#v, want mismatch category", response.Receipt)
	}
}

func TestExecuteRejectsRedirectAndDoesNotLeakBody(t *testing.T) {
	t.Parallel()
	request := testExecutionRequest(t)
	schema := testOutputSchema(t)
	const privateBody = "private redirect body"
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		writer.Header().Set("Location", "http://127.0.0.1:1/private")
		writer.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = writer.Write([]byte(privateBody))
	}))
	defer server.Close()

	_, err := mustExecutor(t, server.URL).Execute(context.Background(), request, schema)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), privateBody) {
		t.Fatalf("Execute() error = %q, want sanitized unavailable failure", err)
	}
}

func TestStreamParserRejectsOversizedLineBeforeDecoding(t *testing.T) {
	t.Parallel()
	parser := streamParser{}
	_, err := parser.consume(strings.NewReader(strings.Repeat("x", maxStreamLine+1) + "\n"))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("consume() error = %v, want bounded protocol failure", err)
	}
}

func testExecutionRequest(t *testing.T) domain.AgentExecutionRequestV1 {
	t.Helper()
	payload, err := os.ReadFile(
		"../../../contracts/agent-execution/v1/fixtures/request.valid.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	var request domain.AgentExecutionRequestV1
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	request.DeadlineAt = time.Now().Add(time.Minute)
	if err := request.Validate(); err != nil {
		t.Fatalf("synthetic request invalid: %v", err)
	}
	return request
}

func testOutputSchema(t *testing.T) domain.StructuredOutputSchema {
	t.Helper()
	schema, err := application.ContentScoutOutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func newExecutionServer(
	t *testing.T,
	executionRequest domain.AgentExecutionRequestV1,
	stream func(*testing.T, string) []byte,
) *httptest.Server {
	t.Helper()
	var message string
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		requireProtected(t, request)
		switch request.URL.Path {
		case "/eve/v1/session":
			var body struct {
				Message string `json:"message"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode session body: %v", err)
			}
			message = body.Message
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("x-eve-session-id", "session_synthetic")
			writer.WriteHeader(http.StatusAccepted)
			_, _ = writer.Write([]byte(
				`{"continuationToken":"continuation_synthetic","ok":true,"sessionId":"session_synthetic"}`,
			))
		case "/eve/v1/session/session_synthetic/stream":
			writeStreamHeaders(writer, "session_synthetic")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(stream(t, message))
		default:
			http.NotFound(writer, request)
		}
	}))
}

func syntheticSuccessStream(
	t *testing.T,
	message string,
	request domain.AgentExecutionRequestV1,
) []byte {
	t.Helper()
	return encodeEvents(t, syntheticSuccessEvents(message, request))
}

// These are synthetic unit events, not the canary-captured public acceptance
// fixture required before declaring Eve 0.27.8 interoperability proven.
func syntheticSuccessEvents(
	message string,
	request domain.AgentExecutionRequestV1,
) []map[string]any {
	return []map[string]any{
		event("session.started", map[string]any{
			"runtime": map[string]any{
				"agentId": request.Agent.Name, "agentName": request.Agent.Name,
				"eveVersion": ExecutorVersion, "modelId": testObservedModelID,
			},
		}, 0),
		event("turn.started", map[string]any{
			"sequence": 0, "turnId": "turn_synthetic",
		}, 1),
		event("message.received", map[string]any{
			"message": message, "sequence": 0, "turnId": "turn_synthetic",
			"parts": []any{map[string]any{"text": message, "type": "text"}},
		}, 2),
		event("step.started", map[string]any{
			"sequence": 0, "stepIndex": 0, "turnId": "turn_synthetic",
		}, 3),
		event("message.appended", map[string]any{
			"messageDelta": "synthetic", "messageSoFar": "synthetic",
			"sequence": 0, "stepIndex": 0, "turnId": "turn_synthetic",
		}, 4),
		event("step.completed", map[string]any{
			"finishReason": "tool-calls",
			"providerMetadata": map[string]any{
				"gateway": map[string]any{"generationId": "generation_synthetic"},
			},
			"sequence": 0, "stepIndex": 0, "turnId": "turn_synthetic",
			"usage": map[string]any{
				"costUsd": 0.00042, "inputTokens": 300, "outputTokens": 140,
				"cacheReadTokens": 12, "cacheWriteTokens": 3,
			},
		}, 5),
		event("result.completed", map[string]any{
			"result":   map[string]any{"ideas": []any{}},
			"sequence": 0, "stepIndex": 0, "turnId": "turn_synthetic",
		}, 6),
		event("turn.completed", map[string]any{
			"sequence": 0, "turnId": "turn_synthetic",
		}, 7),
		event("session.waiting", map[string]any{
			"continuationToken": "continuation_after_turn",
			"wait":              "next-user-message",
		}, 8),
	}
}

func event(eventType string, data map[string]any, offset int) map[string]any {
	return map[string]any{
		"type": eventType,
		"data": data,
		"meta": map[string]any{
			"at": time.Date(2026, 7, 28, 1, 0, offset, 0, time.UTC).Format(time.RFC3339),
		},
	}
}

func encodeEvents(t *testing.T, events []map[string]any) []byte {
	t.Helper()
	var output bytes.Buffer
	for _, streamEvent := range events {
		payload, err := json.Marshal(streamEvent)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(payload)
		output.WriteByte('\n')
	}
	return output.Bytes()
}

func cloneEvent(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(payload, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func testInfoExpectation(instructions string) InfoExpectation {
	digest := sha256.Sum256([]byte(instructions))
	return InfoExpectation{
		AgentName:                 "content-scout",
		GatewayTarget:             "openai",
		ProviderOptionsJSON:       json.RawMessage(`{"gateway":{"disallowPromptTraining":false,"only":["azure"],"order":["azure"],"serviceTier":"flex","zeroDataRetention":false}}`),
		StaticInstructionsDigest:  hex.EncodeToString(digest[:]),
		AllowedFrameworkToolNames: []string{"__eve_output"},
	}
}

func testInfoDocument(instructions string) map[string]any {
	return map[string]any{
		"kind": "eve-agent-info", "version": 1,
		"agent": map[string]any{
			"name": "content-scout",
			"model": map[string]any{
				"id": testObservedModelID,
				"providerOptions": map[string]any{
					"gateway": map[string]any{
						"disallowPromptTraining": false,
						"only":                   []any{"azure"},
						"order":                  []any{"azure"},
						"serviceTier":            "flex",
						"zeroDataRetention":      false,
					},
				},
				"routing": map[string]any{
					"kind": "gateway", "target": "openai",
				},
			},
		},
		"diagnostics": map[string]any{"discoveryErrors": 0, "discoveryWarnings": 0},
		"instructions": map[string]any{
			"static":  map[string]any{"markdown": instructions},
			"dynamic": []any{},
		},
		"tools": map[string]any{
			"authored": []any{}, "dynamic": []any{},
			"available": []any{
				map[string]any{"name": "__eve_output", "origin": "framework"},
			},
			"framework": []any{
				map[string]any{"name": "__eve_output", "status": "active"},
			},
		},
		"connections": []any{}, "hooks": []any{}, "sandbox": nil,
		"schedules": []any{},
		"skills":    map[string]any{"static": []any{}, "dynamic": []any{}},
		"subagents": map[string]any{"local": []any{}, "total": 0},
		"workflow":  map[string]any{"enabled": false},
	}
}

func mustExecutor(t *testing.T, baseURL string) *Executor {
	t.Helper()
	executor, err := NewExecutor(Config{
		BaseURL: baseURL, RoutePassword: testPassword,
		ObservedModelID: testObservedModelID,
		HTTPClient:      &http.Client{Timeout: 5 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func requireProtected(t *testing.T, request *http.Request) {
	t.Helper()
	user, password, ok := request.BasicAuth()
	if !ok || user != routeUsername || password != testPassword {
		t.Errorf("request did not carry expected Basic auth")
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	payload, _ := json.Marshal(value)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func writeStreamHeaders(writer http.ResponseWriter, sessionID string) {
	writer.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	writer.Header().Set("x-eve-session-id", sessionID)
	writer.Header().Set("x-eve-stream-format", streamFormat)
	writer.Header().Set("x-eve-stream-version", streamVersion)
}

func compactJSON(t *testing.T, value []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := json.Compact(&output, value); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestFailureErrorsStayFixed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err      error
		expected string
	}{
		{ErrUnavailable, "Eve executor unavailable"},
		{ErrMismatch, "Eve executor mismatch"},
		{ErrProtocol, "Eve executor protocol invalid"},
		{ErrExecutionFailed, "Eve execution failed"},
	} {
		if got := fmt.Sprint(test.err); got != test.expected {
			t.Fatalf("error = %q, want %q", got, test.expected)
		}
	}
}
