// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/RTBGG/stackfort/internal/releaseartifacts"
)

const (
	maximumSourceFiles = 10_000
	maximumSourceBytes = 512 << 20
	maximumSingleFile  = 256 << 20
)

var semanticVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

func InspectSource(root string) (Source, error) {
	if !filepath.IsAbs(root) {
		return Source{}, errors.New("release source directory must be absolute")
	}
	root = filepath.Clean(root)
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(resolved) != root {
		return Source{}, errors.New("release source directory must exist without symlink indirection")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&0o022 != 0 {
		return Source{}, errors.New("release source directory must be a non-writable directory")
	}

	versionContent, err := readBoundedRegular(filepath.Join(root, "VERSION"), 256)
	if err != nil {
		return Source{}, fmt.Errorf("read release version: %w", err)
	}
	version := strings.TrimSpace(string(versionContent))
	if !semanticVersionPattern.MatchString(version) {
		return Source{}, errors.New("release VERSION is not a supported semantic version")
	}
	manifest, err := releaseartifacts.ReadManifest(filepath.Join(root, releaseartifacts.ManifestFilename))
	if err != nil {
		return Source{}, err
	}
	if manifest.Version != version || manifest.Architecture != runtime.GOARCH {
		return Source{}, errors.New("release component manifest does not match VERSION or the running architecture")
	}
	if !manifest.WAFComplete {
		return Source{}, errors.New("release component manifest does not contain the complete native WAF package matrix")
	}
	if !manifest.VinylComplete {
		return Source{}, errors.New("release component manifest does not contain the complete native Vinyl package matrix")
	}
	if err := releaseartifacts.VerifyFiles(root, manifest); err != nil {
		return Source{}, fmt.Errorf("verify release component artifacts: %w", err)
	}
	for _, relative := range []string{
		"bin/stackfort-api", "bin/stackfort-agent", "bin/stackfort-updater", "bin/stackfort-gh",
		"bin/stackfort-trivy", "web/index.html", "phpmyadmin/index.php",
		"phpmyadmin/config.inc.php", "phpmyadmin-integration/config.inc.php",
		"phpmyadmin-integration/signon.php", "phpmyadmin-integration/stackfort-launch.php",
		"third-party-licenses/trivy-LICENSE", "third-party-licenses/github-cli-LICENSE",
		"COMMIT", "LICENSE", "README.md", releaseartifacts.ManifestFilename,
	} {
		if _, err := readBoundedRegular(filepath.Join(root, filepath.FromSlash(relative)), maximumSingleFile); err != nil {
			return Source{}, fmt.Errorf("validate required release file %s: %w", relative, err)
		}
	}
	if err := inspectELF(filepath.Join(root, "bin", "stackfort-api")); err != nil {
		return Source{}, fmt.Errorf("validate stackfort-api binary: %w", err)
	}
	if err := inspectELF(filepath.Join(root, "bin", "stackfort-agent")); err != nil {
		return Source{}, fmt.Errorf("validate stackfort-agent binary: %w", err)
	}
	if err := inspectELF(filepath.Join(root, "bin", "stackfort-updater")); err != nil {
		return Source{}, fmt.Errorf("validate stackfort-updater binary: %w", err)
	}
	if err := inspectELF(filepath.Join(root, "bin", "stackfort-gh")); err != nil {
		return Source{}, fmt.Errorf("validate stackfort-gh binary: %w", err)
	}
	if err := inspectELF(filepath.Join(root, "bin", "stackfort-trivy")); err != nil {
		return Source{}, fmt.Errorf("validate stackfort-trivy binary: %w", err)
	}

	paths := make([]string, 0, 128)
	var total int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(relative, "..") {
			return errors.New("release path escaped source root")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("release contains unsupported file type: %s", filepath.ToSlash(relative))
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("release entry is group/world writable: %s", filepath.ToSlash(relative))
		}
		if info.IsDir() {
			return nil
		}
		if info.Size() < 0 || info.Size() > maximumSingleFile || total > maximumSourceBytes-info.Size() {
			return errors.New("release payload exceeds size limit")
		}
		total += info.Size()
		paths = append(paths, relative)
		if len(paths) > maximumSourceFiles {
			return errors.New("release payload exceeds file-count limit")
		}
		return nil
	})
	if err != nil {
		return Source{}, fmt.Errorf("inspect release tree: %w", err)
	}
	sort.Strings(paths)
	digest := sha256.New()
	for _, relative := range paths {
		content, err := readBoundedRegular(filepath.Join(root, relative), maximumSingleFile)
		if err != nil {
			return Source{}, err
		}
		_, _ = io.WriteString(digest, filepath.ToSlash(relative))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(content)
		_, _ = digest.Write([]byte{0})
	}
	return Source{Root: root, Version: version, Digest: hex.EncodeToString(digest.Sum(nil)), Manifest: manifest}, nil
}

func readBoundedRegular(path string, maximum int64) ([]byte, error) {
	cleaned := filepath.Clean(path)
	root, err := os.OpenRoot(filepath.Dir(cleaned))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(cleaned))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximum {
		return nil, errors.New("source entry is not a bounded regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(content)) > maximum {
		return nil, errors.New("read bounded source entry")
	}
	return content, nil
}

func inspectELF(path string) error {
	binary, err := elf.Open(path)
	if err != nil {
		return errors.New("release executable is not ELF")
	}
	defer binary.Close()
	wanted := elf.EM_X86_64
	if runtime.GOARCH == "arm64" {
		wanted = elf.EM_AARCH64
	}
	if binary.FileHeader.Class != elf.ELFCLASS64 || binary.FileHeader.Data != elf.ELFDATA2LSB ||
		binary.FileHeader.Machine != wanted || binary.FileHeader.Type != elf.ET_EXEC {
		return errors.New("release executable does not match the running Linux architecture")
	}
	for _, program := range binary.Progs {
		if program.Type == elf.PT_INTERP {
			return errors.New("release executable must be statically linked")
		}
	}
	return nil
}
