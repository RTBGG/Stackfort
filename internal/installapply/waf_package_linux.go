// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bufio"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/releaseartifacts"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

const (
	wafQualificationRoot         = "/usr/share/doc/stackfort-waf/qualification"
	wafQualificationManifestPath = wafQualificationRoot + "/manifest.json"
	wafQualificationInventory    = wafQualificationRoot + "/FILES.SHA256"
	wafMaximumInventoryEntries   = 10_000
	wafMaximumInstalledFileBytes = 64 << 20
)

type wafQualificationManifest struct {
	Schema int `json:"schema"`
	Target struct {
		OS                  string `json:"os"`
		VersionPrefix       string `json:"versionPrefix"`
		Architecture        string `json:"architecture"`
		PackageFormat       string `json:"packageFormat"`
		NGINXSourceVersion  string `json:"nginxSourceVersion"`
		NGINXPackageVersion string `json:"nginxPackageVersion"`
		NGINXWorker         string `json:"nginxWorker"`
	} `json:"target"`
	Components struct {
		Coraza      string `json:"coraza"`
		LibCoraza   string `json:"libCoraza"`
		CorazaNGINX string `json:"corazaNGINX"`
		OWASPCRS    string `json:"owaspCRS"`
		GoToolchain string `json:"goToolchain"`
	} `json:"components"`
	Runtime struct {
		LibraryDirectory string `json:"libraryDirectory"`
		ModuleDirectory  string `json:"moduleDirectory"`
		LoaderPath       string `json:"loaderPath"`
	} `json:"runtime"`
}

type wafRuntimeContract struct {
	worker, moduleDirectory, loaderPath string
}

func (runner *LinuxRunner) applyWAFPackage(ctx context.Context, source Source) (bool, error) {
	artifact, err := source.Manifest.WAFArtifact(runner.distribution)
	if err != nil {
		return false, err
	}
	if err := releaseartifacts.VerifyArtifact(source.Root, artifact); err != nil {
		return false, fmt.Errorf("verify native WAF release artifact: %w", err)
	}
	installedVersion, installed, err := runner.installedWAFPackageVersion(ctx)
	if err != nil {
		return false, err
	}
	if installed {
		if installedVersion != artifact.PackageVersion {
			return false, fmt.Errorf("installed %s version %s conflicts with release version %s",
				releaseartifacts.WAFPackageName, installedVersion, artifact.PackageVersion)
		}
		return false, runner.verifyWAFPackage(ctx, source)
	}
	packagePath := filepath.Join(source.Root, filepath.FromSlash(artifact.Path))
	var installErr error
	switch runner.distribution {
	case "debian", "ubuntu":
		installErr = runner.run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "/usr/bin/apt-get",
			"-o", "DPkg::Lock::Timeout=120", "install", "-y", "--no-install-recommends", packagePath)
	case "rocky":
		installErr = runner.run(ctx, nil, "/usr/bin/rpm", "--upgrade", "--replacepkgs", packagePath)
	default:
		return false, errors.New("unsupported native WAF package manager")
	}
	if installErr == nil {
		installErr = runner.verifyWAFPackage(ctx, source)
	}
	if installErr == nil {
		return true, nil
	}
	rollbackErr := runner.rollbackWAFPackage(ctx)
	if rollbackErr != nil {
		return true, errors.Join(installErr, fmt.Errorf("roll back native WAF package: %w", rollbackErr))
	}
	return true, installErr
}

func (runner *LinuxRunner) verifyWAFPackage(ctx context.Context, source Source) error {
	artifact, err := source.Manifest.WAFArtifact(runner.distribution)
	if err != nil {
		return err
	}
	if err := releaseartifacts.VerifyArtifact(source.Root, artifact); err != nil {
		return fmt.Errorf("verify native WAF release artifact: %w", err)
	}
	installedVersion, installed, err := runner.installedWAFPackageVersion(ctx)
	if err != nil {
		return err
	}
	if !installed || installedVersion != artifact.PackageVersion {
		return errors.New("native WAF package is not installed at the release version")
	}
	drift, err := runner.nativeWAFPackageDrift(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(drift) != "" {
		return errors.New("native WAF package verification reported file drift")
	}
	nginxVersion, err := runner.nativeNGINXPackageVersion(ctx)
	if err != nil || nginxVersion != artifact.NGINXPackageVersion {
		return errors.New("installed NGINX package differs from the WAF release contract")
	}
	qualification, err := readWAFQualificationManifest()
	if err != nil {
		return err
	}
	contract, err := validateWAFQualification(qualification, artifact)
	if err != nil {
		return err
	}
	if err := verifyWAFInstalledInventory(qualification); err != nil {
		return err
	}
	if err := verifyWAFDirectories(contract); err != nil {
		return err
	}
	if err := verifyWAFSymlinks(qualification.Runtime.LibraryDirectory); err != nil {
		return err
	}
	module := qualification.Runtime.ModuleDirectory + "/ngx_http_coraza_module.so"
	coraza := qualification.Runtime.LibraryDirectory + "/libcoraza.so"
	if err := verifyWAFSharedObject(module, "", "", qualification.Runtime.LibraryDirectory); err != nil {
		return fmt.Errorf("verify installed NGINX WAF module: %w", err)
	}
	if err := verifyWAFSharedObject(coraza, "", "", ""); err != nil {
		return fmt.Errorf("verify installed libcoraza: %w", err)
	}
	return runner.testWAFModuleLoad(ctx, module)
}

func (runner *LinuxRunner) installedWAFPackageVersion(ctx context.Context) (string, bool, error) {
	switch runner.distribution {
	case "debian", "ubuntu":
		output, err := runner.capture(ctx, "/usr/bin/dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Version}", releaseartifacts.WAFPackageName)
		if err != nil {
			if strings.Contains(output, "no packages found matching") {
				return "", false, nil
			}
			return "", false, fmt.Errorf("query native WAF package: %w", err)
		}
		fields := strings.Fields(output)
		if len(fields) != 2 || fields[0] != "ii" || fields[1] == "" {
			return "", false, errors.New("native WAF Debian package has an incomplete state")
		}
		return fields[1], true, nil
	case "rocky":
		output, err := runner.capture(ctx, "/usr/bin/rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", releaseartifacts.WAFPackageName)
		if err != nil {
			if strings.Contains(output, "is not installed") {
				return "", false, nil
			}
			return "", false, fmt.Errorf("query native WAF package: %w", err)
		}
		version := strings.TrimSpace(output)
		if version == "" || strings.ContainsAny(version, " \t\r\n") {
			return "", false, errors.New("native WAF RPM version is malformed")
		}
		return version, true, nil
	default:
		return "", false, errors.New("unsupported native WAF package database")
	}
}

func (runner *LinuxRunner) nativeNGINXPackageVersion(ctx context.Context) (string, error) {
	switch runner.distribution {
	case "debian", "ubuntu":
		output, err := runner.capture(ctx, "/usr/bin/dpkg-query", "-W", "-f=${Version}", "nginx")
		return strings.TrimSpace(output), err
	case "rocky":
		output, err := runner.capture(ctx, "/usr/bin/rpm", "-q", "--qf", "%{EPOCHNUM}:%{VERSION}-%{RELEASE}", "nginx")
		return strings.TrimSpace(output), err
	default:
		return "", errors.New("unsupported NGINX package database")
	}
}

func (runner *LinuxRunner) nativeWAFPackageDrift(ctx context.Context) (string, error) {
	switch runner.distribution {
	case "debian", "ubuntu":
		output, err := runner.capture(ctx, "/usr/bin/dpkg", "--verify", releaseartifacts.WAFPackageName)
		return output, err
	case "rocky":
		output, err := runner.capture(ctx, "/usr/bin/rpm", "-V", releaseartifacts.WAFPackageName)
		return output, err
	default:
		return "", errors.New("unsupported native WAF package database")
	}
}

func (runner *LinuxRunner) rollbackWAFPackage(ctx context.Context) error {
	present := false
	switch runner.distribution {
	case "debian", "ubuntu":
		present = runner.commandSucceeds(ctx, "/usr/bin/dpkg-query", "-W", releaseartifacts.WAFPackageName)
		if present {
			return runner.run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "/usr/bin/apt-get",
				"-o", "DPkg::Lock::Timeout=120", "remove", "-y", releaseartifacts.WAFPackageName)
		}
	case "rocky":
		present = runner.commandSucceeds(ctx, "/usr/bin/rpm", "-q", releaseartifacts.WAFPackageName)
		if present {
			return runner.run(ctx, nil, "/usr/bin/rpm", "--erase", releaseartifacts.WAFPackageName)
		}
	default:
		return errors.New("unsupported native WAF package database")
	}
	return nil
}

func readWAFQualificationManifest() (wafQualificationManifest, error) {
	content, err := readBoundedRegular(wafQualificationManifestPath, 64<<10)
	if err != nil {
		return wafQualificationManifest{}, errors.New("read installed WAF qualification manifest")
	}
	var manifest wafQualificationManifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return wafQualificationManifest{}, errors.New("decode installed WAF qualification manifest")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return wafQualificationManifest{}, errors.New("installed WAF qualification manifest contains trailing data")
	}
	return manifest, nil
}

func validateWAFQualification(manifest wafQualificationManifest, artifact releaseartifacts.Artifact) (wafRuntimeContract, error) {
	wanted := wafRuntimeContract{}
	switch artifact.Distribution {
	case "debian", "ubuntu":
		wanted = wafRuntimeContract{worker: "www-data", moduleDirectory: "/usr/lib/nginx/modules", loaderPath: "/etc/nginx/modules-enabled/50-stackfort-coraza.conf"}
	case "rocky":
		wanted = wafRuntimeContract{worker: "nginx", moduleDirectory: "/usr/lib64/nginx/modules", loaderPath: "/usr/share/nginx/modules/50-stackfort-coraza.conf"}
	default:
		return wafRuntimeContract{}, errors.New("unsupported installed WAF target")
	}
	wantedLibrary := "/usr/lib/stackfort/coraza-" + wafconfig.LibCorazaVersion + "/lib"
	if manifest.Schema != 1 || manifest.Target.OS != artifact.Distribution ||
		manifest.Target.VersionPrefix != artifact.VersionPrefix || manifest.Target.Architecture != artifact.Architecture ||
		manifest.Target.PackageFormat != artifact.Format || manifest.Target.NGINXPackageVersion != artifact.NGINXPackageVersion ||
		manifest.Target.NGINXWorker != wanted.worker || manifest.Components.Coraza != artifact.CorazaVersion ||
		manifest.Components.LibCoraza != artifact.LibCorazaVersion || manifest.Components.CorazaNGINX != artifact.CorazaNGINXVersion ||
		manifest.Components.OWASPCRS != artifact.OWASPCRSVersion || manifest.Components.GoToolchain != wafconfig.GoToolchainVersion ||
		manifest.Runtime.LibraryDirectory != wantedLibrary ||
		manifest.Runtime.ModuleDirectory != wanted.moduleDirectory || manifest.Runtime.LoaderPath != wanted.loaderPath {
		return wafRuntimeContract{}, errors.New("installed WAF qualification manifest differs from the release contract")
	}
	return wanted, nil
}

func verifyWAFInstalledInventory(manifest wafQualificationManifest) error {
	content, err := readBoundedRegular(wafQualificationInventory, 4<<20)
	if err != nil {
		return errors.New("read installed WAF qualification inventory")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	entries := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 || !strings.HasPrefix(fields[1], "./") {
			return errors.New("installed WAF inventory contains a malformed record")
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || hex.EncodeToString(digest) != fields[0] {
			return errors.New("installed WAF inventory checksum is malformed")
		}
		path := "/" + strings.TrimPrefix(fields[1], "./")
		if err := validateWAFInventoryPath(path, manifest); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if path == manifest.Runtime.ModuleDirectory+"/ngx_http_coraza_module.so" ||
			path == manifest.Runtime.LibraryDirectory+"/libcoraza.so" {
			mode = 0o755
		}
		if err := verifyWAFInstalledFile(path, fields[0], mode); err != nil {
			return err
		}
		entries++
		if entries > wafMaximumInventoryEntries {
			return errors.New("installed WAF inventory exceeds its entry limit")
		}
	}
	if err := scanner.Err(); err != nil || entries == 0 {
		return errors.New("read installed WAF inventory")
	}
	return nil
}

func validateWAFInventoryPath(path string, manifest wafQualificationManifest) error {
	if filepath.Clean(path) != path || !filepath.IsAbs(path) || strings.Contains(path, "..") {
		return errors.New("installed WAF inventory path is unsafe")
	}
	allowedExact := map[string]struct{}{
		manifest.Runtime.LoaderPath:                                     {},
		manifest.Runtime.ModuleDirectory + "/ngx_http_coraza_module.so": {},
	}
	if _, allowed := allowedExact[path]; allowed {
		return nil
	}
	for _, prefix := range []string{
		manifest.Runtime.LibraryDirectory + "/",
		wafconfig.EngineDataRoot + "/",
		wafconfig.CRSRoot + "/",
		"/usr/share/licenses/stackfort-waf/",
	} {
		if strings.HasPrefix(path, prefix) {
			return nil
		}
	}
	return fmt.Errorf("installed WAF inventory contains an unowned path: %s", path)
}

func verifyWAFInstalledFile(path, expected string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != mode || info.Size() < 0 || info.Size() > wafMaximumInstalledFileBytes {
		return fmt.Errorf("installed WAF file metadata differs from qualification: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return fmt.Errorf("installed WAF file ownership differs from qualification: %s", path)
	}
	file, err := os.Open(path) // #nosec G304 -- path passed the fixed WAF inventory allowlist.
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(file, wafMaximumInstalledFileBytes+1))
	if err != nil || read != info.Size() || hex.EncodeToString(digest.Sum(nil)) != expected {
		return fmt.Errorf("installed WAF file checksum differs from qualification: %s", path)
	}
	return nil
}

func verifyWAFDirectories(contract wafRuntimeContract) error {
	worker, err := user.Lookup(contract.worker)
	if err != nil {
		return errors.New("resolve installed WAF worker account")
	}
	uid, uidErr := strconv.Atoi(worker.Uid)
	gid, gidErr := strconv.Atoi(worker.Gid)
	if uidErr != nil || gidErr != nil || uid == 0 || gid == 0 {
		return errors.New("installed WAF worker identity is invalid")
	}
	for _, spec := range []directorySpec{
		{wafconfig.RuntimeRoot, 0, 0, 0o755},
		{wafconfig.PersistentRoot, uid, gid, 0o700},
	} {
		if err := verifyDirectory(spec); err != nil {
			return err
		}
	}
	return nil
}

func verifyWAFSymlinks(libraryDirectory string) error {
	entries, err := os.ReadDir(libraryDirectory)
	if err != nil {
		return errors.New("read installed WAF private library directory")
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("installed WAF private library directory contains a symlink: %s", entry.Name())
		}
	}
	return nil
}

func expectedWAFSymlinks() map[string]string {
	return map[string]string{}
}

func verifyWAFSharedObject(path, soname, requiredLibrary, libraryDirectory string) error {
	binary, err := elf.Open(path)
	if err != nil {
		return errors.New("file is not a readable ELF shared object")
	}
	defer binary.Close()
	if binary.FileHeader.Class != elf.ELFCLASS64 || binary.FileHeader.Data != elf.ELFDATA2LSB ||
		binary.FileHeader.Machine != elf.EM_X86_64 || binary.FileHeader.Type != elf.ET_DYN {
		return errors.New("ELF shared object does not match amd64")
	}
	if soname != "" {
		sonames, err := binary.DynString(elf.DT_SONAME)
		if err != nil || len(sonames) != 1 || sonames[0] != soname {
			return errors.New("ELF SONAME differs from qualification")
		}
	}
	if requiredLibrary != "" {
		libraries, err := binary.ImportedLibraries()
		if err != nil || !containsExact(libraries, requiredLibrary) {
			return fmt.Errorf("ELF omits required library %s", requiredLibrary)
		}
	}
	if libraryDirectory != "" {
		runpaths, runErr := binary.DynString(elf.DT_RUNPATH)
		if runErr != nil || len(runpaths) == 0 {
			runpaths, runErr = binary.DynString(elf.DT_RPATH)
		}
		if runErr != nil || !containsRunpath(runpaths, libraryDirectory) {
			return errors.New("ELF omits the fixed private runtime library path")
		}
	}
	return nil
}

func containsExact(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsRunpath(values []string, wanted string) bool {
	for _, value := range values {
		for _, component := range strings.Split(value, ":") {
			if component == wanted {
				return true
			}
		}
	}
	return false
}

func (runner *LinuxRunner) testWAFModuleLoad(ctx context.Context, module string) error {
	testRoot, err := os.MkdirTemp(wafconfig.RuntimeRoot, ".installer-worker-test-*")
	if err != nil {
		return errors.New("create isolated WAF worker test directory")
	}
	defer os.RemoveAll(testRoot)
	configurationPath := filepath.Join(testRoot, "nginx.conf")
	socketPath := filepath.Join(testRoot, "nginx.sock")
	pidPath := filepath.Join(testRoot, "nginx.pid")
	errorLogPath := filepath.Join(testRoot, "error.log")
	content := fmt.Sprintf(`load_module %s;
pid %s;
error_log %s notice;
worker_processes 1;
events {}
http {
  access_log off;
  client_body_temp_path %s/client-body;
  proxy_temp_path %s/proxy;
  fastcgi_temp_path %s/fastcgi;
  uwsgi_temp_path %s/uwsgi;
  scgi_temp_path %s/scgi;
  server {
    listen unix:%s;
    coraza on;
    coraza_rules 'SecRuleEngine On';
    coraza_rules 'SecRule REQUEST_URI "@streq /stackfort-waf-block" "id:990001,phase:1,deny,status:403"';
  }
}
`, module, pidPath, errorLogPath, testRoot, testRoot, testRoot, testRoot, testRoot, socketPath)
	if err := os.WriteFile(configurationPath, []byte(content), 0o600); err != nil {
		return errors.New("write isolated WAF worker test configuration")
	}
	if err := runner.run(ctx, nil, "/usr/sbin/nginx", "-c", configurationPath, "-p", testRoot, "-e", errorLogPath); err != nil {
		return fmt.Errorf("start isolated WAF worker test: %w", err)
	}
	defer func() {
		quitContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = runner.run(quitContext, nil, "/usr/sbin/nginx", "-s", "quit", "-c", configurationPath, "-p", testRoot, "-e", errorLogPath)
	}()

	transport := &http.Transport{
		DialContext: func(dialContext context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(dialContext, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/stackfort-waf-block", nil)
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusForbidden {
				return nil
			}
			lastErr = fmt.Errorf("unexpected status %d", response.StatusCode)
		} else {
			lastErr = requestErr
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("isolated Coraza worker did not block its qualification request: %w", lastErr)
}
