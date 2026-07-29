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
	if decoded.ID != event.ID ||
		decoded.Fingerprint != event.Fingerprint ||
		decoded.CreatedAt != event.CreatedAt ||
		!reflect.DeepEqual(decoded.References, event.References) {
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

func TestPublisherRemovesPartialRecordBeforeRetry(t *testing.T) {
	file := &jsonlTestFile{partialWrite: true}
	publisher := &Publisher{
		path: "ignored",
		open: func(string, int, fs.FileMode) (appendFile, error) {
			return file, nil
		},
	}
	event := jsonlTestEvent(t, "analysis-1")

	if _, err := publisher.Publish(context.Background(), event); !errors.Is(err, ErrPublicationFailed) {
		t.Fatalf("partial publication error = %v", err)
	}
	if file.Len() != 0 {
		t.Fatalf("partial publication left %d bytes", file.Len())
	}

	file.partialWrite = false
	if _, err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatalf("retry publication: %v", err)
	}
	contents := file.Bytes()
	if bytes.Count(contents, []byte{'\n'}) != 1 {
		t.Fatalf("retry JSONL = %q", contents)
	}
	var decoded domain.DomainEvent
	if err := json.Unmarshal(bytes.TrimSuffix(contents, []byte{'\n'}), &decoded); err != nil {
		t.Fatalf("decode retry event: %v", err)
	}
	if decoded.ID != event.ID {
		t.Fatalf("retry event ID = %q, want %q", decoded.ID, event.ID)
	}
}

func TestPublisherRefusesRetryUntilIncompleteTailRepairSucceeds(t *testing.T) {
	tests := []struct {
		name string
		file *jsonlTestFile
	}{
		{
			name: "truncate failure",
			file: &jsonlTestFile{
				partialWrite: true,
				truncateErr:  errors.New("truncate failed"),
			},
		},
		{
			name: "repair sync failure",
			file: &jsonlTestFile{
				partialWrite:          true,
				truncateWithoutChange: true,
				syncErr:               errors.New("repair sync failed"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := &Publisher{
				path: "ignored",
				open: func(string, int, fs.FileMode) (appendFile, error) {
					return test.file, nil
				},
			}
			event := jsonlTestEvent(t, "analysis-1")
			if _, err := publisher.Publish(context.Background(), event); !errors.Is(err, ErrPublicationFailed) {
				t.Fatalf("partial publication error = %v", err)
			}
			if test.file.Len() == 0 {
				t.Fatal("fixture did not retain the unsafe partial tail")
			}
			writes := test.file.writeCalls
			test.file.partialWrite = false
			if _, err := publisher.Publish(context.Background(), event); !errors.Is(err, ErrPublicationFailed) {
				t.Fatalf("unsafe retry error = %v", err)
			}
			if test.file.writeCalls != writes {
				t.Fatalf("unsafe retry appended after fragment: writes=%d, want %d", test.file.writeCalls, writes)
			}
		})
	}
}

func jsonlTestEvent(t *testing.T, analysisID string) domain.DomainEvent {
	t.Helper()
	event, err := domain.NewDomainEvent(
		domain.EventTypeAnalysisCompleted,
		domain.EventReferenceAnalysis,
		analysisID,
		map[string]any{
			"analysisId": analysisID,
			"claimIds":   []string{},
		},
		[]domain.EventReference{},
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
	truncateErr  error

	truncateWithoutChange bool
	writeCalls            int
}

func (file *jsonlTestFile) Write(contents []byte) (int, error) {
	file.writeCalls++
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	if file.partialWrite {
		return file.Buffer.Write(contents[:len(contents)-1])
	}
	return file.Buffer.Write(contents)
}

func (file *jsonlTestFile) ReadAt(destination []byte, offset int64) (int, error) {
	if offset < 0 || offset > int64(file.Len()) {
		return 0, errors.New("invalid read offset")
	}
	read := copy(destination, file.Bytes()[offset:])
	if read != len(destination) {
		return read, io.EOF
	}
	return read, nil
}

func (file *jsonlTestFile) Seek(offset int64, whence int) (int64, error) {
	if offset != 0 || whence != io.SeekEnd {
		return 0, errors.New("unsupported seek")
	}
	return int64(file.Len()), nil
}

func (file *jsonlTestFile) Truncate(size int64) error {
	if file.truncateErr != nil {
		return file.truncateErr
	}
	if size < 0 || size > int64(file.Len()) {
		return errors.New("invalid truncate")
	}
	if file.truncateWithoutChange {
		return nil
	}
	contents := append([]byte{}, file.Bytes()[:size]...)
	file.Buffer.Reset()
	_, _ = file.Buffer.Write(contents)
	return nil
}

func (file *jsonlTestFile) Sync() error {
	return file.syncErr
}

func (file *jsonlTestFile) Close() error {
	return file.closeErr
}

var _ appendFile = (*jsonlTestFile)(nil)
