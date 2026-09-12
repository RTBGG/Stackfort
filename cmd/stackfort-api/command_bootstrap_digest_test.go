// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

func commandBootstrapDigestFixture() (string, string) {
	token := "sfb_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{123}, 32))
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:])
}

func TestBootstrapDigestCommandRegistersNonsecretMetadataAndReconciles(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
	t.Setenv("STACKFORT_STATE_PATH", databasePath)
	token, digest := commandBootstrapDigestFixture()
	var output bytes.Buffer
	args := []string{"bootstrap", "import-digest", "--ttl=1m"}
	if err := runCommandWithInput(t.Context(), args, strings.NewReader(digest+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	var first core.RegisteredBootstrapCapability
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&first); err != nil || first.AlreadyRegistered || first.ExpiresAt.Sub(first.CreatedAt) != time.Minute {
		t.Fatalf("initial metadata: %+v %v", first, err)
	}
	if _, err := core.ParseID(string(first.ID)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), token) || strings.Contains(output.String(), digest) {
		t.Fatal("command printed capability material")
	}
	database, err := os.ReadFile(databasePath)
	if err != nil || bytes.Contains(database, []byte(token)) {
		t.Fatalf("raw token persisted: %v", err)
	}
	output.Reset()
	if err := runCommandWithInput(t.Context(), []string{"bootstrap", "import-digest", "--ttl=1h"}, strings.NewReader(digest+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	var retried core.RegisteredBootstrapCapability
	if err := json.Unmarshal(output.Bytes(), &retried); err != nil || !retried.AlreadyRegistered || retried.ID != first.ID || !retried.CreatedAt.Equal(first.CreatedAt) || !retried.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("retry renewed registration: %+v %v", retried, err)
	}
	output.Reset()
	other := sha256.Sum256([]byte("another trusted-producer token"))
	if err := runCommandWithInput(t.Context(), args, strings.NewReader(hex.EncodeToString(other[:])+"\n"), &output); !errors.Is(err, core.ErrConflict) || output.Len() != 0 {
		t.Fatalf("different digest replaced active registration: %v", err)
	}
}

func TestBootstrapDigestCommandRejectsMalformedInputBeforeOpeningDatabase(t *testing.T) {
	token, digest := commandBootstrapDigestFixture()
	for name, input := range map[string]string{
		"empty": "", "raw-token": token + "\n", "no-lf": digest,
		"short": digest[:63] + "\n", "long": digest + "0\n", "uppercase": strings.ToUpper(digest) + "\n",
		"crlf": digest + "\r\n", "double-lf": digest + "\n\n", "suffix": digest + "\nx",
		"leading-space": " " + digest + "\n", "trailing-space": digest + " \n",
		"nonhex": digest[:63] + "g\n", "unicode": strings.Repeat("é", 32) + "\n",
		"zero": strings.Repeat("0", 64) + "\n", "oversized": strings.Repeat("f", 1024*1024),
	} {
		t.Run(name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
			t.Setenv("STACKFORT_STATE_PATH", databasePath)
			var output bytes.Buffer
			err := runCommandWithInput(t.Context(), []string{"bootstrap", "import-digest"}, strings.NewReader(input), &output)
			if err == nil || output.Len() != 0 || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), digest) {
				t.Fatalf("malformed input succeeded or leaked: %v", err)
			}
			if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid input opened state: %v", err)
			}
		})
	}
}

func TestBootstrapDigestCommandRejectsArgumentsWithoutEchoingThem(t *testing.T) {
	token, digest := commandBootstrapDigestFixture()
	for _, options := range [][]string{
		{"--replace"}, {digest}, {token}, {"--ttl=" + token}, {"--" + token},
		{"--ttl=0"}, {"--ttl=59s"}, {"--ttl=1h1ns"}, {"--ttl=-1m"}, {"--ttl"},
	} {
		t.Run(strings.Join(options, " "), func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
			t.Setenv("STACKFORT_STATE_PATH", databasePath)
			input := &bootstrapCountingReader{reader: strings.NewReader(digest + "\n")}
			var output bytes.Buffer
			err := runCommandWithInput(t.Context(), append([]string{"bootstrap", "import-digest"}, options...), input, &output)
			if err == nil || output.Len() != 0 || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), digest) {
				t.Fatalf("bad argument accepted or echoed: %v", err)
			}
			if input.count != 0 {
				t.Fatal("invalid options consumed stdin")
			}
			if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid options opened state: %v", err)
			}
		})
	}
}

type bootstrapCountingReader struct {
	reader io.Reader
	count  int
}

func (r *bootstrapCountingReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.count += n
	return n, err
}

type bootstrapFailedIO struct{}

func (bootstrapFailedIO) Read([]byte) (int, error) {
	return 0, errors.New("sfb_do-not-log-input-error")
}
func (bootstrapFailedIO) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBootstrapDigestReaderIsBoundedAndCancellable(t *testing.T) {
	t.Parallel()
	reader := &bootstrapCountingReader{reader: strings.NewReader(strings.Repeat("f", 1024*1024))}
	if _, err := readBootstrapDigest(t.Context(), reader); err == nil || reader.count != 66 {
		t.Fatalf("input not bounded: read=%d error=%v", reader.count, err)
	}
	if _, err := readBootstrapDigest(t.Context(), bootstrapFailedIO{}); err == nil || strings.Contains(err.Error(), "sfb_") {
		t.Fatalf("input failure logged its value: %v", err)
	}
	if _, err := readBootstrapDigest(nil, strings.NewReader("")); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := readBootstrapDigest(t.Context(), nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	pipe, writer := io.Pipe()
	defer pipe.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := readBootstrapDigest(ctx, pipe); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unterminated stdin did not respect timeout: %v", err)
	}
}

func TestBootstrapDigestCommandOutputFailureCanRetryWithoutRenewal(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
	t.Setenv("STACKFORT_STATE_PATH", databasePath)
	_, digest := commandBootstrapDigestFixture()
	args := []string{"bootstrap", "import-digest", "--ttl=1m"}
	if err := runCommandWithInput(t.Context(), args, strings.NewReader(digest+"\n"), bootstrapFailedIO{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("failed output error: %v", err)
	}
	var output bytes.Buffer
	if err := runCommandWithInput(t.Context(), []string{"bootstrap", "import-digest", "--ttl=1h"}, strings.NewReader(digest+"\n"), &output); err != nil {
		t.Fatalf("reconcile committed output failure: %v", err)
	}
	var result core.RegisteredBootstrapCapability
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.AlreadyRegistered || result.ExpiresAt.Sub(result.CreatedAt) != time.Minute {
		t.Fatalf("output failure retry renewed capability: %+v %v", result, err)
	}
}

func TestBootstrapDigestCommandMissingIOCannotCreateState(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
	t.Setenv("STACKFORT_STATE_PATH", databasePath)
	_, digest := commandBootstrapDigestFixture()
	args := []string{"bootstrap", "import-digest"}
	if err := runCommandWithInput(t.Context(), args, strings.NewReader(digest+"\n"), nil); err == nil {
		t.Fatal("nil output accepted")
	}
	if err := runCommandWithInput(t.Context(), args, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("nil input accepted")
	}
	if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing IO opened state: %v", err)
	}
}
