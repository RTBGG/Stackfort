// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/phpruntime"
)

const (
	phpMyAdminConfigurationRoot = "/etc/stackfort/phpmyadmin"
	phpMyAdminFPMConfigPath     = phpMyAdminConfigurationRoot + "/php-fpm.conf"
	phpMyAdminSignonPath        = phpMyAdminConfigurationRoot + "/signon.php"
	phpMyAdminLaunchPath        = phpMyAdminConfigurationRoot + "/stackfort-launch.php"
	phpMyAdminStateRoot         = "/var/lib/stackfort-phpmyadmin"
	phpMyAdminBrokerRoot        = "/var/lib/stackfort-phpmyadmin-broker"
	phpMyAdminBrokerKeyPath     = phpMyAdminBrokerRoot + "/broker.key"
	phpMyAdminBlowfishKeyPath   = phpMyAdminStateRoot + "/blowfish.key"
	phpMyAdminRuntimeRoot       = "/run/stackfort-phpmyadmin"
	phpMyAdminSocketPath        = phpMyAdminRuntimeRoot + "/phpmyadmin.sock"
	phpMyAdminUnit              = "stackfort-phpmyadmin.service"
)

func phpMyAdminDocumentRoot(distribution string) (string, error) {
	switch distribution {
	case "debian", "ubuntu":
		return "/usr/share/phpmyadmin", nil
	case "rocky":
		return "/usr/share/stackfort/phpmyadmin", nil
	default:
		return "", errors.New("unsupported phpMyAdmin distribution")
	}
}

func phpMyAdminNativeConfigurationPath(distribution string) (string, bool) {
	if distribution == "debian" || distribution == "ubuntu" {
		return "/etc/phpmyadmin/conf.d/99-stackfort.php", true
	}
	return "", false
}

func phpMyAdminFPMConfiguration(distribution string) (string, error) {
	profile, err := phpMyAdminPHPProfile(distribution)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`; Managed by Stackfort. Do not edit.
[global]
pid = %s/php-fpm.pid
error_log = syslog
syslog.ident = stackfort-phpmyadmin
daemonize = no

[stackfort-phpmyadmin]
user = stackfort-pma
group = %s
listen = %s
listen.mode = 0660
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 10s
pm.max_requests = 500
request_terminate_timeout = 120s
catch_workers_output = yes
decorate_workers_output = no
clear_env = yes
security.limit_extensions = .php
php_admin_flag[display_errors] = off
php_admin_flag[log_errors] = on
php_admin_flag[expose_php] = off
php_admin_flag[allow_url_fopen] = off
php_admin_flag[allow_url_include] = off
php_admin_value[cgi.fix_pathinfo] = 0
php_admin_value[memory_limit] = 256M
php_admin_value[max_execution_time] = 120
php_admin_value[post_max_size] = 256M
php_admin_value[upload_max_filesize] = 256M
php_admin_value[upload_tmp_dir] = %s/tmp
php_admin_value[session.save_path] = %s/sessions
php_admin_value[session.name] = STACKFORTPMASESSID
php_admin_flag[session.use_strict_mode] = on
php_admin_flag[session.cookie_secure] = on
php_admin_flag[session.cookie_httponly] = on
php_admin_value[session.cookie_samesite] = Strict
php_admin_value[session.cookie_path] = /phpmyadmin/
php_admin_value[open_basedir] = %s:/etc/phpmyadmin:/usr/share/phpmyadmin:/usr/share/stackfort/phpmyadmin:%s:%s:/etc/ssl/certs:/usr/share/ca-certificates
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec
`, phpMyAdminRuntimeRoot, profile.NGINXUser, phpMyAdminSocketPath,
		phpMyAdminStateRoot, phpMyAdminStateRoot, phpMyAdminStateRoot,
		phpMyAdminConfigurationRoot, phpMyAdminBrokerRoot), nil
}

func phpMyAdminServiceUnit(distribution string) string {
	profile, err := phpMyAdminPHPProfile(distribution)
	if err != nil {
		return ""
	}
	return managedHeader + fmt.Sprintf(`[Unit]
Description=Stackfort isolated phpMyAdmin runtime
After=network.target mariadb.service stackfort-api.service
Requires=mariadb.service stackfort-api.service

[Service]
Type=notify
NotifyAccess=main
User=stackfort-pma
Group=%s
SupplementaryGroups=stackfort-pma
ExecStart=%s --nodaemonize --fpm-config %s
ExecReload=/bin/kill -USR2 $MAINPID
Restart=on-failure
RestartSec=2s
TimeoutStartSec=30s
TimeoutStopSec=30s
KillMode=mixed
Slice=stackfort-core.slice
RuntimeDirectory=stackfort-phpmyadmin
RuntimeDirectoryMode=0750
UMask=0007
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
RestrictAddressFamilies=AF_UNIX AF_INET
IPAddressDeny=any
IPAddressAllow=localhost
ReadWritePaths=%s
ReadOnlyPaths=%s %s

[Install]
WantedBy=multi-user.target
`, profile.NGINXUser, profile.BinaryPath, phpMyAdminFPMConfigPath, phpMyAdminStateRoot,
		phpMyAdminConfigurationRoot, phpMyAdminBrokerRoot)
}

func phpMyAdminPHPProfile(distribution string) (phpruntime.Profile, error) {
	version, err := phpruntime.ApprovedVersion(distribution)
	if err != nil {
		return phpruntime.Profile{}, errors.New("unsupported phpMyAdmin PHP runtime distribution")
	}
	return phpruntime.ForDistribution(distribution, version)
}
