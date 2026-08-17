package jsonl

import (
	"bytes"
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
	io.ReaderAt
	io.Writer
	io.Seeker
	Truncate(int64) error
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
		os.O_CREATE|os.O_RDWR|os.O_APPEND,
		0o600,
	)
	if err != nil {
		return "", ErrPublicationFailed
	}
	startOffset, err := repairIncompleteTail(file)
	if err != nil {
		_ = file.Close()
		return "", ErrPublicationFailed
	}
	written, writeErr := file.Write(encoded)
	if writeErr == nil && written != len(encoded) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		_ = rollbackAppend(file, startOffset)
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

func repairIncompleteTail(file appendFile) (int64, error) {
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil || size == 0 {
		return size, err
	}
	last := []byte{0}
	if _, err := file.ReadAt(last, size-1); err != nil {
		return 0, err
	}
	if last[0] == '\n' {
		return size, nil
	}

	truncateAt, err := finalCompleteLineOffset(file, size)
	if err != nil {
		return 0, err
	}
	if err := rollbackAppend(file, truncateAt); err != nil {
		return 0, err
	}
	return truncateAt, nil
}

func finalCompleteLineOffset(file appendFile, size int64) (int64, error) {
	const blockSize = int64(4 * 1024)
	buffer := make([]byte, blockSize)
	for end := size; end > 0; {
		start := max(int64(0), end-blockSize)
		length := end - start
		read, err := file.ReadAt(buffer[:length], start)
		if err != nil && err != io.EOF {
			return 0, err
		}
		if index := bytes.LastIndexByte(buffer[:read], '\n'); index >= 0 {
			return start + int64(index) + 1, nil
		}
		end = start
	}
	return 0, nil
}

func rollbackAppend(file appendFile, offset int64) error {
	if err := file.Truncate(offset); err != nil {
		return err
	}
	return file.Sync()
}
