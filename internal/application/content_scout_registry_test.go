package application

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/domain"
)

func TestLoadContentScoutConfigurationCanonicalizesSafeIdentity(t *testing.T) {
	first := loadContentScoutConfigurationForTest(
		t,
		contentScoutAgentJSON(ContentScoutInstructionsDigest),
		`{"schemaVersion":1,"approvedPublicTerms":["Go","Cerebras"]}`,
	)
	second := loadContentScoutConfigurationForTest(
		t,
		"\n"+contentScoutAgentJSON(ContentScoutInstructionsDigest)+"\n",
		`{
			"approvedPublicTerms": ["Cerebras", "Go"],
			"schemaVersion": 1
		}`,
	)
	if first.identity.Digest != second.identity.Digest ||
		first.agentFileDigest != second.agentFileDigest {
		t.Fatalf("equivalent configuration digests differ: %#v / %#v", first, second)
	}
	var handler map[string]any
	if err := json.Unmarshal(first.identity.HandlerConfigurationJSON, &handler); err != nil {
		t.Fatalf("decode handler configuration: %v", err)
	}
	if len(handler) != 3 || handler["agentFileDigest"] != first.agentFileDigest ||
		handler["disclosureConfigurationDigest"] == "" {
		t.Fatalf("handler configuration = %#v", handler)
	}
	terms, ok := handler["approvedPublicTerms"].([]any)
	if !ok || len(terms) != 2 || terms[0] != "Cerebras" || terms[1] != "Go" {
		t.Fatalf("normalized approved terms = %#v", handler["approvedPublicTerms"])
	}
	for _, forbidden := range []string{
		"openai/gpt-5.4-mini",
		"NOEMA_EVE_ROUTE_PASSWORD",
		"AI_GATEWAY_API_KEY",
	} {
		if bytes.Contains(first.identity.HandlerConfigurationJSON, []byte(forbidden)) {
			t.Fatalf("handler configuration contains %q", forbidden)
		}
	}

	changed := first
	changed.identity.Route.Provider = "cerebras"
	var err error
	changed.identity.Digest, err = domain.AgentConfigurationDigest(changed.identity)
	if err != nil {
		t.Fatalf("digest changed configuration: %v", err)
	}
	if changed.validate() == nil {
		t.Fatal("constructed configuration outside the registered route was accepted")
	}
}

func TestLoadContentScoutConfigurationRejectsUnapprovedShape(t *testing.T) {
	for name, agentJSON := range map[string]string{
		"unknown field": strings.Replace(
			contentScoutAgentJSON(ContentScoutInstructionsDigest),
			`"schemaVersion":1`,
			`"schemaVersion":1,"endpoint":"http://localhost:9999"`,
			1,
		),
		"missing explicit privacy choice": strings.Replace(
			contentScoutAgentJSON(ContentScoutInstructionsDigest),
			`"zeroDataRetention":false,`,
			"",
			1,
		),
		"changed provider": strings.Replace(
			contentScoutAgentJSON(ContentScoutInstructionsDigest),
			`"provider":"azure"`,
			`"provider":"openai"`,
			1,
		),
		"enabled tool surface": strings.Replace(
			contentScoutAgentJSON(ContentScoutInstructionsDigest),
			`"skills":false`,
			`"skills":true`,
			1,
		),
		"changed instructions without a version": contentScoutAgentJSON(strings.Repeat("b", 64)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadContentScoutConfiguration(
				strings.NewReader(agentJSON),
				strings.NewReader(`{"schemaVersion":1,"approvedPublicTerms":[]}`),
			)
			if err == nil {
				t.Fatal("invalid agent configuration was accepted")
			}
		})
	}
}

func TestLoadContentScoutConfigurationRejectsUnsafeDisclosure(t *testing.T) {
	for name, disclosureJSON := range map[string]string{
		"unknown field":  `{"schemaVersion":1,"approvedPublicTerms":[],"privateTerms":[]}`,
		"case duplicate": `{"schemaVersion":1,"approvedPublicTerms":["Go","go"]}`,
		"local path":     `{"schemaVersion":1,"approvedPublicTerms":["/Users/person/private"]}`,
		"provider token": `{"schemaVersion":1,"approvedPublicTerms":["sk-proj-abcdefghijklmnopqrstuvwxyz"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadContentScoutConfiguration(
				strings.NewReader(contentScoutAgentJSON(ContentScoutInstructionsDigest)),
				strings.NewReader(disclosureJSON),
			)
			if err == nil {
				t.Fatal("unsafe disclosure configuration was accepted")
			}
		})
	}
}

func loadContentScoutConfigurationForTest(
	t *testing.T,
	agentJSON string,
	disclosureJSON string,
) ContentScoutConfiguration {
	t.Helper()
	configuration, err := LoadContentScoutConfiguration(
		strings.NewReader(agentJSON),
		strings.NewReader(disclosureJSON),
	)
	if err != nil {
		t.Fatalf("load Content Scout configuration: %v", err)
	}
	return configuration
}

func contentScoutAgentJSON(instructionsDigest string) string {
	return `{
		"schemaVersion":1,
		"agent":{"name":"content-scout","version":"content-scout-v1"},
		"agentDefinitionVersion":"content-scout-definition-v1",
		"instructions":{"version":"content-scout-instructions-v1","digest":"` + instructionsDigest + `"},
		"executor":{
			"kind":"eve",
			"version":"0.27.8",
			"contractVersion":1,
			"recoveryPolicyVersion":"eve-0.27.8-default-recovery-v1"
		},
		"outputSchema":{
			"name":"content-scout-candidates",
			"version":1,
			"disposition":"strict",
			"digest":"6859d77424e02745841a7348a3c88e74874a928c4ef0f0b831257b81e5c8c965"
		},
		"route":{
			"alias":"content-scout-v1",
			"gateway":"vercel-ai-gateway",
			"model":"openai/gpt-5.4-mini",
			"provider":"azure",
			"routeVersion":"content-scout-route-v1",
			"serviceTier":"flex"
		},
		"privacy":{
			"policyVersion":"deterministic-privacy-v1",
			"zeroDataRetention":false,
			"disallowPromptTraining":false
		},
		"disclosurePolicyVersion":"content-disclosure-v1",
		"safetyPolicyVersion":"content-safety-v1",
		"retrievalPolicyVersion":"content-scout-knowledge-v1",
		"temperature":0,
		"limits":{
			"deadlineMilliseconds":180000,
			"maxOutputTokens":4096,
			"sessionTokenBudget":8192,
			"maxResponseBytes":262144,
			"maxStreamEvents":4096,
			"maximumIdeas":5
		},
		"capabilities":{
			"disabledTools":[
				"agent","ask_question","bash","glob","grep","read_file","todo",
				"web_fetch","web_search","write_file"
			],
			"skills":false,
			"connections":false,
			"sandbox":false,
			"subagents":false,
			"schedules":false,
			"additionalChannels":false,
			"agentState":false,
			"inputTelemetry":false,
			"outputTelemetry":false
		}
	}`
}
