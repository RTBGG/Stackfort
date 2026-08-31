// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"context"
	"fmt"
	"time"
)

const (
	panelHealthAttempts = 20
	panelHealthDelay    = 100 * time.Millisecond
)

func waitForPanelHealth(ctx context.Context, component string, probe func(context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt < panelHealthAttempts; attempt++ {
		if err := probe(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(panelHealthDelay):
		}
	}
	return fmt.Errorf("panel HTTPS %s did not become ready: %w", component, lastErr)
}
