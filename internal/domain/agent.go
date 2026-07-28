package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"time"

	"github.com/ferueda/noema/internal/platform"
)

const (
	AgentJobPayloadSchemaVersion     = 1
	AgentExecutionContractVersion    = 1
	AgentRunResultSchemaVersion      = 1
	AgentExecutionDispositionSkipped = "skipped-no-claims"
	AgentExecutionDispositionNone    = "not-invoked"
	AgentExecutionDispositionInvoked = "invoked"
	AgentRunOutcomeSucceeded         = "succeeded"
	AgentRunOutcomeFailed            = "failed"
	AgentPolicyOutcomePassed         = "passed"
	AgentPolicyOutcomeBlocked        = "blocked"
	AgentFailureStagePreparation     = "preparation"
	AgentFailureStageExecution       = "execution"
	AgentFailureStageResponseDecode  = "response-decoding"
	AgentFailureStageAdmission       = "local-admission"

	AgentFailureCategoryConfigurationInvalid = "configuration-invalid"
	AgentFailureCategoryInputInvalid         = "input-invalid"
	AgentFailureCategoryPrivacyBlocked       = "privacy-blocked"
	AgentFailureCategoryDisclosureBlocked    = "disclosure-blocked"
	AgentFailureCategoryExecutorUnavailable  = "executor-unavailable"
	AgentFailureCategoryExecutorMismatch     = "executor-mismatch"
	AgentFailureCategoryExecutorProtocol     = "executor-protocol-invalid"
	AgentFailureCategoryExecutorFailed       = "executor-failed"
	AgentFailureCategoryResponseInvalid      = "response-invalid"
	AgentFailureCategoryCandidateInvalid     = "candidate-invalid"
	AgentFailureCategoryPersistenceFailed    = "persistence-failed"

	maxAgentIDBytes                    = 256
	maxAgentNameBytes                  = 128
	maxAgentConfigurationBytes         = 64 * 1024
	maxAgentPayloadBytes               = 256 * 1024
	maxAgentKnowledgeClaims            = 128
	maxAgentArtifacts                  = 5
	maxAgentPolicyStages               = 8
	maxAgentPolicyCategoryCounts       = 32
	maxAgentCompletedModelSteps        = 4_096
	maxAgentLatencyMilliseconds  int64 = 24 * 60 * 60 * 1_000
)

var agentDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var agentSafeValuePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type AgentIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type KnowledgeInputRefsV1 struct {
	AnalysisRunID string   `json:"analysisRunId"`
	ClaimIDs      []string `json:"claimIds"`
}

type AgentRouteIdentity struct {
	Alias        string `json:"alias"`
	Gateway      string `json:"gateway"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	RouteVersion string `json:"routeVersion"`
	ServiceTier  string `json:"serviceTier"`
}

type AgentConfigurationIdentity struct {
	PromptVersion            string                         `json:"promptVersion"`
	OutputSchema             StructuredOutputSchemaIdentity `json:"outputSchema"`
	Route                    AgentRouteIdentity             `json:"route"`
	PrivacyPolicyVersion     string                         `json:"privacyPolicyVersion"`
	DisclosurePolicyVersion  string                         `json:"disclosurePolicyVersion"`
	SafetyPolicyVersion      string                         `json:"safetyPolicyVersion"`
	RetrievalPolicyVersion   string                         `json:"retrievalPolicyVersion"`
	Digest                   string                         `json:"digest"`
	HandlerConfigurationJSON json.RawMessage                `json:"handlerConfigurationJson"`
}

type AgentJobPayloadV1 struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Inputs        KnowledgeInputRefsV1       `json:"inputs"`
	Configuration AgentConfigurationIdentity `json:"configuration"`
}

type AgentExecutionIdentity struct {
	ExecutorKind          string `json:"executorKind"`
	ExecutorVersion       string `json:"executorVersion"`
	AgentDefinitionDigest string `json:"agentDefinitionDigest"`
	ContractVersion       int    `json:"contractVersion"`
	RecoveryPolicyVersion string `json:"recoveryPolicyVersion"`
}

type AgentExecutionInputV1 struct {
	SchemaName    string          `json:"schemaName"`
	SchemaVersion int             `json:"schemaVersion"`
	SchemaDigest  string          `json:"schemaDigest"`
	CanonicalJSON json.RawMessage `json:"canonicalJson"`
}

type AgentExecutionAuthorityV1 struct {
	AllowRemote bool `json:"allowRemote"`
}

type AgentExecutionRequestV1 struct {
	ContractVersion      int                            `json:"contractVersion"`
	JobID                string                         `json:"jobId"`
	JobFingerprint       string                         `json:"jobFingerprint"`
	TriggerEventID       string                         `json:"triggerEventId"`
	Agent                AgentIdentity                  `json:"agent"`
	Configuration        AgentConfigurationIdentity     `json:"configuration"`
	Execution            AgentExecutionIdentity         `json:"execution"`
	Input                AgentExecutionInputV1          `json:"input"`
	RequiredOutputSchema StructuredOutputSchemaIdentity `json:"requiredOutputSchema"`
	DeadlineAt           time.Time                      `json:"deadlineAt"`
	Authority            AgentExecutionAuthorityV1      `json:"authority"`
}

type AgentUsageV1 struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type AgentExecutionReceiptV1 struct {
	ExecutorKind        string              `json:"executorKind,omitempty"`
	ExecutorVersion     string              `json:"executorVersion,omitempty"`
	SessionID           string              `json:"sessionId,omitempty"`
	TurnID              string              `json:"turnId,omitempty"`
	CompletedModelSteps int                 `json:"completedModelSteps"`
	RequestedRoute      *AgentRouteIdentity `json:"requestedRoute,omitempty"`
	GatewayGenerationID string              `json:"gatewayGenerationId,omitempty"`
	Usage               *AgentUsageV1       `json:"usage,omitempty"`
	CostUSD             string              `json:"costUsd,omitempty"`
	LatencyMilliseconds *int64              `json:"latencyMilliseconds,omitempty"`
	FailureCategory     string              `json:"failureCategory,omitempty"`
}

type AgentExecutionResponseV1 struct {
	ContractVersion int                     `json:"contractVersion"`
	CandidateJSON   json.RawMessage         `json:"candidateJson"`
	Receipt         AgentExecutionReceiptV1 `json:"receipt"`
}

type AgentPolicyCategoryCountV1 struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type AgentPolicyStageV1 struct {
	Name          string                       `json:"name"`
	PolicyVersion string                       `json:"policyVersion"`
	Outcome       string                       `json:"outcome"`
	Categories    []AgentPolicyCategoryCountV1 `json:"categories"`
}

type AgentPrivacyOutcomeV1 struct {
	CompletedStages []AgentPolicyStageV1 `json:"completedStages"`
}

type AgentFailureV1 struct {
	Stage    string `json:"stage"`
	Category string `json:"category"`
}

type AgentRunResultV1 struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Outcome       string                   `json:"outcome"`
	Disposition   string                   `json:"disposition"`
	Execution     *AgentExecutionIdentity  `json:"execution,omitempty"`
	Receipt       *AgentExecutionReceiptV1 `json:"receipt,omitempty"`
	Privacy       AgentPrivacyOutcomeV1    `json:"privacy"`
	Failure       *AgentFailureV1          `json:"failure,omitempty"`
	ArtifactIDs   []string                 `json:"artifactIds"`
}

func (value AgentIdentity) Validate() error {
	if !validAgentName(value.Name) || !validAgentName(value.Version) {
		return errors.New("agent identity is invalid")
	}
	return nil
}

func (value KnowledgeInputRefsV1) Validate() error {
	if !validAgentID(value.AnalysisRunID) ||
		len(value.ClaimIDs) > maxAgentKnowledgeClaims ||
		!validUniqueAgentIDs(value.ClaimIDs) {
		return errors.New("knowledge input references are invalid")
	}
	return nil
}

func (value AgentRouteIdentity) Validate() error {
	for _, field := range []string{
		value.Alias,
		value.Gateway,
		value.Model,
		value.Provider,
		value.RouteVersion,
		value.ServiceTier,
	} {
		if !validAgentName(field) {
			return errors.New("agent route identity is invalid")
		}
	}
	return nil
}

func (value AgentConfigurationIdentity) Validate() error {
	if !validAgentName(value.PromptVersion) ||
		!validStructuredOutputIdentity(value.OutputSchema) ||
		value.Route.Validate() != nil ||
		!validAgentName(value.PrivacyPolicyVersion) ||
		!validAgentName(value.DisclosurePolicyVersion) ||
		!validAgentName(value.SafetyPolicyVersion) ||
		!validAgentName(value.RetrievalPolicyVersion) ||
		!agentDigestPattern.MatchString(value.Digest) {
		return errors.New("agent configuration identity is invalid")
	}
	if _, err := stableJSONObject(value.HandlerConfigurationJSON, maxAgentConfigurationBytes, 32); err != nil {
		return errors.New("agent handler configuration is invalid")
	}
	digest, err := AgentConfigurationDigest(value)
	if err != nil || digest != value.Digest {
		return errors.New("agent configuration digest mismatch")
	}
	return nil
}

func (value AgentJobPayloadV1) Validate() error {
	if value.SchemaVersion != AgentJobPayloadSchemaVersion ||
		value.Inputs.Validate() != nil ||
		value.Configuration.Validate() != nil {
		return errors.New("agent job payload is invalid")
	}
	return nil
}

func (value AgentExecutionIdentity) Validate() error {
	if !validAgentName(value.ExecutorKind) ||
		!validAgentName(value.ExecutorVersion) ||
		!agentDigestPattern.MatchString(value.AgentDefinitionDigest) ||
		value.ContractVersion != AgentExecutionContractVersion ||
		!validAgentName(value.RecoveryPolicyVersion) {
		return errors.New("agent execution identity is invalid")
	}
	return nil
}

func (value AgentExecutionInputV1) Validate() error {
	if !validAgentName(value.SchemaName) ||
		value.SchemaVersion < 1 ||
		!agentDigestPattern.MatchString(value.SchemaDigest) {
		return errors.New("agent execution input identity is invalid")
	}
	if _, err := compactJSONObject(value.CanonicalJSON, maxAgentPayloadBytes, 32); err != nil {
		return errors.New("agent execution input is invalid")
	}
	return nil
}

func (value AgentExecutionRequestV1) Validate() error {
	if value.ContractVersion != AgentExecutionContractVersion ||
		!validAgentID(value.JobID) ||
		!agentDigestPattern.MatchString(value.JobFingerprint) ||
		!validAgentID(value.TriggerEventID) ||
		value.Agent.Validate() != nil ||
		value.Configuration.Validate() != nil ||
		value.Execution.Validate() != nil ||
		value.Input.Validate() != nil ||
		!validStructuredOutputIdentity(value.RequiredOutputSchema) ||
		value.RequiredOutputSchema != value.Configuration.OutputSchema ||
		value.DeadlineAt.IsZero() ||
		!value.Authority.AllowRemote {
		return errors.New("agent execution request is invalid")
	}
	return nil
}

func (value AgentUsageV1) Validate() error {
	if value.InputTokens < 0 || value.OutputTokens < 0 || value.TotalTokens < 0 ||
		value.TotalTokens != value.InputTokens+value.OutputTokens {
		return errors.New("agent usage is invalid")
	}
	return nil
}

func (value AgentExecutionReceiptV1) ValidateComplete() error {
	if !validAgentName(value.ExecutorKind) ||
		!validAgentName(value.ExecutorVersion) ||
		!validAgentID(value.SessionID) ||
		!validAgentID(value.TurnID) ||
		value.CompletedModelSteps < 1 ||
		value.CompletedModelSteps > maxAgentCompletedModelSteps ||
		value.RequestedRoute == nil ||
		value.RequestedRoute.Validate() != nil ||
		value.FailureCategory != "" {
		return errors.New("agent execution receipt is invalid")
	}
	return value.validateOptionalMetadata()
}

func (value AgentExecutionReceiptV1) ValidateObserved() error {
	if value.ExecutorKind != "" && !validAgentName(value.ExecutorKind) ||
		value.ExecutorVersion != "" && !validAgentName(value.ExecutorVersion) ||
		value.SessionID != "" && !validAgentID(value.SessionID) ||
		value.TurnID != "" && !validAgentID(value.TurnID) ||
		value.CompletedModelSteps < 0 ||
		value.CompletedModelSteps > maxAgentCompletedModelSteps ||
		value.RequestedRoute != nil && value.RequestedRoute.Validate() != nil ||
		value.FailureCategory != "" && !validAgentFailureCategory(value.FailureCategory) {
		return errors.New("observed agent execution receipt is invalid")
	}
	if value.ExecutorKind == "" &&
		value.ExecutorVersion == "" &&
		value.SessionID == "" &&
		value.TurnID == "" &&
		value.CompletedModelSteps == 0 &&
		value.RequestedRoute == nil &&
		value.GatewayGenerationID == "" &&
		value.Usage == nil &&
		value.CostUSD == "" &&
		value.LatencyMilliseconds == nil &&
		value.FailureCategory == "" {
		return errors.New("observed agent execution receipt is empty")
	}
	return value.validateOptionalMetadata()
}

func (value AgentExecutionReceiptV1) validateOptionalMetadata() error {
	if value.GatewayGenerationID != "" && !validAgentID(value.GatewayGenerationID) {
		return errors.New("agent gateway generation identity is invalid")
	}
	if value.Usage != nil && value.Usage.Validate() != nil {
		return errors.New("agent receipt usage is invalid")
	}
	if value.CostUSD != "" && !ValidModelCostUSD(value.CostUSD) {
		return errors.New("agent receipt cost is invalid")
	}
	if value.LatencyMilliseconds != nil &&
		(*value.LatencyMilliseconds < 0 || *value.LatencyMilliseconds > maxAgentLatencyMilliseconds) {
		return errors.New("agent receipt latency is invalid")
	}
	return nil
}

func (value AgentExecutionResponseV1) Validate() error {
	if value.ContractVersion != AgentExecutionContractVersion ||
		value.Receipt.ValidateComplete() != nil {
		return errors.New("agent execution response is invalid")
	}
	if _, err := compactJSONObject(value.CandidateJSON, maxAgentPayloadBytes, 32); err != nil {
		return errors.New("agent candidate result is invalid")
	}
	return nil
}

func (value AgentPrivacyOutcomeV1) Validate() error {
	if len(value.CompletedStages) > maxAgentPolicyStages {
		return errors.New("agent privacy outcome is invalid")
	}
	seen := make(map[string]struct{}, len(value.CompletedStages))
	for _, stage := range value.CompletedStages {
		if !validSafeAgentValue(stage.Name) ||
			!validAgentName(stage.PolicyVersion) ||
			(stage.Outcome != AgentPolicyOutcomePassed && stage.Outcome != AgentPolicyOutcomeBlocked) ||
			len(stage.Categories) > maxAgentPolicyCategoryCounts {
			return errors.New("agent privacy stage is invalid")
		}
		if _, duplicate := seen[stage.Name]; duplicate {
			return errors.New("agent privacy stage is duplicated")
		}
		seen[stage.Name] = struct{}{}
		seenCategories := make(map[string]struct{}, len(stage.Categories))
		for _, category := range stage.Categories {
			if !validSafeAgentValue(category.Category) || category.Count < 1 {
				return errors.New("agent privacy category count is invalid")
			}
			if _, duplicate := seenCategories[category.Category]; duplicate {
				return errors.New("agent privacy category is duplicated")
			}
			seenCategories[category.Category] = struct{}{}
		}
	}
	return nil
}

func (value AgentPrivacyOutcomeV1) passed() bool {
	for _, stage := range value.CompletedStages {
		if stage.Outcome != AgentPolicyOutcomePassed {
			return false
		}
	}
	return true
}

func (value AgentRunResultV1) Validate() error {
	if value.SchemaVersion != AgentRunResultSchemaVersion ||
		value.Privacy.Validate() != nil ||
		len(value.ArtifactIDs) > maxAgentArtifacts ||
		!validUniqueAgentIDs(value.ArtifactIDs) {
		return errors.New("agent run result is invalid")
	}
	switch {
	case value.Outcome == AgentRunOutcomeSucceeded &&
		value.Disposition == AgentExecutionDispositionSkipped:
		if value.Execution != nil || value.Receipt != nil || value.Failure != nil ||
			len(value.ArtifactIDs) != 0 || len(value.Privacy.CompletedStages) != 0 {
			return errors.New("skipped agent result is invalid")
		}
	case value.Outcome == AgentRunOutcomeSucceeded &&
		value.Disposition == AgentExecutionDispositionInvoked:
		if value.Execution == nil || value.Execution.Validate() != nil ||
			value.Receipt == nil || value.Receipt.ValidateComplete() != nil ||
			value.Failure != nil || !value.Privacy.passed() {
			return errors.New("successful invoked agent result is invalid")
		}
	case value.Outcome == AgentRunOutcomeFailed &&
		value.Disposition == AgentExecutionDispositionNone:
		if value.Receipt != nil || len(value.ArtifactIDs) != 0 ||
			value.Failure == nil || value.Failure.Stage != AgentFailureStagePreparation ||
			!validFailure(value.Failure) ||
			value.Execution != nil && value.Execution.Validate() != nil {
			return errors.New("failed non-invoked agent result is invalid")
		}
	case value.Outcome == AgentRunOutcomeFailed &&
		value.Disposition == AgentExecutionDispositionInvoked:
		if value.Execution == nil || value.Execution.Validate() != nil ||
			len(value.ArtifactIDs) != 0 ||
			value.Failure == nil || value.Failure.Stage == AgentFailureStagePreparation ||
			!validFailure(value.Failure) ||
			value.Receipt != nil && value.Receipt.ValidateObserved() != nil {
			return errors.New("failed invoked agent result is invalid")
		}
	default:
		return errors.New("agent run outcome and disposition are invalid")
	}
	return nil
}

func AgentConfigurationDigest(value AgentConfigurationIdentity) (string, error) {
	canonical, err := stableJSONObject(value.HandlerConfigurationJSON, maxAgentConfigurationBytes, 32)
	if err != nil {
		return "", err
	}
	return platform.Fingerprint(struct {
		PromptVersion           string                         `json:"promptVersion"`
		OutputSchema            StructuredOutputSchemaIdentity `json:"outputSchema"`
		Route                   AgentRouteIdentity             `json:"route"`
		PrivacyPolicyVersion    string                         `json:"privacyPolicyVersion"`
		DisclosurePolicyVersion string                         `json:"disclosurePolicyVersion"`
		SafetyPolicyVersion     string                         `json:"safetyPolicyVersion"`
		RetrievalPolicyVersion  string                         `json:"retrievalPolicyVersion"`
		HandlerConfiguration    json.RawMessage                `json:"handlerConfiguration"`
	}{
		PromptVersion:           value.PromptVersion,
		OutputSchema:            value.OutputSchema,
		Route:                   value.Route,
		PrivacyPolicyVersion:    value.PrivacyPolicyVersion,
		DisclosurePolicyVersion: value.DisclosurePolicyVersion,
		SafetyPolicyVersion:     value.SafetyPolicyVersion,
		RetrievalPolicyVersion:  value.RetrievalPolicyVersion,
		HandlerConfiguration:    canonical,
	})
}

func AgentJobFingerprint(
	triggerEventID string,
	agent AgentIdentity,
	payload AgentJobPayloadV1,
) (string, error) {
	if !validAgentID(triggerEventID) || agent.Validate() != nil || payload.Validate() != nil {
		return "", errors.New("agent job fingerprint input is invalid")
	}
	return platform.Fingerprint(struct {
		TriggerEventID      string               `json:"triggerEventId"`
		Agent               AgentIdentity        `json:"agent"`
		SchemaVersion       int                  `json:"schemaVersion"`
		Inputs              KnowledgeInputRefsV1 `json:"inputs"`
		ConfigurationDigest string               `json:"configurationDigest"`
	}{
		TriggerEventID: triggerEventID, Agent: agent,
		SchemaVersion: payload.SchemaVersion, Inputs: payload.Inputs,
		ConfigurationDigest: payload.Configuration.Digest,
	})
}

func validFailure(value *AgentFailureV1) bool {
	if value == nil || !validAgentFailureCategory(value.Category) {
		return false
	}
	switch value.Stage {
	case AgentFailureStagePreparation, AgentFailureStageExecution,
		AgentFailureStageResponseDecode, AgentFailureStageAdmission:
		return true
	default:
		return false
	}
}

func validAgentFailureCategory(value string) bool {
	switch value {
	case AgentFailureCategoryConfigurationInvalid,
		AgentFailureCategoryInputInvalid,
		AgentFailureCategoryPrivacyBlocked,
		AgentFailureCategoryDisclosureBlocked,
		AgentFailureCategoryExecutorUnavailable,
		AgentFailureCategoryExecutorMismatch,
		AgentFailureCategoryExecutorProtocol,
		AgentFailureCategoryExecutorFailed,
		AgentFailureCategoryResponseInvalid,
		AgentFailureCategoryCandidateInvalid,
		AgentFailureCategoryPersistenceFailed:
		return true
	default:
		return false
	}
}

func validStructuredOutputIdentity(value StructuredOutputSchemaIdentity) bool {
	return validAgentName(value.Name) &&
		value.Version >= 1 &&
		value.Disposition == StructuredOutputDispositionStrict &&
		agentDigestPattern.MatchString(value.Digest)
}

func validAgentID(value string) bool {
	return len(value) > 0 && len(value) <= maxAgentIDBytes
}

func validAgentName(value string) bool {
	return len(value) > 0 && len(value) <= maxAgentNameBytes
}

func validSafeAgentValue(value string) bool {
	return len(value) > 0 &&
		len(value) <= maxAgentNameBytes &&
		agentSafeValuePattern.MatchString(value)
}

func validUniqueAgentIDs(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validAgentID(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func compactJSONObject(
	value json.RawMessage,
	maxBytes int,
	maxProperties int,
) (json.RawMessage, error) {
	if len(value) == 0 || len(value) > maxBytes || !json.Valid(value) {
		return nil, errors.New("json object is invalid")
	}
	var object map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&object); err != nil || object == nil || len(object) > maxProperties {
		return nil, errors.New("json object is invalid")
	}
	if err := decoder.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("json object contains trailing data")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, value); err != nil {
		return nil, errors.New("json object is invalid")
	}
	return json.RawMessage(compact.Bytes()), nil
}

func stableJSONObject(
	value json.RawMessage,
	maxBytes int,
	maxProperties int,
) (json.RawMessage, error) {
	if len(value) == 0 || len(value) > maxBytes || !json.Valid(value) {
		return nil, errors.New("json object is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil || len(object) > maxProperties {
		return nil, errors.New("json object is invalid")
	}
	if err := decoder.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("json object contains trailing data")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, errors.New("json object is invalid")
	}
	return canonical, nil
}

func validConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
