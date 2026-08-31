// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostlogs

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

func TestLogParsingRedactsQueriesCredentialsAndSensitivePaths(t *testing.T) {
	t.Parallel()
	access := parseLogRecord([]byte(`{"timestamp":"2026-08-29T10:00:00+00:00","client":"192.0.2.10","host":"example.test","method":"GET","path":"/reset/super-secret?token=also-secret","status":200,"bytes":42,"duration":"0.125"}`), agentprotocol.HostingLogAccess)
	if access.Path != "/reset/[REDACTED]" || strings.Contains(access.Path, "secret") || access.DurationMS != 125 {
		t.Fatalf("access record = %#v", access)
	}
	errorRecord := parseLogRecord([]byte(`2026/08/29 10:00:00 [error] 1#1: request: "GET /login?token=secret HTTP/1.1", authorization=Bearer bearer-secret, cookie: session=secret`), agentprotocol.HostingLogError)
	if strings.Contains(strings.ToLower(errorRecord.Message), "bearer-secret") || strings.Contains(errorRecord.Message, "token=secret") ||
		strings.Contains(errorRecord.Message, "session=secret") || !strings.Contains(errorRecord.Message, "[REDACTED]") {
		t.Fatalf("error record was not redacted: %#v", errorRecord)
	}
}

func TestCorazaDiagnosticsAreEntirelyWithheld(t *testing.T) {
	t.Parallel()
	record := parseLogRecord([]byte(`2026/08/30 10:00:00 [error] 1#1: *1 Coraza: Warning. Matched "password=super-secret" [id "942100"] [hostname "example.test"]`), agentprotocol.HostingLogError)
	if record.Message != "Web application firewall event details withheld." ||
		strings.Contains(record.Message, "super-secret") || strings.Contains(record.Message, "942100") {
		t.Fatalf("Coraza diagnostic crossed account log boundary: %#v", record)
	}
}

func TestCorazaEventParserReturnsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()
	line := []byte(`2026/08/31 10:00:00 [error] 1#1: *1 Coraza: Warning. Matched "password=super-secret" [id "942100"] [msg "SQL payload super-secret"] [severity "CRITICAL"] [unique_id "abcdef0123456789"] request: "GET /reset/super-secret?token=also-secret HTTP/1.1"`)
	event, ok := parseWAFEvent(line, 42, 128)
	if !ok || event.RuleID != 942100 || event.Category != agentprotocol.WAFEventSQLInjection ||
		event.Severity != agentprotocol.WAFSeverityCritical || event.Outcome != agentprotocol.WAFEventDetected ||
		event.Method != "GET" || event.Path != "/reset/[REDACTED]" || event.CorrelationID != "abcdef0123456789" {
		t.Fatalf("sanitized event = %#v, parsed=%t", event, ok)
	}
	encoded := strings.ToLower(fmt.Sprintf("%#v", event))
	for _, forbidden := range []string{"super-secret", "also-secret", "password=", "sql payload"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("sanitized event leaked %q: %s", forbidden, encoded)
		}
	}
	if _, ok := parseWAFEvent([]byte(`2026/08/31 10:00:00 [error] ordinary error token=secret`), 42, 256); ok {
		t.Fatal("ordinary error line became a WAF event")
	}
}

func TestCorazaEventParserClassifiesBlockingThreshold(t *testing.T) {
	t.Parallel()
	event, ok := parseWAFEvent([]byte(`2026/08/31 10:00:00 [error] Coraza: intervention [id "949110"] [severity "CRITICAL"] request: "POST /checkout HTTP/1.1"`), 42, 512)
	if !ok || event.Outcome != agentprotocol.WAFEventBlocked || event.Category != agentprotocol.WAFEventAnomalyThreshold {
		t.Fatalf("blocking event = %#v, parsed=%t", event, ok)
	}
}

func TestReverseReaderIsNewestFirstAndCursorBounded(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "stackfort-log-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content := strings.Join([]string{
		`{"timestamp":"2026-08-29T10:00:00+00:00","client":"192.0.2.1","host":"example.test","method":"GET","path":"/one","status":200,"bytes":1,"duration":"0.001"}`,
		`{"timestamp":"2026-08-29T10:01:00+00:00","client":"192.0.2.2","host":"example.test","method":"GET","path":"/two","status":201,"bytes":2,"duration":"0.002"}`,
		`{"timestamp":"2026-08-29T10:02:00+00:00","client":"192.0.2.3","host":"example.test","method":"GET","path":"/three","status":202,"bytes":3,"duration":"0.003"}`,
	}, "\n") + "\n"
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	source := logSource{file: file, size: uint64(len(content))}
	first, cursor, err := readPreviousRecords(source, source.size, 2, agentprotocol.HostingLogAccess)
	if err != nil || len(first) != 2 || first[0].Path != "/three" || first[1].Path != "/two" || cursor == 0 {
		t.Fatalf("first=%#v cursor=%d err=%v", first, cursor, err)
	}
	second, next, err := readPreviousRecords(source, cursor, 2, agentprotocol.HostingLogAccess)
	if err != nil || len(second) != 1 || second[0].Path != "/one" || next != 0 {
		t.Fatalf("second=%#v next=%d err=%v", second, next, err)
	}
}
