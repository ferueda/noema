package eve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const (
	ExecutorKind       = "eve"
	ExecutorVersion    = "0.27.8"
	streamFormat       = "ndjson"
	streamVersion      = "19"
	routeUsername      = "noema"
	maxControlBody     = 64 * 1024
	maxInfoBody        = 1024 * 1024
	maxSchemaBody      = 256 * 1024
	maxContinuation    = 4 * 1024
	maxIdentifierBytes = 256
)

var (
	ErrUnavailable     = errors.New("Eve executor unavailable")
	ErrMismatch        = errors.New("Eve executor mismatch")
	ErrProtocol        = errors.New("Eve executor protocol invalid")
	ErrExecutionFailed = errors.New("Eve execution failed")
)

// Config contains operational connection details plus the exact model identity
// Eve exposes at runtime. These values never become part of a receipt.
type Config struct {
	BaseURL         string
	RoutePassword   string
	ObservedModelID string
	HTTPClient      *http.Client
}

// Executor is a loopback-only adapter for the pinned Eve HTTP protocol.
type Executor struct {
	baseURL         *url.URL
	password        string
	observedModelID string
	client          *http.Client
}

func NewExecutor(config Config) (*Executor, error) {
	baseURL, err := validateBaseURL(config.BaseURL)
	if err != nil || strings.TrimSpace(config.RoutePassword) == "" ||
		!validID(config.ObservedModelID) {
		return nil, errors.New("Eve executor configuration invalid")
	}
	client := http.DefaultClient
	if config.HTTPClient != nil {
		client = config.HTTPClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Executor{
		baseURL:         baseURL,
		password:        config.RoutePassword,
		observedModelID: config.ObservedModelID,
		client:          &copyClient,
	}, nil
}

func validateBaseURL(raw string) (*url.URL, error) {
	value, err := url.Parse(raw)
	if err != nil || (value.Scheme != "http" && value.Scheme != "https") ||
		value.User != nil || value.RawQuery != "" || value.Fragment != "" ||
		(value.Path != "" && value.Path != "/") || value.Hostname() == "" ||
		value.Port() == "" {
		return nil, errors.New("invalid Eve base URL")
	}
	host := value.Hostname()
	if !strings.EqualFold(host, "localhost") {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			return nil, errors.New("invalid Eve base URL")
		}
	}
	value.Path = ""
	return value, nil
}

func (executor *Executor) endpoint(path string) string {
	value := *executor.baseURL
	value.Path = path
	return value.String()
}

func (executor *Executor) protectedRequest(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, executor.endpoint(path), body)
	if err != nil {
		return nil, ErrUnavailable
	}
	request.SetBasicAuth(routeUsername, executor.password)
	return request, nil
}

func (executor *Executor) Execute(
	ctx context.Context,
	request domain.AgentExecutionRequestV1,
	schema domain.StructuredOutputSchema,
) (domain.AgentExecutionResponseV1, error) {
	if request.Validate() != nil || request.RequiredOutputSchema != schema.Identity ||
		schema.Identity.Disposition != domain.StructuredOutputDispositionStrict {
		return domain.AgentExecutionResponseV1{}, categorized(
			ErrProtocol,
			domain.AgentFailureCategoryInputInvalid,
		)
	}
	canonicalSchema, err := compactJSONObject(schema.CanonicalJSON, maxSchemaBody)
	if err != nil {
		return domain.AgentExecutionResponseV1{}, categorized(
			ErrProtocol,
			domain.AgentFailureCategoryInputInvalid,
		)
	}
	digest, err := platform.Fingerprint(json.RawMessage(canonicalSchema))
	if err != nil || digest != schema.Identity.Digest {
		return domain.AgentExecutionResponseV1{}, categorized(
			ErrProtocol,
			domain.AgentFailureCategoryInputInvalid,
		)
	}

	deadlineContext, cancel := context.WithDeadline(ctx, request.DeadlineAt)
	defer cancel()
	if err := deadlineContext.Err(); err != nil {
		return domain.AgentExecutionResponseV1{}, categorized(
			ErrUnavailable,
			domain.AgentFailureCategoryExecutorUnavailable,
		)
	}
	message, err := json.Marshal(request)
	if err != nil {
		return domain.AgentExecutionResponseV1{}, categorized(
			ErrProtocol,
			domain.AgentFailureCategoryInputInvalid,
		)
	}

	startedAt := time.Now()
	receipt := domain.AgentExecutionReceiptV1{
		ExecutorKind:   ExecutorKind,
		RequestedRoute: &request.Configuration.Route,
	}
	sessionID, err := executor.createSession(deadlineContext, message, canonicalSchema)
	if err != nil {
		return failedResponse(receipt, startedAt, err)
	}
	receipt.SessionID = sessionID

	candidate, err := executor.consumeSession(deadlineContext, sessionID, message, request, &receipt)
	if err != nil {
		return failedResponse(receipt, startedAt, err)
	}
	receipt.LatencyMilliseconds = elapsedMilliseconds(startedAt)
	response := domain.AgentExecutionResponseV1{
		ContractVersion: domain.AgentExecutionContractVersion,
		CandidateJSON:   candidate,
		Receipt:         receipt,
	}
	if response.Validate() != nil {
		return failedResponse(receipt, startedAt, categorized(
			ErrProtocol,
			domain.AgentFailureCategoryResponseInvalid,
		))
	}
	return response, nil
}

func (executor *Executor) createSession(
	ctx context.Context,
	message json.RawMessage,
	schema json.RawMessage,
) (string, error) {
	body, err := json.Marshal(struct {
		Message      string          `json:"message"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	}{
		Message:      string(message),
		OutputSchema: schema,
	})
	if err != nil {
		return "", categorized(ErrProtocol, domain.AgentFailureCategoryInputInvalid)
	}
	request, err := executor.protectedRequest(
		ctx,
		http.MethodPost,
		"/eve/v1/session",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := executor.client.Do(request)
	if err != nil {
		return "", categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return "", categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	if !isJSONContentType(response.Header.Get("Content-Type")) {
		return "", categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	payload, err := readBounded(response.Body, maxControlBody)
	if err != nil {
		return "", categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	var result struct {
		ContinuationToken string `json:"continuationToken"`
		OK                bool   `json:"ok"`
		SessionID         string `json:"sessionId"`
	}
	if decodeStrictJSON(payload, &result) != nil || !result.OK ||
		!validID(result.SessionID) || len(result.ContinuationToken) == 0 ||
		len(result.ContinuationToken) > maxContinuation ||
		response.Header.Get("x-eve-session-id") != result.SessionID {
		return "", categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	return result.SessionID, nil
}

func failedResponse(
	receipt domain.AgentExecutionReceiptV1,
	startedAt time.Time,
	err error,
) (domain.AgentExecutionResponseV1, error) {
	receipt.LatencyMilliseconds = elapsedMilliseconds(startedAt)
	receipt.FailureCategory = FailureCategory(err)
	if receipt.ValidateObserved() != nil {
		return domain.AgentExecutionResponseV1{}, err
	}
	return domain.AgentExecutionResponseV1{Receipt: receipt}, err
}

func elapsedMilliseconds(startedAt time.Time) *int64 {
	value := time.Since(startedAt).Milliseconds()
	if value < 0 {
		value = 0
	}
	return &value
}

type executorError struct {
	cause    error
	category string
}

func (value *executorError) Error() string { return value.cause.Error() }
func (value *executorError) Unwrap() error { return value.cause }

func categorized(cause error, category string) error {
	return &executorError{cause: cause, category: category}
}

// FailureCategory returns the fixed safe failure category carried by an
// adapter error. It never exposes a remote error code or response body.
func FailureCategory(err error) string {
	var value *executorError
	if errors.As(err, &value) {
		return value.category
	}
	return domain.AgentFailureCategoryExecutorFailed
}

func compactJSONObject(value json.RawMessage, maxBytes int) ([]byte, error) {
	if len(value) == 0 || len(value) > maxBytes {
		return nil, ErrProtocol
	}
	var decoded map[string]json.RawMessage
	if decodeStrictJSON(value, &decoded) != nil || decoded == nil {
		return nil, ErrProtocol
	}
	var output bytes.Buffer
	if err := json.Compact(&output, value); err != nil || output.Len() > maxBytes {
		return nil, ErrProtocol
	}
	return output.Bytes(), nil
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || int64(len(value)) > maxBytes {
		return nil, ErrProtocol
	}
	return value, nil
}

func decodeStrictJSON(value []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("extra JSON content")
	}
	return nil
}

func isJSONContentType(value string) bool {
	return value == "application/json" || strings.HasPrefix(value, "application/json;")
}

func validID(value string) bool {
	return value != "" && len(value) <= maxIdentifierBytes &&
		!strings.ContainsAny(value, "\r\n\t ")
}
