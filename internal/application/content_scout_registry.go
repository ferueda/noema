package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const (
	ContentScoutAgentName                   = "content-scout"
	ContentScoutAgentVersion                = "content-scout-v1"
	ContentScoutAgentDefinitionVersion      = "content-scout-definition-v1"
	ContentScoutInstructionsVersion         = "content-scout-instructions-v1"
	ContentScoutEventType                   = "analysis.completed"
	ContentScoutDisclosurePolicyVersion     = "content-disclosure-v1"
	ContentScoutSafetyPolicyVersion         = "content-safety-v1"
	ContentScoutRetrievalPolicyVersion      = "content-scout-knowledge-v1"
	ContentScoutExecutorKind                = "eve"
	ContentScoutExecutorVersion             = "0.27.8"
	ContentScoutRecoveryPolicyVersion       = "eve-0.27.8-default-recovery-v1"
	ContentScoutRouteAlias                  = "content-scout-v1"
	ContentScoutRouteVersion                = "content-scout-route-v1"
	ContentScoutGateway                     = "vercel-ai-gateway"
	ContentScoutModel                       = "openai/gpt-5.4-mini"
	ContentScoutProvider                    = "azure"
	ContentScoutServiceTier                 = "flex"
	ContentScoutAgentFileSchemaVersion      = 1
	ContentScoutDisclosureSchemaVersion     = 1
	ContentScoutDeadlineMilliseconds        = 180_000
	ContentScoutMaximumOutputTokens         = 4_096
	ContentScoutSessionTokenBudget          = 8_192
	ContentScoutMaximumResponseBytes        = 256 * 1_024
	ContentScoutMaximumStreamEvents         = 4_096
	ContentScoutMaximumIdeas                = 5
	maxContentScoutConfigurationBytes       = 64 * 1_024
	maxContentScoutDisclosureTerms          = 64
	maxContentScoutDisclosureTermBytes      = 256
	maxContentScoutDisclosureTotalTermBytes = 4 * 1_024
)

var contentScoutDisabledTools = []string{
	"agent",
	"ask_question",
	"bash",
	"glob",
	"grep",
	"read_file",
	"todo",
	"web_fetch",
	"web_search",
	"write_file",
}

var contentScoutDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type contentScoutAgentFile struct {
	SchemaVersion           int                                   `json:"schemaVersion"`
	Agent                   domain.AgentIdentity                  `json:"agent"`
	AgentDefinitionVersion  string                                `json:"agentDefinitionVersion"`
	Instructions            contentScoutInstructions              `json:"instructions"`
	Executor                contentScoutExecutor                  `json:"executor"`
	OutputSchema            domain.StructuredOutputSchemaIdentity `json:"outputSchema"`
	Route                   domain.AgentRouteIdentity             `json:"route"`
	Privacy                 contentScoutPrivacy                   `json:"privacy"`
	DisclosurePolicyVersion string                                `json:"disclosurePolicyVersion"`
	SafetyPolicyVersion     string                                `json:"safetyPolicyVersion"`
	RetrievalPolicyVersion  string                                `json:"retrievalPolicyVersion"`
	Temperature             *float64                              `json:"temperature"`
	Limits                  contentScoutLimits                    `json:"limits"`
	Capabilities            contentScoutCapabilities              `json:"capabilities"`
}

type contentScoutInstructions struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type contentScoutExecutor struct {
	Kind                  string `json:"kind"`
	Version               string `json:"version"`
	ContractVersion       int    `json:"contractVersion"`
	RecoveryPolicyVersion string `json:"recoveryPolicyVersion"`
}

type contentScoutPrivacy struct {
	PolicyVersion          string `json:"policyVersion"`
	ZeroDataRetention      *bool  `json:"zeroDataRetention"`
	DisallowPromptTraining *bool  `json:"disallowPromptTraining"`
}

type contentScoutLimits struct {
	DeadlineMilliseconds int `json:"deadlineMilliseconds"`
	MaxOutputTokens      int `json:"maxOutputTokens"`
	SessionTokenBudget   int `json:"sessionTokenBudget"`
	MaxResponseBytes     int `json:"maxResponseBytes"`
	MaxStreamEvents      int `json:"maxStreamEvents"`
	MaximumIdeas         int `json:"maximumIdeas"`
}

type contentScoutCapabilities struct {
	DisabledTools      []string `json:"disabledTools"`
	Skills             *bool    `json:"skills"`
	Connections        *bool    `json:"connections"`
	Sandbox            *bool    `json:"sandbox"`
	Subagents          *bool    `json:"subagents"`
	Schedules          *bool    `json:"schedules"`
	AdditionalChannels *bool    `json:"additionalChannels"`
	AgentState         *bool    `json:"agentState"`
	InputTelemetry     *bool    `json:"inputTelemetry"`
	OutputTelemetry    *bool    `json:"outputTelemetry"`
}

type contentScoutDisclosureFile struct {
	SchemaVersion       int      `json:"schemaVersion"`
	ApprovedPublicTerms []string `json:"approvedPublicTerms"`
}

type contentScoutHandlerConfiguration struct {
	AgentFileDigest               string   `json:"agentFileDigest"`
	DisclosureConfigurationDigest string   `json:"disclosureConfigurationDigest"`
	ApprovedPublicTerms           []string `json:"approvedPublicTerms"`
}

// ContentScoutConfiguration is the complete local identity required to create
// a Content Scout job. It never contains an endpoint or credential.
type ContentScoutConfiguration struct {
	Agent           domain.AgentIdentity
	Identity        domain.AgentConfigurationIdentity
	AgentFileDigest string
}

// LoadContentScoutConfiguration strictly validates both local configuration
// files and projects only approved public terms and canonical digests into the
// durable handler configuration.
func LoadContentScoutConfiguration(
	agentFile io.Reader,
	disclosureFile io.Reader,
) (ContentScoutConfiguration, error) {
	var agent contentScoutAgentFile
	if _, err := decodeStrictBoundedJSON(
		agentFile, maxContentScoutConfigurationBytes, &agent,
	); err != nil || validateContentScoutAgentFile(agent) != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout agent configuration is invalid")
	}
	agentCanonical, err := json.Marshal(agent)
	if err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout agent configuration is invalid")
	}
	agentFileDigest, err := platform.Fingerprint(json.RawMessage(agentCanonical))
	if err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout agent configuration identity is unavailable")
	}

	var disclosure contentScoutDisclosureFile
	if _, err := decodeStrictBoundedJSON(
		disclosureFile, maxContentScoutConfigurationBytes, &disclosure,
	); err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout disclosure configuration is invalid")
	}
	terms, err := normalizeApprovedPublicTerms(disclosure)
	if err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout disclosure configuration is invalid")
	}
	disclosure.ApprovedPublicTerms = terms
	disclosureCanonical, err := json.Marshal(disclosure)
	if err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout disclosure configuration is invalid")
	}
	disclosureDigest, err := platform.Fingerprint(json.RawMessage(disclosureCanonical))
	if err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout disclosure configuration identity is unavailable")
	}

	handler, err := json.Marshal(contentScoutHandlerConfiguration{
		AgentFileDigest:               agentFileDigest,
		DisclosureConfigurationDigest: disclosureDigest,
		ApprovedPublicTerms:           terms,
	})
	if err != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout handler configuration is invalid")
	}
	identity := domain.AgentConfigurationIdentity{
		PromptVersion:            agent.Instructions.Version,
		OutputSchema:             agent.OutputSchema,
		Route:                    agent.Route,
		PrivacyPolicyVersion:     agent.Privacy.PolicyVersion,
		DisclosurePolicyVersion:  agent.DisclosurePolicyVersion,
		SafetyPolicyVersion:      agent.SafetyPolicyVersion,
		RetrievalPolicyVersion:   agent.RetrievalPolicyVersion,
		HandlerConfigurationJSON: handler,
	}
	identity.Digest, err = domain.AgentConfigurationDigest(identity)
	if err != nil || identity.Validate() != nil {
		return ContentScoutConfiguration{}, errors.New("Content Scout configuration identity is invalid")
	}
	return ContentScoutConfiguration{
		Agent: agent.Agent, Identity: identity, AgentFileDigest: agentFileDigest,
	}, nil
}

func validateContentScoutAgentFile(value contentScoutAgentFile) error {
	zero := 0.0
	output := domain.StructuredOutputSchemaIdentity{
		Name:        ContentScoutCandidatesSchemaName,
		Version:     domain.ContentIdeaSchemaVersion,
		Disposition: domain.StructuredOutputDispositionStrict,
		Digest:      ContentScoutCandidatesSchemaDigest,
	}
	if value.SchemaVersion != ContentScoutAgentFileSchemaVersion ||
		value.Agent != (domain.AgentIdentity{Name: ContentScoutAgentName, Version: ContentScoutAgentVersion}) ||
		value.AgentDefinitionVersion != ContentScoutAgentDefinitionVersion ||
		value.Instructions.Version != ContentScoutInstructionsVersion ||
		!contentScoutDigestPattern.MatchString(value.Instructions.Digest) ||
		value.Executor != (contentScoutExecutor{
			Kind:                  ContentScoutExecutorKind,
			Version:               ContentScoutExecutorVersion,
			ContractVersion:       domain.AgentExecutionContractVersion,
			RecoveryPolicyVersion: ContentScoutRecoveryPolicyVersion,
		}) ||
		value.OutputSchema != output ||
		value.Route != (domain.AgentRouteIdentity{
			Alias:        ContentScoutRouteAlias,
			Gateway:      ContentScoutGateway,
			Model:        ContentScoutModel,
			Provider:     ContentScoutProvider,
			RouteVersion: ContentScoutRouteVersion,
			ServiceTier:  ContentScoutServiceTier,
		}) ||
		value.Privacy.PolicyVersion != PrivacyPolicyVersion ||
		value.Privacy.ZeroDataRetention == nil || *value.Privacy.ZeroDataRetention ||
		value.Privacy.DisallowPromptTraining == nil || *value.Privacy.DisallowPromptTraining ||
		value.DisclosurePolicyVersion != ContentScoutDisclosurePolicyVersion ||
		value.SafetyPolicyVersion != ContentScoutSafetyPolicyVersion ||
		value.RetrievalPolicyVersion != ContentScoutRetrievalPolicyVersion ||
		value.Temperature == nil || *value.Temperature != zero ||
		value.Limits != (contentScoutLimits{
			DeadlineMilliseconds: ContentScoutDeadlineMilliseconds,
			MaxOutputTokens:      ContentScoutMaximumOutputTokens,
			SessionTokenBudget:   ContentScoutSessionTokenBudget,
			MaxResponseBytes:     ContentScoutMaximumResponseBytes,
			MaxStreamEvents:      ContentScoutMaximumStreamEvents,
			MaximumIdeas:         ContentScoutMaximumIdeas,
		}) ||
		!sameStrings(value.Capabilities.DisabledTools, contentScoutDisabledTools) ||
		!allExplicitlyDisabled(value.Capabilities) {
		return errors.New("Content Scout agent configuration does not match the V1 definition")
	}
	return nil
}

func allExplicitlyDisabled(value contentScoutCapabilities) bool {
	values := []*bool{
		value.Skills,
		value.Connections,
		value.Sandbox,
		value.Subagents,
		value.Schedules,
		value.AdditionalChannels,
		value.AgentState,
		value.InputTelemetry,
		value.OutputTelemetry,
	}
	for _, enabled := range values {
		if enabled == nil || *enabled {
			return false
		}
	}
	return true
}

func normalizeApprovedPublicTerms(
	value contentScoutDisclosureFile,
) ([]string, error) {
	if value.SchemaVersion != ContentScoutDisclosureSchemaVersion ||
		len(value.ApprovedPublicTerms) > maxContentScoutDisclosureTerms {
		return nil, errors.New("approved public terms are invalid")
	}
	terms := make([]string, 0, len(value.ApprovedPublicTerms))
	seen := make(map[string]bool, len(value.ApprovedPublicTerms))
	totalBytes := 0
	for _, term := range value.ApprovedPublicTerms {
		term = strings.TrimSpace(term)
		key := strings.ToLower(term)
		if term == "" || !utf8.ValidString(term) || len(term) > maxContentScoutDisclosureTermBytes ||
			seen[key] || hasUnsafeDisclosureRune(term) {
			return nil, errors.New("approved public terms are invalid")
		}
		if _, err := (PrivacyPolicy{}).Postflight(term); err != nil {
			return nil, errors.New("approved public terms are unsafe")
		}
		totalBytes += len(term)
		if totalBytes > maxContentScoutDisclosureTotalTermBytes {
			return nil, errors.New("approved public terms are too large")
		}
		seen[key] = true
		terms = append(terms, term)
	}
	sort.Slice(terms, func(left, right int) bool {
		leftFolded := strings.ToLower(terms[left])
		rightFolded := strings.ToLower(terms[right])
		if leftFolded == rightFolded {
			return terms[left] < terms[right]
		}
		return leftFolded < rightFolded
	})
	return terms, nil
}

func hasUnsafeDisclosureRune(value string) bool {
	for _, candidate := range value {
		if unicode.IsControl(candidate) {
			return true
		}
	}
	return false
}

func decodeStrictBoundedJSON(reader io.Reader, limit int, target any) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("configuration is unavailable")
	}
	document, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil || len(document) == 0 || len(document) > limit {
		return nil, errors.New("configuration document is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, errors.New("configuration document is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("configuration document has trailing data")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, document); err != nil {
		return nil, errors.New("configuration document is invalid")
	}
	return compact.Bytes(), nil
}

func sameStrings(left, right []string) bool {
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
