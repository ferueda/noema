package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectionCommandsCreateAndReadEmptyDatabase(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "noema.db")
	for _, test := range []struct {
		name    string
		command string
	}{
		{name: "jobs", command: "jobs"},
		{name: "ideas", command: "ideas"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := run(
				context.Background(),
				[]string{test.command, "list", "--database", databasePath},
				&stdout,
				&stderr,
			)
			if err != nil {
				t.Fatalf("run %s list: %v", test.command, err)
			}
			if got := strings.TrimSpace(stdout.String()); got != "[]" {
				t.Fatalf("%s output = %q, want []", test.command, got)
			}
			if stderr.Len() != 0 {
				t.Fatalf("%s stderr = %q, want empty", test.command, stderr.String())
			}
		})
	}
}

func TestWorkerRefusesBeforeClaimingWithoutRemoteApproval(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "noema.db")
	var stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"worker", "--once", "--database", databasePath},
		&bytes.Buffer{},
		&stderr,
	)
	if err == nil || !strings.Contains(err.Error(), "--allow-remote") {
		t.Fatalf("worker error = %v, want explicit remote approval error", err)
	}
}
