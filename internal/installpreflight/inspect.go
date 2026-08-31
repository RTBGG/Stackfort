// SPDX-License-Identifier: AGPL-3.0-or-later

package installpreflight

import (
	"context"
	"fmt"

	"github.com/RTBGG/stackfort/internal/hostcapabilities"
)

// Inspect performs only bounded reads, statfs, and allowlisted package/service
// queries. It deliberately exposes no installation or apply operation.
func Inspect(ctx context.Context) (Result, error) {
	capabilities, err := hostcapabilities.NewInspector().Inspect(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("inspect host capabilities: %w", err)
	}
	return Evaluate(capabilities, InspectResources()), nil
}
