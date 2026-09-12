// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

const nativeBootBuildName = "native-boot-build"
const nativeBootConfigSource = "/etc/initramfs-tools"
const nativeBootConfigMaximumFile = 4 << 20
const nativeBootConfigMaximumBytes = 16 << 20
const nativeBootConfigMaximumEntries = 128

// Only the local configuration is copied. Debian's mkinitramfs still runs its
// ordinary /usr/share hooks and reads installed kernel modules under the caller's
// package guard. This is not a hermetic build or a persistent boot-writer fence.
type nativeBootConfigEntry struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Dir    bool   `json:"directory,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	data   []byte
}

type nativeBootBuildReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	OperationID   string `json:"operationID"`
	Manifest      string `json:"manifest"`
	Kernel        string `json:"kernel"`
	SourceSHA256  string `json:"sourceSHA256"`
	ConfigSHA256  string `json:"configSHA256"`
}

type nativeBootBuild struct {
	path    string
	receipt nativeBootBuildReceipt
}

func nativeBootConfigName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 100 {
		return false
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

func nativeBootConfigSnapshot(ctx context.Context, root int) ([]nativeBootConfigEntry, error) {
	var entries []nativeBootConfigEntry
	var bytes int64
	var initial unix.Stat_t
	if ctx == nil || ctx.Err() != nil || unix.Fstat(root, &initial) != nil {
		return nil, errors.New("invalid native initramfs configuration source")
	}
	var walk func(int, string, int) error
	walk = func(dir int, prefix string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var before unix.Stat_t
		if err := unix.Fstat(dir, &before); err != nil || before.Uid != 0 || before.Gid != 0 || before.Mode&unix.S_IFMT != unix.S_IFDIR || before.Mode&07022 != 0 || before.Dev != initial.Dev || depth > 8 {
			return errors.New("unsafe native initramfs configuration directory")
		}
		fd, err := unix.Openat(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		listing := os.NewFile(uintptr(fd), ".")
		names, err := listing.Readdirnames(nativeBootConfigMaximumEntries + 1)
		_ = listing.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		sort.Strings(names)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			relative := prefix + name
			if !nativeBootConfigName(name) || len(relative) > 240 || len(entries) >= nativeBootConfigMaximumEntries {
				return errors.New("native initramfs configuration name or entry limit")
			}
			var stat unix.Stat_t
			if err := unix.Fstatat(dir, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&07022 != 0 || stat.Dev != initial.Dev {
				return errors.New("unsafe native initramfs configuration metadata")
			}
			entry := nativeBootConfigEntry{Path: relative, Mode: stat.Mode & 0777}
			switch stat.Mode & unix.S_IFMT {
			case unix.S_IFDIR:
				entry.Dir = true
				entries = append(entries, entry)
				child, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if err != nil {
					return err
				}
				var opened unix.Stat_t
				if err := unix.Fstat(child, &opened); err != nil || opened.Dev != stat.Dev || opened.Ino != stat.Ino {
					_ = unix.Close(child)
					return errors.New("native initramfs configuration directory changed")
				}
				err = walk(child, relative+"/", depth+1)
				_ = unix.Close(child)
				if err != nil {
					return err
				}
			case unix.S_IFREG:
				file, err := openResumeFile(dir, name, nativeBootConfigMaximumFile)
				if err != nil {
					return err
				}
				var opened, after unix.Stat_t
				if err := unix.Fstat(int(file.Fd()), &opened); err != nil || opened != stat || bytes > nativeBootConfigMaximumBytes-stat.Size {
					_ = file.Close()
					return errors.New("native initramfs configuration changed or exceeded byte limit")
				}
				entry.data, err = io.ReadAll(io.LimitReader(file, stat.Size+1))
				statErr := unix.Fstat(int(file.Fd()), &after)
				_ = file.Close()
				if err != nil || statErr != nil || int64(len(entry.data)) != stat.Size || after.Ino != stat.Ino || after.Size != stat.Size || after.Mtim != stat.Mtim || after.Ctim != stat.Ctim {
					return errors.New("native initramfs configuration file changed while reading")
				}
				bytes += stat.Size
				entry.SHA256 = admissionDigest(entry.data)
				entries = append(entries, entry)
			default:
				return errors.New("native initramfs configuration contains a link or special file")
			}
		}
		var after unix.Stat_t
		if err := unix.Fstat(dir, &after); err != nil || before.Ino != after.Ino || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
			return errors.New("native initramfs configuration directory changed while reading")
		}
		return nil
	}
	if err := walk(root, "", 0); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func nativeBootConfigScripts(entries []nativeBootConfigEntry, operation string, guarded bool) ([]nativeBootConfigEntry, error) {
	hook, premount, err := nativeBootScripts(operation)
	if guarded {
		hook, premount, err = nativeBootGuardedScripts(operation)
	}
	if err != nil {
		return nil, err
	}
	entries = slices.Clone(entries)
	if !slices.ContainsFunc(entries, func(entry nativeBootConfigEntry) bool { return entry.Path == "initramfs.conf" && !entry.Dir }) {
		return nil, errors.New("missing ordinary initramfs configuration")
	}
	for _, path := range []string{"hooks", "scripts", "scripts/local-premount"} {
		index := slices.IndexFunc(entries, func(entry nativeBootConfigEntry) bool { return entry.Path == path })
		if index >= 0 && !entries[index].Dir {
			return nil, errors.New("conflicting native initramfs configuration directory")
		}
		if index < 0 {
			entries = append(entries, nativeBootConfigEntry{Path: path, Dir: true, Mode: 0755})
		}
	}
	for path, content := range map[string]string{"hooks/stackfort-native-quota": hook, "scripts/local-premount/stackfort-native-quota": premount} {
		if slices.ContainsFunc(entries, func(entry nativeBootConfigEntry) bool { return entry.Path == path }) {
			return nil, errors.New("existing native initramfs hook cannot be adopted")
		}
		entries = append(entries, nativeBootConfigEntry{Path: path, Mode: 0755, SHA256: admissionDigest([]byte(content)), data: []byte(content)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	if len(entries) > nativeBootConfigMaximumEntries {
		return nil, errors.New("native initramfs configuration entry limit")
	}
	return entries, nil
}

func prepareNativeBootBuild(ctx context.Context, stage *SourceStage, plan storageprep.Plan, intent NativeRuntimeIntent) (*nativeBootBuild, error) {
	if err := stage.check(); err != nil {
		return nil, err
	}
	if ctx == nil || ctx.Err() != nil || stage.journal == nil || plan.Validate() != nil || intent.Offline == nil {
		return nil, errors.New("private initramfs build requires locked native boot state")
	}
	source, err := openSourceDirectory(nativeBootConfigSource, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(source)
	entries, err := nativeBootConfigSnapshot(ctx, source)
	if err != nil {
		return nil, err
	}
	receipt := nativeBootBuildReceipt{SchemaVersion: 1, OperationID: plan.OperationID, Manifest: plan.ManifestDigest, Kernel: plan.Kernel, SourceSHA256: admissionDigest(nativeBootJSON(entries))}
	entries, err = nativeBootConfigScripts(entries, plan.OperationID, intent.Offline.PowerLossGuard)
	if err != nil {
		return nil, err
	}
	receipt.ConfigSHA256 = admissionDigest(nativeBootJSON(entries))
	if err := unix.Mkdirat(stage.dir, nativeBootBuildName, 0700); err != nil {
		return nil, err // Preserve existing or interrupted builds; no reuse/reset.
	}
	if err := unix.Fsync(stage.dir); err != nil {
		return nil, err
	}
	dir, err := openPrivateChild(stage.dir, nativeBootBuildName)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	if err := writeOriginRecord(dir, "intent.json", nativeBootJSON(receipt)); err != nil {
		return nil, err
	}
	if err := unix.Mkdirat(dir, "config", 0700); err != nil {
		return nil, err
	}
	config, err := openPrivateChild(dir, "config")
	if err != nil {
		return nil, err
	}
	defer unix.Close(config)
	if err := nativeBootConfigWrite(ctx, config, entries); err != nil {
		return nil, err
	}
	if err := unix.Fsync(dir); err != nil {
		return nil, err
	}
	build := &nativeBootBuild{path: filepath.Join(DefaultJournalDirectory, nativeBootBuildName), receipt: receipt}
	if err := build.verify(ctx); err != nil {
		return nil, err
	}
	return build, nil
}

func nativeBootConfigWrite(ctx context.Context, root int, entries []nativeBootConfigEntry) error {
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Path == "" || len(entry.Path) > 240 || entry.Mode&^uint32(0777) != 0 || entry.Mode&0022 != 0 || len(entry.data) > nativeBootConfigMaximumFile || (entry.Dir && (entry.SHA256 != "" || len(entry.data) != 0)) || (!entry.Dir && admissionDigest(entry.data) != entry.SHA256) {
			return errors.New("invalid private initramfs configuration entry")
		}
		parts := strings.Split(entry.Path, "/")
		for _, part := range parts {
			if !nativeBootConfigName(part) {
				return errors.New("invalid private initramfs configuration path")
			}
		}
		// Entries come only from the validated snapshot and fixed script renderer.
		// Walk each parent with no-follow descriptors; never use a relative path
		// containing slashes as an openat target.
		parent, err := unix.Openat(root, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		for _, part := range parts[:len(parts)-1] {
			next, nextErr := unix.Openat(parent, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			_ = unix.Close(parent)
			parent, err = next, nextErr
			if err != nil {
				return err
			}
		}
		name := parts[len(parts)-1]
		if entry.Dir {
			err = unix.Mkdirat(parent, name, entry.Mode)
			if err == nil {
				child, openErr := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				err = openErr
				if err == nil {
					err = unix.Fchmod(child, entry.Mode)
					if err == nil {
						err = unix.Fsync(child)
					}
					_ = unix.Close(child)
				}
			}
		} else {
			var fd int
			fd, err = unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
			if err == nil {
				file := os.NewFile(uintptr(fd), name)
				_, err = file.Write(entry.data)
				if err == nil {
					err = file.Chmod(os.FileMode(entry.Mode))
				}
				if err == nil {
					err = file.Sync()
				}
				err = errors.Join(err, file.Close())
			}
		}
		if err == nil {
			err = unix.Fsync(parent)
		}
		_ = unix.Close(parent)
		if err != nil {
			return err
		}
	}
	return unix.Fsync(root)
}

func (build *nativeBootBuild) verify(ctx context.Context) error {
	if build == nil || build.path != filepath.Join(DefaultJournalDirectory, nativeBootBuildName) {
		return errors.New("invalid private initramfs build")
	}
	dir, err := openSourceDirectory(build.path, false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	if err := equalOriginRecord(dir, "intent.json", nativeBootJSON(build.receipt)); err != nil {
		return err
	}
	for path, expected := range map[string]string{nativeBootConfigSource: build.receipt.SourceSHA256, filepath.Join(build.path, "config"): build.receipt.ConfigSHA256} {
		root, err := openSourceDirectory(path, false)
		if err != nil {
			return err
		}
		entries, err := nativeBootConfigSnapshot(ctx, root)
		_ = unix.Close(root)
		if err != nil || admissionDigest(nativeBootJSON(entries)) != expected {
			return errors.Join(err, errors.New("native initramfs build configuration drift: "+path))
		}
	}
	return nil
}
