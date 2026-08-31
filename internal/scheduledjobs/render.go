// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduledjobs

import (
	"fmt"
	"strconv"
)

const ManagedUnitHeader = "# Managed by Stackfort. Do not edit.\n"

type RenderedUnits struct {
	ServiceName string
	TimerName   string
	Service     []byte
	Timer       []byte
}

func Render(profile RuntimeProfile, spec Spec) (RenderedUnits, error) {
	if Validate(spec) != nil {
		return RenderedUnits{}, ErrInvalidSpec
	}
	wantedProfile, err := Profile(profile.DistributionID, spec.Definition)
	if err != nil || wantedProfile != profile {
		return RenderedUnits{}, ErrInvalidSpec
	}
	serviceName, timerName, _ := UnitNames(spec.Identity, spec.Definition.ID)
	script, _ := ScriptAbsolutePath(spec.Identity, spec.Definition.ScriptPath)
	calendar, _ := Calendar(spec.Definition.Schedule)
	slice := "stackfort-accounts-" + strconv.FormatUint(uint64(spec.Identity.UID), 10) + ".slice"
	service := fmt.Sprintf(`%s[Unit]
Description=Stackfort scheduled account job %s
After=local-fs.target
RequiresMountsFor=%s

[Service]
Type=oneshot
User=%s
Group=%s
Slice=%s
WorkingDirectory=%s
ExecStart=%s %s
Environment=HOME=%s USER=%s LOGNAME=%s PATH=/usr/local/bin:/usr/bin:/bin TMPDIR=%s/tmp
UMask=0077
RuntimeMaxSec=5min
TimeoutStopSec=15s
KillMode=mixed
Nice=10
CPUWeight=50
TasksMax=64
NoNewPrivileges=yes
PrivateDevices=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ProtectHostname=yes
ProtectClock=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectProc=invisible
ProcSubset=pid
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
SystemCallArchitectures=native
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SocketBindDeny=any
ReadWritePaths=%s
InaccessiblePaths=/etc/stackfort /var/lib/stackfort /var/lib/stackfort-agent /run/stackfort
StandardInput=null
StandardOutput=null
StandardError=null
`, ManagedUnitHeader, spec.Definition.ID, spec.Identity.HomeDirectory,
		spec.Identity.Username, spec.Identity.Username, slice, spec.Identity.HomeDirectory,
		profile.Executable, script, spec.Identity.HomeDirectory, spec.Identity.Username,
		spec.Identity.Username, spec.Identity.HomeDirectory, spec.Identity.HomeDirectory)
	timer := fmt.Sprintf(`%s[Unit]
Description=Stackfort schedule for account job %s

[Timer]
OnCalendar=%s
AccuracySec=1min
RandomizedDelaySec=30s
FixedRandomDelay=yes
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`, ManagedUnitHeader, spec.Definition.ID, calendar, serviceName)
	return RenderedUnits{
		ServiceName: serviceName, TimerName: timerName,
		Service: []byte(service), Timer: []byte(timer),
	}, nil
}
