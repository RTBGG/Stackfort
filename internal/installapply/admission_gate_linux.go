// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/hostcapabilities"
)

const admissionTable = "stackfort_install_admission"

// LinuxAdmissionGate protects the fixed public web listeners (TCP and UDP,
// IPv4 and IPv6). It is not a firewall for arbitrary tenant-published ports.
// SSH and loopback health probes are deliberately unaffected. Debian lab only.
type LinuxAdmissionGate struct {
	operation string
	run       func(context.Context, string, string, ...string) (string, error)
}

func NewLinuxAdmissionGate(operation string) (*LinuxAdmissionGate, error) {
	if os.Geteuid() != 0 || !validSourceOperation(operation) || hostcapabilities.NewInspector().InspectPlatform().DistributionID != "debian" {
		return nil, errors.New("native admission gate is currently qualified only for root on Debian")
	}
	return &LinuxAdmissionGate{operation: operation, run: runAdmissionCommand}, nil
}

func (gate *LinuxAdmissionGate) listing(closed bool) string {
	result := "table inet " + admissionTable + " {\ncomment \"stackfort-install-admission-v1:" + gate.operation + "\"\nchain ingress {\ntype filter hook input priority -150; policy accept;\n"
	if closed {
		result += "iifname != \"lo\" tcp dport { 80, 443, 8443 } drop\niifname != \"lo\" udp dport { 80, 443, 8443 } drop\n"
	}
	return result + "}\n}\n"
}

func sameNFT(actual, expected string) bool {
	// nft -s -n -y emits no dynamic counters or handles here. Accept whitespace
	// only; unknown flags, chains, rules, comments or expressions are conflicts.
	return strings.Join(strings.Fields(actual), " ") == strings.Join(strings.Fields(expected), " ")
}

func (gate *LinuxAdmissionGate) state(ctx context.Context) (string, error) {
	if gate == nil || gate.run == nil || !validSourceOperation(gate.operation) {
		return "", errors.New("invalid admission gate")
	}
	tables, err := gate.run(ctx, "", "/usr/sbin/nft", "list", "tables", "inet")
	if err != nil {
		return "", err
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(tables), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "table" || fields[1] != "inet" {
			return "", errors.New("unexpected nft table inventory")
		}
		if fields[2] == admissionTable {
			found = true
		}
	}
	if !found {
		return "absent", nil
	}
	content, err := gate.run(ctx, "", "/usr/sbin/nft", "-s", "-n", "-y", "list", "table", "inet", admissionTable)
	if err != nil {
		return "", err
	}
	if sameNFT(content, gate.listing(true)) {
		return "closed", nil
	}
	if sameNFT(content, gate.listing(false)) {
		return "open", nil
	}
	return "", errors.New("admission nft table conflicts with exact operation/policy; refusing to overwrite")
}

func (gate *LinuxAdmissionGate) Close(ctx context.Context) error {
	state, err := gate.state(ctx)
	if err != nil || state == "closed" {
		return err
	}
	script := ""
	if state == "absent" {
		script = "create table inet " + admissionTable + " { comment \"stackfort-install-admission-v1:" + gate.operation + "\"; }\n" +
			"add chain inet " + admissionTable + " ingress { type filter hook input priority -150; policy accept; }\n"
	}
	for _, protocol := range []string{"tcp", "udp"} {
		script += "add rule inet " + admissionTable + " ingress iifname != \"lo\" " + protocol + " dport { 80, 443, 8443 } drop\n"
	}
	if _, err := gate.run(ctx, script, "/usr/sbin/nft", "-f", "-"); err != nil {
		return err
	}
	return gate.VerifyClosed(ctx)
}

func (gate *LinuxAdmissionGate) VerifyClosed(ctx context.Context) error {
	state, err := gate.state(ctx)
	if err != nil {
		return err
	}
	if state != "closed" {
		return errors.New("public web admission gate is not closed")
	}
	return nil
}

func (gate *LinuxAdmissionGate) Open(ctx context.Context) error {
	if err := gate.VerifyClosed(ctx); err != nil {
		return err
	}
	// One atomic transaction in our exact validated chain. Never flush ruleset
	// or another service's firewall table. The owned empty table remains marked.
	if _, err := gate.run(ctx, "flush chain inet "+admissionTable+" ingress\n", "/usr/sbin/nft", "-f", "-"); err != nil {
		return err
	}
	return gate.VerifyOpen(ctx)
}

func (gate *LinuxAdmissionGate) VerifyOpen(ctx context.Context) error {
	state, err := gate.state(ctx)
	if err != nil {
		return err
	}
	if state != "open" {
		return errors.New("public web admission gate is not open")
	}
	return nil
}

func (gate *LinuxAdmissionGate) StopConsumers(ctx context.Context) error {
	if gate == nil || gate.run == nil {
		return errors.New("invalid admission gate")
	}
	var failures []error
	for _, unit := range []string{"stackfort-panel-renew.timer", "stackfort-panel-renew.service", "nginx.service", "stackfort-api.service", "stackfort-phpmyadmin.service", "stackfort-agent.service", "vinyl.service", "mariadb.service", "php8.4-fpm.service"} {
		load, err := gate.run(ctx, "", "/usr/bin/systemctl", "show", "--property=LoadState", "--value", unit)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if strings.TrimSpace(load) == "not-found" {
			continue
		}
		if _, err := gate.run(ctx, "", "/usr/bin/systemctl", "stop", unit); err != nil {
			failures = append(failures, err)
			continue
		}
		active, err := gate.run(ctx, "", "/usr/bin/systemctl", "show", "--property=ActiveState", "--value", unit)
		if err != nil || (strings.TrimSpace(active) != "inactive" && strings.TrimSpace(active) != "failed") {
			failures = append(failures, errors.Join(err, fmt.Errorf("consumer not stopped: %s", unit)))
		}
	}
	return errors.Join(failures...)
}

func runAdmissionCommand(ctx context.Context, input, executable string, arguments ...string) (string, error) {
	if ctx == nil || (executable != "/usr/sbin/nft" && executable != "/usr/bin/systemctl") {
		return "", errors.New("invalid admission command")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// #nosec G204 -- executable and arguments are fixed by the gate methods; no shell.
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdin = strings.NewReader(input)
	var stdout, stderr boundedOriginOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("admission command failed: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}
