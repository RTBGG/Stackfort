// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostociimage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

const manifestSchema = 1

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type capabilityInspector interface {
	InspectOCIRuntime(context.Context) (agentprotocol.OCIRuntimeCapabilities, error)
}

type linuxManager struct {
	commands     commandRunner
	capabilities capabilityInspector
	transactions string
	artifacts    string
	scannerCache string
	stateUID     uint32
	stateGID     uint32
	chown        func(string, int, int) error
}

type replayManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	RequestDigest string          `json:"requestDigest"`
	Result        ociimage.Result `json:"result"`
}

func NewManager() Manager {
	return &linuxManager{
		commands: agentexec.NewRunner(), capabilities: hostcapabilities.NewInspector(),
		transactions: ociimage.TransactionRoot, artifacts: ociimage.ArtifactRoot,
		scannerCache: ociimage.ScannerCacheRoot, stateUID: 0, stateGID: 0, chown: os.Chown,
	}
}

func (manager *linuxManager) Prepare(
	ctx context.Context, operationID string, spec ociimage.PrepareSpec,
) (ociimage.Result, error) {
	if manager == nil || manager.commands == nil || manager.capabilities == nil || ociimage.ValidateSpec(spec) != nil {
		return ociimage.Result{}, ErrInvalid
	}
	if _, err := ociimage.TransactionDirectory(operationID); err != nil {
		return ociimage.Result{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(ociimage.PreparationTimeoutSeconds)*time.Second)
	defer cancel()
	transaction := filepath.Join(manager.transactions, operationID)
	requestDigest, err := ociimage.SemanticDigest(spec)
	if err != nil {
		return ociimage.Result{}, ErrInvalid
	}
	sourceDigest := ""
	if spec.Source.Kind == ociapps.SourceImageDigest {
		sourceDigest, _ = ociimage.RequestedDigest(spec.Source.ImageReference)
		if result, found, err := manager.loadManifest(spec, requestDigest); err != nil {
			return ociimage.Result{}, err
		} else if found {
			result.Reused = true
			return result, nil
		}
	}
	runtime, err := manager.capabilities.InspectOCIRuntime(ctx)
	if err != nil {
		return ociimage.Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-image-capability-inspection-failed",
		}}
	}
	if capability := firstUnavailableCapability(runtime); capability.Status != agentprotocol.CapabilityAvailable {
		return ociimage.Result{}, &CapabilityError{Capability: capability}
	}
	if manager.chown == nil {
		return ociimage.Result{}, ErrInvalid
	}
	if err := ensureOwnedDirectory(manager.transactions, manager.stateUID, manager.stateGID, 0o711); err != nil ||
		ensureOwnedDirectory(manager.artifacts, manager.stateUID, manager.stateGID, 0o700) != nil ||
		ensureOwnedDirectory(manager.scannerCache, manager.stateUID, manager.stateGID, 0o700) != nil {
		return ociimage.Result{}, ErrConflict
	}
	if err := os.Mkdir(transaction, 0o711); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ociimage.Result{}, ErrConflict
		}
		return ociimage.Result{}, ErrMutation
	}
	defer func() { _ = os.RemoveAll(transaction) }()
	if spec.Source.Kind == ociapps.SourceContainerfile {
		sourceDigest, err = snapshotBuildInputs(spec, transaction, manager.chown)
		if err != nil {
			return ociimage.Result{}, err
		}
		requestDigest = contextBoundRequestDigest(requestDigest, sourceDigest)
		if result, found, replayErr := manager.loadManifest(spec, requestDigest); replayErr != nil {
			return ociimage.Result{}, replayErr
		} else if found {
			result.Reused = true
			return result, nil
		}
	}
	values, _ := ociimage.InvocationValues(spec)
	profile := agentexec.ProfilePodmanPull
	invocationValues := values
	if spec.Source.Kind == ociapps.SourceContainerfile {
		profile = agentexec.ProfilePodmanBuild
		invocationValues = append(invocationValues, operationID)
	}
	if err := manager.run(ctx, profile, invocationValues); err != nil {
		if spec.Source.Kind == ociapps.SourceContainerfile {
			return ociimage.Result{}, ociimage.ErrBuildFailed
		}
		return ociimage.Result{}, ociimage.ErrPullFailed
	}
	retainImage := false
	defer func() {
		if retainImage {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		_ = manager.run(cleanupContext, agentexec.ProfilePodmanRemove, values)
	}()
	inspect, err := manager.commands.Run(ctx, agentexec.Invocation{Profile: agentexec.ProfilePodmanInspect, Values: values})
	imageDigest := strings.TrimSpace(inspect.Stdout)
	if err != nil || inspect.ExitCode != 0 || !ociimage.ValidDigest(imageDigest) {
		return ociimage.Result{}, ociimage.ErrInspectFailed
	}
	archive := filepath.Join(transaction, "image.tar")
	file, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- fixed basename in a UUIDv7 transaction directory.
	if err != nil || file.Close() != nil || manager.chown(archive, int(spec.Identity.UID), int(spec.Identity.GID)) != nil {
		return ociimage.Result{}, ErrMutation
	}
	if err := manager.run(ctx, agentexec.ProfilePodmanSave, append(values, operationID)); err != nil {
		return ociimage.Result{}, ociimage.ErrScanFailed
	}
	info, err := os.Lstat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 ||
		info.Size() > ociimage.MaximumImageArchiveBytes || manager.chown(archive, int(manager.stateUID), int(manager.stateGID)) != nil || os.Chmod(archive, 0o600) != nil {
		return ociimage.Result{}, ociimage.ErrScanFailed
	}
	scan, err := manager.commands.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfileTrivyScan, Values: []string{operationID},
	})
	if err != nil || scan.ExitCode != 0 {
		return ociimage.Result{}, ociimage.ErrScanFailed
	}
	summary, err := ociimage.ParseTrivyReport([]byte(scan.Stdout))
	if err != nil {
		return ociimage.Result{}, err
	}
	result := ociimage.Result{
		ImageDigest: imageDigest, SourceDigest: sourceDigest, PolicyVersion: ociimage.PolicyVersion,
		ScannerProvider: ociimage.ScannerProvider, ScannerVersion: runtime.ScannerVersion,
		Vulnerabilities: summary,
	}
	if err := ociimage.ValidateResult(result); err != nil {
		if errors.Is(err, ociimage.ErrScanRejected) {
			return ociimage.Result{}, ErrScanRejected
		}
		return ociimage.Result{}, ociimage.ErrScanFailed
	}
	if err := manager.storeManifest(spec, requestDigest, result); err != nil {
		return ociimage.Result{}, err
	}
	retainImage = true
	return result, nil
}

func (manager *linuxManager) run(ctx context.Context, profile agentexec.ProfileID, values []string) error {
	result, err := manager.commands.Run(ctx, agentexec.Invocation{Profile: profile, Values: values})
	if err != nil || result.ExitCode != 0 {
		return ErrMutation
	}
	return nil
}

func snapshotBuildInputs(
	spec ociimage.PrepareSpec,
	transaction string,
	chown func(string, int, int) error,
) (string, error) {
	fail := func() (string, error) { return "", ociimage.ErrBuildContext }
	if chown == nil {
		return fail()
	}
	account, err := os.OpenRoot(spec.Identity.HomeDirectory)
	if err != nil {
		return fail()
	}
	defer account.Close()
	containerSource, err := account.OpenFile(
		filepath.FromSlash(spec.Source.ContainerfilePath), os.O_RDONLY|syscall.O_NOFOLLOW, 0,
	)
	if err != nil {
		return fail()
	}
	containerInfo, statErr := containerSource.Stat()
	containerfile, readErr := io.ReadAll(io.LimitReader(containerSource, ociimage.MaximumContainerfileBytes+1))
	closeErr := containerSource.Close()
	if statErr != nil || !containerInfo.Mode().IsRegular() || containerInfo.Mode()&os.ModeSymlink != 0 ||
		containerInfo.Size() <= 0 || containerInfo.Size() > ociimage.MaximumContainerfileBytes ||
		readErr != nil || closeErr != nil || int64(len(containerfile)) != containerInfo.Size() ||
		ociimage.ValidateContainerfile(containerfile) != nil {
		return fail()
	}
	contextTarget := filepath.Join(transaction, "context")
	if err := os.Mkdir(contextTarget, 0o700); err != nil {
		return fail()
	}
	digest := sha256.New()
	_, _ = io.WriteString(digest, "stackfort-build-context-v1\x00containerfile\x00")
	_, _ = digest.Write(containerfile)
	_, _ = digest.Write([]byte{0})
	var files int
	var size int64
	contextPath := filepath.FromSlash(spec.Source.BuildContext)
	err = fs.WalkDir(account.FS(), contextPath, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := account.Lstat(name)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode()&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 {
			return ociimage.ErrBuildContext
		}
		relative, err := filepath.Rel(contextPath, name)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ociimage.ErrBuildContext
		}
		relative = filepath.ToSlash(relative)
		target := filepath.Join(contextTarget, filepath.FromSlash(relative))
		if info.IsDir() {
			_, _ = io.WriteString(digest, "directory\x00"+relative+"\x00")
			return os.MkdirAll(target, 0o700)
		}
		if !info.Mode().IsRegular() || info.Size() < 0 {
			return ociimage.ErrBuildContext
		}
		source, err := account.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		openedInfo, statErr := source.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || openedInfo.Size() < 0 {
			_ = source.Close()
			return ociimage.ErrBuildContext
		}
		files++
		size += openedInfo.Size()
		if files > ociimage.MaximumBuildContextFiles || size > ociimage.MaximumBuildContextBytes {
			_ = source.Close()
			return ociimage.ErrBuildContext
		}
		mode := os.FileMode(0o400)
		executable := "plain"
		if openedInfo.Mode().Perm()&0o111 != 0 {
			mode = 0o500
			executable = "executable"
		}
		_, _ = io.WriteString(digest, "file\x00"+relative+"\x00"+executable+"\x00")
		destination, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) // #nosec G304 -- target is confined to the new transaction snapshot.
		if err != nil {
			_ = source.Close()
			return err
		}
		written, copyErr := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(source, openedInfo.Size()+1))
		_, _ = digest.Write([]byte{0})
		sourceCloseErr := source.Close()
		closeErr := destination.Close()
		if copyErr != nil || sourceCloseErr != nil || closeErr != nil || written != openedInfo.Size() ||
			os.Chmod(target, mode) != nil {
			return ociimage.ErrBuildContext
		}
		return chown(target, int(spec.Identity.UID), int(spec.Identity.GID))
	})
	if err != nil || files == 0 {
		return fail()
	}
	containerTarget := filepath.Join(transaction, "Containerfile")
	if err := os.WriteFile(containerTarget, containerfile, 0o400); err != nil { // #nosec G306 -- immutable account-readable build input.
		return fail()
	}
	if err := os.Chmod(containerTarget, 0o400); err != nil ||
		chown(containerTarget, int(spec.Identity.UID), int(spec.Identity.GID)) != nil ||
		chownDirectories(contextTarget, spec.Identity.UID, spec.Identity.GID, chown) != nil {
		return fail()
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func contextBoundRequestDigest(specDigest, sourceDigest string) string {
	digest := sha256.Sum256([]byte(specDigest + "\x00" + sourceDigest))
	return hex.EncodeToString(digest[:])
}

func chownDirectories(root string, uid, gid uint32, chown func(string, int, int) error) error {
	return filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := chown(name, int(uid), int(gid)); err != nil {
				return err
			}
			return os.Chmod(name, 0o500)
		}
		return nil
	})
}

func (manager *linuxManager) manifestPath(spec ociimage.PrepareSpec) string {
	return filepath.Join(manager.artifacts, spec.Identity.AccountID, spec.ApplicationID, strconv.FormatInt(spec.Revision, 10)+".json")
}

func (manager *linuxManager) loadManifest(
	spec ociimage.PrepareSpec, requestDigest string,
) (ociimage.Result, bool, error) {
	target := manager.manifestPath(spec)
	info, metadataErr := os.Lstat(target)
	if errors.Is(metadataErr, os.ErrNotExist) {
		return ociimage.Result{}, false, nil
	}
	if metadataErr != nil {
		return ociimage.Result{}, false, ErrConflict
	}
	status, metadataOK := info.Sys().(*syscall.Stat_t)
	if !metadataOK || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || status.Uid != manager.stateUID || status.Gid != manager.stateGID || status.Nlink != 1 {
		return ociimage.Result{}, false, ErrConflict
	}
	content, err := readBoundedRegular(target, 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return ociimage.Result{}, false, nil
	}
	if err != nil {
		return ociimage.Result{}, false, ErrConflict
	}
	var manifest replayManifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.SchemaVersion != manifestSchema ||
		manifest.RequestDigest != requestDigest || ociimage.ValidateResult(manifest.Result) != nil {
		return ociimage.Result{}, false, ErrConflict
	}
	return manifest.Result, true, nil
}

func (manager *linuxManager) storeManifest(spec ociimage.PrepareSpec, requestDigest string, result ociimage.Result) error {
	applicationRoot := filepath.Dir(manager.manifestPath(spec))
	if err := ensureOwnedDirectory(filepath.Dir(applicationRoot), manager.stateUID, manager.stateGID, 0o700); err != nil ||
		ensureOwnedDirectory(applicationRoot, manager.stateUID, manager.stateGID, 0o700) != nil {
		return ErrConflict
	}
	content, err := json.Marshal(replayManifest{SchemaVersion: manifestSchema, RequestDigest: requestDigest, Result: result})
	if err != nil {
		return ErrMutation
	}
	content = append(content, '\n')
	target := manager.manifestPath(spec)
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- target components derive only from validated immutable IDs.
	if errors.Is(err, os.ErrExist) {
		loaded, found, loadErr := manager.loadManifest(spec, requestDigest)
		if loadErr == nil && found && loaded.ImageDigest == result.ImageDigest {
			return nil
		}
		return ErrConflict
	}
	if err != nil {
		return ErrMutation
	}
	if _, err := file.Write(content); err != nil || file.Sync() != nil || file.Close() != nil {
		_ = file.Close()
		_ = os.Remove(target)
		return ErrMutation
	}
	return nil
}

func ensureOwnedDirectory(path string, uid, gid uint32, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil { // #nosec G301 -- exact caller-internal state directory mode is verified below.
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode {
		return ErrConflict
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != uid || status.Gid != gid {
		return ErrConflict
	}
	return nil
}

func readBoundedRegular(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > maximum {
		return nil, ErrConflict
	}
	file, err := os.Open(path) // #nosec G304 -- callers provide fixed paths below validated state roots.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(content)) > maximum {
		return nil, fmt.Errorf("read bounded regular file")
	}
	return content, nil
}
