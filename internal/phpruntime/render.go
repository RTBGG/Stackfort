// SPDX-License-Identifier: AGPL-3.0-or-later

package phpruntime

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingresources"
)

const managedHeader = "; Managed by Stackfort. Do not edit.\n"

func RenderFPMConfiguration(profile Profile, spec PoolSetSpec) ([]byte, error) {
	if err := Validate(spec); err != nil || !slicesContains(spec.Versions, profile.Version) {
		return nil, ErrInvalidSpec
	}
	wanted, err := ForDistribution(profile.DistributionID, profile.Version)
	if err != nil || wanted != profile {
		return nil, ErrInvalidSpec
	}
	socket, _ := SocketPath(spec.Identity, profile.Version)
	pid, _ := PIDPath(spec.Identity, profile.Version)
	var output strings.Builder
	output.WriteString(managedHeader)
	output.WriteString("[global]\n")
	output.WriteString("pid = " + pid + "\n")
	output.WriteString("error_log = syslog\n")
	output.WriteString("syslog.facility = daemon\n")
	output.WriteString("syslog.ident = stackfort-php-" + strconv.FormatUint(uint64(spec.Identity.UID), 10) + "\n")
	output.WriteString("daemonize = no\n\n")
	output.WriteString("[stackfort]\n")
	output.WriteString("user = " + spec.Identity.Username + "\n")
	output.WriteString("group = " + spec.Identity.Username + "\n")
	output.WriteString("listen = " + socket + "\n")
	output.WriteString("listen.owner = " + profile.NGINXUser + "\n")
	output.WriteString("listen.group = " + profile.NGINXUser + "\n")
	output.WriteString("listen.mode = 0600\n")
	output.WriteString("pm = ondemand\n")
	output.WriteString("pm.max_children = " + strconv.FormatUint(uint64(spec.MaxChildren), 10) + "\n")
	output.WriteString("pm.process_idle_timeout = 10s\n")
	output.WriteString("pm.max_requests = 500\n")
	output.WriteString("request_terminate_timeout = 120s\n")
	output.WriteString("catch_workers_output = yes\n")
	output.WriteString("decorate_workers_output = no\n")
	output.WriteString("clear_env = yes\n")
	output.WriteString("security.limit_extensions = .php\n")
	output.WriteString("php_admin_flag[display_errors] = off\n")
	output.WriteString("php_admin_flag[log_errors] = on\n")
	output.WriteString("php_admin_flag[expose_php] = off\n")
	output.WriteString("php_admin_flag[allow_url_include] = off\n")
	output.WriteString("php_admin_value[cgi.fix_pathinfo] = 0\n")
	output.WriteString("php_admin_value[memory_limit] = " + strconv.FormatUint(uint64(spec.MemoryLimitMiB), 10) + "M\n")
	output.WriteString("php_admin_value[upload_tmp_dir] = " + spec.Identity.HomeDirectory + "/tmp\n")
	output.WriteString("php_admin_value[session.save_path] = " + spec.Identity.HomeDirectory + "/tmp\n")
	output.WriteString("php_admin_flag[session.cookie_httponly] = on\n")
	output.WriteString("php_admin_value[session.cookie_samesite] = Lax\n")
	return []byte(output.String()), nil
}

func RenderSystemdUnit(profile Profile, spec PoolSetSpec) ([]byte, error) {
	configuration, err := ConfigurationPath(spec.Identity, profile.Version)
	if err != nil {
		return nil, err
	}
	if _, err := RenderFPMConfiguration(profile, spec); err != nil {
		return nil, err
	}
	slice, err := hostingresources.AccountSliceName(spec.Identity.UID)
	if err != nil {
		return nil, ErrInvalidSpec
	}
	unit := fmt.Sprintf(`# Managed by Stackfort. Do not edit.
[Unit]
Description=Stackfort PHP %s pool for account %d
After=local-fs.target
RequiresMountsFor=%s

[Service]
Type=notify
NotifyAccess=main
Slice=%s
ExecStart=%s --nodaemonize --fpm-config %s
ExecReload=/bin/kill -USR2 $MAINPID
Restart=on-failure
RestartSec=2s
TimeoutStartSec=30s
TimeoutStopSec=30s
KillMode=mixed
UMask=0077
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
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
SystemCallArchitectures=native
CapabilityBoundingSet=CAP_CHOWN CAP_KILL CAP_SETGID CAP_SETUID
AmbientCapabilities=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=%s %s

[Install]
WantedBy=multi-user.target
`, profile.Version, spec.Identity.UID, spec.Identity.HomeDirectory, slice,
		profile.BinaryPath, configuration, spec.Identity.HomeDirectory, RuntimeRoot)
	return []byte(unit), nil
}

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
