package eve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

// InfoExpectation is the bounded, observable part of the checked-in agent
// definition. Runtime-only choices that /info does not expose stay outside it.
type InfoExpectation struct {
	AgentName                 string
	GatewayTarget             string
	ProviderOptionsJSON       json.RawMessage
	StaticInstructionsDigest  string
	AllowedFrameworkToolNames []string
}

type PreflightResult struct {
	AgentName string
	ModelID   string
}

func (executor *Executor) Preflight(
	ctx context.Context,
	expectation InfoExpectation,
) (PreflightResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	expectedOptions, err := compactJSONObject(expectation.ProviderOptionsJSON, maxSchemaBody)
	if err != nil || !validID(expectation.AgentName) ||
		!validID(expectation.GatewayTarget) ||
		len(expectation.StaticInstructionsDigest) != sha256.Size*2 {
		return PreflightResult{}, categorized(
			ErrMismatch,
			domain.AgentFailureCategoryConfigurationInvalid,
		)
	}
	if _, err := hex.DecodeString(expectation.StaticInstructionsDigest); err != nil {
		return PreflightResult{}, categorized(
			ErrMismatch,
			domain.AgentFailureCategoryConfigurationInvalid,
		)
	}
	if !validUniqueNames(expectation.AllowedFrameworkToolNames) {
		return PreflightResult{}, categorized(
			ErrMismatch,
			domain.AgentFailureCategoryConfigurationInvalid,
		)
	}
	if err := executor.checkHealth(ctx); err != nil {
		return PreflightResult{}, err
	}
	info, err := executor.loadInfo(ctx)
	if err != nil {
		return PreflightResult{}, err
	}
	if !info.matches(expectation, executor.observedModelID, expectedOptions) {
		return PreflightResult{}, categorized(
			ErrMismatch,
			domain.AgentFailureCategoryExecutorMismatch,
		)
	}
	return PreflightResult{AgentName: info.Agent.Name, ModelID: info.Agent.Model.ID}, nil
}

func (executor *Executor) checkHealth(ctx context.Context) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		executor.endpoint("/eve/v1/health"),
		nil,
	)
	if err != nil {
		return categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	response, err := executor.client.Do(request)
	if err != nil {
		return categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	if !isJSONContentType(response.Header.Get("Content-Type")) {
		return categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	payload, err := readBounded(response.Body, maxControlBody)
	if err != nil {
		return categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	var health struct {
		OK         bool   `json:"ok"`
		Status     string `json:"status"`
		WorkflowID string `json:"workflowId"`
	}
	if decodeStrictJSON(payload, &health) != nil || !health.OK ||
		health.Status != "ready" || !validID(health.WorkflowID) {
		return categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	return nil
}

type infoDocument struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Agent   struct {
		Name  string `json:"name"`
		Model struct {
			ID              string          `json:"id"`
			ProviderOptions json.RawMessage `json:"providerOptions"`
			Routing         *struct {
				Kind   string `json:"kind"`
				Target string `json:"target"`
			} `json:"routing"`
		} `json:"model"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	} `json:"agent"`
	Diagnostics struct {
		DiscoveryErrors   int `json:"discoveryErrors"`
		DiscoveryWarnings int `json:"discoveryWarnings"`
	} `json:"diagnostics"`
	Instructions struct {
		Static *struct {
			Markdown string `json:"markdown"`
		} `json:"static"`
		Dynamic []json.RawMessage `json:"dynamic"`
	} `json:"instructions"`
	Tools struct {
		Authored  []json.RawMessage `json:"authored"`
		Dynamic   []json.RawMessage `json:"dynamic"`
		Available []struct {
			Name   string `json:"name"`
			Origin string `json:"origin"`
		} `json:"available"`
		Framework []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"framework"`
	} `json:"tools"`
	Connections []json.RawMessage `json:"connections"`
	Hooks       []json.RawMessage `json:"hooks"`
	Sandbox     json.RawMessage   `json:"sandbox"`
	Schedules   []json.RawMessage `json:"schedules"`
	Skills      struct {
		Static  []json.RawMessage `json:"static"`
		Dynamic []json.RawMessage `json:"dynamic"`
	} `json:"skills"`
	Subagents struct {
		Local []json.RawMessage `json:"local"`
		Total int               `json:"total"`
	} `json:"subagents"`
	Workflow struct {
		Enabled bool `json:"enabled"`
	} `json:"workflow"`
}

func (executor *Executor) loadInfo(ctx context.Context) (infoDocument, error) {
	request, err := executor.protectedRequest(ctx, http.MethodGet, "/eve/v1/info", nil)
	if err != nil {
		return infoDocument{}, categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	response, err := executor.client.Do(request)
	if err != nil {
		return infoDocument{}, categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return infoDocument{}, categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	if !isJSONContentType(response.Header.Get("Content-Type")) {
		return infoDocument{}, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	payload, err := readBounded(response.Body, maxInfoBody)
	if err != nil {
		return infoDocument{}, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	var info infoDocument
	if !hasRequiredInfoProjection(payload) || json.Unmarshal(payload, &info) != nil {
		return infoDocument{}, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	return info, nil
}

func hasRequiredInfoProjection(payload json.RawMessage) bool {
	if !hasFields(
		payload,
		"kind", "version", "agent", "diagnostics", "instructions", "tools",
		"connections", "hooks", "sandbox", "schedules", "skills", "subagents", "workflow",
	) {
		return false
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(payload, &root) != nil ||
		!hasFields(root["agent"], "name", "model") ||
		!hasFields(root["diagnostics"], "discoveryErrors", "discoveryWarnings") ||
		!hasFields(root["instructions"], "static", "dynamic") ||
		!hasFields(root["tools"], "authored", "available", "dynamic", "framework") ||
		!hasFields(root["skills"], "static", "dynamic") ||
		!hasFields(root["subagents"], "local", "total") ||
		!hasFields(root["workflow"], "enabled") {
		return false
	}
	var agent map[string]json.RawMessage
	if json.Unmarshal(root["agent"], &agent) != nil ||
		!hasFields(agent["model"], "id", "providerOptions", "routing") {
		return false
	}
	return true
}

func (info infoDocument) matches(
	expectation InfoExpectation,
	observedModelID string,
	expectedOptions []byte,
) bool {
	if info.Kind != "eve-agent-info" || info.Version != 1 ||
		info.Agent.Name != expectation.AgentName ||
		info.Agent.Model.ID != observedModelID ||
		info.Agent.Model.Routing == nil ||
		info.Agent.Model.Routing.Kind != "gateway" ||
		info.Agent.Model.Routing.Target != expectation.GatewayTarget ||
		info.Agent.OutputSchema != nil && string(bytes.TrimSpace(info.Agent.OutputSchema)) != "null" ||
		info.Diagnostics.DiscoveryErrors != 0 ||
		info.Diagnostics.DiscoveryWarnings != 0 ||
		info.Instructions.Static == nil ||
		len(info.Instructions.Dynamic) != 0 ||
		len(info.Tools.Authored) != 0 ||
		len(info.Tools.Dynamic) != 0 ||
		len(info.Connections) != 0 ||
		len(info.Hooks) != 0 ||
		len(info.Schedules) != 0 ||
		len(info.Skills.Static) != 0 ||
		len(info.Skills.Dynamic) != 0 ||
		len(info.Subagents.Local) != 0 ||
		info.Subagents.Total != 0 ||
		info.Workflow.Enabled ||
		!isNullJSON(info.Sandbox) {
		return false
	}
	actualOptions, err := compactJSONObject(info.Agent.Model.ProviderOptions, maxSchemaBody)
	if err != nil || !bytes.Equal(actualOptions, expectedOptions) {
		return false
	}
	digest := sha256.Sum256([]byte(info.Instructions.Static.Markdown))
	if hex.EncodeToString(digest[:]) != expectation.StaticInstructionsDigest {
		return false
	}
	return info.matchesTools(expectation.AllowedFrameworkToolNames)
}

func (info infoDocument) matchesTools(allowed []string) bool {
	expected := append([]string(nil), allowed...)
	sort.Strings(expected)
	actual := make([]string, 0, len(info.Tools.Available))
	for _, tool := range info.Tools.Available {
		if tool.Origin != "framework" || !validID(tool.Name) {
			return false
		}
		actual = append(actual, tool.Name)
	}
	sort.Strings(actual)
	if !equalStrings(actual, expected) {
		return false
	}
	allowedSet := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		allowedSet[name] = struct{}{}
	}
	for _, tool := range info.Tools.Framework {
		if tool.Status != "active" && tool.Status != "disabled" && tool.Status != "replaced" {
			return false
		}
		if tool.Status == "active" {
			if _, ok := allowedSet[tool.Name]; !ok {
				return false
			}
		}
	}
	return true
}

func validUniqueNames(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validID(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func isNullJSON(value json.RawMessage) bool {
	return value == nil || string(bytes.TrimSpace(value)) == "null"
}
