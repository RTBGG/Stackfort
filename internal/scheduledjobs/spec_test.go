// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduledjobs

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const testJobID = "019c1234-5678-7abc-8def-0123456789ab"

func TestSchedulesRenderCanonicalUTCExpressions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		schedule Schedule
		want     string
	}{
		{Schedule{Kind: ScheduleInterval, IntervalMinutes: 5}, "*-*-* *:00/5:00 UTC"},
		{Schedule{Kind: ScheduleHourly, MinuteUTC: 17}, "*-*-* *:17:00 UTC"},
		{Schedule{Kind: ScheduleDaily, HourUTC: 2, MinuteUTC: 30}, "*-*-* 02:30:00 UTC"},
		{Schedule{Kind: ScheduleWeekly, HourUTC: 23, MinuteUTC: 59, Weekday: Sunday}, "Sun *-*-* 23:59:00 UTC"},
	}
	for _, test := range tests {
		got, err := Calendar(test.schedule)
		if err != nil || got != test.want {
			t.Errorf("Calendar(%#v) = %q, %v, want %q", test.schedule, got, err, test.want)
		}
	}
}

func TestDefinitionRejectsRawCommandsAndInvalidUnions(t *testing.T) {
	t.Parallel()
	valid := Definition{
		ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/refresh.sh",
		Schedule: Schedule{Kind: ScheduleInterval, IntervalMinutes: 15}, Enabled: true,
	}
	if err := ValidateDefinition(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []Definition{
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "/bin/sh -c id", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/../secret.sh", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/refresh.php", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/refresh.sh", PHPVersion: "8.4", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimePHP, ScriptPath: "jobs/refresh.php", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimePHP, ScriptPath: "jobs/refresh.php", PHPVersion: "9.9", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/run job.sh", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/%n.sh", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/$HOME.sh", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/Grüße.sh", Schedule: valid.Schedule},
		{ID: testJobID, Runtime: RuntimeShell, ScriptPath: "jobs/" + strings.Repeat("a", 248) + ".sh", Schedule: valid.Schedule},
		{ID: "not-an-id", Runtime: RuntimeShell, ScriptPath: "jobs/refresh.sh", Schedule: valid.Schedule},
	}
	for index, candidate := range invalid {
		if err := ValidateDefinition(candidate); err == nil {
			t.Errorf("invalid definition %d accepted: %#v", index, candidate)
		}
	}
}

func TestRendererUsesOnlyDerivedExecutablePathAndHardenedAccountUnit(t *testing.T) {
	t.Parallel()
	identity := hostingidentity.Spec{
		AccountID: testJobID, Username: "sf_3456787abc8def0123456789ab", UID: 200123, GID: 200123,
		HomeDirectory: "/srv/hosting/accounts/" + testJobID,
	}
	definition := Definition{
		ID: testJobID, Runtime: RuntimePHP, ScriptPath: "jobs/refresh.php", PHPVersion: "8.4",
		Schedule: Schedule{Kind: ScheduleDaily, HourUTC: 3, MinuteUTC: 15}, Enabled: true,
	}
	profile, err := Profile("debian", definition)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(profile, Spec{Identity: identity, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	service := string(rendered.Service)
	timer := string(rendered.Timer)
	for _, required := range []string{
		"User=sf_3456787abc8def0123456789ab\n", "Slice=stackfort-accounts-200123.slice\n",
		"ExecStart=/usr/bin/php8.4 /srv/hosting/accounts/" + testJobID + "/jobs/refresh.php\n",
		"RuntimeMaxSec=5min\n", "NoNewPrivileges=yes\n", "ProtectSystem=strict\n",
		"ReadWritePaths=/srv/hosting/accounts/" + testJobID + "\n", "StandardOutput=null\n",
	} {
		if !strings.Contains(service, required) {
			t.Errorf("service omits %q:\n%s", required, service)
		}
	}
	for _, forbidden := range []string{"/bin/sh -c", "EnvironmentFile", "RootDirectoryStartOnly", "%", ";"} {
		if strings.Contains(service, forbidden) {
			t.Errorf("service contains forbidden %q:\n%s", forbidden, service)
		}
	}
	if !strings.Contains(timer, "OnCalendar=*-*-* 03:15:00 UTC\n") ||
		!strings.Contains(timer, "Persistent=true\n") || !strings.Contains(timer, "FixedRandomDelay=yes\n") {
		t.Fatalf("timer = %s", timer)
	}
}
