// SPDX-License-Identifier: AGPL-3.0-or-later

package nginxbaseline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

// This opt-in test only runs nginx -t against test-owned temporary files. It
// never starts/reloads a service or reads the installed hosting configuration.
func TestNGINXLongServerNamesSyntax(t *testing.T) {
	if os.Getenv("STACKFORT_NGINX_SYNTAX_TEST") != "1" {
		t.Skip("set STACKFORT_NGINX_SYNTAX_TEST=1 with /usr/sbin/nginx installed")
	}
	if _, err := os.Stat("/usr/sbin/nginx"); err != nil {
		t.Fatalf("explicit NGINX syntax opt-in requires the executable: %v", err)
	}
	// The longest fixture has a 249-byte base and a valid 253-byte www alias.
	longBase := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." +
		strings.Repeat("c", 63) + "." + strings.Repeat("d", 57)
	fixtures := []string{
		"example.test", "sf-candidate-900356f0971a-static.example.test",
		longBase, strings.Repeat("é", 30) + ".example.test",
	}
	if len(longBase) != 249 {
		t.Fatal("long-name boundary fixture changed")
	}
	directivePattern := regexp.MustCompile(`(?m)^    server_names_hash_bucket_size [0-9]+;$`)
	for _, distribution := range []string{"debian", "ubuntu", "rocky"} {
		spec, err := ForDistribution(distribution)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := CandidateMain(spec, "019c1234-5678-7abc-8def-0123456789ab")
		if err != nil {
			t.Fatal(err)
		}
		for mode, main := range map[string]string{"baseline": Main(spec), "candidate": candidate} {
			directives := directivePattern.FindAllString(main, -1)
			if len(directives) != 1 {
				t.Fatal("rendered configuration needs exactly one server-name hash directive")
			}
			for index, fixture := range fixtures {
				t.Run(fmt.Sprintf("%s/%s/name-%d", distribution, mode, index), func(t *testing.T) {
					name, err := core.NormalizeDomainName(fixture)
					if err != nil {
						t.Fatal(err)
					}
					alias, err := core.NormalizeDomainName("www." + name.ASCII)
					if err != nil {
						t.Fatal(err)
					}
					output, err := testServerNamesSyntax(t, directives[0], name.ASCII+" "+alias.ASCII)
					if err != nil {
						t.Fatalf("rendered hash setting rejects valid hostnames: %v\n%s", err, output)
					}
				})
			}
		}
	}
	// A second virtual server on the same port is essential: NGINX can skip
	// building the name hash entirely when only one virtual server exists.
	for _, fixture := range []struct{ directive, names string }{
		{"server_names_hash_bucket_size 64;", "sf-candidate-900356f0971a-static.example.test www.sf-candidate-900356f0971a-static.example.test"},
		{"server_names_hash_bucket_size 256;", longBase + " www." + longBase},
	} {
		output, err := testServerNamesSyntax(t, fixture.directive, fixture.names)
		if err == nil || !strings.Contains(string(output), "could not build server_names_hash") {
			t.Fatalf("undersized-bucket negative control did not reproduce the hash failure: %v\n%s", err, output)
		}
	}
}

func testServerNamesSyntax(t *testing.T, directive, names string) ([]byte, error) {
	t.Helper()
	root := t.TempDir()
	configuration := fmt.Sprintf(`error_log stderr;
pid %s/nginx.pid;
events {}
http {
    access_log off;
    %s
    server { listen 127.0.0.1:18088 default_server; server_name _; return 444; }
    server { listen 127.0.0.1:18088; server_name %s; return 204; }
}
`, root, directive, names)
	path := filepath.Join(root, "nginx.conf")
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/sbin/nginx", "-t", "-q", "-e", "stderr", "-p", root, "-c", path).CombinedOutput()
}
