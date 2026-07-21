package sessions

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

const defaultMaxOutputBytes int64 = 64 * 1024 * 1024

type ExecRunner struct {
	MaxOutputBytes int64
}

type CommandError struct {
	Kind     string
	ExitCode int
}

func (err CommandError) Error() string {
	if err.ExitCode >= 0 {
		return "Sessions command failed with exit status"
	}
	return "Sessions command failed: " + err.Kind
}

func (runner ExecRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return runner.run(ctx, executable, args...)
}

func (runner ExecRunner) run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	maximum := runner.MaxOutputBytes
	if maximum <= 0 {
		maximum = defaultMaxOutputBytes
	}
	command := exec.CommandContext(ctx, executable, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, CommandError{Kind: "unavailable", ExitCode: -1}
	}
	if err := command.Start(); err != nil {
		return nil, classifyCommandError(ctx, err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maximum+1))
	if readErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, CommandError{Kind: "unavailable", ExitCode: -1}
	}
	if int64(len(output)) > maximum {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, CommandError{Kind: "output-too-large", ExitCode: -1}
	}
	if err := command.Wait(); err != nil {
		return nil, classifyCommandError(ctx, err)
	}
	return output, nil
}

func classifyCommandError(ctx context.Context, err error) CommandError {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CommandError{Kind: "canceled", ExitCode: -1}
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return CommandError{Kind: "canceled", ExitCode: -1}
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return CommandError{Kind: "exit", ExitCode: exitError.ExitCode()}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return CommandError{Kind: "not-found", ExitCode: -1}
	}
	return CommandError{Kind: "unavailable", ExitCode: -1}
}
