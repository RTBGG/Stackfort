// SPDX-License-Identifier: AGPL-3.0-or-later

package nginxbaseline

import (
	"errors"
	"strings"
	"testing"
)

func TestSupportedDistributionWorkerUsers(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct{ distribution, user string }{
		{"debian", "www-data"}, {"ubuntu", "www-data"}, {"rocky", "nginx"},
	} {
		spec, err := ForDistribution(fixture.distribution)
		if err != nil || spec.WorkerUser != fixture.user {
			t.Fatalf("ForDistribution(%q) = %#v, %v", fixture.distribution, spec, err)
		}
		configuration := Main(spec)
		for _, required := range []string{
			"user " + fixture.user + ";", "worker_rlimit_nofile 65536;", "server_tokens off;",
			"include /etc/nginx/stackfort/default/*.conf;",
			"include /etc/nginx/stackfort/panel-enabled/*.conf;",
			"include /etc/nginx/stackfort/sites-enabled/*.conf;",
		} {
			if !strings.Contains(configuration, required) {
				t.Fatalf("main configuration omits %q", required)
			}
		}
	}
}

func TestUnsupportedDistributionFailsClosed(t *testing.T) {
	t.Parallel()
	_, err := ForDistribution("arch")
	if !errors.Is(err, ErrUnsupportedDistribution) {
		t.Fatalf("expected unsupported distribution, got %v", err)
	}
}

func TestBaselineRejectsUnknownHostsAndTrustsOnlyLoopback(t *testing.T) {
	t.Parallel()
	for content, required := range map[string][]string{
		DefaultHTTP():  {"listen 80 default_server;", "return 444;"},
		DefaultHTTPS(): {"listen 443 ssl default_server;", "ssl_reject_handshake on;"},
		TrustedProxies(): {
			"set_real_ip_from 127.0.0.1;", "set_real_ip_from ::1;",
			"real_ip_header X-Forwarded-For;", "real_ip_recursive on;",
		},
	} {
		for _, value := range required {
			if !strings.Contains(content, value) {
				t.Fatalf("baseline fragment omits %q", value)
			}
		}
	}
	if strings.Contains(TrustedProxies(), "0.0.0.0") || strings.Contains(TrustedProxies(), "::/0") {
		t.Fatal("trusted proxy configuration must not trust public peers")
	}
}

func TestManagedAccessFormatDoesNotPersistRequestSecrets(t *testing.T) {
	t.Parallel()
	spec, _ := ForDistribution("debian")
	content := Main(spec)
	for _, required := range []string{"log_format stackfort_redacted escape=json", `"path":"$uri"`, `"duration":"$request_time"`} {
		if !strings.Contains(content, required) {
			t.Fatalf("managed log format omits %q:\n%s", required, content)
		}
	}
	for _, forbidden := range []string{"$request\"", "$args", "$query_string", "$http_cookie", "$http_authorization", "$http_referer", "$http_user_agent"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("managed log format persists %q:\n%s", forbidden, content)
		}
	}
}

func TestPanelUsesDedicatedTLSPortAndFixedLocalUpstream(t *testing.T) {
	t.Parallel()
	spec, _ := ForDistribution("debian")
	content := Panel(spec)
	for _, required := range []string{
		"listen 8443 ssl default_server;", "ssl_certificate " + PanelTLSBundlePath + ";",
		"proxy_pass http://127.0.0.1:8080;", "root /usr/share/stackfort/web;",
		"try_files $uri $uri/ /index.html;", "script-src 'self'", "frame-ancestors 'none'",
		"fastcgi_pass unix:/run/stackfort-phpmyadmin/phpmyadmin.sock;",
		"fastcgi_param SCRIPT_FILENAME /usr/share/phpmyadmin/$1;",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("panel configuration omits %q:\n%s", required, content)
		}
	}
	for _, forbidden := range []string{"proxy_pass http://$", "ssl_certificate $", "alias $"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("panel configuration contains %q:\n%s", forbidden, content)
		}
	}
	rocky, _ := ForDistribution("rocky")
	if rockyPanel := Panel(rocky); !strings.Contains(rockyPanel, "fastcgi_param SCRIPT_FILENAME /usr/share/stackfort/phpmyadmin/$1;") {
		t.Fatalf("Rocky panel does not select bundled phpMyAdmin:\n%s", rockyPanel)
	}
}

func TestServiceAlwaysUsesOwnedConfigurationAndCoreSlice(t *testing.T) {
	t.Parallel()
	content := SystemdDropIn()
	for _, required := range []string{
		"Slice=stackfort-core.slice", "LimitNOFILE=65536", "ExecStartPre=", "ExecStartPre=/usr/bin/rm -f /run/nginx.pid",
		"nginx -t -q -c " + MainConfiguration,
		"ExecStart=", "nginx -c " + MainConfiguration, "ExecReload=",
		"ExecReload=/usr/sbin/nginx -t -q -c " + MainConfiguration,
		"ExecReload=/usr/sbin/nginx -c " + MainConfiguration + " -s reload",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("systemd drop-in omits %q", required)
		}
	}
}
