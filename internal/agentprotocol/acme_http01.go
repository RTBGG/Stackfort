// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
)

type ACMEHTTP01Request struct {
	Intent acmehttp01.Intent `json:"intent"`
}

type ACMEHTTP01Response struct {
	Action    acmehttp01.Action `json:"action"`
	Changed   bool              `json:"changed"`
	Presented bool              `json:"presented"`
}

func validateACMEHTTP01Response(response ACMEHTTP01Response, operation Operation) error {
	if operation != OperationReconcileACMEHTTP01 {
		return errors.New("agent ACME HTTP-01 response has the wrong operation")
	}
	switch response.Action {
	case acmehttp01.ActionPresent:
		if !response.Presented {
			return errors.New("agent ACME HTTP-01 present response is malformed")
		}
	case acmehttp01.ActionCleanup:
		if response.Presented {
			return errors.New("agent ACME HTTP-01 cleanup response is malformed")
		}
	default:
		return errors.New("agent ACME HTTP-01 response action is malformed")
	}
	return nil
}
