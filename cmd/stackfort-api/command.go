// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/store"
)

func runCommand(ctx context.Context, args []string, output io.Writer) error {
	if len(args) >= 2 && args[0] == "database" && (args[1] == "migrate" || args[1] == "check") {
		if len(args) != 2 {
			return errors.New("usage: stackfort-api database migrate|check")
		}
		return runDatabaseCommand(ctx, args[1], output)
	}
	if len(args) < 2 || args[0] != "bootstrap" || args[1] != "create" {
		return errors.New("usage: stackfort-api bootstrap create [--ttl duration] [--replace] | database migrate|check")
	}
	return runBootstrapCreateCommand(ctx, args[2:], output)
}

func runDatabaseCommand(ctx context.Context, action string, output io.Writer) (returnErr error) {
	databasePath, err := panelStatePath()
	if err != nil {
		return err
	}
	state, err := store.Open(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("%s panel state: %w", action, err)
	}
	defer func() { returnErr = errors.Join(returnErr, state.Close()) }()
	if err := state.Ping(ctx); err != nil {
		return err
	}
	if output != nil {
		_, err = fmt.Fprintf(output, "Stackfort database %s completed.\n", action)
	}
	return err
}

func runBootstrapCreateCommand(ctx context.Context, args []string, output io.Writer) (returnErr error) {
	flags := flag.NewFlagSet("bootstrap create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	ttl := flags.Duration("ttl", 15*time.Minute, "capability lifetime")
	replace := flags.Bool("replace", false, "invalidate and replace an active capability")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse bootstrap create options: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("bootstrap create does not accept positional arguments")
	}
	if output == nil {
		return errors.New("bootstrap command requires an output writer")
	}

	databasePath, err := panelStatePath()
	if err != nil {
		return err
	}
	state, err := store.Open(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("open panel state: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, state.Close())
	}()
	repository, err := core.NewRepository(state)
	if err != nil {
		return fmt.Errorf("initialize control-plane repository: %w", err)
	}
	capability, err := repository.CreateBootstrapCapability(ctx, core.CreateBootstrapCapabilityParams{
		TTL:     *ttl,
		Replace: *replace,
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(output,
		"Bootstrap token (shown once): %s\nExpires at (UTC): %s\nSubmit this token to POST /api/v1/bootstrap.\n",
		capability.Token,
		capability.ExpiresAt.Format(time.RFC3339),
	)
	return err
}
