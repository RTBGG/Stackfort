// SPDX-License-Identifier: AGPL-3.0-or-later

// Package nginxbaseline defines the immutable, distribution-aware NGINX
// configuration owned by Stackfort. Keeping the renderers free of host I/O
// makes the privileged reconciliation boundary small and testable.
package nginxbaseline

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	ManagedRoot               = "/etc/nginx/stackfort"
	MarkerPath                = ManagedRoot + "/.stackfort-managed"
	MainConfiguration         = ManagedRoot + "/nginx.conf"
	GlobalDirectory           = ManagedRoot + "/global"
	TrustedProxiesPath        = GlobalDirectory + "/trusted-proxies.conf"
	DefaultDirectory          = ManagedRoot + "/default"
	DefaultHTTPPath           = DefaultDirectory + "/00-default-http.conf"
	DefaultHTTPSPath          = DefaultDirectory + "/10-default-https.conf"
	PanelDirectory            = ManagedRoot + "/panel-enabled"
	PanelConfigurationPath    = PanelDirectory + "/00-panel.conf"
	PanelTLSBundlePath        = "/etc/stackfort/panel-tls/bootstrap.pem"
	PanelPort                 = 8443
	SitesDirectory            = ManagedRoot + "/sites-enabled"
	SiteRevisionsDirectory    = ManagedRoot + "/site-revisions"
	SiteTransactionsDirectory = ManagedRoot + "/site-transactions"
	CurrentSitesLink          = ManagedRoot + "/sites-current"
	SitesCurrentIncludePath   = SitesDirectory + "/00-current.conf"
	ActivationJournalPath     = ManagedRoot + "/site-activation.json"
	ActivationLockPath        = ManagedRoot + "/.site-activation.lock"
	SystemdDropInDir          = "/etc/systemd/system/nginx.service.d"
	SystemdDropInPath         = SystemdDropInDir + "/20-stackfort.conf"
	OwnershipMarker           = "stackfort-nginx-baseline-v1\n"
	NGINXUnit                 = "nginx.service"
	LoopbackIPv4              = "127.0.0.1/32"
	LoopbackIPv6              = "::1/128"
)

var ErrUnsupportedDistribution = errors.New("NGINX baseline is unsupported on this distribution")

// Spec contains the only distribution-specific value in the baseline.
type Spec struct {
	DistributionID string
	WorkerUser     string
}

// ForDistribution returns a baseline only for the three supported platform
// families. Version support is enforced by host capability inspection.
func ForDistribution(distributionID string) (Spec, error) {
	switch distributionID {
	case "debian", "ubuntu":
		return Spec{DistributionID: distributionID, WorkerUser: "www-data"}, nil
	case "rocky":
		return Spec{DistributionID: distributionID, WorkerUser: "nginx"}, nil
	default:
		return Spec{}, fmt.Errorf("%w: %s", ErrUnsupportedDistribution, distributionID)
	}
}

func Main(spec Spec) string {
	return main(spec, "/etc/nginx/stackfort/sites-enabled/*.conf")
}

// CandidateMain renders an alternate complete root configuration that reads
// one private global site revision without changing the active include pointer.
func CandidateMain(spec Spec, revisionID string) (string, error) {
	parsed, err := uuid.Parse(revisionID)
	if err != nil || parsed.String() != revisionID || parsed.Version() != uuid.Version(7) {
		return "", errors.New("candidate NGINX revision must be a canonical UUIDv7")
	}
	return main(spec, SiteRevisionsDirectory+"/"+revisionID+"/*.conf"), nil
}

func main(spec Spec, sitesInclude string) string {
	return fmt.Sprintf(`# Managed by Stackfort. Do not edit.
user %s;
worker_processes auto;
worker_rlimit_nofile 65536;
pid /run/nginx.pid;
error_log /var/log/nginx/error.log notice;

include /etc/nginx/modules-enabled/*.conf;
include /usr/share/nginx/modules/*.conf;

events {
    worker_connections 4096;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

	# Query strings, credentials, cookies, referrers, and user agents are never
	# persisted by the managed format. The host agent performs a second
	# redaction pass before any account-facing response.
	log_format stackfort_redacted escape=json
	    '{"timestamp":"$time_iso8601","client":"$remote_addr","host":"$host",'
	    '"method":"$request_method","path":"$uri","status":$status,'
	    '"bytes":$body_bytes_sent,"duration":"$request_time",'
	    '"cache":"$upstream_http_x_stackfort_cache"}';
	access_log /var/log/nginx/access.log stackfort_redacted;

    sendfile on;
    tcp_nopush on;
    keepalive_timeout 65;
    server_tokens off;

    include /etc/nginx/stackfort/global/*.conf;
    include /etc/nginx/stackfort/default/*.conf;
    include /etc/nginx/stackfort/panel-enabled/*.conf;
    include %s;
}
`, spec.WorkerUser, sitesInclude)
}

func SitesCurrentInclude() string {
	return `# Managed by Stackfort. Do not edit.
include /etc/nginx/stackfort/sites-current/*.conf;
`
}

func TrustedProxies() string {
	return `# Managed by Stackfort. Do not edit.
# Only same-host reverse proxies may replace the apparent client address.
set_real_ip_from 127.0.0.1;
set_real_ip_from ::1;
real_ip_header X-Forwarded-For;
real_ip_recursive on;
`
}

func DefaultHTTP() string {
	return `# Managed by Stackfort. Do not edit.
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name "";
    access_log off;
    return 444;
}
`
}

func DefaultHTTPS() string {
	return `# Managed by Stackfort. Do not edit.
server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    server_name _;
    ssl_reject_handshake on;
    access_log off;
}
`
}

// Panel renders the dedicated management listener. Keeping the panel on its
// own TLS port preserves the rejecting customer-site default on 443 and gives
// the initial browser flow an encrypted origin before a public panel hostname
// or ACME certificate exists.
func Panel(spec Spec) string {
	phpMyAdminRoot := "/usr/share/phpmyadmin"
	if spec.DistributionID == "rocky" {
		phpMyAdminRoot = "/usr/share/stackfort/phpmyadmin"
	}
	return fmt.Sprintf(`# Managed by Stackfort. Do not edit.
server {
    listen %d ssl default_server;
    listen [::]:%d ssl default_server;
    server_name _;

    ssl_certificate %s;
    ssl_certificate_key %s;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:StackfortPanelTLS:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;

    root /usr/share/stackfort/web;
    index index.html;
    client_max_body_size 256m;

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_request_buffering on;
    }

    location = /phpmyadmin {
        return 308 /phpmyadmin/;
    }

    location = /phpmyadmin/stackfort-launch.php {
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME /etc/stackfort/phpmyadmin/stackfort-launch.php;
        fastcgi_param HTTPS on;
        fastcgi_param HTTP_PROXY "";
        fastcgi_pass unix:/run/stackfort-phpmyadmin/phpmyadmin.sock;
        fastcgi_request_buffering on;
        client_max_body_size 4k;
    }

    location ~ ^/phpmyadmin/(?:doc|examples|libraries|setup|templates|vendor)/ {
        return 404;
    }

    location ~ ^/phpmyadmin/([A-Za-z0-9_./-]+[.]php)$ {
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME %s/$1;
        fastcgi_param HTTPS on;
        fastcgi_param HTTP_PROXY "";
        fastcgi_pass unix:/run/stackfort-phpmyadmin/phpmyadmin.sock;
        fastcgi_request_buffering on;
    }

    location /phpmyadmin/ {
        alias %s/;
        index index.php;
        disable_symlinks on;
    }

    location = /index.html {
        add_header Cache-Control "no-store" always;
        add_header Content-Security-Policy "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'" always;
        add_header Referrer-Policy "no-referrer" always;
        add_header X-Content-Type-Options "nosniff" always;
    }

    location / {
        try_files $uri $uri/ /index.html;
        add_header Content-Security-Policy "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'" always;
        add_header Referrer-Policy "no-referrer" always;
        add_header X-Content-Type-Options "nosniff" always;
    }
}
`, PanelPort, PanelPort, PanelTLSBundlePath, PanelTLSBundlePath, phpMyAdminRoot, phpMyAdminRoot)
}

func SystemdDropIn() string {
	return `# Managed by Stackfort. Do not edit.
[Service]
Slice=stackfort-core.slice
LimitNOFILE=65536
ExecStartPre=
ExecStartPre=/usr/bin/rm -f /run/nginx.pid
ExecStartPre=/usr/sbin/nginx -t -q -c /etc/nginx/stackfort/nginx.conf
ExecStart=
ExecStart=/usr/sbin/nginx -c /etc/nginx/stackfort/nginx.conf
ExecReload=
ExecReload=/usr/sbin/nginx -t -q -c /etc/nginx/stackfort/nginx.conf
ExecReload=/usr/sbin/nginx -c /etc/nginx/stackfort/nginx.conf -s reload
`
}
