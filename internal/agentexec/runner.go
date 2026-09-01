// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentexec runs the small, fixed set of external programs required by
// the privileged host agent. It deliberately does not expose an arbitrary
// executable, environment, working directory, or command-string interface.
package agentexec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingoci"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/google/uuid"
)

const (
	defaultOutputLimit     = 32 << 10
	defaultTimeout         = time.Second
	defaultWaitDelay       = 250 * time.Millisecond
	accountMutationTimeout = 10 * time.Second
)

// ProfileID selects one installation-owned executable and argument template.
type ProfileID string

const (
	ProfileDpkgQuery                  ProfileID = "package.dpkg-query"
	ProfileRPMQuery                   ProfileID = "package.rpm-query"
	ProfileSystemctlShow              ProfileID = "service.systemctl-show"
	ProfileGroupAdd                   ProfileID = "account.group-add"
	ProfileUserAdd                    ProfileID = "account.user-add"
	ProfileUserMod                    ProfileID = "account.user-mod"
	ProfileUserDel                    ProfileID = "account.user-del"
	ProfileGroupDel                   ProfileID = "account.group-del"
	ProfileUserAddSubUIDs             ProfileID = "account.subuid-add"
	ProfileUserAddSubGIDs             ProfileID = "account.subgid-add"
	ProfileUserDeleteSubUIDs          ProfileID = "account.subuid-delete"
	ProfileUserDeleteSubGIDs          ProfileID = "account.subgid-delete"
	ProfileEnableUserLinger           ProfileID = "account.linger-enable"
	ProfileDisableUserLinger          ProfileID = "account.linger-disable"
	ProfileStartUserManager           ProfileID = "account.user-manager-start"
	ProfileTerminateUser              ProfileID = "account.user-manager-terminate"
	ProfileSetProjectQuota            ProfileID = "filesystem.set-project-quota"
	ProfileSetWebAccessACL            ProfileID = "filesystem.set-web-access-acl"
	ProfileAddSELinuxWebContext       ProfileID = "filesystem.selinux-web-context-add"
	ProfileModifySELinuxWebContext    ProfileID = "filesystem.selinux-web-context-modify"
	ProfileRestoreSELinuxWebContext   ProfileID = "filesystem.selinux-web-context-restore"
	ProfileAddSELinuxACMEContext      ProfileID = "filesystem.selinux-acme-context-add"
	ProfileModifySELinuxACMEContext   ProfileID = "filesystem.selinux-acme-context-modify"
	ProfileRestoreSELinuxACMEContext  ProfileID = "filesystem.selinux-acme-context-restore"
	ProfileSystemdDaemonReload        ProfileID = "resource.systemd-daemon-reload"
	ProfileSystemdStartAccountSlice   ProfileID = "resource.systemd-start-account-slice"
	ProfileSystemdApplyAccountLimits  ProfileID = "resource.systemd-apply-account-limits"
	ProfileNGINXVersion               ProfileID = "nginx.version"
	ProfileNGINXTestBaseline          ProfileID = "nginx.baseline-test"
	ProfileNGINXTestCandidate         ProfileID = "nginx.candidate-test"
	ProfileSystemdRestartNGINX        ProfileID = "nginx.systemd-restart"
	ProfileSystemdReloadNGINX         ProfileID = "nginx.systemd-reload"
	ProfileSystemdStopNGINX           ProfileID = "nginx.systemd-stop"
	ProfileSystemdEnableNGINX         ProfileID = "nginx.systemd-enable"
	ProfileSystemdDisableNGINX        ProfileID = "nginx.systemd-disable"
	ProfilePHPFPM83Test               ProfileID = "php.fpm-8.3-test"
	ProfilePHPFPM84Test               ProfileID = "php.fpm-8.4-test"
	ProfilePHPFPM85Test               ProfileID = "php.fpm-8.5-test"
	ProfileSystemdShowPHPPool         ProfileID = "php.systemd-show"
	ProfileSystemdEnablePHPPool       ProfileID = "php.systemd-enable"
	ProfileSystemdRestartPHPPool      ProfileID = "php.systemd-restart"
	ProfileSystemdDisablePHPPool      ProfileID = "php.systemd-disable"
	ProfileSystemdShowScheduledJob    ProfileID = "jobs.systemd-show"
	ProfileSystemdEnableScheduledJob  ProfileID = "jobs.systemd-enable"
	ProfileSystemdRestartScheduledJob ProfileID = "jobs.systemd-restart"
	ProfileSystemdDisableScheduledJob ProfileID = "jobs.systemd-disable"
	ProfileSystemdCleanScheduledJob   ProfileID = "jobs.systemd-clean"
	ProfileVinylBan                   ProfileID = "cache.vinyl-ban"
	ProfilePodmanPull                 ProfileID = "oci.podman-pull"
	ProfilePodmanBuild                ProfileID = "oci.podman-build"
	ProfilePodmanInspect              ProfileID = "oci.podman-inspect"
	ProfilePodmanSave                 ProfileID = "oci.podman-save"
	ProfilePodmanRemove               ProfileID = "oci.podman-remove"
	ProfilePodmanNetworkExists        ProfileID = "oci.podman-network-exists"
	ProfilePodmanNetworkCreate        ProfileID = "oci.podman-network-create"
	ProfilePodmanNetworkInspect       ProfileID = "oci.podman-network-inspect"
	ProfilePodmanSecretCreate         ProfileID = "oci.podman-secret-create" // #nosec G101 -- allowlisted profile identifier, not credential material.
	ProfilePodmanSecretRemove         ProfileID = "oci.podman-secret-remove" // #nosec G101 -- allowlisted profile identifier, not credential material.
	ProfileSystemdUserDaemonReload    ProfileID = "oci.systemd-user-daemon-reload"
	ProfileSystemdUserRestart         ProfileID = "oci.systemd-user-restart"
	ProfileSystemdUserStart           ProfileID = "oci.systemd-user-start"
	ProfileSystemdUserStop            ProfileID = "oci.systemd-user-stop"
	ProfileSystemdUserIsActive        ProfileID = "oci.systemd-user-is-active"
	ProfileJournalUserUnit            ProfileID = "oci.journal-user-unit"
	ProfileTrivyScan                  ProfileID = "oci.trivy-scan"
)

// Invocation contains only the semantic values accepted by a fixed profile.
// It cannot select an executable, raw option list, environment, or directory.
type Invocation struct {
	Profile ProfileID
	Values  []string
	Input   []byte
}

// Result is returned for successful starts, including ordinary non-zero exit
// statuses. Captured output is bounded and redacted before it is returned.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

var (
	ErrNotAllowlisted      = errors.New("external process invocation is not allowlisted")
	ErrInvalidInvocation   = errors.New("external process invocation is invalid")
	ErrUnsupportedPlatform = errors.New("external process isolation is unsupported on this platform")
	ErrTimedOut            = errors.New("external process timed out")
	ErrCancelled           = errors.New("external process was cancelled")
	ErrOutputLimit         = errors.New("external process output exceeds limit")
	ErrStart               = errors.New("external process could not be started")
	ErrWait                = errors.New("external process could not be reaped cleanly")
)

type runError struct {
	kind  error
	cause error
}

func (failure *runError) Error() string { return failure.kind.Error() }

func (failure *runError) Unwrap() error { return failure.cause }

func (failure *runError) Is(target error) bool { return target == failure.kind }

func newRunError(kind, cause error) error {
	return &runError{kind: kind, cause: cause}
}

type argumentResolver func([]string) ([]string, error)

type executionProfile struct {
	executable      string
	timeout         time.Duration
	stdoutLimit     int
	stderrLimit     int
	waitDelay       time.Duration
	resolve         argumentResolver
	sensitiveInputs map[int]struct{}
	accountProcess  bool
	acceptsInput    bool
	inputLimit      int
}

// Runner owns immutable execution profiles. NewRunner is the only production
// constructor so callers cannot register paths or argument templates.
type Runner struct {
	profiles map[ProfileID]executionProfile
}

// NewRunner returns the complete production profile registry.
func NewRunner() *Runner {
	packages := stringSet(
		"nginx", "php-fpm", "mariadb-server", "vinyl-cache", "podman", "netavark",
		"aardvark-dns", "passt", "slirp4netns", "fuse-overlayfs", "uidmap", "shadow-utils-subid",
		"stackfort-waf",
	)
	units := stringSet(
		"nginx.service", "php-fpm.service", "php8.4-fpm.service", "php8.5-fpm.service",
		"mariadb.service", "vinyl.service", "podman.socket", "nftables.service",
		"firewalld.service", "stackfort-api.service", "stackfort-agent.service",
	)
	return &Runner{profiles: map[ProfileID]executionProfile{
		ProfileDpkgQuery: newProfile("/usr/bin/dpkg-query", exactValueResolver(
			[]string{"-W", "-f=${db:Status-Abbrev}\\t${Version}\\n"}, packages,
		)),
		ProfileRPMQuery: newProfile("/usr/bin/rpm", exactValueResolver(
			[]string{"-q", "--qf", "%{VERSION}-%{RELEASE}\\n"}, packages,
		)),
		ProfileSystemctlShow: newProfile("/usr/bin/systemctl", exactValueResolver(
			[]string{
				"show", "--no-pager", "--property=LoadState", "--property=ActiveState",
				"--property=SubState", "--property=UnitFileState",
			},
			units,
		)),
		ProfileGroupAdd: accountMutationProfile("/usr/sbin/groupadd", func(spec hostingidentity.Spec) []string {
			return []string{"-g", strconv.FormatUint(uint64(spec.GID), 10), spec.Username}
		}),
		ProfileUserAdd: accountMutationProfile("/usr/sbin/useradd", func(spec hostingidentity.Spec) []string {
			return []string{
				"-u", strconv.FormatUint(uint64(spec.UID), 10),
				"-g", strconv.FormatUint(uint64(spec.GID), 10),
				"-d", spec.HomeDirectory, "-s", hostingidentity.NoLoginShell,
				"-M", "-N", spec.Username,
			}
		}),
		ProfileUserMod: accountMutationProfile("/usr/sbin/usermod", func(spec hostingidentity.Spec) []string {
			return []string{
				"-g", strconv.FormatUint(uint64(spec.GID), 10),
				"-d", spec.HomeDirectory, "-s", hostingidentity.NoLoginShell,
				"-L", spec.Username,
			}
		}),
		ProfileUserDel: accountMutationProfile("/usr/sbin/userdel", func(spec hostingidentity.Spec) []string {
			return []string{spec.Username}
		}),
		ProfileGroupDel: accountMutationProfile("/usr/sbin/groupdel", func(spec hostingidentity.Spec) []string {
			return []string{spec.Username}
		}),
		ProfileUserAddSubUIDs:    subordinateIDProfile("--add-subuids"),
		ProfileUserAddSubGIDs:    subordinateIDProfile("--add-subgids"),
		ProfileUserDeleteSubUIDs: subordinateIDProfile("--del-subuids"),
		ProfileUserDeleteSubGIDs: subordinateIDProfile("--del-subgids"),
		ProfileEnableUserLinger:  lingerProfile("enable-linger"),
		ProfileDisableUserLinger: lingerProfile("disable-linger"),
		ProfileStartUserManager: accountMutationProfile("/usr/bin/systemctl", func(spec hostingidentity.Spec) []string {
			return []string{"start", "user@" + strconv.FormatUint(uint64(spec.UID), 10) + ".service"}
		}),
		ProfileTerminateUser:            lingerProfile("terminate-user"),
		ProfileSetProjectQuota:          projectQuotaProfile(),
		ProfileSetWebAccessACL:          webAccessACLProfile(),
		ProfileAddSELinuxWebContext:     selinuxWebContextProfile("-a"),
		ProfileModifySELinuxWebContext:  selinuxWebContextProfile("-m"),
		ProfileRestoreSELinuxWebContext: restoreSELinuxWebContextProfile(),
		ProfileAddSELinuxACMEContext: mutationProfile("/usr/sbin/semanage", noValueResolver(
			[]string{"fcontext", "-a", "-t", "httpd_sys_content_t", "/var/lib/stackfort-agent/acme-http01(/.*)?"},
		)),
		ProfileModifySELinuxACMEContext: mutationProfile("/usr/sbin/semanage", noValueResolver(
			[]string{"fcontext", "-m", "-t", "httpd_sys_content_t", "/var/lib/stackfort-agent/acme-http01(/.*)?"},
		)),
		ProfileRestoreSELinuxACMEContext: mutationProfile("/usr/sbin/restorecon", noValueResolver(
			[]string{"-R", "/var/lib/stackfort-agent/acme-http01"},
		)),
		ProfileSystemdDaemonReload: newProfile("/usr/bin/systemctl", noValueResolver(
			[]string{"daemon-reload"},
		)),
		ProfileSystemdStartAccountSlice: accountResourceProfile(func(spec hostingresources.Spec) []string {
			unit, _ := hostingresources.AccountSliceName(spec.Identity.UID)
			return []string{"start", unit}
		}),
		ProfileSystemdApplyAccountLimits: accountResourceProfile(func(spec hostingresources.Spec) []string {
			unit, _ := hostingresources.AccountSliceName(spec.Identity.UID)
			arguments := []string{"set-property", "--runtime", unit}
			properties, _ := hostingresources.SystemdProperties(spec)
			return append(arguments, properties...)
		}),
		ProfileNGINXVersion: newProfile("/usr/sbin/nginx", noValueResolver(
			[]string{"-v"},
		)),
		ProfileNGINXTestBaseline: mutationProfile("/usr/sbin/nginx", noValueResolver(
			[]string{"-t", "-q", "-c", "/etc/nginx/stackfort/nginx.conf"},
		)),
		ProfileNGINXTestCandidate: mutationProfile("/usr/sbin/nginx", nginxCandidateResolver()),
		ProfileSystemdRestartNGINX: mutationProfile("/usr/bin/systemctl", noValueResolver(
			[]string{"restart", "nginx.service"},
		)),
		ProfileSystemdReloadNGINX: mutationProfile("/usr/bin/systemctl", noValueResolver(
			[]string{"reload", "nginx.service"},
		)),
		ProfileSystemdStopNGINX: mutationProfile("/usr/bin/systemctl", noValueResolver(
			[]string{"stop", "nginx.service"},
		)),
		ProfileSystemdEnableNGINX: mutationProfile("/usr/bin/systemctl", noValueResolver(
			[]string{"enable", "nginx.service"},
		)),
		ProfileSystemdDisableNGINX: mutationProfile("/usr/bin/systemctl", noValueResolver(
			[]string{"disable", "nginx.service"},
		)),
		ProfilePHPFPM83Test:               phpConfigurationTestProfile("/usr/sbin/php-fpm", "8.3"),
		ProfilePHPFPM84Test:               phpConfigurationTestProfile("/usr/sbin/php-fpm8.4", "8.4"),
		ProfilePHPFPM85Test:               phpConfigurationTestProfile("/usr/sbin/php-fpm8.5", "8.5"),
		ProfileSystemdShowPHPPool:         phpSystemdProfile("show"),
		ProfileSystemdEnablePHPPool:       phpSystemdProfile("enable"),
		ProfileSystemdRestartPHPPool:      phpSystemdProfile("restart"),
		ProfileSystemdDisablePHPPool:      phpSystemdProfile("disable"),
		ProfileSystemdShowScheduledJob:    scheduledJobSystemdProfile("show"),
		ProfileSystemdEnableScheduledJob:  scheduledJobSystemdProfile("enable"),
		ProfileSystemdRestartScheduledJob: scheduledJobSystemdProfile("restart"),
		ProfileSystemdDisableScheduledJob: scheduledJobSystemdProfile("disable"),
		ProfileSystemdCleanScheduledJob:   scheduledJobSystemdProfile("clean"),
		ProfileVinylBan: mutationProfile("/usr/bin/vinyladm", func(values []string) ([]string, error) {
			if len(values) != 2 {
				return nil, ErrInvalidInvocation
			}
			domain, err := core.NormalizeDomainName(values[0])
			pathPrefix, pathErr := cacheconfig.NormalizePurgePath(values[1])
			if err != nil || domain.ASCII != values[0] || domain.Display != values[0] ||
				pathErr != nil || pathPrefix != values[1] {
				return nil, ErrInvalidInvocation
			}
			pathPattern := "^" + regexp.QuoteMeta(pathPrefix)
			if pathPrefix != "/" {
				pathPattern += "(?:/|\\?|$)"
			}
			// vinyladm parses the expression as VSL query syntax before its regex
			// engine sees the value. Preserve regex escapes through that first
			// quoted-string layer (for example, `\\.` must arrive as `\\\\.`).
			pathPattern = strings.ReplaceAll(pathPattern, "\\", "\\\\")
			expression := `req.http.host == "` + domain.ASCII + `" && req.url ~ "` + pathPattern + `"`
			return []string{"-T", cacheconfig.ManagementAddress, "-S", cacheconfig.SecretPath, "ban", expression}, nil
		}),
		ProfilePodmanPull: accountOCIProfile(func(spec ociimage.PrepareSpec, _ string) ([]string, error) {
			if spec.Source.Kind != ociapps.SourceImageDigest {
				return nil, ErrInvalidInvocation
			}
			return []string{"pull", "--quiet", "--policy=always", "--tls-verify=true", spec.Source.ImageReference}, nil
		}, 5*time.Minute, 1<<20),
		ProfilePodmanBuild: accountOCIProfile(func(spec ociimage.PrepareSpec, operationID string) ([]string, error) {
			if spec.Source.Kind != ociapps.SourceContainerfile {
				return nil, ErrInvalidInvocation
			}
			transaction, err := ociimage.TransactionDirectory(operationID)
			if err != nil {
				return nil, ErrInvalidInvocation
			}
			tag, _ := ociimage.LocalTag(spec)
			return []string{
				"build", "--pull=always", "--no-cache", "--layers=false", "--network=none", "--format=oci",
				"--memory=" + strconv.FormatInt(ociimage.BuildMemoryBytes, 10),
				"--memory-swap=" + strconv.FormatInt(ociimage.BuildMemoryBytes, 10),
				"--cpu-period=" + strconv.FormatInt(ociimage.BuildCPUPeriod, 10),
				"--cpu-quota=" + strconv.FormatInt(ociimage.BuildCPUQuota, 10),
				"--ulimit", "nofile=" + strconv.Itoa(ociimage.BuildOpenFileLimit) + ":" + strconv.Itoa(ociimage.BuildOpenFileLimit),
				"--ulimit", "nproc=" + strconv.Itoa(ociimage.BuildProcessLimit) + ":" + strconv.Itoa(ociimage.BuildProcessLimit),
				"--tag", tag, "--file", path.Join(transaction, "Containerfile"), path.Join(transaction, "context"),
			}, nil
		}, time.Duration(ociimage.BuildTimeoutSeconds)*time.Second, 1<<20),
		ProfilePodmanInspect: accountOCIProfile(func(spec ociimage.PrepareSpec, _ string) ([]string, error) {
			target, err := ociImageTarget(spec)
			if err != nil {
				return nil, err
			}
			return []string{"image", "inspect", "--format", "{{.Id}}", target}, nil
		}, time.Minute, defaultOutputLimit),
		ProfilePodmanSave: accountOCIProfileWithExecutable("/usr/bin/prlimit", func(spec ociimage.PrepareSpec, operationID string) ([]string, error) {
			target, err := ociImageTarget(spec)
			if err != nil {
				return nil, err
			}
			transaction, err := ociimage.TransactionDirectory(operationID)
			if err != nil {
				return nil, ErrInvalidInvocation
			}
			return []string{
				"--fsize=" + strconv.FormatInt(ociimage.MaximumImageArchiveBytes, 10), "--", "/usr/bin/podman",
				"image", "save", "--format", "oci-archive", "--output", path.Join(transaction, "image.tar"), target,
			}, nil
		}, 5*time.Minute, 1<<20),
		ProfilePodmanRemove: accountOCIProfile(func(spec ociimage.PrepareSpec, _ string) ([]string, error) {
			target, err := ociImageTarget(spec)
			if err != nil {
				return nil, err
			}
			return []string{"image", "rm", "--ignore", "--no-prune", target}, nil
		}, time.Minute, defaultOutputLimit),
		ProfilePodmanNetworkExists: accountOCIResourceProfile(func(identity hostingidentity.Spec) []string {
			return []string{"network", "exists", ociresources.NetworkName}
		}, time.Minute, defaultOutputLimit),
		ProfilePodmanNetworkCreate: accountOCIResourceProfile(func(identity hostingidentity.Spec) []string {
			return []string{
				"network", "create", "--driver=bridge", "--opt=isolate=strict",
				"--label=" + ociresources.NetworkLabelManaged + "=true",
				"--label=" + ociresources.NetworkLabelAccount + "=" + identity.AccountID,
				ociresources.NetworkName,
			}
		}, time.Minute, defaultOutputLimit),
		ProfilePodmanNetworkInspect: accountOCIResourceProfile(func(identity hostingidentity.Spec) []string {
			return []string{"network", "inspect", "--format=json", ociresources.NetworkName}
		}, time.Minute, 1<<20),
		ProfilePodmanSecretCreate: func() executionProfile {
			profile := accountOCIDeploymentProfile("/usr/bin/podman", func(_ hostingidentity.Spec, applicationID string, extra []string) ([]string, error) {
				if len(extra) != 3 {
					return nil, ErrInvalidInvocation
				}
				generation, err := strconv.ParseInt(extra[2], 10, 64)
				reference := ocideployment.EnvironmentReference{ValueID: extra[0], Environment: extra[1], Generation: generation}
				name := ocideployment.SecretName(reference)
				normalized, normalizeErr := ociapps.NormalizeSecretReferences([]ociapps.EnvironmentSecretReference{{
					SecretID: reference.ValueID, Environment: reference.Environment}})
				if err != nil || name == "" || normalizeErr != nil || len(normalized) != 1 ||
					normalized[0].Environment != reference.Environment {
					return nil, ErrInvalidInvocation
				}
				return []string{"secret", "create", "--replace",
					"--label=io.stackfort.managed=true", "--label=io.stackfort.application=" + applicationID,
					name, "-"}, nil
			}, time.Minute, defaultOutputLimit)
			profile.acceptsInput, profile.inputLimit = true, ocideployment.MaximumValueBytes
			return profile
		}(),
		ProfilePodmanSecretRemove: accountOCIDeploymentProfile("/usr/bin/podman", func(_ hostingidentity.Spec, _ string, extra []string) ([]string, error) {
			if len(extra) != 3 {
				return nil, ErrInvalidInvocation
			}
			generation, err := strconv.ParseInt(extra[2], 10, 64)
			reference := ocideployment.EnvironmentReference{ValueID: extra[0], Environment: extra[1], Generation: generation}
			name := ocideployment.SecretName(reference)
			if err != nil || name == "" {
				return nil, ErrInvalidInvocation
			}
			return []string{"secret", "rm", "--ignore", name}, nil
		}, time.Minute, defaultOutputLimit),
		ProfileSystemdUserDaemonReload: accountOCIDeploymentProfile("/usr/bin/systemctl", func(_ hostingidentity.Spec, _ string, extra []string) ([]string, error) {
			if len(extra) != 0 {
				return nil, ErrInvalidInvocation
			}
			return []string{"--user", "daemon-reload"}, nil
		}, time.Minute, defaultOutputLimit),
		ProfileSystemdUserRestart: accountOCISystemdProfile("restart"),
		ProfileSystemdUserStart:   accountOCISystemdProfile("start"),
		ProfileSystemdUserStop:    accountOCISystemdProfile("stop"),
		ProfileSystemdUserIsActive: accountOCIDeploymentProfile("/usr/bin/systemctl", func(_ hostingidentity.Spec, applicationID string, extra []string) ([]string, error) {
			if len(extra) != 0 {
				return nil, ErrInvalidInvocation
			}
			return []string{"--user", "is-active", "--quiet", ocideployment.UnitNameFromApplication(applicationID)}, nil
		}, time.Minute, defaultOutputLimit),
		ProfileJournalUserUnit: accountOCIDeploymentProfile("/usr/bin/journalctl", func(_ hostingidentity.Spec, applicationID string, extra []string) ([]string, error) {
			if len(extra) != 1 {
				return nil, ErrInvalidInvocation
			}
			tail, err := strconv.Atoi(extra[0])
			if err != nil || tail < 1 || tail > ocideployment.MaximumLogEntries {
				return nil, ErrInvalidInvocation
			}
			return []string{"--user", "--unit=" + ocideployment.UnitNameFromApplication(applicationID),
				"--lines=" + strconv.Itoa(tail), "--output=json", "--no-pager"}, nil
		}, time.Minute, ocideployment.MaximumLogBytes),
		ProfileTrivyScan: func() executionProfile {
			profile := mutationProfile(ociimage.ScannerExecutable, func(values []string) ([]string, error) {
				if len(values) != 1 {
					return nil, ErrInvalidInvocation
				}
				transaction, err := ociimage.TransactionDirectory(values[0])
				if err != nil {
					return nil, ErrInvalidInvocation
				}
				return []string{
					"--cache-dir", ociimage.ScannerCacheRoot, "image", "--input", path.Join(transaction, "image.tar"),
					"--scanners", "vuln", "--severity", "HIGH,CRITICAL", "--format", "json",
					"--timeout", "10m0s",
				}, nil
			})
			profile.timeout = 11 * time.Minute
			profile.stdoutLimit, profile.stderrLimit = ociimage.MaximumScanReportBytes, 1<<20
			return profile
		}(),
	}}
}

func accountOCIProfile(
	resolve func(ociimage.PrepareSpec, string) ([]string, error), timeout time.Duration, outputLimit int,
) executionProfile {
	return accountOCIProfileWithExecutable("/usr/bin/podman", resolve, timeout, outputLimit)
}

func accountOCIResourceProfile(
	resolve func(hostingidentity.Spec) []string, timeout time.Duration, outputLimit int,
) executionProfile {
	profile := newProfile("/usr/bin/podman", func(values []string) ([]string, error) {
		identity, err := ociresources.IdentityFromInvocationValues(values)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		return resolve(identity), nil
	})
	profile.timeout = timeout
	profile.stdoutLimit, profile.stderrLimit = outputLimit, outputLimit
	profile.accountProcess = true
	return profile
}

func accountOCIProfileWithExecutable(
	executable string,
	resolve func(ociimage.PrepareSpec, string) ([]string, error),
	timeout time.Duration,
	outputLimit int,
) executionProfile {
	profile := newProfile(executable, func(values []string) ([]string, error) {
		operationID := ""
		if len(values) == 12 {
			operationID = values[11]
			values = values[:11]
		}
		spec, err := ociimage.FromInvocationValues(values)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		return resolve(spec, operationID)
	})
	profile.timeout = timeout
	profile.stdoutLimit, profile.stderrLimit = outputLimit, outputLimit
	profile.accountProcess = true
	return profile
}

func accountOCIDeploymentProfile(executable string,
	resolve func(hostingidentity.Spec, string, []string) ([]string, error), timeout time.Duration,
	outputLimit int) executionProfile {
	profile := newProfile(executable, func(values []string) ([]string, error) {
		if len(values) < 6 {
			return nil, ErrInvalidInvocation
		}
		identity, applicationID, err := ocideployment.IdentityAndApplication(values[:6])
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		return resolve(identity, applicationID, values[6:])
	})
	profile.timeout, profile.stdoutLimit, profile.stderrLimit = timeout, outputLimit, outputLimit
	profile.accountProcess = true
	return profile
}

func accountOCISystemdProfile(action string) executionProfile {
	return accountOCIDeploymentProfile("/usr/bin/systemctl", func(_ hostingidentity.Spec, applicationID string, extra []string) ([]string, error) {
		if len(extra) != 0 || action != "start" && action != "restart" && action != "stop" {
			return nil, ErrInvalidInvocation
		}
		return []string{"--user", action, ocideployment.UnitNameFromApplication(applicationID)}, nil
	}, 2*time.Minute, defaultOutputLimit)
}

func ociImageTarget(spec ociimage.PrepareSpec) (string, error) {
	if spec.Source.Kind == ociapps.SourceImageDigest {
		return spec.Source.ImageReference, nil
	}
	if spec.Source.Kind == ociapps.SourceContainerfile {
		return ociimage.LocalTag(spec)
	}
	return "", ErrInvalidInvocation
}

func subordinateIDProfile(action string) executionProfile {
	profile := newProfile("/usr/sbin/usermod", func(values []string) ([]string, error) {
		identity, err := hostingIdentitySpec(values)
		if err != nil {
			return nil, err
		}
		runtimeSpec, err := hostingoci.ForIdentity(identity)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		start, end := runtimeSpec.SubUIDStart, runtimeSpec.SubUIDEnd()
		if action == "--add-subgids" || action == "--del-subgids" {
			start, end = runtimeSpec.SubGIDStart, runtimeSpec.SubGIDEnd()
		}
		switch action {
		case "--add-subuids", "--add-subgids", "--del-subuids", "--del-subgids":
			return []string{action, strconv.FormatUint(uint64(start), 10) + "-" + strconv.FormatUint(uint64(end), 10), identity.Username}, nil
		default:
			return nil, ErrNotAllowlisted
		}
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func lingerProfile(action string) executionProfile {
	profile := newProfile("/usr/bin/loginctl", func(values []string) ([]string, error) {
		identity, err := hostingIdentitySpec(values)
		if err != nil || (action != "enable-linger" && action != "disable-linger" && action != "terminate-user") {
			return nil, ErrInvalidInvocation
		}
		return []string{action, identity.Username}, nil
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func scheduledJobSystemdProfile(action string) executionProfile {
	profile := newProfile("/usr/bin/systemctl", func(values []string) ([]string, error) {
		identity, jobID, err := scheduledjobs.FromInvocationValues(values)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		_, timer, err := scheduledjobs.UnitNames(identity, jobID)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		switch action {
		case "show":
			return []string{
				"show", "--no-pager", "--property=LoadState", "--property=ActiveState",
				"--property=UnitFileState", timer,
			}, nil
		case "enable":
			return []string{"enable", "--now", timer}, nil
		case "restart":
			return []string{"restart", timer}, nil
		case "disable":
			return []string{"disable", "--now", timer}, nil
		case "clean":
			return []string{"clean", "--what=state", timer}, nil
		default:
			return nil, ErrNotAllowlisted
		}
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func phpConfigurationTestProfile(executable, requiredVersion string) executionProfile {
	profile := newProfile(executable, func(values []string) ([]string, error) {
		identity, version, err := phpruntime.SpecFromVersionInvocationValues(values)
		if err != nil || version != requiredVersion {
			return nil, ErrInvalidInvocation
		}
		configuration, err := phpruntime.ConfigurationPath(identity, version)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		return []string{"--test", "--fpm-config", configuration}, nil
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func phpSystemdProfile(action string) executionProfile {
	profile := newProfile("/usr/bin/systemctl", func(values []string) ([]string, error) {
		identity, version, err := phpruntime.SpecFromVersionInvocationValues(values)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		unit, err := phpruntime.UnitName(identity, version)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		switch action {
		case "show":
			return []string{
				"show", "--no-pager", "--property=LoadState", "--property=ActiveState",
				"--property=SubState", "--property=UnitFileState", "--property=ControlGroup",
				"--property=MemoryCurrent", "--property=CPUUsageNSec", "--property=TasksCurrent", unit,
			}, nil
		case "enable", "restart":
			return []string{action, unit}, nil
		case "disable":
			return []string{"disable", "--now", unit}, nil
		default:
			return nil, ErrNotAllowlisted
		}
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func nginxCandidateResolver() argumentResolver {
	return func(values []string) ([]string, error) {
		if len(values) != 1 {
			return nil, ErrInvalidInvocation
		}
		parsed, err := uuid.Parse(values[0])
		if err != nil || parsed.String() != values[0] || parsed.Version() != uuid.Version(7) {
			return nil, ErrInvalidInvocation
		}
		configuration := nginxbaseline.SiteTransactionsDirectory + "/" + values[0] + "/nginx.conf"
		return []string{"-t", "-q", "-c", configuration}, nil
	}
}

func newProfile(executable string, resolver argumentResolver) executionProfile {
	return executionProfile{
		executable: executable, timeout: defaultTimeout,
		stdoutLimit: defaultOutputLimit, stderrLimit: defaultOutputLimit,
		waitDelay: defaultWaitDelay, resolve: resolver,
	}
}

func mutationProfile(executable string, resolver argumentResolver) executionProfile {
	profile := newProfile(executable, resolver)
	profile.timeout = accountMutationTimeout
	return profile
}

func accountMutationProfile(
	executable string,
	arguments func(hostingidentity.Spec) []string,
) executionProfile {
	profile := newProfile(executable, func(values []string) ([]string, error) {
		spec, err := hostingIdentitySpec(values)
		if err != nil {
			return nil, err
		}
		return arguments(spec), nil
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func projectQuotaProfile() executionProfile {
	profile := newProfile("/usr/sbin/setquota", func(values []string) ([]string, error) {
		spec, err := hostingStorageSpec(values)
		if err != nil {
			return nil, err
		}
		blocks := strconv.FormatUint(spec.ByteLimit/hostingstorage.QuotaBlockBytes, 10)
		inodes := strconv.FormatUint(spec.InodeLimit, 10)
		return []string{
			"-P", strconv.FormatUint(uint64(spec.ProjectID), 10),
			blocks, blocks, inodes, inodes, "/srv/hosting",
		}, nil
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func webAccessACLProfile() executionProfile {
	profile := newProfile("/usr/bin/setfacl", func(values []string) ([]string, error) {
		if len(values) != 8 {
			return nil, ErrInvalidInvocation
		}
		identity, err := hostingIdentitySpec(values[:5])
		if err != nil {
			return nil, err
		}
		worker := values[6]
		if worker != "www-data" && worker != "nginx" {
			return nil, ErrNotAllowlisted
		}
		scope := values[7]
		permission := "r-x"
		target := identity.HomeDirectory
		arguments := []string{}
		switch scope {
		case "account":
			if values[5] != "" {
				return nil, ErrInvalidInvocation
			}
			permission = "--x"
		case "ancestor", "document", "default":
			relative, normalizeErr := hostingpath.NormalizeDocumentRoot(values[5])
			if normalizeErr != nil || relative != values[5] {
				return nil, ErrInvalidInvocation
			}
			target = path.Join(identity.HomeDirectory, relative)
			if scope == "ancestor" {
				permission = "--x"
			} else if scope == "default" {
				arguments = append(arguments, "--default")
			}
		default:
			return nil, ErrNotAllowlisted
		}
		acl := "user::rwx,user:" + worker + ":" + permission +
			",group::r-x,mask::r-x,other::---"
		return append(arguments, "--set="+acl, target), nil
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func selinuxWebContextProfile(action string) executionProfile {
	profile := newProfile("/usr/sbin/semanage", func(values []string) ([]string, error) {
		if len(values) != 8 || (action != "-a" && action != "-m") ||
			(values[7] != "static" && values[7] != "php") {
			return nil, ErrInvalidInvocation
		}
		identity, err := hostingIdentitySpec(values[:5])
		if err != nil {
			return nil, err
		}
		target := regexp.QuoteMeta(identity.HomeDirectory)
		switch values[6] {
		case "account":
			if values[5] != "" {
				return nil, ErrInvalidInvocation
			}
		case "ancestor", "document":
			relative, normalizeErr := hostingpath.NormalizeDocumentRoot(values[5])
			if normalizeErr != nil || relative != values[5] {
				return nil, ErrInvalidInvocation
			}
			target = regexp.QuoteMeta(path.Join(identity.HomeDirectory, relative))
			if values[6] == "document" {
				target += "(/.*)?"
			}
		default:
			return nil, ErrNotAllowlisted
		}
		kind := "httpd_sys_content_t"
		if values[6] == "document" && values[7] == "php" {
			kind = "httpd_sys_rw_content_t"
		}
		return []string{"fcontext", action, "-t", kind, target}, nil
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func restoreSELinuxWebContextProfile() executionProfile {
	profile := newProfile("/usr/sbin/restorecon", func(values []string) ([]string, error) {
		if len(values) != 8 || (values[7] != "static" && values[7] != "php") {
			return nil, ErrInvalidInvocation
		}
		identity, err := hostingIdentitySpec(values[:5])
		if err != nil {
			return nil, err
		}
		switch values[6] {
		case "account":
			if values[5] != "" {
				return nil, ErrInvalidInvocation
			}
			return []string{identity.HomeDirectory}, nil
		case "ancestor", "document":
			relative, normalizeErr := hostingpath.NormalizeDocumentRoot(values[5])
			if normalizeErr != nil || relative != values[5] {
				return nil, ErrInvalidInvocation
			}
			target := path.Join(identity.HomeDirectory, relative)
			if values[6] == "ancestor" {
				return []string{target}, nil
			}
			return []string{"-R", target}, nil
		default:
			return nil, ErrNotAllowlisted
		}
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func accountResourceProfile(arguments func(hostingresources.Spec) []string) executionProfile {
	profile := newProfile("/usr/bin/systemctl", func(values []string) ([]string, error) {
		spec, err := hostingresources.SpecFromInvocationValues(values)
		if err != nil {
			return nil, ErrInvalidInvocation
		}
		return arguments(spec), nil
	})
	profile.timeout = accountMutationTimeout
	return profile
}

func hostingIdentitySpec(values []string) (hostingidentity.Spec, error) {
	if len(values) != 5 {
		return hostingidentity.Spec{}, ErrInvalidInvocation
	}
	uid, err := strconv.ParseUint(values[2], 10, 32)
	if err != nil {
		return hostingidentity.Spec{}, ErrInvalidInvocation
	}
	gid, err := strconv.ParseUint(values[3], 10, 32)
	if err != nil {
		return hostingidentity.Spec{}, ErrInvalidInvocation
	}
	spec := hostingidentity.Spec{
		AccountID: values[0], Username: values[1], UID: uint32(uid), GID: uint32(gid),
		HomeDirectory: values[4],
	}
	if err := hostingidentity.Validate(spec); err != nil {
		return hostingidentity.Spec{}, ErrInvalidInvocation
	}
	return spec, nil
}

func hostingStorageSpec(values []string) (hostingstorage.Spec, error) {
	if len(values) != 8 {
		return hostingstorage.Spec{}, ErrInvalidInvocation
	}
	identity, err := hostingIdentitySpec(values[:5])
	if err != nil {
		return hostingstorage.Spec{}, err
	}
	projectID, err := strconv.ParseUint(values[5], 10, 32)
	if err != nil {
		return hostingstorage.Spec{}, ErrInvalidInvocation
	}
	byteLimit, err := strconv.ParseUint(values[6], 10, 64)
	if err != nil {
		return hostingstorage.Spec{}, ErrInvalidInvocation
	}
	inodeLimit, err := strconv.ParseUint(values[7], 10, 64)
	if err != nil {
		return hostingstorage.Spec{}, ErrInvalidInvocation
	}
	spec := hostingstorage.Spec{
		Identity: identity, ProjectID: uint32(projectID), ByteLimit: byteLimit, InodeLimit: inodeLimit,
	}
	if err := hostingstorage.Validate(spec); err != nil {
		return hostingstorage.Spec{}, ErrInvalidInvocation
	}
	return spec, nil
}

func exactValueResolver(prefix []string, allowed map[string]struct{}) argumentResolver {
	return func(values []string) ([]string, error) {
		if len(values) != 1 {
			return nil, ErrInvalidInvocation
		}
		if _, exists := allowed[values[0]]; !exists {
			return nil, ErrNotAllowlisted
		}
		arguments := make([]string, 0, len(prefix)+1)
		arguments = append(arguments, prefix...)
		arguments = append(arguments, values[0])
		return arguments, nil
	}
}

func noValueResolver(arguments []string) argumentResolver {
	return func(values []string) ([]string, error) {
		if len(values) != 0 {
			return nil, ErrInvalidInvocation
		}
		return append([]string(nil), arguments...), nil
	}
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

// Run executes one fixed profile without a shell. A non-zero program exit is a
// Result; inability to safely start, bound, cancel, or reap it is an error.
func (runner *Runner) Run(ctx context.Context, invocation Invocation) (Result, error) {
	if runner == nil {
		return Result{}, newRunError(ErrNotAllowlisted, nil)
	}
	if ctx == nil {
		return Result{}, newRunError(ErrInvalidInvocation, nil)
	}
	profile, exists := runner.profiles[invocation.Profile]
	if !exists {
		return Result{}, newRunError(ErrNotAllowlisted, nil)
	}
	if err := validateProfile(profile); err != nil {
		return Result{}, newRunError(ErrInvalidInvocation, nil)
	}
	if len(invocation.Input) != 0 && (!profile.acceptsInput || len(invocation.Input) > profile.inputLimit) ||
		len(invocation.Input) == 0 && profile.acceptsInput {
		return Result{}, newRunError(ErrInvalidInvocation, nil)
	}
	arguments, err := profile.resolve(append([]string(nil), invocation.Values...))
	if err != nil {
		if errors.Is(err, ErrNotAllowlisted) {
			return Result{}, newRunError(ErrNotAllowlisted, nil)
		}
		return Result{}, newRunError(ErrInvalidInvocation, nil)
	}
	if containsInvalidArgument(arguments) {
		return Result{}, newRunError(ErrInvalidInvocation, nil)
	}
	secrets := sensitiveValues(invocation.Values, profile.sensitiveInputs)

	runContext, cancel := context.WithTimeout(ctx, profile.timeout)
	defer cancel()
	// #nosec G204 -- the executable is an absolute installation-owned path and
	// arguments are assembled only by the selected, internal allowlist profile.
	command := exec.CommandContext(runContext, profile.executable, arguments...)
	command.Env = sanitizedEnvironment()
	command.Dir = "/"
	var identity *hostingidentity.Spec
	if profile.accountProcess {
		parsed, identityErr := hostingIdentitySpec(invocation.Values[:min(len(invocation.Values), 5)])
		if identityErr != nil {
			return Result{}, newRunError(ErrInvalidInvocation, nil)
		}
		identity = &parsed
		command.Env = append(command.Env, "HOME="+parsed.HomeDirectory, "USER="+parsed.Username,
			"LOGNAME="+parsed.Username, "XDG_RUNTIME_DIR=/run/user/"+strconv.FormatUint(uint64(parsed.UID), 10))
		command.Dir = parsed.HomeDirectory
	}
	if profile.acceptsInput {
		command.Stdin = bytes.NewReader(invocation.Input)
	} else {
		command.Stdin = nil
	}
	command.WaitDelay = profile.waitDelay
	if err := configureProcess(command, identity); err != nil {
		return Result{}, err
	}

	overflow := make(chan struct{})
	var overflowOnce sync.Once
	notifyOverflow := func() { overflowOnce.Do(func() { close(overflow) }) }
	stdout := newBoundedCapture(profile.stdoutLimit, notifyOverflow)
	stderr := newBoundedCapture(profile.stderrLimit, notifyOverflow)
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		if contextError := classifyContextError(ctx, runContext); contextError != nil {
			return Result{}, contextError
		}
		return Result{}, newRunError(ErrStart, nil)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()

	trigger := waitTriggerProcess
	var waitError error
	select {
	case waitError = <-waited:
	case <-overflow:
		trigger = waitTriggerOutput
		_ = terminateProcessTree(command)
		waitError = <-waited
	case <-runContext.Done():
		trigger = waitTriggerContext
		_ = terminateProcessTree(command)
		waitError = <-waited
	}

	if trigger == waitTriggerOutput || stdout.Exceeded() || stderr.Exceeded() {
		return Result{}, newRunError(ErrOutputLimit, nil)
	}
	if trigger == waitTriggerContext || waitError != nil && runContext.Err() != nil {
		if contextError := classifyContextError(ctx, runContext); contextError != nil {
			return Result{}, contextError
		}
	}

	result := Result{
		Stdout: redactText(stdout.String(), secrets),
		Stderr: redactText(stderr.String(), secrets),
	}
	if waitError == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(waitError, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	return Result{}, newRunError(ErrWait, nil)
}

type waitTrigger uint8

const (
	waitTriggerProcess waitTrigger = iota
	waitTriggerOutput
	waitTriggerContext
)

func validateProfile(profile executionProfile) error {
	if !path.IsAbs(profile.executable) || path.Clean(profile.executable) != profile.executable ||
		profile.resolve == nil || profile.timeout <= 0 || profile.stdoutLimit <= 0 ||
		profile.stderrLimit <= 0 || profile.waitDelay <= 0 || profile.acceptsInput != (profile.inputLimit > 0) ||
		profile.inputLimit > 1<<20 {
		return ErrInvalidInvocation
	}
	return nil
}

func containsInvalidArgument(arguments []string) bool {
	for _, argument := range arguments {
		if len(argument) > 4_096 || strings.IndexByte(argument, 0) >= 0 {
			return true
		}
	}
	return false
}

func sanitizedEnvironment() []string {
	return []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "TZ=UTC"}
}

func sensitiveValues(values []string, indexes map[int]struct{}) []string {
	secrets := make([]string, 0, len(indexes))
	for index := range indexes {
		if index >= 0 && index < len(values) && values[index] != "" {
			secrets = append(secrets, values[index])
		}
	}
	return secrets
}

func redactText(value string, secrets []string) string {
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}

func classifyContextError(parent, run context.Context) error {
	if errors.Is(parent.Err(), context.Canceled) {
		return newRunError(ErrCancelled, context.Canceled)
	}
	if errors.Is(parent.Err(), context.DeadlineExceeded) {
		return newRunError(ErrTimedOut, context.DeadlineExceeded)
	}
	if errors.Is(run.Err(), context.DeadlineExceeded) {
		return newRunError(ErrTimedOut, context.DeadlineExceeded)
	}
	if errors.Is(run.Err(), context.Canceled) {
		return newRunError(ErrCancelled, context.Canceled)
	}
	return nil
}

type boundedCapture struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	notify   func()
}

func newBoundedCapture(limit int, notify func()) *boundedCapture {
	return &boundedCapture{limit: limit, notify: notify}
}

func (capture *boundedCapture) Write(content []byte) (int, error) {
	originalLength := len(content)
	if originalLength == 0 {
		return 0, nil
	}
	capture.mu.Lock()
	remaining := capture.limit - capture.buffer.Len()
	if remaining <= 0 {
		capture.exceeded = true
	} else {
		if len(content) > remaining {
			capture.exceeded = true
			content = content[:remaining]
		}
		_, _ = capture.buffer.Write(content)
	}
	exceeded := capture.exceeded
	notify := capture.notify
	capture.mu.Unlock()
	if exceeded && notify != nil {
		notify()
	}
	return originalLength, nil
}

func (capture *boundedCapture) Exceeded() bool {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.exceeded
}

func (capture *boundedCapture) String() string {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.buffer.String()
}
