package aigateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const (
	semanticRouteAlias   = "semantic-v1"
	semanticGateway      = "vercel-ai-gateway"
	semanticBaseURL      = "https://ai-gateway.vercel.sh/v1"
	semanticModel        = "openai/gpt-oss-120b"
	semanticProvider     = "cerebras"
	semanticCapability   = "strict-json-schema"
	semanticRouteVersion = "route-v1"
	semanticPrivacy      = "deterministic-privacy-v1"

	semanticTimeoutMilliseconds = 60_000
	semanticMaxOutputTokens     = 4_096
	semanticMaxRetries          = 0
	maxRouteFileBytes           = 64 * 1024
)

var (
	ErrRouteUnavailable = errors.New("semantic route configuration is unavailable")
	ErrRouteInvalid     = errors.New("semantic route configuration is invalid")
)

type routeFile struct {
	Routes semanticRoutes `json:"routes"`
}

type semanticRoutes struct {
	SemanticV1 routeProfile `json:"semantic-v1"`
}

type routeProfile struct {
	Gateway                string   `json:"gateway"`
	BaseURL                string   `json:"baseUrl"`
	Model                  string   `json:"model"`
	ProviderAllowlist      []string `json:"providerAllowlist"`
	ProviderOrder          []string `json:"providerOrder"`
	RequiredCapabilities   []string `json:"requiredCapabilities"`
	ZeroDataRetention      *bool    `json:"zeroDataRetention"`
	DisallowPromptTraining *bool    `json:"disallowPromptTraining"`
	TimeoutMilliseconds    int      `json:"timeoutMilliseconds"`
	MaxOutputTokens        int      `json:"maxOutputTokens"`
	MaxRetries             int      `json:"maxRetries"`
	RouteVersion           string   `json:"routeVersion"`
	PrivacyPolicyVersion   string   `json:"privacyPolicyVersion"`
}

// Route is an exact, composition-root-approved semantic-v1 route. Its fields
// stay private so callers cannot weaken the reviewed transport or policy.
type Route struct {
	profile   routeProfile
	validated domain.ValidatedModelRoute
}

func LoadRoute(path string) (Route, error) {
	if strings.TrimSpace(path) == "" {
		return Route{}, ErrRouteUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return Route{}, ErrRouteUnavailable
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxRouteFileBytes+1))
	if err != nil || len(content) == 0 || len(content) > maxRouteFileBytes {
		return Route{}, ErrRouteInvalid
	}
	var config routeFile
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Route{}, ErrRouteInvalid
	}
	if err := requireJSONEOF(decoder); err != nil || !acceptedProfile(config.Routes.SemanticV1) {
		return Route{}, ErrRouteInvalid
	}

	profile := config.Routes.SemanticV1
	validated, err := buildValidatedRoute(profile)
	if err != nil {
		return Route{}, ErrRouteInvalid
	}
	return Route{profile: profile, validated: validated}, nil
}

func (route Route) Validated() domain.ValidatedModelRoute {
	validated := route.validated
	validated.SanitizedConfig = append(json.RawMessage(nil), route.validated.SanitizedConfig...)
	return validated
}

func acceptedProfile(profile routeProfile) bool {
	return profile.Gateway == semanticGateway &&
		profile.BaseURL == semanticBaseURL &&
		profile.Model == semanticModel &&
		slices.Equal(profile.ProviderAllowlist, []string{semanticProvider}) &&
		slices.Equal(profile.ProviderOrder, profile.ProviderAllowlist) &&
		slices.Equal(profile.RequiredCapabilities, []string{semanticCapability}) &&
		profile.ZeroDataRetention != nil && profile.DisallowPromptTraining != nil &&
		profile.TimeoutMilliseconds == semanticTimeoutMilliseconds &&
		profile.MaxOutputTokens == semanticMaxOutputTokens &&
		profile.MaxRetries == semanticMaxRetries &&
		profile.RouteVersion == semanticRouteVersion &&
		profile.PrivacyPolicyVersion == semanticPrivacy
}

func buildValidatedRoute(profile routeProfile) (domain.ValidatedModelRoute, error) {
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return domain.ValidatedModelRoute{}, err
	}
	digest, err := platform.Fingerprint(json.RawMessage(profileJSON))
	if err != nil {
		return domain.ValidatedModelRoute{}, err
	}
	return domain.ValidatedModelRoute{
		Requested: domain.RequestedModelRoute{
			Alias: semanticRouteAlias, Gateway: profile.Gateway, Model: profile.Model,
			Provider: profile.ProviderAllowlist[0], RouteVersion: profile.RouteVersion,
			PrivacyPolicyVersion: profile.PrivacyPolicyVersion,
		},
		SanitizedConfig: json.RawMessage(profileJSON), ConfigDigest: digest,
	}, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrRouteInvalid
	}
	return nil
}
