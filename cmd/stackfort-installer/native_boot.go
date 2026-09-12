// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/RTBGG/stackfort/internal/installapply"
)

// Internal boot dispatcher only; no public prepare/device/source/reset option.
func runNativeBoot(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return exitError
	}
	flags := flag.NewFlagSet("native-boot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	operation := flags.String("operation-id", "", "sealed operation UUID")
	if err := flags.Parse(args[1:]); err != nil {
		return exitError
	}
	request := installapply.NativeBootRequest{Action: args[0], OperationID: *operation}
	if flags.NArg() != 0 || request.Validate() != nil {
		return exitError
	}
	if err := installapply.RunNativeBoot(ctx, request, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "native boot failed:", err)
		return exitError
	}
	return exitReady
}
