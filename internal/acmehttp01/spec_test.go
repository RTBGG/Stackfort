// SPDX-License-Identifier: AGPL-3.0-or-later

package acmehttp01

import (
	"errors"
	"strings"
	"testing"
)

func TestIntentAcceptsOnlyClosedRFC8555TokenAndKeyAuthorizationShapes(t *testing.T) {
	t.Parallel()
	token := "0123456789abcdefghijkl"
	keyAuthorization := token + "." + strings.Repeat("A", 43)
	for _, intent := range []Intent{
		{Action: ActionPresent, Token: token, KeyAuthorization: keyAuthorization},
		{Action: ActionCleanup, Token: token},
	} {
		if err := Validate(intent); err != nil {
			t.Fatalf("valid intent %#v: %v", intent, err)
		}
	}
	for _, intent := range []Intent{
		{Action: "write", Token: token, KeyAuthorization: keyAuthorization},
		{Action: ActionPresent, Token: "../escape", KeyAuthorization: keyAuthorization},
		{Action: ActionPresent, Token: token, KeyAuthorization: "different." + strings.Repeat("A", 43)},
		{Action: ActionPresent, Token: token, KeyAuthorization: token + "." + strings.Repeat("A", 42)},
		{Action: ActionCleanup, Token: token, KeyAuthorization: keyAuthorization},
	} {
		if err := Validate(intent); !errors.Is(err, ErrInvalidIntent) {
			t.Fatalf("invalid intent %#v error = %v", intent, err)
		}
	}
}
