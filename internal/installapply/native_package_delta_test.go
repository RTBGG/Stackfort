// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"maps"
	"testing"
)

func TestNativePackageDelta(t *testing.T) {
	for _, scenario := range []string{"exact", "unchanged", "multiarch", "residual", "extra", "removed", "upgraded", "missing", "wrong-version", "duplicate", "existing", "unapproved", "bad-version", "empty", "nil-plan"} {
		t.Run(scenario, func(t *testing.T) {
			before := map[string]string{"dpkg": "1.22.21", "linux-image-amd64": "6.12.1"}
			after := maps.Clone(before)
			after["quota"] = "4.09-1"
			plan := map[string]string{"quota": "4.09-1"}
			switch scenario {
			case "unchanged":
				delete(after, "quota")
				plan = map[string]string{}
			case "multiarch":
				delete(after, "quota")
				after["quota:amd64"] = "4.09-1"
			case "residual":
				before["quota"] = ""
			case "extra":
				after["unrelated"] = "1.0"
			case "removed":
				delete(after, "dpkg")
			case "upgraded":
				after["linux-image-amd64"] = "6.12.2"
			case "missing":
				delete(after, "quota")
			case "wrong-version":
				after["quota"] = "4.08-1"
			case "duplicate":
				after["quota:amd64"] = "4.09-1"
			case "existing":
				before["quota"] = "4.08-1"
			case "unapproved":
				plan["linux-image-amd64"] = "6.12.2"
			case "bad-version":
				plan["quota"] = "bad version"
			case "empty":
				before = nil
			case "nil-plan":
				plan = nil
			}
			want := scenario == "exact" || scenario == "unchanged" || scenario == "multiarch" || scenario == "residual"
			beforeCopy, afterCopy := maps.Clone(before), maps.Clone(after)
			if err := checkNativePackageDelta(before, after, plan); (err == nil) != want {
				t.Fatalf("unexpected result: %v", err)
			}
			if !maps.Equal(before, beforeCopy) || !maps.Equal(after, afterCopy) {
				t.Fatal("delta check mutated its inputs")
			}
		})
	}
}
