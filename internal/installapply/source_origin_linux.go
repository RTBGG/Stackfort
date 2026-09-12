// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const originDirectory = "resume-origin"
const maximumOriginBundle = 16 << 20

// Includes file data plus headers, PAX/GNU metadata, padding and the trailer.
// Bounding returned tar.Header sizes alone does not bound metadata read by Next.
const maximumOriginTarBytes = maximumSourceBytes + (64 << 20)

// BindOrigin preserves the attested archive and bundle, verifies the actual
// signature with an independently hash-pinned verifier, and compares every
// archive file to the staged source. An incomplete origin directory is retained.
// No boolean supplied by a caller or a saved gh output can authorize a binding.
func (stage *SourceStage) BindOrigin(ctx context.Context, pin SourcePin, policy OriginPolicy, archivePath, bundlePath string) (ReleaseBinding, error) {
	if err := policy.Validate(); err != nil {
		return ReleaseBinding{}, err
	}
	if pin.Version != policy.Version {
		return ReleaseBinding{}, errors.New("origin version differs from source")
	}
	source, err := stage.Verify(ctx, pin)
	if err != nil {
		return ReleaseBinding{}, err
	}
	if stage.journal != nil {
		if _, exists, err := stage.journal.Load(); err != nil || exists {
			return ReleaseBinding{}, errors.Join(err, errors.New("origin verification must precede storage preparation"))
		}
	}
	if err := unix.Mkdirat(stage.dir, originDirectory, 0o700); err != nil {
		return ReleaseBinding{}, err
	}
	if err := unix.Fsync(stage.dir); err != nil {
		return ReleaseBinding{}, err
	}
	dir, err := openPrivateChild(stage.dir, originDirectory)
	if err != nil {
		return ReleaseBinding{}, err
	}
	defer unix.Close(dir)
	archiveDigest, err := copyOriginFile(dir, "archive.tar.gz", archivePath, maximumSourceBytes)
	if err != nil {
		return ReleaseBinding{}, err
	}
	bundleDigest, err := copyOriginFile(dir, "attestations.jsonl", bundlePath, maximumOriginBundle)
	if err != nil {
		return ReleaseBinding{}, err
	}
	root, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", dir))
	if err != nil {
		return ReleaseBinding{}, err
	}
	if err := verifyOriginCommand(ctx, source.Root, root, policy); err != nil {
		return ReleaseBinding{}, err
	}
	if err := archiveMatchesSource(ctx, filepath.Join(root, "archive.tar.gz"), source); err != nil {
		return ReleaseBinding{}, err
	}
	commit, err := readBoundedRegular(filepath.Join(source.Root, "COMMIT"), 128)
	if err != nil || strings.TrimSpace(string(commit)) != policy.Commit {
		return ReleaseBinding{}, errors.New("source COMMIT differs from attested identity")
	}
	binding := ReleaseBinding{1, pin, policy, archiveDigest, bundleDigest, originVerifierSHA256}
	if err := binding.Validate(); err != nil {
		return ReleaseBinding{}, err
	}
	data, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return ReleaseBinding{}, err
	}
	if err := writeOriginRecord(dir, "receipt.json", append(data, '\n')); err != nil {
		return ReleaseBinding{}, err
	}
	return binding, nil
}

// VerifyBinding checks the retained evidence against the manifest-bound receipt.
// It does not rerun online trust-root discovery after reboot: the successful
// initial signature check is sealed by private durable state and its digest.
func (stage *SourceStage) VerifyBinding(ctx context.Context, expected ReleaseBinding) (Source, error) {
	if err := expected.Validate(); err != nil {
		return Source{}, err
	}
	source, err := stage.Verify(ctx, expected.Source)
	if err != nil {
		return Source{}, err
	}
	dir, err := openPrivateChild(stage.dir, originDirectory)
	if err != nil {
		return Source{}, err
	}
	defer unix.Close(dir)
	want, _ := json.MarshalIndent(expected, "", "  ")
	if err := equalOriginRecord(dir, "receipt.json", append(want, '\n')); err != nil {
		return Source{}, err
	}
	for name, wanted := range map[string]string{"archive.tar.gz": expected.ArchiveSHA256, "attestations.jsonl": expected.BundleSHA256} {
		limit := int64(maximumSourceBytes)
		if name == "attestations.jsonl" {
			limit = maximumOriginBundle
		}
		file, err := openResumeFile(dir, name, limit)
		if err != nil {
			return Source{}, err
		}
		digest, err := originFileDigest(file, limit)
		_ = file.Close()
		if err != nil || digest != wanted {
			return Source{}, errors.Join(err, errors.New("retained origin evidence changed"))
		}
	}
	return source, nil
}

func copyOriginFile(dir int, name, input string, maximum int64) (string, error) {
	parent, err := openSourceDirectory(filepath.Dir(input), true)
	if err != nil {
		return "", err
	}
	defer unix.Close(parent)
	file, err := openResumeFile(parent, filepath.Base(input), maximum)
	if err != nil {
		return "", err
	}
	defer file.Close()
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", err
	}
	target := os.NewFile(uintptr(fd), name)
	defer target.Close()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(target, digest), io.LimitReader(file, maximum+1))
	if err != nil || written < 1 || written > maximum {
		return "", errors.Join(err, errors.New("origin evidence exceeds bounds"))
	}
	if err := target.Sync(); err != nil {
		return "", err
	}
	if err := target.Close(); err != nil {
		return "", err
	}
	if err := unix.Fsync(dir); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func originFileDigest(file *os.File, maximum int64) (string, error) {
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, maximum+1))
	if err != nil || n < 1 || n > maximum {
		return "", errors.Join(err, errors.New("origin evidence exceeds bounds"))
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func writeOriginRecord(dir int, name string, data []byte) error {
	if len(data) > 64<<10 {
		return errors.New("origin record exceeds limit")
	}
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return unix.Fsync(dir)
}

func equalOriginRecord(dir int, name string, expected []byte) error {
	file, err := openResumeFile(dir, name, 64<<10)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Mode().Perm() != 0o600 {
		return errors.New("unsafe origin record permissions")
	}
	actual, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || !bytes.Equal(actual, expected) {
		return errors.Join(err, errors.New("origin record differs from canonical manifest"))
	}
	return nil
}

// Do not embed bytes.Buffer: its promoted ReadFrom/WriteString would let
// io.Copy/io.WriteString bypass the limit enforced by Write.
type boundedOriginOutput struct{ buffer bytes.Buffer }

func (output *boundedOriginOutput) Write(data []byte) (int, error) {
	if output.buffer.Len()+len(data) > 64<<10 {
		return 0, errors.New("attestation verifier output exceeds limit")
	}
	return output.buffer.Write(data)
}

func (output *boundedOriginOutput) Bytes() []byte  { return output.buffer.Bytes() }
func (output *boundedOriginOutput) String() string { return output.buffer.String() }

func verifyOriginCommand(ctx context.Context, sourceRoot, evidenceRoot string, policy OriginPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(sourceRoot) || filepath.Clean(sourceRoot) != sourceRoot {
		return errors.New("attestation source path is not canonical")
	}
	evidence, err := openSourceDirectory(evidenceRoot, false)
	if err != nil {
		return err
	}
	defer unix.Close(evidence)
	verifierPath := filepath.Join(sourceRoot, "bin/stackfort-gh")
	parent, err := openSourceDirectory(filepath.Dir(verifierPath), false)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	file, err := openResumeFile(parent, "stackfort-gh", 64<<20)
	if err != nil {
		return err
	}
	digest, err := originFileDigest(file, 64<<20)
	_ = file.Close()
	if err != nil || digest != originVerifierSHA256 {
		return errors.New("attestation verifier does not match the independent upstream pin")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// No shell, inherited credentials/configuration or caller-supplied options.
	// #nosec G204 -- Verifier is independently SHA-256 pinned after no-follow root-owned directory/file checks; paths are canonical absolute, identity/options follow the closed validated OriginPolicy, and the environment is fixed.
	command := exec.CommandContext(ctx, verifierPath, "attestation", "verify", filepath.Join(evidenceRoot, "archive.tar.gz"),
		"--bundle", filepath.Join(evidenceRoot, "attestations.jsonl"), "--repo", originRepository,
		"--source-ref", policy.sourceRef(), "--source-digest", policy.Commit, "--signer-digest", policy.Commit,
		"--cert-identity", "https://github.com/"+originWorkflow+"@"+policy.sourceRef(),
		"--cert-oidc-issuer", "https://token.actions.githubusercontent.com", "--deny-self-hosted-runners", "--format=json")
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "HOME=" + evidenceRoot,
		"XDG_CACHE_HOME=" + evidenceRoot + "/cache", "XDG_CONFIG_HOME=" + evidenceRoot + "/config", "GH_PROMPT_DISABLED=1", "NO_COLOR=1"}
	var stdout, stderr boundedOriginOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("release attestation rejected: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var results []json.RawMessage
	if json.Unmarshal(stdout.Bytes(), &results) != nil || len(results) == 0 {
		return errors.New("empty or invalid attestation verification result")
	}
	return nil
}

// Comparing the archive to the actual source avoids authenticating one archive
// while staging another caller-supplied extraction. No archive member is executed.
func archiveMatchesSource(ctx context.Context, archivePath string, source Source) error {
	if !filepath.IsAbs(archivePath) || filepath.Clean(archivePath) != archivePath {
		return errors.New("origin archive path is not canonical")
	}
	parent, err := openSourceDirectory(filepath.Dir(archivePath), true)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	file, err := openResumeFile(parent, filepath.Base(archivePath), maximumSourceBytes)
	if err != nil {
		return err
	}
	defer file.Close()
	return matchOriginArchive(ctx, file, source, maximumOriginTarBytes)
}

type originArchiveContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader originArchiveContextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

// The production caller always uses maximumOriginTarBytes. A smaller private
// limit lets tests prove metadata/padding exhaustion without allocating a bomb.
func matchOriginArchive(ctx context.Context, compressed io.Reader, source Source, maximum int64) error {
	if ctx == nil || maximum <= 0 || maximum > maximumOriginTarBytes {
		return errors.New("invalid archive verification limits")
	}
	buffered := bufio.NewReader(originArchiveContextReader{ctx, compressed})
	gz, err := gzip.NewReader(buffered)
	if err != nil {
		return err
	}
	defer gz.Close()
	// Retain the byte-reader position after one gzip member so concatenated
	// streams or compressed suffixes cannot be silently omitted from comparison.
	gz.Multistream(false)
	bounded := &io.LimitedReader{R: originArchiveContextReader{ctx, gz}, N: maximum + 1}
	reader := tar.NewReader(bounded)
	top := "stackfort-" + source.Version + "-linux-amd64"
	seen, files := make(map[string]bool), make(map[string]bool)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n") ||
			(name != top && !strings.HasPrefix(name, top+"/")) || seen[name] || len(seen) >= maximumSourceFiles*2 || header.Mode < 0 || header.Mode > 0o777 || header.Mode&0o022 != 0 {
			return errors.New("unsafe or duplicate archive member")
		}
		seen[name] = true
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || name == top || header.Size < 0 || header.Size > maximumSingleFile || total > maximumSourceBytes-header.Size {
			return errors.New("unsupported archive member or size")
		}
		total += header.Size
		relative := strings.TrimPrefix(name, top+"/")
		local, err := readBoundedRegular(filepath.Join(source.Root, filepath.FromSlash(relative)), maximumSingleFile)
		if err != nil || int64(len(local)) != header.Size {
			return errors.New("archive file missing or changed in staged source")
		}
		digest := sha256.New()
		if _, err := io.CopyN(digest, reader, header.Size); err != nil {
			return err
		}
		wanted := sha256.Sum256(local)
		if !bytes.Equal(digest.Sum(nil), wanted[:]) {
			return errors.New("staged source differs from attested archive")
		}
		files[relative] = true
	}
	if len(files) == 0 {
		return errors.New("empty attested archive")
	}
	// tar stops at its terminator; read remaining zero record-padding through
	// gzip EOF to verify CRC/size and enforce the same global byte budget. A
	// second hidden tar/archive payload is not padding.
	var padding [32 << 10]byte
	for {
		n, err := bounded.Read(padding[:])
		if bounded.N == 0 {
			return errors.New("decompressed origin archive exceeds limit")
		}
		for _, value := range padding[:n] {
			if value != 0 {
				return errors.New("origin archive contains data after its tar terminator")
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if _, err := buffered.ReadByte(); !errors.Is(err, io.EOF) {
		return errors.New("origin archive has trailing compressed data")
	}
	return filepath.WalkDir(source.Root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source.Root, name)
		if err != nil || !files[filepath.ToSlash(relative)] {
			return errors.New("staged source has a file absent from the attested archive")
		}
		return nil
	})
}
