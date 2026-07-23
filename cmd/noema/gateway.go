package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ferueda/noema/internal/adapters/aigateway"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func runGateway(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	dependencies commandDependencies,
) error {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(stderr, "usage: noema gateway check --allow-remote --route-config path")
		return errors.New("gateway currently supports only check")
	}
	flags := flag.NewFlagSet("gateway check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	allowRemote := flags.Bool("allow-remote", false, "allow this public synthetic model request")
	routePath := flags.String("route-config", "", "reviewed semantic route configuration")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("gateway check received unexpected arguments")
	}
	if !*allowRemote {
		return errors.New("gateway check requires --allow-remote")
	}
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("gateway check requires AI_GATEWAY_API_KEY")
	}
	if strings.TrimSpace(*routePath) == "" {
		return errors.New("gateway check requires --route-config")
	}
	route, err := aigateway.LoadRoute(*routePath)
	if err != nil {
		return err
	}
	if dependencies.newSemanticGenerator == nil {
		return errors.New("semantic generator is unavailable")
	}
	generator, err := dependencies.newSemanticGenerator(route, apiKey)
	if err != nil {
		return err
	}
	result, err := (application.SemanticConformance{Generator: generator}).Run(ctx, route.Validated())
	if err != nil {
		return err
	}
	validatedRoute := route.Validated()
	return writeJSON(stdout, gatewayCheckOutput{
		Success: true, Schema: result.Schema,
		RouteDigest: validatedRoute.ConfigDigest, RouteConfig: validatedRoute.SanitizedConfig,
		ResolvedProvider: result.Model.ResolvedProvider, ResolvedModel: result.Model.ResolvedModel,
		RequestID: result.Model.RequestID, InputTokens: result.Model.InputTokens,
		OutputTokens: result.Model.OutputTokens, TotalTokens: result.Model.TotalTokens,
		LatencyMilliseconds: result.Model.LatencyMilliseconds, CostUSD: result.Model.CostUSD,
		CandidateCount: result.CandidateCount,
	})
}

type gatewayCheckOutput struct {
	Success             bool                                  `json:"success"`
	Schema              domain.StructuredOutputSchemaIdentity `json:"schema"`
	RouteDigest         string                                `json:"routeDigest"`
	RouteConfig         json.RawMessage                       `json:"routeConfig"`
	ResolvedProvider    string                                `json:"resolvedProvider"`
	ResolvedModel       string                                `json:"resolvedModel"`
	RequestID           string                                `json:"requestId,omitempty"`
	InputTokens         *int                                  `json:"inputTokens,omitempty"`
	OutputTokens        *int                                  `json:"outputTokens,omitempty"`
	TotalTokens         *int                                  `json:"totalTokens,omitempty"`
	LatencyMilliseconds *int64                                `json:"latencyMilliseconds,omitempty"`
	CostUSD             *string                               `json:"costUsd,omitempty"`
	CandidateCount      int                                   `json:"candidateCount"`
}
