package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/ferueda/noema/internal/adapters/jsonl"
	"github.com/ferueda/noema/internal/application"
)

func runEvents(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		writeEventsUsage(stderr)
		return errors.New("an events command is required")
	}
	switch args[0] {
	case "list":
		return runEventsList(ctx, args[1:], stdout, stderr)
	case "show":
		return runEventsShow(ctx, args[1:], stdout, stderr)
	case "publish":
		return runEventsPublish(ctx, args[1:], stdout, stderr)
	default:
		writeEventsUsage(stderr)
		return fmt.Errorf("unknown events command %q", args[0])
	}
}

func runEventsList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("events list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	status := flags.String("status", "", "publication status: pending or delivered")
	databasePath := flags.String("database", "", "SQLite database path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("events list received unexpected arguments")
	}
	store, closeStore, err := openStore(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer closeStore()
	events, err := store.ListEvents(ctx, *status)
	if err != nil {
		return err
	}
	return writeJSON(stdout, events)
}

func runEventsShow(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: noema events show <event-id> [--database path]")
		return errors.New("event ID is required")
	}
	eventID := args[0]
	flags := flag.NewFlagSet("events show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("database", "", "SQLite database path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("events show received unexpected arguments")
	}
	store, closeStore, err := openStore(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer closeStore()
	event, found, err := store.LoadEvent(ctx, eventID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("event %s was not found", eventID)
	}
	return writeJSON(stdout, event)
}

func runEventsPublish(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("events publish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	once := flags.Bool("once", false, "publish at most one pending event")
	output := flags.String("output", "", "JSONL transport file")
	databasePath := flags.String("database", "", "SQLite database path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("events publish received unexpected arguments")
	}
	if !*once {
		return errors.New("events publish requires --once")
	}
	publisher, err := jsonl.NewPublisher(*output)
	if err != nil {
		return errors.New("events publish requires --output")
	}
	store, closeStore, err := openStore(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer closeStore()
	result, err := (application.EventPublication{
		Store: store, Publisher: publisher,
	}).PublishOne(ctx)
	if err != nil {
		return err
	}
	if err := writeJSON(stdout, result); err != nil {
		return err
	}
	if result.Status == application.PublicationFailed {
		return errors.New(result.LastFailureCategory)
	}
	return nil
}

func writeEventsUsage(writer io.Writer) {
	fmt.Fprintln(writer, `usage: noema events <command>

commands:
  list                          list events and outbox status
  show <event-id>               show one event and outbox status
  publish --once --output path  append one pending event to JSONL`)
}
