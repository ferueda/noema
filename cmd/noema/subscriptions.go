package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ferueda/noema/internal/application"
)

type subscriptionMatchOutput struct {
	AnalysisID          string   `json:"analysisId"`
	EventID             string   `json:"eventId"`
	ConfigurationDigest string   `json:"configurationDigest"`
	JobIDs              []string `json:"jobIds"`
	CreatedJobIDs       []string `json:"createdJobIds"`
	ReusedJobIDs        []string `json:"reusedJobIds"`
}

func runSubscriptions(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
) error {
	if len(args) < 2 || args[0] != "match" {
		fmt.Fprintln(stderr, "usage: noema subscriptions match <semantic-analysis-id> --agent-config path --disclosure-config path [--database path]")
		return errors.New("subscriptions currently supports only match")
	}
	analysisID := args[1]
	flags := flag.NewFlagSet("subscriptions match", flag.ContinueOnError)
	flags.SetOutput(stderr)
	agentConfigPath := flags.String("agent-config", "", "strict Content Scout agent configuration")
	disclosureConfigPath := flags.String("disclosure-config", "", "approved public terms configuration")
	databasePath := flags.String("database", "", "SQLite database path")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("subscriptions match received unexpected arguments")
	}
	if analysisID == "" || *agentConfigPath == "" || *disclosureConfigPath == "" {
		return errors.New("subscriptions match requires an analysis id and both configuration files")
	}

	agentFile, err := os.Open(*agentConfigPath)
	if err != nil {
		return errors.New("Content Scout agent configuration is unavailable")
	}
	defer agentFile.Close()
	disclosureFile, err := os.Open(*disclosureConfigPath)
	if err != nil {
		return errors.New("Content Scout disclosure configuration is unavailable")
	}
	defer disclosureFile.Close()
	configuration, err := application.LoadContentScoutConfiguration(agentFile, disclosureFile)
	if err != nil {
		return err
	}

	store, closeStore, err := openStore(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer closeStore()
	result, err := (application.SubscriptionMatcher{
		Store: store,
		Now:   time.Now,
	}).MatchContentScout(ctx, analysisID, configuration)
	if err != nil {
		return err
	}
	output := subscriptionMatchOutput{
		AnalysisID:          result.AnalysisID,
		EventID:             result.EventID,
		ConfigurationDigest: result.ConfigurationDigest,
		JobIDs:              []string{},
		CreatedJobIDs:       []string{},
		ReusedJobIDs:        []string{},
	}
	for _, job := range result.Jobs {
		output.JobIDs = append(output.JobIDs, job.ID)
		if job.Created {
			output.CreatedJobIDs = append(output.CreatedJobIDs, job.ID)
		} else {
			output.ReusedJobIDs = append(output.ReusedJobIDs, job.ID)
		}
	}
	return writeJSON(stdout, output)
}
