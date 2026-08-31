// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWaitForPanelHealthRetriesTransientFailure(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := waitForPanelHealth(t.Context(), "control API", func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("not ready")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
}

func TestWaitForPanelHealthHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	err := waitForPanelHealth(ctx, "static listener", func(context.Context) error {
		cancel()
		return errors.New("not ready")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestWaitForPanelHealthReturnsBoundedFailure(t *testing.T) {
	t.Parallel()

	err := waitForPanelHealth(t.Context(), "control API", func(context.Context) error {
		return errors.New("last probe failed")
	})
	if err == nil || !strings.Contains(err.Error(), "control API") || !strings.Contains(err.Error(), "last probe failed") {
		t.Fatalf("error=%v", err)
	}
}
