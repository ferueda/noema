package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/platform"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "noema:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		writeUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "scan":
		return runScan(args[1:], stderr)
	case "worker":
		return runWorker(ctx, args[1:], stdout, stderr)
	case "jobs":
		return runJobs(ctx, args[1:], stdout, stderr)
	case "ideas":
		return runIdeas(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		writeUsage(stdout)
		return nil
	default:
		writeUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runScan(args []string, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "sessions" {
		fmt.Fprintln(stderr, "usage: noema scan sessions [options]")
		return errors.New("scan source must be sessions")
	}
	return errors.New("Sessions ingestion is not implemented in the walking-skeleton milestone")
}

func runWorker(_ context.Context, args []string, _ io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("worker", flag.ContinueOnError)
	flags.SetOutput(stderr)
	once := flags.Bool("once", false, "claim at most one pending job")
	flags.String("database", "", "SQLite database path")
	allowRemote := flags.Bool("allow-remote", false, "allow the configured remote model request")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*once {
		return errors.New("worker currently requires --once")
	}
	if !*allowRemote {
		return errors.New("worker requires --allow-remote before an agent model request")
	}
	return errors.New("Content Scout is not implemented in the walking-skeleton milestone")
}

func runJobs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(stderr, "usage: noema jobs list [--database path]")
		return errors.New("jobs currently supports only list")
	}
	flags := flag.NewFlagSet("jobs list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("database", "", "SQLite database path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	store, closeStore, err := openStore(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer closeStore()
	jobs, err := store.ListJobs(ctx)
	if err != nil {
		return err
	}
	return writeJSON(stdout, jobs)
}

func runIdeas(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(stderr, "usage: noema ideas list [--database path]")
		return errors.New("ideas currently supports only list")
	}
	flags := flag.NewFlagSet("ideas list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("database", "", "SQLite database path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	store, closeStore, err := openStore(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer closeStore()
	ideas, err := store.ListIdeas(ctx)
	if err != nil {
		return err
	}
	return writeJSON(stdout, ideas)
}

func openStore(
	ctx context.Context,
	configuredPath string,
) (*sqlitestore.Store, func(), error) {
	path := configuredPath
	if path == "" {
		defaultPath, err := platform.DefaultDatabasePath()
		if err != nil {
			return nil, func() {}, err
		}
		path = defaultPath
	}
	if err := platform.EnsureParentDirectory(path); err != nil {
		return nil, func() {}, err
	}
	database, err := sqlitestore.Open(ctx, path)
	if err != nil {
		return nil, func() {}, err
	}
	return sqlitestore.NewStore(database), func() { database.Close() }, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, `usage: noema <command>

commands:
  scan sessions   ingest Sessions evidence (next milestone)
  worker --once   process one pending agent job (next milestone)
  jobs list       list durable jobs
  ideas list      list content ideas`)
}
