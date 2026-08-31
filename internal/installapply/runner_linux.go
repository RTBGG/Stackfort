// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/installpreflight"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

const maximumCommandErrorBytes = 64 << 10

const (
	rockyPHPNGINXDropIn        = "/etc/systemd/system/nginx.service.d/php-fpm.conf"
	rockyPHPNGINXDropInContent = "[Unit]\nWants=php-fpm.service\n\n"
)

type LinuxRunner struct {
	distribution    string
	output          io.Writer
	runOverride     func(context.Context, []string, string, ...string) error
	captureOverride func(context.Context, string, ...string) (string, error)
}

type selinuxFileContext struct {
	kind       string
	expression string
}

func stackfortSELinuxFileContexts() []selinuxFileContext {
	return []selinuxFileContext{
		{"httpd_sys_content_t", "/usr/share/stackfort/web(/.*)?"},
		{"httpd_sys_content_t", "/usr/share/stackfort/phpmyadmin(/.*)?"},
		{"httpd_sys_content_t", "/var/lib/stackfort-agent/acme-http01(/.*)?"},
		{"httpd_config_t", "/etc/stackfort/panel-tls(/.*)?"},
		{"httpd_config_t", phpMyAdminConfigurationRoot + "(/.*)?"},
		{"httpd_config_t", phpMyAdminBrokerRoot + "(/.*)?"},
		{"httpd_sys_rw_content_t", phpMyAdminStateRoot + "(/.*)?"},
		{"httpd_config_t", phpruntime.ConfigurationRoot + "(/.*)?"},
		{"httpd_var_run_t", phpMyAdminRuntimeRoot + "(/.*)?"},
		{"httpd_var_run_t", phpruntime.RuntimeRoot + "(/.*)?"},
		{"httpd_log_t", "/var/log/stackfort/accounts(/.*)?"},
		// Coraza runs inside the confined NGINX worker. Its bounded persistent
		// collections therefore need the same narrow
		// writable cache type as the distribution's native NGINX cache.
		{"httpd_cache_t", wafconfig.RuntimeRoot + "(/.*)?"},
	}
}

func stackfortSELinuxRestorePaths() []string {
	return []string{
		"/usr/share/stackfort/web", "/usr/share/stackfort/phpmyadmin",
		"/var/lib/stackfort-agent/acme-http01", "/etc/stackfort/panel-tls",
		phpMyAdminConfigurationRoot, phpMyAdminBrokerRoot, phpMyAdminStateRoot,
		"/var/log/stackfort/accounts",
		phpruntime.ConfigurationRoot, phpruntime.RuntimeRoot, wafconfig.RuntimeRoot,
	}
}

func NewLinuxRunner(output io.Writer) (*LinuxRunner, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("Stackfort installation must run as root")
	}
	platform := hostcapabilities.NewInspector().InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable || platform.Architecture != "amd64" {
		return nil, errors.New("Stackfort installation requires a supported amd64 Linux platform")
	}
	if output == nil {
		output = io.Discard
	}
	return &LinuxRunner{distribution: platform.DistributionID, output: output}, nil
}

func (runner *LinuxRunner) Distribution() string { return runner.distribution }

func (runner *LinuxRunner) Preflight(ctx context.Context) error {
	result, err := installpreflight.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("run installer preflight: %w", err)
	}
	if !result.Ready {
		return &PreflightError{Result: result}
	}
	return nil
}

func (runner *LinuxRunner) Apply(ctx context.Context, stage StageID, source Source) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(runner.output, "Applying installer stage: %s\n", stage)
	switch stage {
	case StagePackages:
		return runner.applyPackages(ctx)
	case StageWAFPackage:
		return runner.applyWAFPackage(ctx, source)
	case StageVinylPackage:
		return runner.applyVinylPackage(ctx, source)
	case StageIdentity:
		return runner.applyIdentity(ctx)
	case StagePayload:
		return runner.applyPayload(source)
	case StageConfiguration:
		return runner.applyConfiguration(ctx, source)
	case StageSecurity:
		return runner.applySecurity(ctx)
	case StageNGINX:
		return runner.applyNGINX(ctx)
	case StageServices:
		return runner.applyServices(ctx)
	default:
		return false, errors.New("installer stage is not allowlisted")
	}
}

func (runner *LinuxRunner) Verify(ctx context.Context, stage StageID, source Source) error {
	switch stage {
	case StagePackages:
		return runner.verifyPackages(ctx)
	case StageWAFPackage:
		return runner.verifyWAFPackage(ctx, source)
	case StageVinylPackage:
		return runner.verifyVinylPackage(ctx, source)
	case StageIdentity:
		if _, _, err := serviceIdentity(); err != nil {
			return err
		}
		_, _, err := phpMyAdminIdentity()
		return err
	case StagePayload:
		return runner.verifyPayload(source)
	case StageConfiguration:
		return runner.verifyConfiguration(ctx, source)
	case StageSecurity:
		return runner.verifySecurity(ctx)
	case StageNGINX:
		return runner.verifyNGINX(ctx)
	case StageServices:
		return runner.verifyServices(ctx)
	default:
		return errors.New("installer stage is not allowlisted")
	}
}

func (runner *LinuxRunner) VerifyInstallation(ctx context.Context, source Source) error {
	for _, stage := range orderedStages {
		if err := runner.Verify(ctx, stage, source); err != nil {
			return fmt.Errorf("verify %s: %w", stage, err)
		}
	}
	return nil
}

func (runner *LinuxRunner) applyPackages(ctx context.Context) (bool, error) {
	missing, err := runner.missingPackages(ctx)
	if err != nil {
		return false, err
	}
	changed := false
	if len(missing) > 0 {
		switch runner.distribution {
		case "debian", "ubuntu":
			if err := runner.run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "/usr/bin/apt-get",
				"-o", "DPkg::Lock::Timeout=120", "update"); err != nil {
				return false, err
			}
			arguments := []string{"-o", "DPkg::Lock::Timeout=120", "install", "-y", "--no-install-recommends"}
			if err := runner.run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "/usr/bin/apt-get",
				append(arguments, missing...)...); err != nil {
				return false, err
			}
		case "rocky":
			if err := runner.run(ctx, nil, "/usr/bin/dnf", append([]string{"install", "-y"}, missing...)...); err != nil {
				return false, err
			}
		default:
			return false, errors.New("unsupported package manager")
		}
		changed = true
		// Debian-family package scripts can start an unmanaged NGINX instance.
		// The following baseline stage may adopt only an inactive vendor unit.
		if runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", "nginx.service") {
			if err := runner.run(ctx, nil, "/usr/bin/systemctl", "stop", "nginx.service"); err != nil {
				return true, err
			}
		}
	}
	profile, err := nativePHPProfile(runner.distribution)
	if err != nil {
		return changed, err
	}
	if runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", profile.VendorUnit) ||
		runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-enabled", "--quiet", profile.VendorUnit) {
		if err := runner.run(ctx, nil, "/usr/bin/systemctl", "disable", "--now", profile.VendorUnit); err != nil {
			return true, err
		}
		changed = true
	}
	if runner.distribution == "rocky" {
		dropInRemoved, removeErr := removeKnownRockyPHPNGINXDropIn()
		if removeErr != nil {
			return changed, removeErr
		}
		changed = changed || dropInRemoved
	}
	socketsChanged, socketErr := runner.reconcilePodmanAPISockets(ctx)
	if socketErr != nil {
		return changed, socketErr
	}
	changed = changed || socketsChanged
	return changed, nil
}

func (runner *LinuxRunner) reconcilePodmanAPISockets(ctx context.Context) (bool, error) {
	changed := false
	if !runner.podmanUnitsMasked(ctx, false) {
		if err := runner.run(ctx, nil, "/usr/bin/systemctl", "mask", "--now", "podman.socket", "podman.service"); err != nil {
			return changed, fmt.Errorf("mask rootful Podman API units: %w", err)
		}
		changed = true
	}
	if !runner.podmanUnitsMasked(ctx, true) {
		if err := runner.run(ctx, nil, "/usr/bin/systemctl", "--global", "mask", "podman.socket", "podman.service"); err != nil {
			return changed, fmt.Errorf("mask rootless Podman API units globally: %w", err)
		}
		changed = true
	}
	return changed, runner.verifyPodmanAPISockets(ctx)
}

func (runner *LinuxRunner) podmanUnitsMasked(ctx context.Context, global bool) bool {
	for _, unit := range []string{"podman.socket", "podman.service"} {
		arguments := []string{"is-enabled", unit}
		if global {
			arguments = []string{"--global", "is-enabled", unit}
		}
		output, _ := runner.capture(ctx, "/usr/bin/systemctl", arguments...)
		if strings.TrimSpace(output) != "masked" {
			return false
		}
	}
	return true
}

func (runner *LinuxRunner) verifyPodmanAPISockets(ctx context.Context) error {
	if !runner.podmanUnitsMasked(ctx, false) || !runner.podmanUnitsMasked(ctx, true) {
		return errors.New("Podman API service or socket is not masked")
	}
	if runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", "podman.socket") ||
		runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", "podman.service") {
		return errors.New("rootful Podman API service or socket remains active")
	}
	if _, err := os.Lstat("/run/podman/podman.sock"); err == nil {
		return errors.New("rootful Podman API socket remains present")
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("inspect rootful Podman API socket")
	}
	return nil
}

func removeKnownRockyPHPNGINXDropIn() (bool, error) {
	info, err := os.Lstat(rockyPHPNGINXDropIn)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o644 {
		return false, errors.New("Rocky PHP-FPM NGINX integration conflicts with expected package state")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return false, errors.New("Rocky PHP-FPM NGINX integration ownership conflicts with expected package state")
	}
	content, err := readBoundedRegular(rockyPHPNGINXDropIn, 1<<10)
	if err != nil || string(content) != rockyPHPNGINXDropInContent {
		return false, errors.New("Rocky PHP-FPM NGINX integration content conflicts with expected package state")
	}
	if err := os.Remove(rockyPHPNGINXDropIn); err != nil {
		return false, errors.New("remove Rocky PHP-FPM NGINX integration")
	}
	return true, nil
}

func (runner *LinuxRunner) missingPackages(ctx context.Context) ([]string, error) {
	packages := installerPackages(runner.distribution)
	missing := make([]string, 0, len(packages))
	for _, name := range packages {
		var installed bool
		switch runner.distribution {
		case "debian", "ubuntu":
			status, queryErr := runner.capture(ctx, "/usr/bin/dpkg-query", "-W", "-f=${db:Status-Abbrev}", name)
			installed = queryErr == nil && strings.HasPrefix(status, "ii")
		case "rocky":
			installed = runner.commandSucceeds(ctx, "/usr/bin/rpm", "-q", name)
		default:
			return nil, errors.New("unsupported package database")
		}
		if !installed {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

func installerPackages(distribution string) []string {
	switch distribution {
	case "debian":
		return []string{
			"acl", "apparmor", "apparmor-utils", "ca-certificates", "curl", "logrotate", "mariadb-server", "nginx", "nftables",
			"aardvark-dns", "fuse-overlayfs", "netavark", "passt", "podman", "slirp4netns", "uidmap",
			"php8.4-cli", "php8.4-curl", "php8.4-fpm", "php8.4-gd", "php8.4-intl", "php8.4-mbstring", "php8.4-mysql",
			"php8.4-xml", "php8.4-zip", "phpmyadmin", "quota",
		}
	case "ubuntu":
		return []string{
			"acl", "apparmor", "apparmor-utils", "ca-certificates", "curl", "logrotate", "mariadb-server", "nginx", "nftables",
			"aardvark-dns", "fuse-overlayfs", "netavark", "passt", "podman", "slirp4netns", "uidmap",
			"php8.5-cli", "php8.5-curl", "php8.5-fpm", "php8.5-gd", "php8.5-intl", "php8.5-mbstring", "php8.5-mysql",
			"php8.5-xml", "php8.5-zip", "phpmyadmin", "quota",
		}
	case "rocky":
		return []string{
			"acl", "ca-certificates", "checkpolicy", "curl", "firewalld", "logrotate", "mariadb-server", "nginx",
			"aardvark-dns", "fuse-overlayfs", "netavark", "passt", "podman", "shadow-utils-subid", "slirp4netns",
			"php-cli", "php-common", "php-fpm", "php-gd", "php-intl", "php-mbstring", "php-mysqlnd", "php-pecl-zip", "php-xml",
			"policycoreutils", "policycoreutils-python-utils", "quota",
		}
	default:
		return nil
	}
}

func nativePHPProfile(distribution string) (phpruntime.Profile, error) {
	version, err := phpruntime.ApprovedVersion(distribution)
	if err != nil {
		return phpruntime.Profile{}, errors.New("unsupported PHP runtime distribution")
	}
	return phpruntime.ForDistribution(distribution, version)
}

func (runner *LinuxRunner) verifyPackages(ctx context.Context) error {
	missing, err := runner.missingPackages(ctx)
	if err != nil {
		return err
	}
	if len(missing) != 0 {
		return fmt.Errorf("required packages remain missing: %s", strings.Join(missing, ", "))
	}
	profile, err := nativePHPProfile(runner.distribution)
	if err != nil {
		return err
	}
	for _, executable := range []string{"/usr/sbin/nginx", "/usr/bin/mariadb", "/usr/bin/setfacl", "/usr/sbin/setquota", profile.BinaryPath} {
		if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("required executable is unavailable: %s", executable)
		}
	}
	if runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", profile.VendorUnit) ||
		runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-enabled", "--quiet", profile.VendorUnit) {
		return errors.New("distribution-wide PHP-FPM service remains active or enabled")
	}
	if runner.distribution == "rocky" {
		if _, err := os.Lstat(rockyPHPNGINXDropIn); !errors.Is(err, os.ErrNotExist) {
			return errors.New("Rocky PHP-FPM NGINX integration remains installed")
		}
	}
	return runner.verifyPodmanAPISockets(ctx)
}

func (runner *LinuxRunner) applyIdentity(ctx context.Context) (bool, error) {
	changed := false
	group, groupErr := user.LookupGroup("stackfort")
	if groupErr != nil {
		if err := runner.run(ctx, nil, "/usr/sbin/groupadd", "--system", "stackfort"); err != nil {
			return false, err
		}
		changed = true
		group, groupErr = user.LookupGroup("stackfort")
	}
	if groupErr != nil {
		return false, errors.New("resolve Stackfort service group")
	}
	if _, err := strconv.ParseUint(group.Gid, 10, 32); err != nil {
		return false, errors.New("Stackfort service group has malformed GID")
	}
	if _, userErr := user.Lookup("stackfort"); userErr != nil {
		if err := runner.run(ctx, nil, "/usr/sbin/useradd", "--system", "--gid", "stackfort",
			"--home-dir", "/var/lib/stackfort", "--shell", "/usr/sbin/nologin", "--no-create-home", "stackfort"); err != nil {
			return false, err
		}
		changed = true
	}
	if _, _, err := serviceIdentity(); err != nil {
		return changed, err
	}
	pmaGroup, pmaGroupErr := user.LookupGroup("stackfort-pma")
	if pmaGroupErr != nil {
		if err := runner.run(ctx, nil, "/usr/sbin/groupadd", "--system", "stackfort-pma"); err != nil {
			return changed, err
		}
		changed = true
		pmaGroup, pmaGroupErr = user.LookupGroup("stackfort-pma")
	}
	if pmaGroupErr != nil {
		return changed, errors.New("resolve phpMyAdmin service group")
	}
	if _, err := strconv.ParseUint(pmaGroup.Gid, 10, 32); err != nil {
		return changed, errors.New("phpMyAdmin service group has malformed GID")
	}
	if _, userErr := user.Lookup("stackfort-pma"); userErr != nil {
		if err := runner.run(ctx, nil, "/usr/sbin/useradd", "--system", "--gid", "stackfort-pma",
			"--home-dir", phpMyAdminStateRoot, "--shell", "/usr/sbin/nologin", "--no-create-home", "stackfort-pma"); err != nil {
			return changed, err
		}
		changed = true
	}
	_, _, err := phpMyAdminIdentity()
	return changed, err
}

func serviceIdentity() (int, int, error) {
	account, err := user.Lookup("stackfort")
	if err != nil || account.HomeDir != "/var/lib/stackfort" || account.Username != "stackfort" {
		return 0, 0, errors.New("Stackfort service account conflicts with required identity")
	}
	if account.Gid == "" || account.Uid == "" {
		return 0, 0, errors.New("Stackfort service identity is malformed")
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 31)
	if err != nil || uid == 0 {
		return 0, 0, errors.New("Stackfort service UID is invalid")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 31)
	if err != nil || gid == 0 {
		return 0, 0, errors.New("Stackfort service GID is invalid")
	}
	content, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return 0, 0, errors.New("inspect Stackfort password lock")
	}
	locked := false
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "stackfort:") {
			password := strings.SplitN(line, ":", 3)[1]
			locked = strings.HasPrefix(password, "!") || strings.HasPrefix(password, "*")
			break
		}
	}
	if !locked {
		return 0, 0, errors.New("Stackfort service account is not password-locked")
	}
	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return 0, 0, errors.New("inspect Stackfort service account")
	}
	wantedSuffix := ":/var/lib/stackfort:/usr/sbin/nologin"
	accountValid := false
	for _, line := range strings.Split(string(passwd), "\n") {
		if strings.HasPrefix(line, "stackfort:") && strings.HasSuffix(line, wantedSuffix) {
			accountValid = true
			break
		}
	}
	if !accountValid {
		return 0, 0, errors.New("Stackfort service account home or shell conflicts with required identity")
	}
	return int(uid), int(gid), nil
}

func phpMyAdminIdentity() (int, int, error) {
	account, err := user.Lookup("stackfort-pma")
	if err != nil || account.HomeDir != phpMyAdminStateRoot || account.Username != "stackfort-pma" {
		return 0, 0, errors.New("phpMyAdmin service account conflicts with required identity")
	}
	group, err := user.LookupGroup("stackfort-pma")
	if err != nil || group.Gid == "" || group.Gid != account.Gid || account.Uid == "" {
		return 0, 0, errors.New("phpMyAdmin service identity is malformed")
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 31)
	if err != nil || uid == 0 {
		return 0, 0, errors.New("phpMyAdmin service UID is invalid")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 31)
	if err != nil || gid == 0 {
		return 0, 0, errors.New("phpMyAdmin service GID is invalid")
	}
	shadow, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return 0, 0, errors.New("inspect phpMyAdmin password lock")
	}
	locked := false
	for _, line := range strings.Split(string(shadow), "\n") {
		if strings.HasPrefix(line, "stackfort-pma:") {
			password := strings.SplitN(line, ":", 3)[1]
			locked = strings.HasPrefix(password, "!") || strings.HasPrefix(password, "*")
			break
		}
	}
	if !locked {
		return 0, 0, errors.New("phpMyAdmin service account is not password-locked")
	}
	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return 0, 0, errors.New("inspect phpMyAdmin service account")
	}
	wantedSuffix := ":" + phpMyAdminStateRoot + ":/usr/sbin/nologin"
	valid := false
	for _, line := range strings.Split(string(passwd), "\n") {
		if strings.HasPrefix(line, "stackfort-pma:") && strings.HasSuffix(line, wantedSuffix) {
			valid = true
			break
		}
	}
	if !valid {
		return 0, 0, errors.New("phpMyAdmin service account home or shell conflicts with required identity")
	}
	return int(uid), int(gid), nil
}

func (runner *LinuxRunner) applyPayload(source Source) (bool, error) {
	if err := verifyRootOwnedSource(source); err != nil {
		return false, err
	}
	uid, gid, err := serviceIdentity()
	if err != nil {
		return false, err
	}
	pmaUID, pmaGID, err := phpMyAdminIdentity()
	if err != nil {
		return false, err
	}
	directories := payloadDirectories(runner.distribution, uid, gid, pmaUID, pmaGID)
	changed := false
	for _, directory := range directories {
		directoryChanged, err := ensureDirectory(directory)
		if err != nil {
			return false, err
		}
		changed = changed || directoryChanged
	}
	for _, file := range []struct{ source, target string }{
		{filepath.Join(source.Root, "bin", "stackfort-api"), "/usr/local/bin/stackfort-api"},
		{filepath.Join(source.Root, "bin", "stackfort-agent"), "/usr/local/sbin/stackfort-agent"},
		{filepath.Join(source.Root, "bin", "stackfort-trivy"), ociimage.ScannerExecutable},
	} {
		fileChanged, err := copySourceFile(file.source, file.target, 0o755)
		if err != nil {
			return false, err
		}
		changed = changed || fileChanged
	}
	webChanged, err := deployWebTree(filepath.Join(source.Root, "web"), "/usr/share/stackfort/web")
	if err != nil {
		return changed || webChanged, err
	}
	changed = changed || webChanged
	if runner.distribution == "rocky" {
		phpMyAdminChanged, deployErr := deployWebTree(
			filepath.Join(source.Root, "phpmyadmin"), "/usr/share/stackfort/phpmyadmin",
		)
		return changed || phpMyAdminChanged, deployErr
	}
	return changed, nil
}

func payloadDirectories(distribution string, uid, gid, pmaUID, pmaGID int) []directorySpec {
	directories := []directorySpec{
		{"/etc/stackfort", 0, gid, 0o751},
		{phpMyAdminConfigurationRoot, 0, pmaGID, 0o750},
		{phpruntime.ConfigurationRoot, 0, 0, 0o750},
		{"/etc/stackfort/panel-tls", 0, 0, 0o700},
		{"/var/lib/stackfort", uid, gid, 0o750},
		{"/var/lib/stackfort/staging", uid, gid, 0o750},
		{phpMyAdminStateRoot, pmaUID, pmaGID, 0o750},
		{phpMyAdminStateRoot + "/sessions", pmaUID, pmaGID, 0o700},
		{phpMyAdminStateRoot + "/tmp", pmaUID, pmaGID, 0o700},
		{phpMyAdminBrokerRoot, uid, pmaGID, 0o750},
		// NGINX workers must traverse this root-owned parent to serve the
		// deliberately public HTTP-01 response directory below it.
		{"/var/lib/stackfort-agent", 0, 0, 0o755},
		{"/var/lib/stackfort-agent/acme-http01", 0, 0, 0o755},
		{ociimage.TransactionRoot, 0, 0, 0o711},
		{ociimage.ArtifactRoot, 0, 0, 0o700},
		{ociimage.ScannerCacheRoot, 0, 0, 0o700},
		{"/usr/local/libexec", 0, 0, 0o755},
		{"/usr/share/stackfort", 0, 0, 0o755},
		{"/usr/share/stackfort/web", 0, 0, 0o755},
		{phpruntime.RuntimeRoot, 0, 0, 0o755},
		{"/srv/hosting/accounts", 0, 0, 0o711},
		{"/srv/hosting/backups", 0, 0, 0o710},
		{"/var/log/stackfort", 0, 0, 0o750},
		{"/var/log/stackfort/accounts", 0, 0, 0o700},
	}
	if distribution == "rocky" {
		directories = append(directories, directorySpec{"/usr/share/stackfort/phpmyadmin", 0, 0, 0o755})
	}
	return directories
}

func (runner *LinuxRunner) verifyPayload(source Source) error {
	uid, gid, err := serviceIdentity()
	if err != nil {
		return err
	}
	pmaUID, pmaGID, err := phpMyAdminIdentity()
	if err != nil {
		return err
	}
	for _, file := range []struct {
		source, target string
		mode           os.FileMode
	}{
		{filepath.Join(source.Root, "bin", "stackfort-api"), "/usr/local/bin/stackfort-api", 0o755},
		{filepath.Join(source.Root, "bin", "stackfort-agent"), "/usr/local/sbin/stackfort-agent", 0o755},
		{filepath.Join(source.Root, "bin", "stackfort-trivy"), ociimage.ScannerExecutable, 0o755},
	} {
		content, err := readBoundedRegular(file.source, maximumSingleFile)
		if err != nil {
			return err
		}
		if err := verifyFile(file.target, content, 0, 0, file.mode); err != nil {
			return err
		}
	}
	if err := verifyWebTree(filepath.Join(source.Root, "web"), "/usr/share/stackfort/web"); err != nil {
		return err
	}
	if runner.distribution == "rocky" {
		if err := verifyWebTree(filepath.Join(source.Root, "phpmyadmin"), "/usr/share/stackfort/phpmyadmin"); err != nil {
			return err
		}
	}
	for _, directory := range payloadDirectories(runner.distribution, uid, gid, pmaUID, pmaGID) {
		if err := verifyDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func verifyDirectory(spec directorySpec) error {
	info, err := os.Lstat(spec.path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != spec.mode.Perm() {
		return fmt.Errorf("installed directory metadata mismatch: %s", spec.path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != spec.uid || int(stat.Gid) != spec.gid {
		return fmt.Errorf("installed directory ownership mismatch: %s", spec.path)
	}
	return nil
}

func (runner *LinuxRunner) applyConfiguration(ctx context.Context, source Source) (bool, error) {
	uid, gid, err := serviceIdentity()
	if err != nil {
		return false, err
	}
	pmaUID, pmaGID, err := phpMyAdminIdentity()
	if err != nil {
		return false, err
	}
	changed, err := reconcileFile("/etc/stackfort/stackfort.env", []byte(environmentFile()), 0, gid, 0o640, true)
	if err != nil {
		return false, err
	}
	retentionChanged, err := reconcileFile("/etc/logrotate.d/stackfort", []byte(logrotateFile()), 0, 0, 0o644, true)
	if err != nil {
		return changed, err
	}
	changed = changed || retentionChanged
	for _, secret := range []struct {
		path           string
		size, uid, gid int
		mode           os.FileMode
	}{
		{phpMyAdminBrokerKeyPath, 32, uid, pmaGID, 0o640},
		{phpMyAdminBlowfishKeyPath, 32, pmaUID, pmaGID, 0o600},
	} {
		secretChanged, secretErr := ensureRandomSecret(secret.path, secret.size, secret.uid, secret.gid, secret.mode)
		if secretErr != nil {
			return changed, secretErr
		}
		changed = changed || secretChanged
	}
	for _, file := range []struct {
		source, target string
		uid, gid       int
		mode           os.FileMode
	}{
		{filepath.Join(source.Root, "phpmyadmin-integration", "signon.php"), phpMyAdminSignonPath, 0, pmaGID, 0o640},
		{filepath.Join(source.Root, "phpmyadmin-integration", "stackfort-launch.php"), phpMyAdminLaunchPath, 0, pmaGID, 0o640},
	} {
		content, readErr := readBoundedRegular(file.source, maximumSingleFile)
		if readErr != nil {
			return changed, readErr
		}
		fileChanged, fileErr := reconcileFile(file.target, content, file.uid, file.gid, file.mode, true)
		if fileErr != nil {
			return changed, fileErr
		}
		changed = changed || fileChanged
	}
	fpmConfiguration, err := phpMyAdminFPMConfiguration(runner.distribution)
	if err != nil {
		return changed, err
	}
	fpmChanged, err := reconcileFile(phpMyAdminFPMConfigPath, []byte(fpmConfiguration), 0, pmaGID, 0o640, true)
	if err != nil {
		return changed, err
	}
	changed = changed || fpmChanged
	if nativePath, native := phpMyAdminNativeConfigurationPath(runner.distribution); native {
		content, readErr := readBoundedRegular(filepath.Join(source.Root, "phpmyadmin-integration", "config.inc.php"), maximumSingleFile)
		if readErr != nil {
			return changed, readErr
		}
		nativeChanged, nativeErr := reconcileFile(nativePath, content, 0, 0, 0o644, true)
		if nativeErr != nil {
			return changed, nativeErr
		}
		changed = changed || nativeChanged
	}
	if runner.distribution == "rocky" {
		policyChanged, err := reconcileFile(selinuxNGINXPanelPolicyPath, []byte(selinuxNGINXPanelPolicy()), 0, 0, 0o644, true)
		if err != nil {
			return changed, err
		}
		changed = changed || policyChanged
	}
	panelTLSChanged, err := reconcilePanelTLS(time.Now().UTC())
	if err != nil {
		return changed, err
	}
	changed = changed || panelTLSChanged
	units := serviceUnits(runner.distribution)
	names := []string{"stackfort.slice", "stackfort-core.slice", "stackfort-accounts.slice", "stackfort-agent.service", "stackfort-api.service", phpMyAdminUnit}
	if runner.distribution != "rocky" {
		names = append(names, "stackfort-firewall.service")
		fileChanged, err := reconcileFile("/etc/stackfort/firewall.nft", []byte(nftablesFile()), 0, gid, 0o640, true)
		if err != nil {
			return false, err
		}
		changed = changed || fileChanged
	}
	for _, name := range names {
		fileChanged, err := reconcileFile(filepath.Join("/etc/systemd/system", name), []byte(units[name]), 0, 0, 0o644, true)
		if err != nil {
			return false, err
		}
		changed = changed || fileChanged
	}
	verifyArguments := []string{"verify"}
	for _, name := range names {
		verifyArguments = append(verifyArguments, filepath.Join("/etc/systemd/system", name))
	}
	if err := runner.run(ctx, nil, "/usr/bin/systemd-analyze", verifyArguments...); err != nil {
		return changed, err
	}
	if err := runner.run(ctx, nil, "/usr/bin/systemctl", "daemon-reload"); err != nil {
		return changed, err
	}
	return changed, nil
}

func (runner *LinuxRunner) verifyConfiguration(ctx context.Context, source Source) error {
	uid, gid, err := serviceIdentity()
	if err != nil {
		return err
	}
	pmaUID, pmaGID, err := phpMyAdminIdentity()
	if err != nil {
		return err
	}
	if err := verifyFile("/etc/stackfort/stackfort.env", []byte(environmentFile()), 0, gid, 0o640); err != nil {
		return err
	}
	if err := verifyFile("/etc/logrotate.d/stackfort", []byte(logrotateFile()), 0, 0, 0o644); err != nil {
		return err
	}
	if runner.distribution == "rocky" {
		if err := verifyFile(selinuxNGINXPanelPolicyPath, []byte(selinuxNGINXPanelPolicy()), 0, 0, 0o644); err != nil {
			return err
		}
	}
	if _, err := verifyPanelTLS(time.Now().UTC()); err != nil {
		return err
	}
	if err := verifySecret(phpMyAdminBrokerKeyPath, 32, uid, pmaGID, 0o640); err != nil {
		return err
	}
	if err := verifySecret(phpMyAdminBlowfishKeyPath, 32, pmaUID, pmaGID, 0o600); err != nil {
		return err
	}
	for _, file := range []struct {
		source, target string
		uid, gid       int
		mode           os.FileMode
	}{
		{filepath.Join(source.Root, "phpmyadmin-integration", "signon.php"), phpMyAdminSignonPath, 0, pmaGID, 0o640},
		{filepath.Join(source.Root, "phpmyadmin-integration", "stackfort-launch.php"), phpMyAdminLaunchPath, 0, pmaGID, 0o640},
	} {
		content, readErr := readBoundedRegular(file.source, maximumSingleFile)
		if readErr != nil {
			return readErr
		}
		if err := verifyFile(file.target, content, file.uid, file.gid, file.mode); err != nil {
			return err
		}
	}
	fpmConfiguration, err := phpMyAdminFPMConfiguration(runner.distribution)
	if err != nil {
		return err
	}
	if err := verifyFile(phpMyAdminFPMConfigPath, []byte(fpmConfiguration), 0, pmaGID, 0o640); err != nil {
		return err
	}
	if nativePath, native := phpMyAdminNativeConfigurationPath(runner.distribution); native {
		content, readErr := readBoundedRegular(filepath.Join(source.Root, "phpmyadmin-integration", "config.inc.php"), maximumSingleFile)
		if readErr != nil {
			return readErr
		}
		if err := verifyFile(nativePath, content, 0, 0, 0o644); err != nil {
			return err
		}
	}
	units := serviceUnits(runner.distribution)
	for _, name := range []string{"stackfort.slice", "stackfort-core.slice", "stackfort-accounts.slice", "stackfort-agent.service", "stackfort-api.service", phpMyAdminUnit} {
		if err := verifyFile(filepath.Join("/etc/systemd/system", name), []byte(units[name]), 0, 0, 0o644); err != nil {
			return err
		}
		state, err := runner.capture(ctx, "/usr/bin/systemctl", "show", "--property=LoadState", "--value", name)
		if err != nil || strings.TrimSpace(state) != "loaded" {
			return fmt.Errorf("systemd did not load %s", name)
		}
	}
	if runner.distribution != "rocky" {
		if err := verifyFile("/etc/systemd/system/stackfort-firewall.service",
			[]byte(units["stackfort-firewall.service"]), 0, 0, 0o644); err != nil {
			return err
		}
		if err := verifyFile("/etc/stackfort/firewall.nft", []byte(nftablesFile()), 0, gid, 0o640); err != nil {
			return err
		}
	}
	return nil
}

func (runner *LinuxRunner) applySecurity(ctx context.Context) (bool, error) {
	switch runner.distribution {
	case "debian", "ubuntu":
		profileChanged, err := reconcileFile("/etc/apparmor.d/stackfort-api", []byte(appArmorProfile()), 0, 0, 0o644, true)
		if err != nil {
			return false, err
		}
		if err := runner.run(ctx, nil, "/usr/sbin/apparmor_parser", "-r", "/etc/apparmor.d/stackfort-api"); err != nil {
			return profileChanged, err
		}
		if err := runner.run(ctx, nil, "/usr/bin/systemctl", "enable", "--now", "stackfort-firewall.service"); err != nil {
			return true, err
		}
		return true, nil
	case "rocky":
		if err := runner.installSELinuxNGINXPanelPolicy(ctx); err != nil {
			return false, err
		}
		for _, policy := range stackfortSELinuxFileContexts() {
			if err := runner.reconcileSELinuxContext(ctx, policy.kind, policy.expression); err != nil {
				return false, err
			}
		}
		for _, path := range stackfortSELinuxRestorePaths() {
			if err := runner.run(ctx, nil, "/usr/sbin/restorecon", "-R", path); err != nil {
				return true, err
			}
		}
		if err := runner.run(ctx, nil, "/usr/bin/systemctl", "enable", "--now", "firewalld.service"); err != nil {
			return true, err
		}
		for _, port := range []string{"80/tcp", "443/tcp", "8443/tcp"} {
			if err := runner.run(ctx, nil, "/usr/bin/firewall-cmd", "--permanent", "--add-port="+port); err != nil {
				return true, err
			}
			if err := runner.run(ctx, nil, "/usr/bin/firewall-cmd", "--add-port="+port); err != nil {
				return true, err
			}
		}
		return true, nil
	default:
		return false, errors.New("unsupported security provider")
	}
}

func (runner *LinuxRunner) installSELinuxNGINXPanelPolicy(ctx context.Context) error {
	temporaryDirectory, err := os.MkdirTemp("/run", "stackfort-selinux-")
	if err != nil {
		return fmt.Errorf("create SELinux policy workspace: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)
	// #nosec G302 -- this is a private directory, so owner-only 0700 is the required secure mode.
	if err := os.Chmod(temporaryDirectory, 0o700); err != nil {
		return fmt.Errorf("secure SELinux policy workspace: %w", err)
	}
	modulePath := filepath.Join(temporaryDirectory, "stackfort_nginx_panel.mod")
	packagePath := filepath.Join(temporaryDirectory, "stackfort_nginx_panel.pp")
	if err := runner.run(ctx, nil, "/usr/bin/checkmodule", "-M", "-m", "-o", modulePath, selinuxNGINXPanelPolicyPath); err != nil {
		return fmt.Errorf("compile Stackfort SELinux policy: %w", err)
	}
	if err := runner.run(ctx, nil, "/usr/bin/semodule_package", "-o", packagePath, "-m", modulePath); err != nil {
		return fmt.Errorf("package Stackfort SELinux policy: %w", err)
	}
	if err := runner.run(ctx, nil, "/usr/sbin/semodule", "-i", packagePath); err != nil {
		return fmt.Errorf("install Stackfort SELinux policy: %w", err)
	}
	for _, port := range []string{"8080", "8081"} {
		if runner.commandSucceeds(ctx, "/usr/sbin/semanage", "port", "-a", "-t", "stackfort_api_port_t", "-p", "tcp", port) {
			continue
		}
		if err := runner.run(ctx, nil, "/usr/sbin/semanage", "port", "-m", "-t", "stackfort_api_port_t", "-p", "tcp", port); err != nil {
			return fmt.Errorf("label Stackfort local API port %s: %w", port, err)
		}
	}
	return nil
}

func (runner *LinuxRunner) reconcileSELinuxContext(ctx context.Context, kind, expression string) error {
	if runner.commandSucceeds(ctx, "/usr/sbin/semanage", "fcontext", "-a", "-t", kind, expression) {
		return nil
	}
	return runner.run(ctx, nil, "/usr/sbin/semanage", "fcontext", "-m", "-t", kind, expression)
}

func (runner *LinuxRunner) verifySecurity(ctx context.Context) error {
	switch runner.distribution {
	case "debian", "ubuntu":
		profiles, err := os.ReadFile("/sys/kernel/security/apparmor/profiles")
		if err != nil || !bytes.Contains(profiles, []byte("stackfort-api (enforce)")) {
			return errors.New("Stackfort AppArmor profile is not loaded in enforce mode")
		}
		if !runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", "stackfort-firewall.service") ||
			!runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-enabled", "--quiet", "stackfort-firewall.service") ||
			!runner.commandSucceeds(ctx, "/usr/sbin/nft", "list", "table", "inet", "stackfort") {
			return errors.New("dedicated Stackfort nftables policy is not active and persistent")
		}
	case "rocky":
		mode, err := runner.capture(ctx, "/usr/sbin/getenforce")
		if err != nil || strings.TrimSpace(mode) != "Enforcing" {
			return errors.New("SELinux is not enforcing")
		}
		for _, path := range []string{
			"/usr/share/stackfort/web/index.html", "/usr/share/stackfort/phpmyadmin/index.php",
			"/var/lib/stackfort-agent/acme-http01", nginxbaseline.PanelTLSBundlePath,
			phpMyAdminConfigurationRoot, phpMyAdminBrokerRoot, phpMyAdminStateRoot,
			phpruntime.ConfigurationRoot, phpruntime.RuntimeRoot, wafconfig.RuntimeRoot,
		} {
			if !runner.commandSucceeds(ctx, "/usr/sbin/matchpathcon", "-V", path) {
				return fmt.Errorf("SELinux file-context policy is not applied: %s", path)
			}
		}
		modules, err := runner.capture(ctx, "/usr/sbin/semodule", "-l")
		if err != nil || !hasSELinuxModule(modules, "stackfort_nginx_panel") {
			return errors.New("Stackfort NGINX panel SELinux module is not installed")
		}
		ports, err := runner.capture(ctx, "/usr/sbin/semanage", "port", "-l")
		if err != nil || !hasSELinuxPortLabel(ports, "stackfort_api_port_t", "tcp", "8080") ||
			!hasSELinuxPortLabel(ports, "stackfort_api_port_t", "tcp", "8081") {
			return errors.New("Stackfort local API SELinux port labels are not applied")
		}
		if !runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", "firewalld.service") ||
			!runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-enabled", "--quiet", "firewalld.service") {
			return errors.New("firewalld is not active and persistent")
		}
		for _, port := range []string{"80/tcp", "443/tcp", "8443/tcp"} {
			if !runner.commandSucceeds(ctx, "/usr/bin/firewall-cmd", "--query-port="+port) ||
				!runner.commandSucceeds(ctx, "/usr/bin/firewall-cmd", "--permanent", "--query-port="+port) {
				return fmt.Errorf("firewalld port is not active and persistent: %s", port)
			}
		}
	}
	return nil
}

func hasSELinuxModule(output, wanted string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == wanted {
			return true
		}
	}
	return false
}

func hasSELinuxPortLabel(output, kind, protocol, port string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == kind && fields[1] == protocol {
			for _, value := range fields[2:] {
				if strings.TrimSuffix(value, ",") == port {
					return true
				}
			}
		}
	}
	return false
}

func (runner *LinuxRunner) verifyNGINX(ctx context.Context) error {
	spec, err := nginxbaseline.ForDistribution(runner.distribution)
	if err != nil {
		return err
	}
	marker, err := os.ReadFile("/etc/nginx/stackfort/.stackfort-managed")
	if err != nil || string(marker) != "stackfort-nginx-baseline-v1\n" {
		return errors.New("Stackfort NGINX ownership marker is unavailable")
	}
	if !runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", "nginx.service") ||
		!runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-enabled", "--quiet", "nginx.service") {
		return errors.New("managed NGINX service is not active and enabled")
	}
	if err := verifyFile(nginxbaseline.PanelConfigurationPath, []byte(nginxbaseline.Panel(spec)), 0, 0, 0o640); err != nil {
		return err
	}
	for path, content := range map[string]string{
		wafconfig.EnginePath:       wafconfig.Engine(),
		wafconfig.BasePL1Path:      wafconfig.BasePL1(),
		wafconfig.DetectionPL1Path: wafconfig.DetectionPL1(),
		wafconfig.BlockingPL1Path:  wafconfig.BlockingPL1(),
	} {
		if err := verifyFile(path, []byte(content), 0, 0, 0o644); err != nil {
			return err
		}
	}
	if err := verifyFile(wafconfig.SharedPL1Path, []byte(wafconfig.SharedPL1()), 0, 0, 0o640); err != nil {
		return err
	}
	if _, err := verifyPanelTLS(time.Now().UTC()); err != nil {
		return err
	}
	if err := runner.run(ctx, nil, "/usr/sbin/nginx", "-t", "-q", "-c", "/etc/nginx/stackfort/nginx.conf"); err != nil {
		return err
	}
	return panelStaticHealth(ctx)
}

func (runner *LinuxRunner) applyNGINX(ctx context.Context) (bool, error) {
	result, err := hostnginx.NewReconciler().Reconcile(ctx)
	if err != nil {
		return result.Changed, err
	}
	spec, err := nginxbaseline.ForDistribution(runner.distribution)
	if err != nil {
		return result.Changed, err
	}
	previous, existed, _, err := readExistingFile(nginxbaseline.PanelConfigurationPath)
	if err != nil {
		return result.Changed, err
	}
	panelChanged, err := reconcileFile(
		nginxbaseline.PanelConfigurationPath, []byte(nginxbaseline.Panel(spec)), 0, 0, 0o640, true,
	)
	if err != nil {
		return result.Changed, err
	}
	rollback := func() {
		if existed {
			_ = atomicWriteFile(nginxbaseline.PanelConfigurationPath, previous, 0, 0, 0o640)
		} else {
			_ = os.Remove(nginxbaseline.PanelConfigurationPath)
		}
	}
	if panelChanged {
		if err := runner.run(ctx, nil, "/usr/sbin/nginx", "-t", "-q", "-c", nginxbaseline.MainConfiguration); err != nil {
			rollback()
			return true, err
		}
		if err := runner.run(ctx, nil, "/usr/bin/systemctl", "reload", "nginx.service"); err != nil {
			rollback()
			_ = runner.run(ctx, nil, "/usr/bin/systemctl", "reload", "nginx.service")
			return true, err
		}
	}
	return result.Changed || panelChanged, panelStaticHealth(ctx)
}

func (runner *LinuxRunner) applyServices(ctx context.Context) (bool, error) {
	changed := false
	for _, unit := range []string{"mariadb.service", "vinyl.service", "stackfort-agent.service", "stackfort-api.service", phpMyAdminUnit} {
		wasActive := runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", unit)
		wasEnabled := runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-enabled", "--quiet", unit)
		if err := runner.run(ctx, nil, "/usr/bin/systemctl", "enable", "--now", unit); err != nil {
			return changed, err
		}
		changed = changed || !wasActive || !wasEnabled
	}
	return changed, runner.waitForHealth(ctx)
}

func (runner *LinuxRunner) verifyServices(ctx context.Context) error {
	for _, unit := range []string{"mariadb.service", "vinyl.service", "stackfort-agent.service", "stackfort-api.service", phpMyAdminUnit} {
		if !runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", unit) ||
			!runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-enabled", "--quiet", unit) {
			return fmt.Errorf("Stackfort service is not active and enabled: %s", unit)
		}
	}
	required := map[string]map[string]string{
		"vinyl.service": {
			"User": "vinyl", "Slice": "stackfort-core.slice", "NoNewPrivileges": "yes",
			"PrivateDevices": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict",
		},
		"stackfort-agent.service": {
			"User": "root", "Slice": "stackfort-core.slice", "NoNewPrivileges": "yes",
			"PrivateDevices": "yes", "PrivateTmp": "yes", "ProtectSystem": "full",
		},
		"stackfort-api.service": {
			"User": "stackfort", "Slice": "stackfort-core.slice", "NoNewPrivileges": "yes",
			"PrivateDevices": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict",
		},
		phpMyAdminUnit: {
			"User": "stackfort-pma", "Slice": "stackfort-core.slice", "NoNewPrivileges": "yes",
			"PrivateDevices": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict",
		},
	}
	for unit, properties := range required {
		arguments := []string{"show", "--no-pager"}
		keys := make([]string, 0, len(properties))
		for key := range properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			arguments = append(arguments, "--property="+key)
		}
		arguments = append(arguments, unit)
		output, err := runner.capture(ctx, "/usr/bin/systemctl", arguments...)
		if err != nil {
			return err
		}
		observed := parseAssignments(output)
		for key, wanted := range properties {
			if observed[key] != wanted {
				return fmt.Errorf("systemd sandbox mismatch for %s: %s=%q", unit, key, observed[key])
			}
		}
	}
	if runner.distribution == "debian" || runner.distribution == "ubuntu" {
		pidText, err := runner.capture(ctx, "/usr/bin/systemctl", "show", "--property=MainPID", "--value", "stackfort-api.service")
		if err != nil {
			return err
		}
		pid := strings.TrimSpace(pidText)
		pidValue, parseErr := strconv.ParseUint(pid, 10, 31)
		if parseErr != nil || pidValue == 0 || strconv.FormatUint(pidValue, 10) != pid {
			return errors.New("control API MainPID is invalid")
		}
		profilePath := filepath.Join("/proc", pid, "attr", "current")
		// #nosec G304 -- pid is a canonical positive decimal systemd MainPID and cannot alter the /proc path shape.
		profile, err := os.ReadFile(profilePath)
		if err != nil || !strings.HasPrefix(string(profile), "stackfort-api") {
			return errors.New("control API is not confined by the Stackfort AppArmor profile")
		}
	}
	if err := runner.waitForHealth(ctx); err != nil {
		return err
	}
	if err := panelAPIHealth(ctx); err != nil {
		return err
	}
	return panelPHPMyAdminHealth(ctx)
}

func (runner *LinuxRunner) waitForHealth(ctx context.Context) error {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := apiHealth(ctx); err == nil {
			if err := runner.agentHealth(ctx); err == nil {
				return nil
			} else {
				lastErr = err
			}
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("Stackfort service health check timed out: %w", lastErr)
}

func apiHealth(ctx context.Context) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8080/api/v1/health", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("control API health status is not 200")
	}
	var body struct{ Service, Status, Storage string }
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&body); err != nil ||
		body.Service != "stackfort-api" || body.Status != "ok" || body.Storage != "ok" {
		return errors.New("control API health response is invalid")
	}
	return nil
}

func panelStaticHealth(ctx context.Context) error {
	return waitForPanelHealth(ctx, "static listener", probePanelStatic)
}

func probePanelStatic(ctx context.Context) error {
	client, err := panelHTTPClient(time.Now().UTC())
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1:8443/", nil)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(content, []byte(`id="app"`)) {
		return errors.New("panel static application response is invalid")
	}
	return nil
}

func panelAPIHealth(ctx context.Context) error {
	return waitForPanelHealth(ctx, "control API", probePanelAPI)
}

func probePanelAPI(ctx context.Context) error {
	client, err := panelHTTPClient(time.Now().UTC())
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1:8443/api/v1/health", nil)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("panel control API health status is not 200")
	}
	var body struct{ Service, Status, Storage string }
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&body); err != nil ||
		body.Service != "stackfort-api" || body.Status != "ok" || body.Storage != "ok" {
		return errors.New("panel control API health response is invalid")
	}
	return nil
}

func panelPHPMyAdminHealth(ctx context.Context) error {
	return waitForPanelHealth(ctx, "phpMyAdmin listener", probePanelPHPMyAdmin)
}

func probePanelPHPMyAdmin(ctx context.Context) error {
	client, err := panelHTTPClient(time.Now().UTC())
	if err != nil {
		return err
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://127.0.0.1:8443/phpmyadmin/stackfort-launch.php", nil)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/" ||
		response.Header.Get("Cache-Control") != "no-store" {
		return errors.New("phpMyAdmin launch endpoint response is invalid")
	}
	return nil
}

func (runner *LinuxRunner) agentHealth(ctx context.Context) error {
	// The agent deliberately accepts only the kernel-reported control API UID.
	// Probe it under that same identity instead of weakening peer validation to
	// admit the root-owned installer process.
	output, err := runner.capture(ctx, "/usr/sbin/runuser", "--user", "stackfort", "--",
		"/usr/bin/curl", "--fail", "--silent", "--show-error", "--max-time", "2",
		"--unix-socket", agentprotocol.DefaultSocketPath, "http://localhost/v1/health")
	if err != nil {
		return err
	}
	var body struct{ Service, Status string }
	if err := json.Unmarshal([]byte(output), &body); err != nil ||
		body.Service != "stackfort-agent" || body.Status != "ok" {
		return errors.New("host agent health response is invalid")
	}
	return nil
}

func (runner *LinuxRunner) run(ctx context.Context, extraEnvironment []string, executable string, arguments ...string) error {
	if !allowedInstallerExecutable(executable) {
		return errors.New("installer executable is not allowlisted")
	}
	if runner.runOverride != nil {
		return runner.runOverride(ctx, extraEnvironment, executable, arguments...)
	}
	// #nosec G204 -- executable is an absolute path accepted by allowedInstallerExecutable; arguments are passed without a shell.
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = append([]string{
		"HOME=/root", "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
	}, extraEnvironment...)
	command.Stdout = runner.output
	var errorOutput boundedBuffer
	command.Stderr = io.MultiWriter(runner.output, &errorOutput)
	if err := command.Run(); err != nil {
		return fmt.Errorf("command %s failed: %w: %s", filepath.Base(executable), err, strings.TrimSpace(errorOutput.String()))
	}
	return nil
}

func (runner *LinuxRunner) capture(ctx context.Context, executable string, arguments ...string) (string, error) {
	if !allowedInstallerExecutable(executable) {
		return "", errors.New("installer executable is not allowlisted")
	}
	if runner.captureOverride != nil {
		return runner.captureOverride(ctx, executable, arguments...)
	}
	// #nosec G204 -- executable is an absolute path accepted by allowedInstallerExecutable; arguments are passed without a shell.
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = []string{"HOME=/root", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	var output boundedBuffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if output.exceeded {
		return "", errors.New("command output exceeded installer limit")
	}
	if err != nil {
		return output.String(), err
	}
	return output.String(), nil
}

func (runner *LinuxRunner) commandSucceeds(ctx context.Context, executable string, arguments ...string) bool {
	_, err := runner.capture(ctx, executable, arguments...)
	return err == nil
}

func allowedInstallerExecutable(executable string) bool {
	switch executable {
	case "/usr/bin/apt-get", "/usr/bin/checkmodule", "/usr/bin/dnf", "/usr/bin/dpkg", "/usr/bin/dpkg-query",
		"/usr/bin/firewall-cmd", "/usr/bin/rpm", "/usr/bin/semodule_package", "/usr/bin/systemctl",
		"/usr/bin/systemd-analyze", "/usr/sbin/apparmor_parser", "/usr/sbin/getenforce",
		"/usr/sbin/groupadd", "/usr/sbin/matchpathcon", "/usr/sbin/nft", "/usr/sbin/nginx",
		"/usr/sbin/restorecon", "/usr/sbin/runuser", "/usr/sbin/semanage", "/usr/sbin/semodule",
		"/usr/sbin/useradd":
		return true
	default:
		return false
	}
}

type boundedBuffer struct {
	content  bytes.Buffer
	exceeded bool
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	original := len(content)
	remaining := maximumCommandErrorBytes - buffer.content.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return original, nil
	}
	if len(content) > remaining {
		buffer.exceeded = true
		content = content[:remaining]
	}
	_, _ = buffer.content.Write(content)
	return original, nil
}

func (buffer *boundedBuffer) String() string { return buffer.content.String() }

func parseAssignments(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			values[key] = value
		}
	}
	return values
}
