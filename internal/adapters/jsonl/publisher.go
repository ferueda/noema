package jsonl

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/ferueda/noema/internal/domain"
)

var (
	ErrInvalidConfiguration = errors.New("JSONL publisher configuration is invalid")
	ErrPublicationFailed    = errors.New("JSONL publication failed")
)

type appendFile interface {
	io.Writer
	Sync() error
	Close() error
}

type openFile func(string, int, fs.FileMode) (appendFile, error)

// Publisher appends complete domain events to one local JSONL file.
type Publisher struct {
	path string
	open openFile
}

func NewPublisher(path string) (*Publisher, error) {
	if path == "" {
		return nil, ErrInvalidConfiguration
	}
	return &Publisher{path: path, open: openAppendFile}, nil
}

func openAppendFile(path string, flags int, mode fs.FileMode) (appendFile, error) {
	return os.OpenFile(path, flags, mode)
}

func (publisher *Publisher) Publish(
	ctx context.Context,
	event domain.DomainEvent,
) (string, error) {
	if publisher == nil || publisher.path == "" || publisher.open == nil ||
		event.Validate() != nil || ctx.Err() != nil {
		return "", ErrPublicationFailed
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", ErrPublicationFailed
	}
	encoded = append(encoded, '\n')

	file, err := publisher.open(
		publisher.path,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o600,
	)
	if err != nil {
		return "", ErrPublicationFailed
	}
	written, writeErr := file.Write(encoded)
	if writeErr == nil && written != len(encoded) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		_ = file.Close()
		return "", ErrPublicationFailed
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", ErrPublicationFailed
	}
	if err := file.Close(); err != nil {
		return "", ErrPublicationFailed
	}
	return "", nil
}
