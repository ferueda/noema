package sessions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecRunnerRejectsOversizedOutputWithoutExposingContent(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "large-output")
	secret := strings.Repeat("private-session-content", 32)
	script := "#!/bin/sh\nprintf '%s' '" + secret + "'\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	output, err := (ExecRunner{MaxOutputBytes: 64}).Run(context.Background(), executable)
	var commandError CommandError
	if !errors.As(err, &commandError) || commandError.Kind != "output-too-large" {
		t.Fatalf("command error = %v, want output-too-large", err)
	}
	if output != nil || strings.Contains(err.Error(), "private-session-content") {
		t.Fatalf("oversized output/error = %q/%v", output, err)
	}
}

func TestExecRunnerReturnsBoundedOutput(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "small-output")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s' 'valid-output'\n"), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	output, err := (ExecRunner{MaxOutputBytes: 64}).Run(context.Background(), executable)
	if err != nil || string(output) != "valid-output" {
		t.Fatalf("output/error = %q/%v", output, err)
	}
}
