package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	maxAPIKeyBytes                = 4 * 1024
	maxRequestIDBytes             = 256
	maxGatewayResponseBytes       = 2 * 1024 * 1024
	maxUsageTokens          int64 = 1_000_000_000
)

var (
	ErrGeneratorUnavailable = errors.New("AI Gateway generator is unavailable")
	ErrRequestInvalid       = errors.New("AI Gateway request is invalid")
	ErrRequestFailed        = errors.New("AI Gateway request failed")
	ErrResponseInvalid      = errors.New("AI Gateway response is invalid")
	errRedirectBlocked      = errors.New("AI Gateway redirect is blocked")
	errResponseTooLarge     = errors.New("AI Gateway response is too large")

	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,256}$`)
)

type Generator struct {
	route  Route
	client openai.ChatService
}

type generationFailure struct {
	cause    error
	category string
}

func (failure generationFailure) Error() string {
	return failure.cause.Error()
}

func (failure generationFailure) Unwrap() error {
	return failure.cause
}

func (failure generationFailure) SemanticGenerationFailureCategory() string {
	return failure.category
}

func NewGenerator(route Route, apiKey string, httpClient *http.Client) (*Generator, error) {
	if err := validateRoute(route); err != nil || !validSecret(apiKey) {
		return nil, ErrGeneratorUnavailable
	}
	client := &http.Client{}
	if httpClient != nil {
		copy := *httpClient
		client = &copy
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = boundedResponseTransport{next: transport, limit: maxGatewayResponseBytes}
	// Approval covers only the locked Gateway origin. Never replay the request
	// body to a redirect target, even when an injected client allows redirects.
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errRedirectBlocked
	}
	client.Timeout = time.Duration(route.profile.TimeoutMilliseconds) * time.Millisecond
	service := openai.NewChatService(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(route.profile.BaseURL),
		option.WithHTTPClient(client),
		option.WithMaxRetries(route.profile.MaxRetries),
	)
	return &Generator{route: route, client: service}, nil
}

func (generator *Generator) Generate(
	ctx context.Context,
	request application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	if generator == nil || validateRoute(generator.route) != nil {
		return application.SemanticGenerationResult{}, generationFailure{
			cause: ErrGeneratorUnavailable, category: application.SemanticGenerationFailureRequest,
		}
	}
	schema, err := validateGenerationRequest(request, generator.route)
	if err != nil {
		return application.SemanticGenerationResult{}, generationFailure{
			cause: err, category: application.SemanticGenerationFailureRequest,
		}
	}
	input, err := json.Marshal(request.Input)
	if err != nil {
		return application.SemanticGenerationResult{}, generationFailure{
			cause: ErrRequestInvalid, category: application.SemanticGenerationFailureRequest,
		}
	}
	system := fmt.Sprintf(
		"Prompt version: %s\nOutput schema: %s version %d\n\n%s",
		request.PromptVersion, request.Schema.Identity.Name, request.Schema.Identity.Version,
		request.Instructions,
	)
	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModel(generator.route.profile.Model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(string(input)),
		},
		N:                   openai.Int(1),
		MaxCompletionTokens: openai.Int(int64(generator.route.profile.MaxOutputTokens)),
		Store:               openai.Bool(false),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name: request.Schema.Identity.Name, Strict: openai.Bool(true), Schema: schema,
				},
			},
		},
	}
	providerOptions := map[string]any{
		"only":                   append([]string(nil), generator.route.profile.ProviderAllowlist...),
		"order":                  append([]string(nil), generator.route.profile.ProviderOrder...),
		"zeroDataRetention":      *generator.route.profile.ZeroDataRetention,
		"disallowPromptTraining": *generator.route.profile.DisallowPromptTraining,
	}
	started := time.Now()
	response, err := generator.client.Completions.New(
		ctx, params,
		option.WithJSONSet("stream", false),
		option.WithJSONSet("providerOptions.gateway", providerOptions),
	)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return application.SemanticGenerationResult{}, classifyRequestFailure(err)
	}
	result, err := decodeResponse(response, generator.route, latency)
	if err != nil {
		return application.SemanticGenerationResult{}, generationFailure{
			cause: err, category: application.SemanticGenerationFailureResponse,
		}
	}
	return result, nil
}

func classifyRequestFailure(err error) error {
	category := application.SemanticGenerationFailureTransport
	var apiError *openai.Error
	var networkError net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		category = application.SemanticGenerationFailureTimeout
	case errors.As(err, &apiError):
		switch {
		case apiError.StatusCode == http.StatusUnauthorized:
			category = application.SemanticGenerationFailureAuthentication
		case apiError.StatusCode == http.StatusPaymentRequired || apiError.StatusCode == http.StatusForbidden:
			category = application.SemanticGenerationFailurePermission
		case apiError.StatusCode == http.StatusRequestTimeout:
			category = application.SemanticGenerationFailureTimeout
		case apiError.StatusCode == http.StatusTooManyRequests:
			category = application.SemanticGenerationFailureRateLimited
		case apiError.StatusCode >= http.StatusInternalServerError:
			category = application.SemanticGenerationFailureUpstream
		case apiError.StatusCode >= http.StatusBadRequest:
			category = rejectedRequestCategory(apiError.Message)
		}
	case errors.As(err, &networkError) && networkError.Timeout():
		category = application.SemanticGenerationFailureTimeout
	}
	return generationFailure{cause: ErrRequestFailed, category: category}
}

func rejectedRequestCategory(message string) string {
	message = strings.ToLower(message)
	switch {
	case strings.Contains(message, "schema") || strings.Contains(message, "response_format"):
		return application.SemanticGenerationFailureSchema
	case strings.Contains(message, "context length") || strings.Contains(message, "maximum context") ||
		strings.Contains(message, "context window") || strings.Contains(message, "too many tokens"):
		return application.SemanticGenerationFailureContext
	case strings.Contains(message, "content policy") || strings.Contains(message, "safety policy") ||
		strings.Contains(message, "moderation"):
		return application.SemanticGenerationFailureContent
	default:
		return application.SemanticGenerationFailureRequest
	}
}

func validateGenerationRequest(request application.SemanticGenerationRequest, route Route) (any, error) {
	if request.Route != route.validated.Requested || strings.TrimSpace(request.Instructions) == "" ||
		strings.TrimSpace(request.PromptVersion) == "" || request.Schema.Identity.Name == "" ||
		request.Schema.Identity.Version < 1 ||
		request.Schema.Identity.Disposition != domain.StructuredOutputDispositionStrict ||
		len(request.Schema.CanonicalJSON) == 0 || !json.Valid(request.Schema.CanonicalJSON) {
		return nil, ErrRequestInvalid
	}
	digest, err := platform.Fingerprint(json.RawMessage(request.Schema.CanonicalJSON))
	if err != nil || digest != request.Schema.Identity.Digest {
		return nil, ErrRequestInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(request.Schema.CanonicalJSON))
	var schema map[string]any
	if err := decoder.Decode(&schema); err != nil || schema == nil || requireJSONEOF(decoder) != nil {
		return nil, ErrRequestInvalid
	}
	return schema, nil
}

type gatewayResponseMetadata struct {
	Choices []gatewayChoiceMetadata `json:"choices"`
	Usage   *gatewayUsage           `json:"usage"`
}

type gatewayChoiceMetadata struct {
	Message gatewayMessageMetadata `json:"message"`
}

type gatewayMessageMetadata struct {
	ProviderMetadata gatewayProviderMetadata `json:"provider_metadata"`
}

type gatewayProviderMetadata struct {
	Gateway gatewayMetadata `json:"gateway"`
}

type gatewayMetadata struct {
	Routing      gatewayRouting  `json:"routing"`
	GenerationID string          `json:"generationId"`
	Cost         json.RawMessage `json:"cost"`
}

type gatewayRouting struct {
	OriginalModelID  string `json:"originalModelId"`
	ResolvedProvider string `json:"resolvedProvider"`
	CanonicalSlug    string `json:"canonicalSlug"`
}

type gatewayUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	TotalTokens      *int64 `json:"total_tokens"`
}

func decodeResponse(
	response *openai.ChatCompletion,
	route Route,
	latency int64,
) (application.SemanticGenerationResult, error) {
	if response == nil || len(response.Choices) != 1 || latency < 0 {
		return application.SemanticGenerationResult{}, ErrResponseInvalid
	}
	choice := response.Choices[0]
	if choice.FinishReason != "stop" || choice.Message.Refusal != "" ||
		len(choice.Message.ToolCalls) != 0 || strings.TrimSpace(choice.Message.Content) == "" {
		return application.SemanticGenerationResult{}, ErrResponseInvalid
	}
	var extra gatewayResponseMetadata
	if err := json.Unmarshal([]byte(response.RawJSON()), &extra); err != nil {
		return application.SemanticGenerationResult{}, ErrResponseInvalid
	}
	if len(extra.Choices) != 1 {
		return application.SemanticGenerationResult{}, ErrResponseInvalid
	}
	gateway := extra.Choices[0].Message.ProviderMetadata.Gateway
	routing := gateway.Routing
	if routing.OriginalModelID != route.profile.Model ||
		routing.CanonicalSlug != route.profile.Model ||
		routing.ResolvedProvider != route.profile.ProviderAllowlist[0] {
		return application.SemanticGenerationResult{}, ErrResponseInvalid
	}
	cost, err := parseCost(gateway.Cost)
	if err != nil {
		return application.SemanticGenerationResult{}, err
	}
	requestID, err := boundedRequestID(gateway.GenerationID, response.ID)
	if err != nil {
		return application.SemanticGenerationResult{}, err
	}
	inputTokens, outputTokens, totalTokens, err := parseUsage(extra.Usage)
	if err != nil {
		return application.SemanticGenerationResult{}, err
	}
	candidates, err := decodeCandidates(choice.Message.Content)
	if err != nil {
		return application.SemanticGenerationResult{}, err
	}
	return application.SemanticGenerationResult{
		Candidates: candidates,
		Model: domain.ModelExecutionMetadata{
			ResolvedProvider: routing.ResolvedProvider, ResolvedModel: routing.CanonicalSlug,
			RequestID: requestID, InputTokens: inputTokens, OutputTokens: outputTokens,
			TotalTokens: totalTokens, LatencyMilliseconds: &latency, CostUSD: cost,
		},
	}, nil
}

func decodeCandidates(content string) ([]domain.ClaimCandidate, error) {
	var envelope struct {
		Claims []domain.ClaimCandidate `json:"claims"`
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || envelope.Claims == nil {
		return nil, ErrResponseInvalid
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, ErrResponseInvalid
	}
	return envelope.Claims, nil
}

func parseCost(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || !domain.ValidModelCostUSD(value) {
		return nil, ErrResponseInvalid
	}
	return &value, nil
}

func parseUsage(usage *gatewayUsage) (input, output, total *int, err error) {
	if usage == nil {
		return nil, nil, nil, nil
	}
	input, err = boundedTokenCount(usage.PromptTokens)
	if err != nil {
		return nil, nil, nil, err
	}
	output, err = boundedTokenCount(usage.CompletionTokens)
	if err != nil {
		return nil, nil, nil, err
	}
	total, err = boundedTokenCount(usage.TotalTokens)
	if err != nil {
		return nil, nil, nil, err
	}
	if input != nil && output != nil && total != nil && *input+*output != *total {
		return nil, nil, nil, ErrResponseInvalid
	}
	return input, output, total, nil
}

func boundedTokenCount(value *int64) (*int, error) {
	if value == nil {
		return nil, nil
	}
	if *value < 0 || *value > maxUsageTokens {
		return nil, ErrResponseInvalid
	}
	converted := int(*value)
	return &converted, nil
}

func boundedRequestID(generationID, completionID string) (string, error) {
	value := generationID
	if value == "" {
		value = completionID
	}
	if value == "" {
		return "", nil
	}
	if len(value) > maxRequestIDBytes || !requestIDPattern.MatchString(value) {
		return "", ErrResponseInvalid
	}
	return value, nil
}

func validateRoute(route Route) error {
	if !acceptedProfile(route.profile) {
		return ErrGeneratorUnavailable
	}
	want, err := buildValidatedRoute(route.profile)
	if err != nil || route.validated.Requested != want.Requested ||
		route.validated.ConfigDigest != want.ConfigDigest ||
		!bytes.Equal(route.validated.SanitizedConfig, want.SanitizedConfig) {
		return ErrGeneratorUnavailable
	}
	return nil
}

func validSecret(value string) bool {
	if value == "" || len(value) > maxAPIKeyBytes {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char == 0x7f {
			return false
		}
	}
	return true
}

type boundedResponseTransport struct {
	next  http.RoundTripper
	limit int64
}

func (transport boundedResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.next.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = &boundedResponseBody{body: response.Body, remaining: transport.limit}
	return response, nil
}

type boundedResponseBody struct {
	body      io.ReadCloser
	remaining int64
	exceeded  bool
}

func (body *boundedResponseBody) Read(buffer []byte) (int, error) {
	if body.exceeded {
		return 0, errResponseTooLarge
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if body.remaining == 0 {
		var probe [1]byte
		count, err := body.body.Read(probe[:])
		if count > 0 {
			body.exceeded = true
			return 0, errResponseTooLarge
		}
		return 0, err
	}
	if int64(len(buffer)) > body.remaining {
		buffer = buffer[:body.remaining]
	}
	count, err := body.body.Read(buffer)
	body.remaining -= int64(count)
	return count, err
}

func (body *boundedResponseBody) Close() error {
	return body.body.Close()
}

var _ application.SemanticGenerator = (*Generator)(nil)
