// SPDX-License-Identifier: AGPL-3.0-or-later

package wafconfig

import (
	"testing"
	"time"
)

func TestValidateExceptionScopeIsClosedAndNarrow(t *testing.T) {
	t.Parallel()
	for _, valid := range []struct {
		rule uint32
		path string
		arg  string
	}{{920274, "/login", "username"}, {941100, "/search/results", "q"}, {932100, "", "command"}} {
		if err := ValidateExceptionScope(valid.rule, valid.path, valid.arg); err != nil {
			t.Fatalf("ValidateExceptionScope(%#v): %v", valid, err)
		}
	}
	for _, invalid := range []struct {
		rule uint32
		path string
		arg  string
	}{{949110, "/login", "q"}, {941100, "", ""}, {941100, "/foo.*", ""}, {941100, "/x?secret=y", ""}, {941100, "/x", "q;ctl:ruleEngine=Off"}, {941100, "/x'", ""}} {
		if err := ValidateExceptionScope(invalid.rule, invalid.path, invalid.arg); err == nil {
			t.Fatalf("ValidateExceptionScope(%#v) accepted unsafe scope", invalid)
		}
	}
}

func TestValidateExceptionExpiryBoundsLifetime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	if err := ValidateExceptionExpiry(now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExceptionExpiry(now, now.Add(time.Minute)); err == nil {
		t.Fatal("accepted one-minute exception")
	}
	if err := ValidateExceptionExpiry(now, now.Add(31*24*time.Hour)); err == nil {
		t.Fatal("accepted exception beyond thirty days")
	}
}
