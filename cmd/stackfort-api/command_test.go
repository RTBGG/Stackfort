// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
)

func TestBootstrapCreateCommandDisplaysRawTokenExactlyOnce(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
	t.Setenv("STACKFORT_STATE_PATH", databasePath)
	var output bytes.Buffer
	if err := runCommand(context.Background(), []string{"bootstrap", "create", "--ttl=1m"}, &output); err != nil {
		t.Fatalf("runCommand: %v", err)
	}

	const marker = "Bootstrap token (shown once): "
	if strings.Count(output.String(), marker) != 1 {
		t.Fatalf("marker count in output = %d, output=%q", strings.Count(output.String(), marker), output.String())
	}
	tokenLine := strings.SplitN(strings.SplitN(output.String(), marker, 2)[1], "\n", 2)[0]
	if !strings.HasPrefix(tokenLine, "sfb_") || strings.Count(output.String(), tokenLine) != 1 {
		t.Fatalf("token was not displayed exactly once: %q", output.String())
	}

	databaseBytes, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatalf("read database: %v", err)
	}
	if bytes.Contains(databaseBytes, []byte(tokenLine)) {
		t.Fatal("raw bootstrap token was present in the SQLite database")
	}

	var secondOutput bytes.Buffer
	err = runCommand(context.Background(), []string{"bootstrap", "create"}, &secondOutput)
	if !errors.Is(err, core.ErrConflict) || secondOutput.Len() != 0 {
		t.Fatalf("second command error=%v output=%q", err, secondOutput.String())
	}
	if err := runCommand(context.Background(), []string{"bootstrap", "create", "--replace"}, &secondOutput); err != nil {
		t.Fatalf("replace command: %v", err)
	}
	if !strings.Contains(secondOutput.String(), marker) || strings.Contains(secondOutput.String(), tokenLine) {
		t.Fatalf("replacement output = %q", secondOutput.String())
	}
}

func TestUnknownCommandReturnsUsageWithoutOpeningState(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
	t.Setenv("STACKFORT_STATE_PATH", databasePath)
	if err := runCommand(context.Background(), []string{"unknown"}, &bytes.Buffer{}); err == nil {
		t.Fatal("unknown command unexpectedly succeeded")
	}
	if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unknown command changed state path: %v", err)
	}
}
