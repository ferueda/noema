package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

func TestPublisherAppendsCompleteDomainEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	publisher, err := NewPublisher(path)
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}
	event := jsonlTestEvent(t, "analysis-1")

	acknowledgementID, err := publisher.Publish(context.Background(), event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if acknowledgementID != "" {
		t.Fatalf("acknowledgement ID = %q, want empty", acknowledgementID)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read JSONL: %v", err)
	}
	if bytes.Count(contents, []byte{'\n'}) != 1 ||
		len(contents) == 0 || contents[len(contents)-1] != '\n' {
		t.Fatalf("JSONL framing is invalid: %q", contents)
	}
	var decoded domain.DomainEvent
	if err := json.Unmarshal(bytes.TrimSuffix(contents, []byte{'\n'}), &decoded); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("validate decoded event: %v", err)
	}
	if !reflect.DeepEqual(decoded, event) {
		t.Fatalf("decoded event does not match: %#v", decoded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat JSONL: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("JSONL permissions = %o, want 600", info.Mode().Perm())
	}

	second := jsonlTestEvent(t, "analysis-2")
	if _, err := publisher.Publish(context.Background(), second); err != nil {
		t.Fatalf("publish second: %v", err)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read second JSONL: %v", err)
	}
	if bytes.Count(contents, []byte{'\n'}) != 2 {
		t.Fatalf("JSONL line count = %d, want 2", bytes.Count(contents, []byte{'\n'}))
	}
}

func TestPublisherRejectsPartialOrFailedWrites(t *testing.T) {
	tests := []struct {
		name string
		file *jsonlTestFile
	}{
		{name: "partial", file: &jsonlTestFile{partialWrite: true}},
		{name: "write", file: &jsonlTestFile{writeErr: errors.New("write failed")}},
		{name: "sync", file: &jsonlTestFile{syncErr: errors.New("sync failed")}},
		{name: "close", file: &jsonlTestFile{closeErr: errors.New("close failed")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := &Publisher{
				path: "ignored",
				open: func(string, int, fs.FileMode) (appendFile, error) {
					return test.file, nil
				},
			}
			_, err := publisher.Publish(
				context.Background(),
				jsonlTestEvent(t, "analysis-1"),
			)
			if !errors.Is(err, ErrPublicationFailed) {
				t.Fatalf("error = %v, want publication failure", err)
			}
		})
	}
}

func TestPublisherRejectsCanceledContextBeforeOpeningFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened := false
	publisher := &Publisher{
		path: "ignored",
		open: func(string, int, fs.FileMode) (appendFile, error) {
			opened = true
			return &jsonlTestFile{}, nil
		},
	}

	_, err := publisher.Publish(ctx, jsonlTestEvent(t, "analysis-1"))
	if !errors.Is(err, ErrPublicationFailed) {
		t.Fatalf("error = %v, want publication failure", err)
	}
	if opened {
		t.Fatal("publisher opened a file after cancellation")
	}
}

func jsonlTestEvent(t *testing.T, analysisID string) domain.DomainEvent {
	t.Helper()
	event, err := domain.NewDomainEvent(
		"analysis.completed",
		domain.EventReferenceAnalysis,
		analysisID,
		map[string]any{
			"analysisId": analysisID,
		},
		[]domain.EventReference{{
			RecordType: domain.EventReferenceAnalysis,
			RecordID:   analysisID,
		}},
		time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("new event: %v", err)
	}
	return event
}

type jsonlTestFile struct {
	bytes.Buffer
	partialWrite bool
	writeErr     error
	syncErr      error
	closeErr     error
}

func (file *jsonlTestFile) Write(contents []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	if file.partialWrite {
		return len(contents) - 1, nil
	}
	return file.Buffer.Write(contents)
}

func (file *jsonlTestFile) Sync() error {
	return file.syncErr
}

func (file *jsonlTestFile) Close() error {
	return file.closeErr
}

var _ io.Writer = (*jsonlTestFile)(nil)
