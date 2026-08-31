// SPDX-License-Identifier: AGPL-3.0-or-later

package hostcapabilities

import (
	"context"

	"github.com/RTBGG/stackfort/internal/agentexec"
)

type operatingSystemRunner struct {
	runner *agentexec.Runner
}

func newOperatingSystemRunner() operatingSystemRunner {
	return operatingSystemRunner{runner: agentexec.NewRunner()}
}

func (runner operatingSystemRunner) Run(
	ctx context.Context,
	executable string,
	arguments ...string,
) (commandResult, error) {
	invocation, ok := capabilityInvocation(executable, arguments)
	if !ok {
		return commandResult{}, agentexec.ErrNotAllowlisted
	}
	if runner.runner == nil {
		runner.runner = agentexec.NewRunner()
	}
	result, err := runner.runner.Run(ctx, invocation)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{Output: result.Stdout, ExitCode: result.ExitCode}, nil
}

func capabilityInvocation(executable string, arguments []string) (agentexec.Invocation, bool) {
	switch executable {
	case "/usr/bin/dpkg-query":
		if len(arguments) != 3 || arguments[0] != "-W" ||
			arguments[1] != "-f=${db:Status-Abbrev}\\t${Version}\\n" {
			return agentexec.Invocation{}, false
		}
		return agentexec.Invocation{Profile: agentexec.ProfileDpkgQuery, Values: []string{arguments[2]}}, true
	case "/usr/bin/rpm":
		if len(arguments) != 4 || arguments[0] != "-q" || arguments[1] != "--qf" ||
			arguments[2] != "%{VERSION}-%{RELEASE}\\n" {
			return agentexec.Invocation{}, false
		}
		return agentexec.Invocation{Profile: agentexec.ProfileRPMQuery, Values: []string{arguments[3]}}, true
	case "/usr/bin/systemctl":
		if len(arguments) != 7 || arguments[0] != "show" || arguments[1] != "--no-pager" ||
			arguments[2] != "--property=LoadState" || arguments[3] != "--property=ActiveState" ||
			arguments[4] != "--property=SubState" || arguments[5] != "--property=UnitFileState" {
			return agentexec.Invocation{}, false
		}
		return agentexec.Invocation{Profile: agentexec.ProfileSystemctlShow, Values: []string{arguments[6]}}, true
	default:
		return agentexec.Invocation{}, false
	}
}

var _ commandRunner = operatingSystemRunner{}
