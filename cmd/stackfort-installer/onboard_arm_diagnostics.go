// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const onboardArmDiagnosticLimit = 8192

var onboardArmSetupToken = regexp.MustCompile(`sfb_[A-Za-z0-9_-]*`)

// Retain a bounded prefix, but drain the pipe even after truncation. A verbose
// diagnostic must not itself kill a boot mutation in progress. No setup code or
// controlling-terminal input is passed to the child; redaction is defense in depth.
type onboardArmDiagnostic struct {
	data      [onboardArmDiagnosticLimit]byte
	size      int
	truncated bool
}

func (diagnostic *onboardArmDiagnostic) Write(data []byte) (int, error) {
	n := copy(diagnostic.data[diagnostic.size:], data)
	diagnostic.size += n
	diagnostic.truncated = diagnostic.truncated || n != len(data)
	return len(data), nil
}

func onboardArmFailure(runErr, contextErr error, diagnostic *onboardArmDiagnostic) error {
	message := "native one-shot boot arming did not complete; reboot was not requested; preserve installer state for inspection"
	if contextErr == context.DeadlineExceeded {
		message += "; arming timed out"
	} else if contextErr != nil {
		message += "; arming was cancelled"
	}
	if diagnostic != nil && diagnostic.size != 0 {
		text := strings.TrimSpace(string(diagnostic.data[:diagnostic.size]))
		text = onboardArmSetupToken.ReplaceAllString(text, "[setup-code-redacted]")
		// Quote control/ANSI/non-ASCII bytes instead of replaying terminal escapes.
		message += "; runtime diagnostic: " + strconv.QuoteToASCII(text)
		if diagnostic.truncated {
			message += " [truncated after 8192 bytes]"
		}
	}
	return fmt.Errorf("%s: %w", message, runErr)
}
