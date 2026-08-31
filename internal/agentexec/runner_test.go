// SPDX-License-Identifier: AGPL-3.0-or-later

package agentexec

import (
	"errors"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

func TestProductionProfilesUseFixedPathsAndTemplates(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	tests := []struct {
		id         ProfileID
		values     []string
		executable string
		arguments  []string
		timeout    time.Duration
	}{
		{
			ProfileDpkgQuery, []string{"nginx"}, "/usr/bin/dpkg-query",
			[]string{"-W", "-f=${db:Status-Abbrev}\\t${Version}\\n", "nginx"},
			defaultTimeout,
		},
		{
			ProfileRPMQuery, []string{"nginx"}, "/usr/bin/rpm",
			[]string{"-q", "--qf", "%{VERSION}-%{RELEASE}\\n", "nginx"},
			defaultTimeout,
		},
		{
			ProfileSystemctlShow, []string{"nginx.service"}, "/usr/bin/systemctl",
			[]string{
				"show", "--no-pager", "--property=LoadState", "--property=ActiveState",
				"--property=SubState", "--property=UnitFileState", "nginx.service",
			},
			defaultTimeout,
		},
	}
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	accountValues := []string{accountID, username, strconv.Itoa(int(hostingidentity.MinimumID)), strconv.Itoa(int(hostingidentity.MinimumID)), home}
	resourceSpec := hostingresources.Spec{
		Identity: hostingidentity.Spec{
			AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
			GID: hostingidentity.MinimumID, HomeDirectory: home,
		},
		CPUQuotaPercent: hostingresources.OptionalUint64{Set: true, Value: 250},
		CPUWeight:       hostingresources.OptionalUint64{Set: true, Value: 800},
		MemoryBytes:     hostingresources.OptionalUint64{Set: true, Value: 512 << 20},
		SwapBytes:       hostingresources.OptionalUint64{Set: true, Value: 0},
		ProcessLimit:    hostingresources.OptionalUint64{Set: true, Value: 64},
	}
	resourceValues, err := hostingresources.InvocationValues(resourceSpec)
	if err != nil {
		t.Fatalf("resource invocation values: %v", err)
	}
	tests = append(tests,
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileGroupAdd, accountValues, "/usr/sbin/groupadd", []string{"-g", "200000", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileUserAdd, accountValues, "/usr/sbin/useradd", []string{"-u", "200000", "-g", "200000", "-d", home, "-s", hostingidentity.NoLoginShell, "-M", "-N", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileUserMod, accountValues, "/usr/sbin/usermod", []string{"-g", "200000", "-d", home, "-s", hostingidentity.NoLoginShell, "-L", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileUserDel, accountValues, "/usr/sbin/userdel", []string{username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileGroupDel, accountValues, "/usr/sbin/groupdel", []string{username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileUserAddSubUIDs, accountValues, "/usr/sbin/usermod", []string{"--add-subuids", "1000000-1065535", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileUserAddSubGIDs, accountValues, "/usr/sbin/usermod", []string{"--add-subgids", "1000000-1065535", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileEnableUserLinger, accountValues, "/usr/bin/loginctl", []string{"enable-linger", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileUserDeleteSubUIDs, accountValues, "/usr/sbin/usermod", []string{"--del-subuids", "1000000-1065535", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileUserDeleteSubGIDs, accountValues, "/usr/sbin/usermod", []string{"--del-subgids", "1000000-1065535", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileDisableUserLinger, accountValues, "/usr/bin/loginctl", []string{"disable-linger", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileTerminateUser, accountValues, "/usr/bin/loginctl", []string{"terminate-user", username}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileStartUserManager, accountValues, "/usr/bin/systemctl", []string{"start", "user@200000.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileSetProjectQuota,
			append(append([]string(nil), accountValues...), "200000", "10737418240", "100000"),
			"/usr/sbin/setquota",
			[]string{"-P", "200000", "10485760", "10485760", "100000", "100000", "/srv/hosting"},
			accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileAddSELinuxWebContext,
			append(append([]string(nil), accountValues...), "public_html", "document", "php"),
			"/usr/sbin/semanage",
			[]string{"fcontext", "-a", "-t", "httpd_sys_rw_content_t", home + "/public_html(/.*)?"},
			accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileModifySELinuxWebContext,
			append(append([]string(nil), accountValues...), "", "account", "static"),
			"/usr/sbin/semanage",
			[]string{"fcontext", "-m", "-t", "httpd_sys_content_t", home},
			accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileRestoreSELinuxWebContext,
			append(append([]string(nil), accountValues...), "domains/site", "document", "php"),
			"/usr/sbin/restorecon",
			[]string{"-R", home + "/domains/site"},
			accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileSetWebAccessACL,
			append(append([]string(nil), accountValues...), "", "www-data", "account"),
			"/usr/bin/setfacl",
			[]string{
				"--set=user::rwx,user:www-data:--x,group::r-x,mask::r-x,other::---", home,
			},
			accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileSystemdDaemonReload, nil, "/usr/bin/systemctl",
			[]string{"daemon-reload"}, defaultTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileSystemdStartAccountSlice, resourceValues, "/usr/bin/systemctl",
			[]string{"start", "stackfort-accounts-200000.slice"}, accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileSystemdApplyAccountLimits, resourceValues, "/usr/bin/systemctl",
			[]string{
				"set-property", "--runtime", "stackfort-accounts-200000.slice",
				"CPUAccounting=yes", "CPUQuota=250%", "CPUQuotaPeriodSec=100ms", "CPUWeight=800",
				"MemoryAccounting=yes", "MemoryMax=536870912", "MemorySwapMax=0",
				"TasksAccounting=yes", "TasksMax=64",
			},
			accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileNGINXVersion, nil, "/usr/sbin/nginx", []string{"-v"}, defaultTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileNGINXTestBaseline, nil, "/usr/sbin/nginx", []string{"-t", "-q", "-c", "/etc/nginx/stackfort/nginx.conf"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{
			ProfileNGINXTestCandidate,
			[]string{"019c1234-5678-7abc-8def-0123456789ab"},
			"/usr/sbin/nginx",
			[]string{
				"-t", "-q", "-c",
				"/etc/nginx/stackfort/site-transactions/019c1234-5678-7abc-8def-0123456789ab/nginx.conf",
			},
			accountMutationTimeout,
		},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdRestartNGINX, nil, "/usr/bin/systemctl", []string{"restart", "nginx.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdReloadNGINX, nil, "/usr/bin/systemctl", []string{"reload", "nginx.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdStopNGINX, nil, "/usr/bin/systemctl", []string{"stop", "nginx.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdEnableNGINX, nil, "/usr/bin/systemctl", []string{"enable", "nginx.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdDisableNGINX, nil, "/usr/bin/systemctl", []string{"disable", "nginx.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfilePHPFPM83Test, append(append([]string(nil), accountValues...), "8.3"), "/usr/sbin/php-fpm", []string{
			"--test", "--fpm-config", "/etc/stackfort/php/account-200000-php8.3.conf",
		}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfilePHPFPM84Test, append(append([]string(nil), accountValues...), "8.4"), "/usr/sbin/php-fpm8.4", []string{
			"--test", "--fpm-config", "/etc/stackfort/php/account-200000-php8.4.conf",
		}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfilePHPFPM85Test, append(append([]string(nil), accountValues...), "8.5"), "/usr/sbin/php-fpm8.5", []string{
			"--test", "--fpm-config", "/etc/stackfort/php/account-200000-php8.5.conf",
		}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdShowPHPPool, append(append([]string(nil), accountValues...), "8.4"), "/usr/bin/systemctl", []string{
			"show", "--no-pager", "--property=LoadState", "--property=ActiveState", "--property=SubState",
			"--property=UnitFileState", "--property=ControlGroup", "--property=MemoryCurrent",
			"--property=CPUUsageNSec", "--property=TasksCurrent", "stackfort-php-8-4-200000.service",
		}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdEnablePHPPool, append(append([]string(nil), accountValues...), "8.4"), "/usr/bin/systemctl",
			[]string{"enable", "stackfort-php-8-4-200000.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdRestartPHPPool, append(append([]string(nil), accountValues...), "8.4"), "/usr/bin/systemctl",
			[]string{"restart", "stackfort-php-8-4-200000.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdDisablePHPPool, append(append([]string(nil), accountValues...), "8.4"), "/usr/bin/systemctl",
			[]string{"disable", "--now", "stackfort-php-8-4-200000.service"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdShowScheduledJob, append(append([]string(nil), accountValues...), accountID), "/usr/bin/systemctl",
			[]string{"show", "--no-pager", "--property=LoadState", "--property=ActiveState", "--property=UnitFileState", "stackfort-job-200000-019c123456787abc8def0123456789ab.timer"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdEnableScheduledJob, append(append([]string(nil), accountValues...), accountID), "/usr/bin/systemctl",
			[]string{"enable", "--now", "stackfort-job-200000-019c123456787abc8def0123456789ab.timer"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdRestartScheduledJob, append(append([]string(nil), accountValues...), accountID), "/usr/bin/systemctl",
			[]string{"restart", "stackfort-job-200000-019c123456787abc8def0123456789ab.timer"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdDisableScheduledJob, append(append([]string(nil), accountValues...), accountID), "/usr/bin/systemctl",
			[]string{"disable", "--now", "stackfort-job-200000-019c123456787abc8def0123456789ab.timer"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileSystemdCleanScheduledJob, append(append([]string(nil), accountValues...), accountID), "/usr/bin/systemctl",
			[]string{"clean", "--what=state", "stackfort-job-200000-019c123456787abc8def0123456789ab.timer"}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileAddSELinuxACMEContext, nil, "/usr/sbin/semanage", []string{
			"fcontext", "-a", "-t", "httpd_sys_content_t", "/var/lib/stackfort-agent/acme-http01(/.*)?",
		}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileModifySELinuxACMEContext, nil, "/usr/sbin/semanage", []string{
			"fcontext", "-m", "-t", "httpd_sys_content_t", "/var/lib/stackfort-agent/acme-http01(/.*)?",
		}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileRestoreSELinuxACMEContext, nil, "/usr/sbin/restorecon", []string{
			"-R", "/var/lib/stackfort-agent/acme-http01",
		}, accountMutationTimeout},
		struct {
			id         ProfileID
			values     []string
			executable string
			arguments  []string
			timeout    time.Duration
		}{ProfileVinylBan, []string{"example.test", "/news.php"}, "/usr/bin/vinyladm", []string{
			"-T", "127.0.0.1:6082", "-S", "/etc/vinyl-cache/secret", "ban",
			`req.http.host == "example.test" && req.url ~ "^/news\\.php(?:/|\\?|$)"`,
		}, accountMutationTimeout},
	)
	if len(runner.profiles) != len(tests)+6 {
		t.Fatalf("production profile count = %d", len(runner.profiles))
	}
	for _, test := range tests {
		profile := runner.profiles[test.id]
		if profile.executable != test.executable || strings.Contains(profile.executable, "sh") {
			t.Fatalf("profile %s executable = %q", test.id, profile.executable)
		}
		if profile.timeout != test.timeout || profile.stdoutLimit != defaultOutputLimit ||
			profile.stderrLimit != defaultOutputLimit || profile.waitDelay != defaultWaitDelay {
			t.Fatalf("profile %s bounds = %#v", test.id, profile)
		}
		arguments, err := profile.resolve(test.values)
		if err != nil || !reflect.DeepEqual(arguments, test.arguments) {
			t.Fatalf("profile %s arguments = %#v, error = %v", test.id, arguments, err)
		}
	}
}

func TestOCIProfilesDeriveAccountExecutionAndFixedLimits(t *testing.T) {
	t.Parallel()
	runner := NewRunner()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	spec := ociimage.PrepareSpec{
		Identity:      hostingidentity.Spec{AccountID: accountID, Username: username, UID: 200000, GID: 200000, HomeDirectory: home},
		ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 3,
		Source: ociapps.Source{Kind: ociapps.SourceContainerfile, BuildContext: "apps/web", ContainerfilePath: "deploy/Web.Containerfile"},
	}
	values, err := ociimage.InvocationValues(spec)
	if err != nil {
		t.Fatal(err)
	}
	operationID := "019d2eaa-62d0-7f52-8ac7-0aeb932455db"
	profile := runner.profiles[ProfilePodmanBuild]
	arguments, err := profile.resolve(append(values, operationID))
	joined := strings.Join(arguments, " ")
	if err != nil || profile.executable != "/usr/bin/podman" || !profile.accountProcess ||
		profile.timeout != time.Duration(ociimage.BuildTimeoutSeconds)*time.Second ||
		!strings.Contains(joined, "--network=none") || !strings.Contains(joined, "--no-cache") ||
		!strings.Contains(joined, "--memory=1073741824") || !strings.Contains(joined, "--cpu-quota=100000") ||
		strings.Contains(joined, spec.Source.BuildContext) || strings.Contains(joined, spec.Source.ContainerfilePath) {
		t.Fatalf("bounded build profile=%#v arguments=%#v err=%v", profile, arguments, err)
	}
	for _, id := range []ProfileID{ProfilePodmanPull, ProfilePodmanBuild, ProfilePodmanInspect, ProfilePodmanSave, ProfilePodmanRemove} {
		if !runner.profiles[id].accountProcess {
			t.Fatalf("profile %s does not drop to the hosting account", id)
		}
	}
	save := runner.profiles[ProfilePodmanSave]
	saveArguments, saveErr := save.resolve(append(values, operationID))
	if saveErr != nil || save.executable != "/usr/bin/prlimit" || len(saveArguments) < 4 ||
		saveArguments[0] != "--fsize=2147483648" || saveArguments[2] != "/usr/bin/podman" {
		t.Fatalf("bounded save profile=%#v arguments=%#v err=%v", save, saveArguments, saveErr)
	}
	remove := runner.profiles[ProfilePodmanRemove]
	removeArguments, removeErr := remove.resolve(values)
	localTag, _ := ociimage.LocalTag(spec)
	if removeErr != nil || !reflect.DeepEqual(removeArguments, []string{
		"image", "rm", "--ignore", "--no-prune", localTag,
	}) {
		t.Fatalf("non-destructive remove arguments=%#v err=%v", removeArguments, removeErr)
	}
	scan := runner.profiles[ProfileTrivyScan]
	scanArguments, scanErr := scan.resolve([]string{operationID})
	if scanErr != nil || scan.executable != ociimage.ScannerExecutable || scan.accountProcess ||
		scan.stdoutLimit != ociimage.MaximumScanReportBytes || slices.Contains(scanArguments, "--output") {
		t.Fatalf("scanner profile = %#v", scan)
	}
}

func TestWebAccessACLProfileHasClosedAccountDocumentAndDefaultScopes(t *testing.T) {
	t.Parallel()
	runner := NewRunner()
	profile := runner.profiles[ProfileSetWebAccessACL]
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := []string{accountID, username, "200000", "200000", home}
	tests := []struct {
		values    []string
		arguments []string
	}{
		{
			append(append([]string(nil), identity...), "domains", "www-data", "ancestor"),
			[]string{"--set=user::rwx,user:www-data:--x,group::r-x,mask::r-x,other::---", home + "/domains"},
		},
		{
			append(append([]string(nil), identity...), "public_html", "www-data", "document"),
			[]string{"--set=user::rwx,user:www-data:r-x,group::r-x,mask::r-x,other::---", home + "/public_html"},
		},
		{
			append(append([]string(nil), identity...), "domains/site", "nginx", "default"),
			[]string{"--default", "--set=user::rwx,user:nginx:r-x,group::r-x,mask::r-x,other::---", home + "/domains/site"},
		},
	}
	for _, test := range tests {
		arguments, err := profile.resolve(test.values)
		if err != nil || !reflect.DeepEqual(arguments, test.arguments) {
			t.Fatalf("ACL arguments = %#v, error = %v", arguments, err)
		}
	}
}

func TestRunnerRejectsUnknownProfilesAndValues(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	accountValues := []string{accountID, username, "200000", "200000", home}
	for _, invocation := range []Invocation{
		{Profile: "unknown", Values: []string{"nginx"}},
		{Profile: ProfileDpkgQuery},
		{Profile: ProfileDpkgQuery, Values: []string{"nginx", "podman"}},
		{Profile: ProfileDpkgQuery, Values: []string{"../../bin/sh"}},
		{Profile: ProfileSystemctlShow, Values: []string{"nginx.service;id"}},
		{Profile: ProfileUserDel, Values: []string{"account", "root", "0", "0", "/root"}},
		{Profile: ProfileSetProjectQuota, Values: []string{
			accountValues[0], accountValues[1], accountValues[2], accountValues[3], accountValues[4],
			"200001", "1024", "1",
		}},
		{Profile: ProfileSetWebAccessACL, Values: append(append([]string(nil), accountValues...), "../private", "www-data", "document")},
		{Profile: ProfileSetWebAccessACL, Values: append(append([]string(nil), accountValues...), "public_html", "root", "document")},
		{Profile: ProfileSetWebAccessACL, Values: append(append([]string(nil), accountValues...), "public_html", "www-data", "recursive")},
		{Profile: ProfileAddSELinuxWebContext, Values: append(append([]string(nil), accountValues...), "../private", "document", "static")},
		{Profile: ProfileModifySELinuxWebContext, Values: append(append([]string(nil), accountValues...), "public_html", "recursive", "static")},
		{Profile: ProfileRestoreSELinuxWebContext, Values: append(append([]string(nil), accountValues...), "/etc", "document", "static")},
		{Profile: ProfileAddSELinuxWebContext, Values: append(append([]string(nil), accountValues...), "public_html", "document", "write-all")},
		{Profile: ProfileSystemdDaemonReload, Values: []string{"unexpected"}},
		{Profile: ProfileNGINXVersion, Values: []string{"-T"}},
		{Profile: ProfileNGINXTestBaseline, Values: []string{"/tmp/attacker.conf"}},
		{Profile: ProfileSystemdRestartNGINX, Values: []string{"other.service"}},
		{Profile: ProfileSystemdEnableNGINX, Values: []string{"--now"}},
		{Profile: ProfileSystemdStartAccountSlice, Values: append(append([]string(nil), accountValues...), "-", "-", "-", "-", "0")},
		{Profile: ProfileSystemdApplyAccountLimits, Values: append(append([]string(nil), accountValues...), "1%;id", "-", "-", "-", "-")},
		{Profile: ProfilePHPFPM84Test, Values: append(append([]string(nil), accountValues...), "8.4;id")},
		{Profile: ProfilePHPFPM84Test, Values: append(append([]string(nil), accountValues...), "8.5")},
		{Profile: ProfileSystemdRestartPHPPool, Values: append(append([]string(nil), accountValues...), "latest")},
		{Profile: ProfileSystemdEnableScheduledJob, Values: append(append([]string(nil), accountValues...), "../../evil.timer")},
	} {
		_, err := runner.Run(t.Context(), invocation)
		if err == nil || !(errors.Is(err, ErrNotAllowlisted) || errors.Is(err, ErrInvalidInvocation)) {
			t.Fatalf("invocation %#v error = %v", invocation, err)
		}
	}
	if _, err := runner.Run(nil, Invocation{
		Profile: ProfileDpkgQuery, Values: []string{"nginx"},
	}); !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestBoundedCaptureSignalsAndCannotBypassWrite(t *testing.T) {
	t.Parallel()

	notified := 0
	capture := newBoundedCapture(8, func() { notified++ })
	written, err := capture.Write([]byte("0123456789"))
	if err != nil || written != 10 || capture.String() != "01234567" ||
		!capture.Exceeded() || notified != 1 {
		t.Fatalf("bounded write = written %d value %q exceeded %t notified %d error %v",
			written, capture.String(), capture.Exceeded(), notified, err)
	}
	if _, bypassesWrite := any(capture).(io.ReaderFrom); bypassesWrite {
		t.Fatal("bounded capture unexpectedly exposes an unbounded ReaderFrom path")
	}
}

func TestSanitizedEnvironmentAndRedaction(t *testing.T) {
	t.Parallel()

	wantEnvironment := []string{
		"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "TZ=UTC",
	}
	if environment := sanitizedEnvironment(); !reflect.DeepEqual(environment, wantEnvironment) {
		t.Fatalf("environment = %#v", environment)
	}
	secret := "do-not-log-this-value"
	redacted := redactText("before "+secret+" between "+secret+" after", []string{secret})
	if strings.Contains(redacted, secret) || redacted != "before [REDACTED] between [REDACTED] after" {
		t.Fatalf("redacted output = %q", redacted)
	}
	err := newRunError(ErrStart, errors.New(secret))
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("safe execution error leaked its cause: %v", err)
	}
}
