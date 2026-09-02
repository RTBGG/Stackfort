// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/releaseartifacts"
)

func (runner *LinuxRunner) applyVinylPackage(ctx context.Context, source Source) (bool, error) {
	artifact, err := source.Manifest.VinylArtifact(runner.distribution)
	if err != nil {
		return false, err
	}
	if err := releaseartifacts.VerifyArtifact(source.Root, artifact); err != nil {
		return false, fmt.Errorf("verify native Vinyl release artifact: %w", err)
	}
	installedVersion, installed, err := runner.installedVinylPackageVersion(ctx)
	if err != nil {
		return false, err
	}
	if installed {
		if installedVersion != artifact.PackageVersion {
			if !runner.allowPackageTransition {
				return false, fmt.Errorf("installed %s version %s conflicts with release version %s",
					releaseartifacts.VinylPackageName, installedVersion, artifact.PackageVersion)
			}
		} else {
			return false, runner.verifyVinylPackage(ctx, source)
		}
	}
	packagePath := filepath.Join(source.Root, filepath.FromSlash(artifact.Path))
	var installErr error
	switch runner.distribution {
	case "debian", "ubuntu":
		installErr = runner.run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "/usr/bin/apt-get",
			"-o", "DPkg::Lock::Timeout=120", "install", "-y", "--allow-downgrades", "--no-install-recommends", packagePath)
	case "rocky":
		// Vinyl links against jemalloc on EL10. The dependency is distributed
		// through EPEL, whose signed repository definition is provided by Rocky
		// Extras. DNF must resolve the local RPM so its generated shared-library
		// requirements are installed as part of the same package transaction.
		if installErr = runner.run(ctx, nil, "/usr/bin/dnf", "install", "-y", "epel-release"); installErr == nil {
			installErr = runner.run(ctx, nil, "/usr/bin/rpm", "--upgrade", "--oldpackage", "--replacepkgs", packagePath)
		}
	default:
		return false, errors.New("unsupported native Vinyl package manager")
	}
	if installErr == nil {
		installErr = runner.verifyVinylPackage(ctx, source)
	}
	if installErr == nil {
		return true, nil
	}
	if runner.allowPackageTransition {
		return true, installErr
	}
	rollbackErr := runner.rollbackVinylPackage(ctx)
	if rollbackErr != nil {
		return true, errors.Join(installErr, fmt.Errorf("roll back native Vinyl package: %w", rollbackErr))
	}
	return true, installErr
}

func (runner *LinuxRunner) verifyVinylPackage(ctx context.Context, source Source) error {
	artifact, err := source.Manifest.VinylArtifact(runner.distribution)
	if err != nil {
		return err
	}
	if err := releaseartifacts.VerifyArtifact(source.Root, artifact); err != nil {
		return fmt.Errorf("verify native Vinyl release artifact: %w", err)
	}
	installedVersion, installed, err := runner.installedVinylPackageVersion(ctx)
	if err != nil {
		return err
	}
	if !installed || installedVersion != artifact.PackageVersion {
		return errors.New("native Vinyl package is not installed at the release version")
	}
	drift, err := runner.nativeVinylPackageDrift(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(drift) != "" {
		return errors.New("native Vinyl package verification reported file drift")
	}
	for _, path := range []string{"/usr/sbin/vinyld", "/usr/bin/vinyladm", "/usr/bin/vinylstat"} {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("installed Vinyl executable is invalid: %s", path)
		}
	}
	vcl, err := readBoundedRegular(cacheconfig.VCLPath, 64<<10)
	if err != nil || string(vcl) != cacheconfig.ManagedVCL() {
		return errors.New("installed Vinyl VCL differs from the closed Stackfort policy")
	}
	secret, err := os.Lstat(cacheconfig.SecretPath)
	if err != nil || !secret.Mode().IsRegular() || secret.Mode().Perm() != 0o600 || secret.Mode()&os.ModeSymlink != 0 {
		return errors.New("Vinyl management secret has unsafe metadata")
	}
	if err := runner.validateVinylVCL(ctx); err != nil {
		return errors.New("installed Vinyl could not compile the managed VCL")
	}
	return nil
}

func (runner *LinuxRunner) validateVinylVCL(ctx context.Context) error {
	// Vinyl prints the complete generated C translation in compile mode. The
	// selected stream varies by build and is potentially large. Discard both
	// streams for this fixed validation command and retain its authoritative
	// exit code.
	return runner.runDiscardingOutput(ctx, nil, "/usr/sbin/vinyld", "-C", "-f", cacheconfig.VCLPath)
}

func (runner *LinuxRunner) installedVinylPackageVersion(ctx context.Context) (string, bool, error) {
	switch runner.distribution {
	case "debian", "ubuntu":
		output, err := runner.capture(ctx, "/usr/bin/dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Version}", releaseartifacts.VinylPackageName)
		if err != nil {
			if strings.Contains(output, "no packages found matching") {
				return "", false, nil
			}
			return "", false, fmt.Errorf("query native Vinyl package: %w", err)
		}
		fields := strings.Fields(output)
		if len(fields) != 2 || fields[0] != "ii" || fields[1] == "" {
			return "", false, errors.New("native Vinyl Debian package has an incomplete state")
		}
		return fields[1], true, nil
	case "rocky":
		output, err := runner.capture(ctx, "/usr/bin/rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", releaseartifacts.VinylPackageName)
		if err != nil {
			if strings.Contains(output, "is not installed") {
				return "", false, nil
			}
			return "", false, fmt.Errorf("query native Vinyl package: %w", err)
		}
		version := strings.TrimSpace(output)
		if version == "" || strings.ContainsAny(version, " \t\r\n") {
			return "", false, errors.New("native Vinyl RPM version is malformed")
		}
		return version, true, nil
	default:
		return "", false, errors.New("unsupported native Vinyl package database")
	}
}

func (runner *LinuxRunner) nativeVinylPackageDrift(ctx context.Context) (string, error) {
	switch runner.distribution {
	case "debian", "ubuntu":
		return runner.capture(ctx, "/usr/bin/dpkg", "--verify", releaseartifacts.VinylPackageName)
	case "rocky":
		return runner.capture(ctx, "/usr/bin/rpm", "-V", releaseartifacts.VinylPackageName)
	default:
		return "", errors.New("unsupported native Vinyl package database")
	}
}

func (runner *LinuxRunner) rollbackVinylPackage(ctx context.Context) error {
	switch runner.distribution {
	case "debian", "ubuntu":
		if runner.commandSucceeds(ctx, "/usr/bin/dpkg-query", "-W", releaseartifacts.VinylPackageName) {
			return runner.run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "/usr/bin/apt-get",
				"-o", "DPkg::Lock::Timeout=120", "remove", "-y", releaseartifacts.VinylPackageName)
		}
	case "rocky":
		if runner.commandSucceeds(ctx, "/usr/bin/rpm", "-q", releaseartifacts.VinylPackageName) {
			return runner.run(ctx, nil, "/usr/bin/rpm", "--erase", releaseartifacts.VinylPackageName)
		}
	default:
		return errors.New("unsupported native Vinyl package database")
	}
	return nil
}
