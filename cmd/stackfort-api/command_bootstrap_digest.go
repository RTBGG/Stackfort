// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/store"
)

// This is a private-database-owner CLI, not a network endpoint. The installer
// must execute it as the verified database service UID/GID, through a fixed
// binary, and pipe only the digest of its operation-bound setup capability.
func runBootstrapImportDigestCommand(ctx context.Context, args []string, input io.Reader, output io.Writer) (returnErr error) {
	flags := flag.NewFlagSet("bootstrap import-digest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	ttl := flags.Duration("ttl", 15*time.Minute, "capability lifetime")
	if err := flags.Parse(args); err != nil {
		// Flag errors can echo arbitrary argument contents. Never copy them to
		// logs, even if a caller mistakenly provided a token on the command line.
		return errors.New("invalid bootstrap import-digest options: only --ttl is accepted")
	}
	if flags.NArg() != 0 {
		return errors.New("bootstrap import-digest does not accept positional arguments")
	}
	if *ttl < time.Minute || *ttl > time.Hour {
		return errors.New("bootstrap import-digest TTL must be between 1m and 1h")
	}
	if output == nil {
		return errors.New("bootstrap import-digest requires an output writer")
	}
	digest, err := readBootstrapDigest(ctx, input)
	if err != nil {
		return err
	}

	// No path creation, database migration or state change before the complete
	// input and option grammar have been accepted.
	databasePath, err := panelStatePath()
	if err != nil {
		return err
	}
	state, err := store.Open(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("open panel state: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, state.Close()) }()
	repository, err := core.NewRepository(state)
	if err != nil {
		return fmt.Errorf("initialize control-plane repository: %w", err)
	}
	registered, err := repository.RegisterBootstrapCapabilityDigest(ctx, core.RegisterBootstrapCapabilityDigestParams{
		Digest: digest,
		TTL:    *ttl,
	})
	if err != nil {
		return err
	}
	// If output is interrupted after commit, an exact still-active retry returns
	// this same ID and expiry; it does not create or prolong a capability.
	return json.NewEncoder(output).Encode(registered)
}

func readBootstrapDigest(ctx context.Context, input io.Reader) (core.BootstrapCapabilityDigest, error) {
	var zero core.BootstrapCapabilityDigest
	if ctx == nil || input == nil {
		return zero, errors.New("bootstrap import-digest requires a context and standard input")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	type readResult struct {
		data []byte
		err  error
	}
	result := make(chan readResult, 1)
	go func() {
		// A valid input is exactly 64 lowercase hex characters followed by LF
		// and EOF. The extra byte detects trailing data without unbounded reads.
		data, err := io.ReadAll(io.LimitReader(input, 66))
		result <- readResult{data: data, err: err}
	}()
	select {
	case <-ctx.Done():
		// An arbitrary Reader is not cancellable; do not close a caller-owned
		// descriptor. The CLI's main exits on this timeout, ending the read too.
		return zero, ctx.Err()
	case read := <-result:
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if read.err != nil {
			return zero, errors.New("read bootstrap digest from standard input failed")
		}
		if len(read.data) != 65 || read.data[64] != '\n' {
			return zero, errors.New("bootstrap digest input must be exactly 64 lowercase hexadecimal characters followed by LF and EOF")
		}
		return core.ParseBootstrapCapabilityDigest(string(read.data[:64]))
	}
}
