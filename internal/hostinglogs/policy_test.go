// SPDX-License-Identifier: AGPL-3.0-or-later

package hostinglogs

import (
	"strings"
	"testing"
)

func TestDomainFileIsOpaqueAndAlwaysUsesLinuxPaths(t *testing.T) {
	t.Parallel()
	path := DomainFile("019c1234-5678-7abc-8def-0123456789ab", "example.test", "access")
	if path != "/var/log/stackfort/accounts/019c1234-5678-7abc-8def-0123456789ab/domain-9b263fbcb589853137b33ddcafa5bcc5403464ead4da766d0f819348bf8d472c.access.log" {
		t.Fatalf("DomainFile() = %q", path)
	}
}

func TestRetentionConfigurationMatchesTheReaderContract(t *testing.T) {
	t.Parallel()
	content := RetentionConfiguration()
	for _, required := range []string{
		RootDirectory + "/*/*.access.log", "rotate 7\n", "maxage 7\n", "maxsize 8M\n",
		"compress\n", "delaycompress\n", "nodateext\n", "create 0640 root root\n",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("retention configuration omits %q:\n%s", required, content)
		}
	}
}
