package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ferueda/noema/internal/adapters/aigateway"
	"github.com/ferueda/noema/internal/application"
)

type commandDependencies struct {
	newGenerator func(aigateway.Route, string) (application.SemanticGenerator, error)
	now          func() time.Time
}

func defaultDependencies() commandDependencies {
	return commandDependencies{
		newGenerator: func(route aigateway.Route, apiKey string) (application.SemanticGenerator, error) {
			return aigateway.NewGenerator(route, apiKey, nil)
		},
		now: time.Now,
	}
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, defaultDependencies()); err != nil {
		fmt.Fprintln(os.Stderr, "noema-semantic-eval:", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	dependencies commandDependencies,
) error {
	if len(args) == 0 {
		writeUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "run":
		return runLiveEvaluation(ctx, args[1:], stdout, stderr, dependencies)
	case "score":
		return runScore(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		writeUsage(stdout)
		return nil
	default:
		writeUsage(stderr)
		return errors.New("unknown evaluation command")
	}
}

func runLiveEvaluation(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	dependencies commandDependencies,
) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	corpusPath := flags.String("corpus", "", "reviewed semantic evaluation corpus")
	routePath := flags.String("route-config", "", "reviewed semantic route configuration")
	outputPath := flags.String("output", "", "new path for the machine report")
	reviewPath := flags.String("review-output", "", "new path for the human review template")
	allowRemote := flags.Bool("allow-remote", false, "allow the selected digest-pinned remote evaluation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("evaluation run received unexpected arguments")
	}
	if !*allowRemote {
		return errors.New("evaluation run requires --allow-remote")
	}
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("evaluation run requires AI_GATEWAY_API_KEY")
	}
	if err := validateOutputPaths(*outputPath, *reviewPath); err != nil {
		return err
	}
	route, err := aigateway.LoadRoute(*routePath)
	if err != nil {
		return err
	}
	corpus, err := loadEvaluationCorpus(*corpusPath)
	if err != nil {
		return err
	}
	if err := preflightCorpus(corpus, route.Validated()); err != nil {
		return err
	}
	if dependencies.newGenerator == nil {
		return errors.New("evaluation generator is unavailable")
	}
	generator, err := dependencies.newGenerator(route, apiKey)
	if err != nil {
		return errors.New("evaluation generator is unavailable")
	}
	report := executeEvaluation(ctx, corpus, route.Validated(), generator, dependencies.now)
	review, err := buildReviewTemplate(report, corpus)
	if err != nil {
		return errors.New("build evaluation review template")
	}
	if err := writeJSONExclusive(*outputPath, report); err != nil {
		return err
	}
	if err := writeJSONExclusive(*reviewPath, review); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "evaluation report: %s\nreview template: %s\n", *outputPath, *reviewPath)
	if report.StopCategory != "" {
		return fmt.Errorf("evaluation stopped: %s", report.StopCategory)
	}
	return nil
}

func writeUsage(output io.Writer) {
	fmt.Fprintln(output, "usage:")
	fmt.Fprintln(output, "  noema-semantic-eval run --corpus path --allow-remote --route-config path --output path --review-output path")
	fmt.Fprintln(output, "  noema-semantic-eval score --report path --reviews path --output path")
}
