// SPDX-License-Identifier: AGPL-3.0-or-later

package installpreflight

import (
	"bufio"
	"errors"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

func available() agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
}

func unknown(reason string) agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: reason}
}

func parseMemoryTotal(content string) (uint64, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || fields[0] != "MemTotal:" || fields[2] != "kB" {
			continue
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || kilobytes > ^uint64(0)/1024 {
			return 0, errors.New("malformed MemTotal")
		}
		return kilobytes * 1024, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("MemTotal is unavailable")
}
