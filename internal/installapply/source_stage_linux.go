// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/releaseartifacts"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

const resumeSourceName = "resume-source"

// SourceStage retains the shared installation lock. The operator CLI opens only
// existing state. Staging, origin verification, boot coordination and installation
// continuation are separate explicit methods. Close only after the transaction.
type SourceStage struct {
	journal *storageprep.FileStore
	dir     int
	closed  bool
}

func OpenSourceStage() (*SourceStage, error) {
	journal, err := storageprep.OpenFileStore()
	if err != nil {
		return nil, err
	}
	dir, err := openSourceDirectory(DefaultJournalDirectory, false)
	if err != nil {
		_ = journal.Close()
		return nil, err
	}
	return &SourceStage{journal: journal, dir: dir}, nil
}

func openExistingSourceStage() (*SourceStage, bool, error) {
	journal, exists, err := storageprep.OpenExistingFileStore()
	if err != nil || !exists {
		return nil, false, err
	}
	dir, err := openSourceDirectory(DefaultJournalDirectory, false)
	if err != nil {
		_ = journal.Close()
		return nil, false, err
	}
	return &SourceStage{journal: journal, dir: dir}, true, nil
}

func (stage *SourceStage) Close() error {
	if stage == nil || stage.closed {
		return nil
	}
	stage.closed = true
	var lockErr error
	if stage.journal != nil {
		lockErr = stage.journal.Close()
	}
	return errors.Join(unix.Close(stage.dir), lockErr)
}

func (stage *SourceStage) check() error {
	if stage == nil || stage.closed {
		return errors.New("resume source stage is closed")
	}
	return nil
}

// Prepare must run before a native storage journal exists. Its input must have
// already passed the bootstrap's release-origin checks. Ownership and hashes
// alone cannot establish provenance. No network or attestation fallback exists.
// A failed/partial directory is preserved and never overwritten or auto-repaired.
func (stage *SourceStage) Prepare(ctx context.Context, root, operationID string) (SourcePin, error) {
	if err := stage.check(); err != nil {
		return SourcePin{}, err
	}
	if stage.journal != nil {
		if _, exists, err := stage.journal.Load(); err != nil || exists {
			return SourcePin{}, errors.Join(err, errors.New("release staging must precede storage preparation"))
		}
	}
	var installed unix.Stat_t
	if err := unix.Fstatat(stage.dir, "install-state.json", &installed, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return SourcePin{}, errors.Join(err, errors.New("release staging must precede package installation"))
	}
	source, pin, err := inspectResumeSource(ctx, root, operationID)
	if err != nil {
		return SourcePin{}, err
	}
	if err := ctx.Err(); err != nil {
		return SourcePin{}, err
	}
	if err := unix.Mkdirat(stage.dir, resumeSourceName, 0o700); errors.Is(err, unix.EEXIST) {
		_, err := stage.Verify(ctx, pin)
		return pin, err
	} else if err != nil {
		return SourcePin{}, err
	}
	// Persist an incomplete stage before copying. After any interruption, absence
	// of the final canonical receipt blocks use; there is no blind recopy.
	if err := unix.Fsync(stage.dir); err != nil {
		return SourcePin{}, err
	}
	dir, err := openPrivateChild(stage.dir, resumeSourceName)
	if err != nil {
		return SourcePin{}, err
	}
	defer unix.Close(dir)
	if err := unix.Mkdirat(dir, "source", 0o700); err != nil {
		return SourcePin{}, err
	}
	destination, err := openPrivateChild(dir, "source")
	if err != nil {
		return SourcePin{}, err
	}
	defer unix.Close(destination)
	origin, err := openSourceDirectory(source.Root, true)
	if err != nil {
		return SourcePin{}, err
	}
	defer unix.Close(origin)
	if _, err := scanSourceTree(ctx, origin, destination); err != nil {
		return SourcePin{}, err
	}
	// Resolve the held private directory for the existing path-based inspector.
	// Its ancestors are root-controlled; this is not protection against hostile
	// root processes swapping paths while verification runs.
	stagedRoot, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", destination))
	if err != nil {
		return SourcePin{}, err
	}
	_, copied, err := inspectResumeSource(ctx, stagedRoot, operationID)
	if err != nil || copied != pin {
		return SourcePin{}, errors.Join(err, errors.New("release changed while staging"))
	}
	if err := ctx.Err(); err != nil {
		return SourcePin{}, err
	}
	content, err := pin.encode()
	if err != nil {
		return SourcePin{}, err
	}
	fd, err := unix.Openat(dir, "receipt.json", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return SourcePin{}, err
	}
	file := os.NewFile(uintptr(fd), "receipt.json")
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		return SourcePin{}, err
	}
	if err := file.Sync(); err != nil {
		return SourcePin{}, err
	}
	if err := file.Close(); err != nil {
		return SourcePin{}, err
	}
	if err := unix.Fsync(dir); err != nil {
		return SourcePin{}, err
	}
	return pin, nil
}

// Verify requires an independently pinned expectation and rehashes every file.
// It does not download, repair, delete, run the installer or change any journal.
func (stage *SourceStage) Verify(ctx context.Context, expected SourcePin) (Source, error) {
	if err := stage.check(); err != nil {
		return Source{}, err
	}
	if err := expected.Validate(); err != nil {
		return Source{}, err
	}
	dir, err := openPrivateChild(stage.dir, resumeSourceName)
	if err != nil {
		return Source{}, err
	}
	defer unix.Close(dir)
	file, err := openResumeFile(dir, "receipt.json", maximumSourcePinBytes)
	if err != nil {
		return Source{}, err
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil || stat.Mode != unix.S_IFREG|0o600 {
		return Source{}, errors.New("resume source receipt has unsafe permissions")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumSourcePinBytes+1))
	if err != nil {
		return Source{}, err
	}
	pin, err := decodeSourcePin(content)
	if err != nil || pin != expected {
		return Source{}, errors.Join(err, errors.New("resume source receipt differs from pinned expectation"))
	}
	sourceDir, err := openPrivateChild(dir, "source")
	if err != nil {
		return Source{}, err
	}
	defer unix.Close(sourceDir)
	root, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", sourceDir))
	if err != nil {
		return Source{}, err
	}
	source, actual, err := inspectResumeSource(ctx, root, pin.OperationID)
	if err != nil || actual != pin {
		return Source{}, errors.Join(err, errors.New("staged release integrity check failed"))
	}
	return source, nil
}

func (stage *SourceStage) VerifyForPlan(ctx context.Context, plan storageprep.Plan, pin SourcePin) (Source, error) {
	if err := pin.ValidatePlan(plan); err != nil {
		return Source{}, err
	}
	return stage.Verify(ctx, pin)
}

func inspectResumeSource(ctx context.Context, root, operationID string) (Source, SourcePin, error) {
	if ctx == nil {
		return Source{}, SourcePin{}, errors.New("resume source requires a context")
	}
	// Validate the operation before reading an arbitrary source or making state.
	if !validSourceOperation(operationID) {
		return Source{}, SourcePin{}, errors.New("invalid resume source operation")
	}
	dir, err := openSourceDirectory(root, true)
	if err != nil {
		return Source{}, SourcePin{}, err
	}
	defer unix.Close(dir)
	tree, err := scanSourceTree(ctx, dir, -1)
	if err != nil {
		return Source{}, SourcePin{}, err
	}
	source, err := InspectSource(root)
	if err != nil {
		return Source{}, SourcePin{}, err
	}
	if err := ValidateSourceTrust(source); err != nil {
		return Source{}, SourcePin{}, err
	}
	installer := filepath.Join(root, "bin", "stackfort-installer")
	if err := inspectELF(installer); err != nil {
		return Source{}, SourcePin{}, fmt.Errorf("resume installer: %w", err)
	}
	installerInfo, err := os.Lstat(installer)
	if err != nil || installerInfo.Mode().Perm()&0o100 == 0 {
		return Source{}, SourcePin{}, errors.New("resume installer is not owner-executable")
	}
	digests := make([]string, 2)
	for index, relative := range []string{"bin/stackfort-installer", releaseartifacts.ManifestFilename} {
		content, err := readBoundedRegular(filepath.Join(root, relative), maximumSingleFile)
		if err != nil {
			return Source{}, SourcePin{}, err
		}
		digest := sha256.Sum256(content)
		digests[index] = hex.EncodeToString(digest[:])
	}
	pin := SourcePin{1, operationID, source.Version, source.Digest, tree, digests[0], digests[1]}
	return source, pin, pin.Validate()
}

// Only root-controlled ancestors are accepted. The two standard root-owned
// sticky temporary directories are allowed for a bootstrap input, never output.
func openSourceDirectory(path string, temporaryInput bool) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return -1, errors.New("resume source path is not canonical")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	current := ""
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if err != nil {
			return -1, err
		}
		fd = next
		current += "/" + component
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			_ = unix.Close(fd)
			return -1, err
		}
		sticky := temporaryInput && current != path && (current == "/tmp" || current == "/var/tmp") && stat.Mode&unix.S_ISVTX != 0
		if stat.Uid != 0 || stat.Gid != 0 || (stat.Mode&0o022 != 0 && !sticky) {
			_ = unix.Close(fd)
			return -1, errors.New("resume source has unsafe ancestors")
		}
	}
	return fd, nil
}

func openPrivateChild(parent int, name string) (int, error) {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Uid != 0 || stat.Gid != 0 || stat.Mode != unix.S_IFDIR|0o700 {
		_ = unix.Close(fd)
		return -1, errors.New("resume source directory has unsafe metadata")
	}
	return fd, nil
}

func openResumeFile(parent int, name string, maximum int64) (*os.File, error) {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o7022 != 0 || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || stat.Size < 0 || stat.Size > maximum {
		_ = unix.Close(fd)
		return nil, errors.New("resume source file has unsafe metadata")
	}
	return os.NewFile(uintptr(fd), name), nil
}

type sourceScan struct {
	entries int
	bytes   int64
	digest  hash.Hash
	device  uint64
}

func scanSourceTree(ctx context.Context, root, destination int) (string, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(root, &stat); err != nil {
		return "", err
	}
	scan := sourceScan{digest: sha256.New(), device: stat.Dev}
	if err := scan.walk(ctx, root, destination, "", 0); err != nil {
		return "", err
	}
	return hex.EncodeToString(scan.digest.Sum(nil)), nil
}

func (scan *sourceScan) walk(ctx context.Context, dir, destination int, prefix string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 64 {
		return errors.New("resume source exceeds directory depth limit")
	}
	// Opening '.' gives an independent directory offset for repeat verification.
	fd, err := unix.Openat(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), ".")
	names, err := file.Readdirnames(maximumSourceFiles*2 + 1)
	_ = file.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		scan.entries++
		if scan.entries > maximumSourceFiles*2 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\r\n\x00") {
			return errors.New("resume source exceeds entry limit or has unsafe names")
		}
		var before unix.Stat_t
		if err := unix.Fstatat(dir, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if before.Uid != 0 || before.Gid != 0 || before.Mode&0o7022 != 0 || before.Dev != scan.device {
			return errors.New("resume source entry ownership, permissions or device is unsafe")
		}
		relative := prefix + name
		mode := before.Mode & 0o777
		switch before.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			child, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			destChild := -1
			if destination >= 0 {
				if err := unix.Mkdirat(destination, name, 0o700); err != nil {
					_ = unix.Close(child)
					return err
				}
				destChild, err = unix.Openat(destination, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if err != nil {
					_ = unix.Close(child)
					return err
				}
			}
			_, _ = fmt.Fprintf(scan.digest, "d\x00%s\x00%o\n", relative, mode)
			err = scan.walk(ctx, child, destChild, relative+"/", depth+1)
			_ = unix.Close(child)
			if destChild >= 0 {
				if err == nil {
					err = unix.Fchmod(destChild, mode)
				}
				if err == nil {
					err = unix.Fsync(destChild)
				}
				_ = unix.Close(destChild)
			}
			if err != nil {
				return err
			}
		case unix.S_IFREG:
			if err := scan.copyFile(dir, destination, name, relative, mode, before); err != nil {
				return err
			}
		default:
			return errors.New("resume source contains a link or special file")
		}
	}
	if destination >= 0 {
		return unix.Fsync(destination)
	}
	return nil
}

func (scan *sourceScan) copyFile(dir, destination int, name, relative string, mode uint32, before unix.Stat_t) error {
	file, err := openResumeFile(dir, name, maximumSingleFile)
	if err != nil {
		return err
	}
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &opened); err != nil || opened != before {
		return errors.New("resume source changed before reading")
	}
	if scan.bytes > maximumSourceBytes-before.Size {
		return errors.New("resume source exceeds byte limit")
	}
	scan.bytes += before.Size
	digest := sha256.New()
	var writer io.Writer = digest
	var target *os.File
	if destination >= 0 {
		fd, err := unix.Openat(destination, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if err != nil {
			return err
		}
		target = os.NewFile(uintptr(fd), name)
		defer target.Close()
		writer = io.MultiWriter(digest, target)
	}
	written, err := io.Copy(writer, io.LimitReader(file, before.Size+1))
	if err != nil || written != before.Size {
		return errors.Join(err, errors.New("resume source size changed"))
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &after); err != nil || before.Ino != after.Ino ||
		before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return errors.New("resume source changed while reading")
	}
	if target != nil {
		if err := unix.Fchmod(int(target.Fd()), mode); err != nil {
			return err
		}
		if err := target.Sync(); err != nil {
			return err
		}
		if err := target.Close(); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(scan.digest, "f\x00%s\x00%o\x00%d\x00%x\n", relative, mode, written, digest.Sum(nil))
	return nil
}
