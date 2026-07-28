package eve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

const (
	maxStreamBody       = 2 * 1024 * 1024
	maxStreamLine       = 256 * 1024
	maxStreamEvents     = 4_096
	maxDiscardedText    = 256 * 1024
	maxTimestampBytes   = 64
	maxFailureCodeBytes = 128
)

type streamState int

const (
	expectSessionStarted streamState = iota
	expectTurnStarted
	expectMessageReceived
	expectStepStarted
	insideStep
	afterStepCompleted
	afterResultCompleted
	expectSessionWaiting
	expectTurnFailed
	expectFailureTerminal
)

type streamParser struct {
	state           streamState
	requestMessage  []byte
	request         domain.AgentExecutionRequestV1
	observedModelID string
	receipt         *domain.AgentExecutionReceiptV1
	sessionID       string
	turnID          string
	eventCount      int
	totalBytes      int
	lastTimestamp   time.Time
	reasoningSoFar  string
	messageSoFar    string
	candidate       json.RawMessage
	remoteFailed    bool
}

func (executor *Executor) consumeSession(
	ctx context.Context,
	sessionID string,
	message []byte,
	request domain.AgentExecutionRequestV1,
	receipt *domain.AgentExecutionReceiptV1,
) (json.RawMessage, error) {
	streamRequest, err := executor.protectedRequest(
		ctx,
		http.MethodGet,
		"/eve/v1/session/"+sessionID+"/stream",
		nil,
	)
	if err != nil {
		return nil, categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	query := streamRequest.URL.Query()
	query.Set("startIndex", "0")
	streamRequest.URL.RawQuery = query.Encode()
	response, err := executor.client.Do(streamRequest)
	if err != nil {
		return nil, categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, categorized(ErrUnavailable, domain.AgentFailureCategoryExecutorUnavailable)
	}
	if response.Header.Get("Content-Type") != "application/x-ndjson; charset=utf-8" ||
		response.Header.Get("x-eve-stream-format") != streamFormat ||
		response.Header.Get("x-eve-stream-version") != streamVersion ||
		response.Header.Get("x-eve-session-id") != sessionID {
		return nil, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}

	parser := streamParser{
		state:           expectSessionStarted,
		requestMessage:  message,
		request:         request,
		observedModelID: executor.observedModelID,
		receipt:         receipt,
		sessionID:       sessionID,
	}
	return parser.consume(response.Body)
}

func (parser *streamParser) consume(body io.Reader) (json.RawMessage, error) {
	reader := bufio.NewReaderSize(body, maxStreamLine+1)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxStreamLine {
			return nil, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
		}
		parser.totalBytes += len(line)
		if parser.totalBytes > maxStreamBody {
			return nil, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
		}
		if err != nil {
			// Durable stream events are complete newline-delimited records.
			return nil, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
		}
		parser.eventCount++
		if parser.eventCount > maxStreamEvents || len(bytes.TrimSpace(line)) == 0 {
			return nil, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
		}
		terminal, eventErr := parser.accept(bytes.TrimSuffix(line, []byte{'\n'}))
		if eventErr != nil {
			return nil, eventErr
		}
		if terminal {
			// ReadSlice may have buffered an already-written event after the
			// terminal boundary. Reject it without waiting on the live stream.
			if reader.Buffered() != 0 {
				return nil, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
			}
			if parser.remoteFailed {
				return nil, categorized(ErrExecutionFailed, domain.AgentFailureCategoryExecutorFailed)
			}
			return parser.candidate, nil
		}
	}
}

type eventEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	Meta *struct {
		At string `json:"at"`
	} `json:"meta"`
}

func (parser *streamParser) accept(line []byte) (bool, error) {
	var event eventEnvelope
	if decodeStrictJSON(line, &event) != nil || event.Data == nil ||
		event.Meta == nil || len(event.Meta.At) == 0 ||
		len(event.Meta.At) > maxTimestampBytes {
		return false, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	at, err := time.Parse(time.RFC3339Nano, event.Meta.At)
	if err != nil || !parser.lastTimestamp.IsZero() && at.Before(parser.lastTimestamp) {
		return false, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
	parser.lastTimestamp = at

	switch event.Type {
	case "session.started":
		return false, parser.acceptSessionStarted(event.Data)
	case "turn.started":
		return false, parser.acceptTurnStarted(event.Data)
	case "message.received":
		return false, parser.acceptMessageReceived(event.Data)
	case "step.started":
		return false, parser.acceptStepStarted(event.Data)
	case "reasoning.appended":
		return false, parser.acceptReasoningAppended(event.Data)
	case "reasoning.completed":
		return false, parser.acceptReasoningCompleted(event.Data)
	case "message.appended":
		return false, parser.acceptMessageAppended(event.Data)
	case "message.completed":
		return false, parser.acceptMessageCompleted(event.Data)
	case "step.completed":
		return false, parser.acceptStepCompleted(event.Data)
	case "result.completed":
		return false, parser.acceptResultCompleted(event.Data)
	case "turn.completed":
		return false, parser.acceptTurnCompleted(event.Data)
	case "step.failed":
		return false, parser.acceptStepFailed(event.Data)
	case "turn.failed":
		return false, parser.acceptTurnFailed(event.Data)
	case "session.failed":
		if err := parser.acceptSessionFailed(event.Data); err != nil {
			return false, err
		}
		return true, nil
	case "session.waiting":
		if err := parser.acceptSessionWaiting(event.Data); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
	}
}

type sessionStartedData struct {
	Invocation json.RawMessage `json:"invocation"`
	Runtime    *struct {
		AgentID    string          `json:"agentId"`
		AgentName  string          `json:"agentName"`
		EveVersion string          `json:"eveVersion"`
		Build      json.RawMessage `json:"build"`
		ModelID    string          `json:"modelId"`
	} `json:"runtime"`
}

func (parser *streamParser) acceptSessionStarted(data json.RawMessage) error {
	if parser.state != expectSessionStarted {
		return protocolError()
	}
	var value sessionStartedData
	if !hasFields(data, "runtime") ||
		decodeStrictJSON(data, &value) != nil || value.Runtime == nil ||
		value.Invocation != nil || !validID(value.Runtime.AgentID) ||
		value.Runtime.AgentName != "" && !validID(value.Runtime.AgentName) ||
		!validID(value.Runtime.EveVersion) || !validID(value.Runtime.ModelID) {
		return protocolError()
	}
	parser.receipt.ExecutorKind = ExecutorKind
	parser.receipt.ExecutorVersion = value.Runtime.EveVersion
	if value.Runtime.AgentID != parser.request.Agent.Name ||
		value.Runtime.AgentName != "" && value.Runtime.AgentName != parser.request.Agent.Name ||
		value.Runtime.ModelID != parser.observedModelID {
		return categorized(ErrMismatch, domain.AgentFailureCategoryExecutorMismatch)
	}
	if value.Runtime.EveVersion != ExecutorVersion {
		return categorized(ErrMismatch, domain.AgentFailureCategoryExecutorMismatch)
	}
	parser.state = expectTurnStarted
	return nil
}

type turnData struct {
	Sequence int    `json:"sequence"`
	TurnID   string `json:"turnId"`
}

func (parser *streamParser) acceptTurnStarted(data json.RawMessage) error {
	if parser.state != expectTurnStarted {
		return protocolError()
	}
	var value turnData
	if !hasFields(data, "sequence", "turnId") ||
		decodeStrictJSON(data, &value) != nil || value.Sequence != 0 || !validID(value.TurnID) {
		return protocolError()
	}
	parser.turnID = value.TurnID
	parser.receipt.TurnID = value.TurnID
	parser.state = expectMessageReceived
	return nil
}

type messageReceivedData struct {
	Message  string          `json:"message"`
	Parts    json.RawMessage `json:"parts"`
	Sequence int             `json:"sequence"`
	TurnID   string          `json:"turnId"`
}

func (parser *streamParser) acceptMessageReceived(data json.RawMessage) error {
	if parser.state != expectMessageReceived {
		return protocolError()
	}
	var value messageReceivedData
	if !hasFields(data, "message", "sequence", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validTurn(value.Sequence, value.TurnID) ||
		!bytes.Equal([]byte(value.Message), parser.requestMessage) ||
		!validMessageParts(value.Parts, value.Message) {
		return protocolError()
	}
	parser.state = expectStepStarted
	return nil
}

type stepCoordinates struct {
	Sequence  int    `json:"sequence"`
	StepIndex int    `json:"stepIndex"`
	TurnID    string `json:"turnId"`
}

func (parser *streamParser) acceptStepStarted(data json.RawMessage) error {
	if parser.state != expectStepStarted {
		return protocolError()
	}
	var value stepCoordinates
	if !hasFields(data, "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validStep(value) {
		return protocolError()
	}
	parser.state = insideStep
	return nil
}

type reasoningAppendedData struct {
	ReasoningDelta string `json:"reasoningDelta"`
	ReasoningSoFar string `json:"reasoningSoFar"`
	stepCoordinates
}

func (parser *streamParser) acceptReasoningAppended(data json.RawMessage) error {
	if parser.state != insideStep {
		return protocolError()
	}
	var value reasoningAppendedData
	if !hasFields(data, "reasoningDelta", "reasoningSoFar", "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validStep(value.stepCoordinates) ||
		len(value.ReasoningSoFar) > maxDiscardedText ||
		value.ReasoningSoFar != parser.reasoningSoFar+value.ReasoningDelta {
		return protocolError()
	}
	parser.reasoningSoFar = value.ReasoningSoFar
	return nil
}

type reasoningCompletedData struct {
	Reasoning string `json:"reasoning"`
	stepCoordinates
}

func (parser *streamParser) acceptReasoningCompleted(data json.RawMessage) error {
	if parser.state != insideStep {
		return protocolError()
	}
	var value reasoningCompletedData
	if !hasFields(data, "reasoning", "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validStep(value.stepCoordinates) ||
		len(value.Reasoning) > maxDiscardedText ||
		parser.reasoningSoFar != "" && value.Reasoning != parser.reasoningSoFar {
		return protocolError()
	}
	parser.reasoningSoFar = ""
	return nil
}

type messageAppendedData struct {
	MessageDelta string `json:"messageDelta"`
	MessageSoFar string `json:"messageSoFar"`
	stepCoordinates
}

func (parser *streamParser) acceptMessageAppended(data json.RawMessage) error {
	if parser.state != insideStep {
		return protocolError()
	}
	var value messageAppendedData
	if !hasFields(data, "messageDelta", "messageSoFar", "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validStep(value.stepCoordinates) ||
		len(value.MessageSoFar) > maxDiscardedText ||
		value.MessageSoFar != parser.messageSoFar+value.MessageDelta {
		return protocolError()
	}
	parser.messageSoFar = value.MessageSoFar
	return nil
}

type messageCompletedData struct {
	FinishReason string  `json:"finishReason"`
	Message      *string `json:"message"`
	stepCoordinates
}

func (parser *streamParser) acceptMessageCompleted(data json.RawMessage) error {
	if parser.state != insideStep {
		return protocolError()
	}
	var value messageCompletedData
	if !hasFields(data, "finishReason", "message", "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validStep(value.stepCoordinates) ||
		!validFinishReason(value.FinishReason) {
		return protocolError()
	}
	if value.Message != nil {
		if len(*value.Message) > maxDiscardedText ||
			parser.messageSoFar != "" && *value.Message != parser.messageSoFar {
			return protocolError()
		}
	} else if parser.messageSoFar != "" {
		return protocolError()
	}
	parser.messageSoFar = ""
	return nil
}

type stepCompletedData struct {
	FinishReason     string `json:"finishReason"`
	ProviderMetadata *struct {
		Gateway struct {
			GenerationID string `json:"generationId"`
		} `json:"gateway"`
	} `json:"providerMetadata"`
	Sequence  int    `json:"sequence"`
	StepIndex int    `json:"stepIndex"`
	TurnID    string `json:"turnId"`
	Usage     *struct {
		CostUSD          *json.Number `json:"costUsd"`
		InputTokens      *int         `json:"inputTokens"`
		OutputTokens     *int         `json:"outputTokens"`
		CacheReadTokens  *int         `json:"cacheReadTokens"`
		CacheWriteTokens *int         `json:"cacheWriteTokens"`
	} `json:"usage"`
}

func (parser *streamParser) acceptStepCompleted(data json.RawMessage) error {
	if parser.state != insideStep {
		return protocolError()
	}
	var value stepCompletedData
	if !hasFields(data, "finishReason", "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil ||
		!parser.validStep(stepCoordinates{
			Sequence: value.Sequence, StepIndex: value.StepIndex, TurnID: value.TurnID,
		}) ||
		value.FinishReason != "tool-calls" {
		return protocolError()
	}
	if value.ProviderMetadata != nil {
		generationID := value.ProviderMetadata.Gateway.GenerationID
		if !validID(generationID) {
			return protocolError()
		}
		parser.receipt.GatewayGenerationID = generationID
	}
	if value.Usage != nil {
		if !validOptionalCount(value.Usage.InputTokens) ||
			!validOptionalCount(value.Usage.OutputTokens) ||
			!validOptionalCount(value.Usage.CacheReadTokens) ||
			!validOptionalCount(value.Usage.CacheWriteTokens) {
			return protocolError()
		}
		inputTokens := optionalCount(value.Usage.InputTokens)
		outputTokens := optionalCount(value.Usage.OutputTokens)
		if inputTokens > int(^uint(0)>>1)-outputTokens {
			return protocolError()
		}
		parser.receipt.Usage = &domain.AgentUsageV1{
			InputTokens: inputTokens, OutputTokens: outputTokens,
			TotalTokens: inputTokens + outputTokens,
		}
		if value.Usage.CostUSD != nil {
			cost := value.Usage.CostUSD.String()
			if !domain.ValidModelCostUSD(cost) {
				return protocolError()
			}
			parser.receipt.CostUSD = cost
		}
	}
	parser.receipt.CompletedModelSteps = 1
	parser.state = afterStepCompleted
	return nil
}

type resultCompletedData struct {
	Result json.RawMessage `json:"result"`
	stepCoordinates
}

func (parser *streamParser) acceptResultCompleted(data json.RawMessage) error {
	if parser.state != afterStepCompleted || parser.candidate != nil {
		return protocolError()
	}
	var value resultCompletedData
	if !hasFields(data, "result", "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validStep(value.stepCoordinates) {
		return protocolError()
	}
	candidate, err := compactJSONObject(value.Result, maxSchemaBody)
	if err != nil {
		return protocolError()
	}
	parser.candidate = append(json.RawMessage(nil), candidate...)
	parser.state = afterResultCompleted
	return nil
}

func (parser *streamParser) acceptTurnCompleted(data json.RawMessage) error {
	if parser.state != afterResultCompleted {
		return protocolError()
	}
	var value turnData
	if !hasFields(data, "sequence", "turnId") ||
		decodeStrictJSON(data, &value) != nil || !parser.validTurn(value.Sequence, value.TurnID) {
		return protocolError()
	}
	parser.state = expectSessionWaiting
	return nil
}

type failureStepData struct {
	Code      string          `json:"code"`
	Details   json.RawMessage `json:"details"`
	Message   string          `json:"message"`
	Sequence  int             `json:"sequence"`
	StepIndex int             `json:"stepIndex"`
	TurnID    string          `json:"turnId"`
}

func (parser *streamParser) acceptStepFailed(data json.RawMessage) error {
	if parser.state != insideStep && parser.state != afterStepCompleted {
		return protocolError()
	}
	var value failureStepData
	if !hasFields(data, "code", "message", "sequence", "stepIndex", "turnId") ||
		decodeStrictJSON(data, &value) != nil ||
		!parser.validStep(stepCoordinates{
			Sequence: value.Sequence, StepIndex: value.StepIndex, TurnID: value.TurnID,
		}) || !validFailureCode(value.Code) || !validOptionalJSONObject(value.Details) {
		return protocolError()
	}
	parser.remoteFailed = true
	parser.state = expectTurnFailed
	return nil
}

type failureTurnData struct {
	Code     string          `json:"code"`
	Details  json.RawMessage `json:"details"`
	Message  string          `json:"message"`
	Sequence int             `json:"sequence"`
	TurnID   string          `json:"turnId"`
}

func (parser *streamParser) acceptTurnFailed(data json.RawMessage) error {
	if parser.state != expectTurnFailed {
		return protocolError()
	}
	var value failureTurnData
	if !hasFields(data, "code", "message", "sequence", "turnId") ||
		decodeStrictJSON(data, &value) != nil ||
		!parser.validTurn(value.Sequence, value.TurnID) ||
		!validFailureCode(value.Code) || !validOptionalJSONObject(value.Details) {
		return protocolError()
	}
	parser.state = expectFailureTerminal
	return nil
}

type failureSessionData struct {
	Code      string          `json:"code"`
	Details   json.RawMessage `json:"details"`
	Message   string          `json:"message"`
	SessionID string          `json:"sessionId"`
}

func (parser *streamParser) acceptSessionFailed(data json.RawMessage) error {
	if parser.state != expectFailureTerminal {
		return protocolError()
	}
	var value failureSessionData
	if !hasFields(data, "code", "message", "sessionId") ||
		decodeStrictJSON(data, &value) != nil ||
		value.SessionID != parser.sessionID ||
		!validFailureCode(value.Code) || !validOptionalJSONObject(value.Details) {
		return protocolError()
	}
	return nil
}

type sessionWaitingData struct {
	ContinuationToken string `json:"continuationToken"`
	Wait              string `json:"wait"`
}

func (parser *streamParser) acceptSessionWaiting(data json.RawMessage) error {
	if parser.state != expectSessionWaiting && parser.state != expectFailureTerminal {
		return protocolError()
	}
	var value sessionWaitingData
	if !hasFields(data, "continuationToken", "wait") ||
		decodeStrictJSON(data, &value) != nil ||
		value.Wait != "next-user-message" ||
		len(value.ContinuationToken) == 0 ||
		len(value.ContinuationToken) > maxContinuation {
		return protocolError()
	}
	if parser.state == expectSessionWaiting && parser.candidate == nil {
		return protocolError()
	}
	return nil
}

func (parser *streamParser) validTurn(sequence int, turnID string) bool {
	return sequence == 0 && turnID == parser.turnID
}

func (parser *streamParser) validStep(value stepCoordinates) bool {
	return value.StepIndex == 0 && parser.validTurn(value.Sequence, value.TurnID)
}

func validMessageParts(value json.RawMessage, message string) bool {
	if value == nil {
		return true
	}
	var entries []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	}
	return decodeStrictJSON(value, &entries) == nil &&
		len(entries) == 1 &&
		entries[0].Type == "text" &&
		entries[0].Text == message
}

func validFinishReason(value string) bool {
	switch value {
	case "content-filter", "error", "length", "other", "stop", "tool-calls":
		return true
	default:
		return false
	}
}

func validOptionalCount(value *int) bool {
	return value == nil || *value >= 0
}

func optionalCount(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func validFailureCode(value string) bool {
	return value != "" && len(value) <= maxFailureCodeBytes &&
		strings.IndexFunc(value, func(character rune) bool {
			return character < 0x21 || character > 0x7e
		}) == -1
}

func hasFields(value json.RawMessage, fields ...string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(value, &object) != nil || object == nil {
		return false
	}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return false
		}
	}
	return true
}

func validOptionalJSONObject(value json.RawMessage) bool {
	if value == nil {
		return true
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}

func protocolError() error {
	return categorized(ErrProtocol, domain.AgentFailureCategoryExecutorProtocol)
}
