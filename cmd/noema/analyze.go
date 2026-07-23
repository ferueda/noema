package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ferueda/noema/internal/adapters/aigateway"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/platform"
)

type commandDependencies struct {
	newSemanticGenerator func(aigateway.Route, string) (application.SemanticGenerator, error)
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		newSemanticGenerator: func(route aigateway.Route, apiKey string) (application.SemanticGenerator, error) {
			generator, err := aigateway.NewGenerator(route, apiKey, nil)
			if err != nil {
				return nil, err
			}
			return generator, nil
		},
	}
}

func runAnalyze(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	dependencies commandDependencies,
) error {
	if len(args) < 2 || args[0] != "claims" {
		fmt.Fprintln(stderr, "usage: noema analyze claims <fact-analysis-id> --allow-remote --route-config path [--first-entry n --last-entry n] [--database path]")
		return errors.New("analyze currently supports only claims")
	}
	factAnalysisID := args[1]
	flags := flag.NewFlagSet("analyze claims", flag.ContinueOnError)
	flags.SetOutput(stderr)
	allowRemote := flags.Bool("allow-remote", false, "allow this remote semantic model request")
	routePath := flags.String("route-config", "", "reviewed semantic route configuration")
	databasePath := flags.String("database", "", "SQLite database path")
	firstEntry := flags.Int("first-entry", 0, "first included entry ordinal")
	lastEntry := flags.Int("last-entry", 0, "last included entry ordinal")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("analyze claims received unexpected arguments")
	}
	if strings.TrimSpace(factAnalysisID) == "" {
		return errors.New("analyze claims requires a fact analysis identity")
	}
	firstSet, lastSet := false, false
	flags.Visit(func(item *flag.Flag) {
		switch item.Name {
		case "first-entry":
			firstSet = true
		case "last-entry":
			lastSet = true
		}
	})
	if !*allowRemote {
		return errors.New("analyze claims requires --allow-remote")
	}
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("analyze claims requires AI_GATEWAY_API_KEY")
	}
	if strings.TrimSpace(*routePath) == "" {
		return errors.New("analyze claims requires --route-config")
	}
	if firstSet != lastSet || (firstSet && (*firstEntry < 0 || *lastEntry < *firstEntry)) {
		return errors.New("analyze claims entry bounds must be a valid pair")
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
	store, closeStore, err := openStore(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer closeStore()

	bounds := application.EntryBounds{}
	if firstSet {
		bounds.First = firstEntry
		bounds.Last = lastEntry
	}
	result, err := (application.SemanticWorkflow{
		Source: newSessionsReader(), Facts: store, Store: store,
		Analyzer: application.SemanticAnalyzer{
			Generator: generator, Privacy: application.PrivacyPolicy{}, NewID: platform.NewID,
		},
	}).Run(ctx, application.SemanticWorkflowRequest{
		FactAnalysisID: factAnalysisID, Bounds: bounds, Route: route.Validated(),
	})
	if err != nil {
		return err
	}
	run := result.Record.Analysis.Run
	coverage := ""
	if run.Selection != nil {
		coverage = run.Selection.Coverage
	}
	if run.Model == nil {
		return errors.New("semantic result model metadata is unavailable")
	}
	model := *run.Model
	return writeJSON(stdout, struct {
		AnalysisID string `json:"analysisId"`
		Reused     bool   `json:"reused"`
		Coverage   string `json:"coverage"`
		ClaimCount int    `json:"claimCount"`
		Model      string `json:"model"`
		Provider   string `json:"provider"`
	}{
		AnalysisID: run.ID, Reused: result.Reused, Coverage: coverage,
		ClaimCount: len(result.Record.Analysis.Claims), Model: model.ResolvedModel,
		Provider: model.ResolvedProvider,
	})
}
