package aigateway

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRouteAcceptsOnlyReviewedSemanticProfile(t *testing.T) {
	path := writeRouteFile(t, acceptedRouteObject())
	first, err := LoadRoute(path)
	if err != nil {
		t.Fatalf("load route: %v", err)
	}
	validated := first.Validated()
	if validated.Requested.Alias != semanticRouteAlias ||
		validated.Requested.Gateway != semanticGateway ||
		validated.Requested.Model != semanticModel ||
		validated.Requested.Provider != semanticProvider ||
		validated.Requested.RouteVersion != semanticRouteVersion ||
		validated.Requested.PrivacyPolicyVersion != semanticPrivacy {
		t.Fatalf("validated route = %#v", validated.Requested)
	}
	if strings.Contains(string(validated.SanitizedConfig), "secret") || len(validated.ConfigDigest) != 64 {
		t.Fatalf("sanitized route = %s / %q", validated.SanitizedConfig, validated.ConfigDigest)
	}

	prettyPath := filepath.Join(t.TempDir(), "route.json")
	pretty, err := json.MarshalIndent(acceptedRouteObject(), "", "  ")
	if err != nil {
		t.Fatalf("marshal pretty route: %v", err)
	}
	if err := os.WriteFile(prettyPath, pretty, 0o600); err != nil {
		t.Fatalf("write pretty route: %v", err)
	}
	second, err := LoadRoute(prettyPath)
	if err != nil {
		t.Fatalf("load pretty route: %v", err)
	}
	if second.Validated().ConfigDigest != validated.ConfigDigest ||
		string(second.Validated().SanitizedConfig) != string(validated.SanitizedConfig) {
		t.Fatal("equivalent route formatting changed the canonical identity")
	}
}

func TestLoadRouteAcceptsExplicitPrivacyPolicyChoices(t *testing.T) {
	for _, test := range []struct {
		name      string
		retention bool
		training  bool
	}{
		{name: "neither requested"},
		{name: "retention requested", retention: true},
		{name: "training requested", training: true},
		{name: "both requested", retention: true, training: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := acceptedRouteObject()
			profileObject(config)["zeroDataRetention"] = test.retention
			profileObject(config)["disallowPromptTraining"] = test.training
			route, err := LoadRoute(writeRouteFile(t, config))
			if err != nil {
				t.Fatalf("load route: %v", err)
			}
			if route.profile.ZeroDataRetention == nil || *route.profile.ZeroDataRetention != test.retention ||
				route.profile.DisallowPromptTraining == nil || *route.profile.DisallowPromptTraining != test.training {
				t.Fatalf("privacy choices = %#v", route.profile)
			}
		})
	}
}

func TestLoadRouteRejectsUnavailableAndUnknownConfiguration(t *testing.T) {
	if _, err := LoadRoute(""); !errors.Is(err, ErrRouteUnavailable) {
		t.Fatalf("empty path error = %v", err)
	}
	if _, err := LoadRoute(filepath.Join(t.TempDir(), "missing.json")); !errors.Is(err, ErrRouteUnavailable) {
		t.Fatalf("missing path error = %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "unknown top-level field", mutate: func(config map[string]any) { config["apiKey"] = "secret" }},
		{name: "unknown route alias", mutate: func(config map[string]any) {
			config["routes"].(map[string]any)["semantic-v2"] = acceptedProfileObject()
		}},
		{name: "unknown profile field", mutate: func(config map[string]any) {
			profileObject(config)["fallback"] = true
		}},
		{name: "missing semantic route", mutate: func(config map[string]any) {
			delete(config["routes"].(map[string]any), semanticRouteAlias)
		}},
		{name: "missing retention choice", mutate: func(config map[string]any) {
			delete(profileObject(config), "zeroDataRetention")
		}},
		{name: "missing training choice", mutate: func(config map[string]any) {
			delete(profileObject(config), "disallowPromptTraining")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := acceptedRouteObject()
			test.mutate(config)
			if _, err := LoadRoute(writeRouteFile(t, config)); !errors.Is(err, ErrRouteInvalid) {
				t.Fatalf("error = %v, want ErrRouteInvalid", err)
			}
		})
	}
}

func TestLoadRouteRejectsEveryChangedPolicyField(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		value any
	}{
		{name: "gateway", field: "gateway", value: "other-gateway"},
		{name: "base URL", field: "baseUrl", value: "https://example.invalid/v1"},
		{name: "model", field: "model", value: "openai/other"},
		{name: "provider allowlist", field: "providerAllowlist", value: []string{"cerebras", "other"}},
		{name: "provider order", field: "providerOrder", value: []string{"other"}},
		{name: "capability", field: "requiredCapabilities", value: []string{"json"}},
		{name: "timeout", field: "timeoutMilliseconds", value: 59_999},
		{name: "output tokens", field: "maxOutputTokens", value: 2_048},
		{name: "retries", field: "maxRetries", value: 1},
		{name: "route version", field: "routeVersion", value: "route-v2"},
		{name: "privacy version", field: "privacyPolicyVersion", value: "other-privacy-v1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := acceptedRouteObject()
			profileObject(config)[test.field] = test.value
			if _, err := LoadRoute(writeRouteFile(t, config)); !errors.Is(err, ErrRouteInvalid) {
				t.Fatalf("error = %v, want ErrRouteInvalid", err)
			}
		})
	}
}

func acceptedRouteObject() map[string]any {
	return map[string]any{"routes": map[string]any{semanticRouteAlias: acceptedProfileObject()}}
}

func acceptedProfileObject() map[string]any {
	return map[string]any{
		"gateway": semanticGateway, "baseUrl": semanticBaseURL, "model": semanticModel,
		"providerAllowlist": []string{semanticProvider}, "providerOrder": []string{semanticProvider},
		"requiredCapabilities": []string{semanticCapability}, "zeroDataRetention": false,
		"disallowPromptTraining": false, "timeoutMilliseconds": semanticTimeoutMilliseconds,
		"maxOutputTokens": semanticMaxOutputTokens, "maxRetries": semanticMaxRetries,
		"routeVersion": semanticRouteVersion, "privacyPolicyVersion": semanticPrivacy,
	}
}

func profileObject(config map[string]any) map[string]any {
	return config["routes"].(map[string]any)[semanticRouteAlias].(map[string]any)
}

func writeRouteFile(t *testing.T, config map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal route: %v", err)
	}
	path := filepath.Join(t.TempDir(), "route.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write route: %v", err)
	}
	return path
}
