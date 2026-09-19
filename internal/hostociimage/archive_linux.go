// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostociimage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/RTBGG/stackfort/internal/ociimage"
)

const (
	maximumArchiveEntries             = 1024
	maximumArchiveLayers              = 128
	maximumArchiveMetadata            = 1 << 20
	maximumArchiveExpandedBytes int64 = 4 << 30
	ociManifestType                   = "application/vnd.oci.image.manifest.v1+json"
	ociConfigType                     = "application/vnd.oci.image.config.v1+json"
	ociLayerType                      = "application/vnd.oci.image.layer.v1.tar"
)

// sealImageArchive never treats chown as revocation of existing writable file
// descriptors. Copy into a fresh root-owned inode, validate that stable copy,
// then materialize its verified outer OCI layout for Trivy. Tenant-held FDs
// still refer to the old, unlinked export, not the bytes the scanner receives.
func sealImageArchive(ctx context.Context, archive string, tenantUID, tenantGID, ownerUID, ownerGID uint32, imageDigest string) error {
	if !ociimage.ValidDigest(imageDigest) {
		return ociimage.ErrScanFailed
	}
	// #nosec G304 -- private caller derives image.tar below an exclusively created canonical UUIDv7 transaction; no-follow and descriptor metadata checks enforce the file boundary.
	source, err := os.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ociimage.ErrScanFailed
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !ownedArchive(info, tenantUID, tenantGID) {
		return ociimage.ErrScanFailed
	}
	stable, err := os.CreateTemp(filepath.Dir(archive), ".scan-image-*")
	if err != nil {
		return ociimage.ErrScanFailed
	}
	stableName := stable.Name()
	defer func() { _ = stable.Close(); _ = os.Remove(stableName) }()
	if stable.Chmod(0o600) != nil || stable.Chown(int(ownerUID), int(ownerGID)) != nil {
		return ociimage.ErrScanFailed
	}
	n, err := io.Copy(stable, io.LimitReader(archiveContextReader{ctx, source}, ociimage.MaximumImageArchiveBytes+1))
	if err != nil || n != info.Size() || n > ociimage.MaximumImageArchiveBytes || stable.Sync() != nil {
		return ociimage.ErrScanFailed
	}
	var layout verifiedArchiveLayout
	if err := verifyImageArchiveLayout(ctx, stable, imageDigest, &layout); err != nil {
		return ociimage.ErrScanFailed
	}
	if info, err := stable.Stat(); err != nil || !ownedArchive(info, ownerUID, ownerGID) || info.Size() != n {
		return ociimage.ErrScanFailed
	}
	if os.Rename(stableName, archive) != nil {
		return ociimage.ErrScanFailed
	}
	// Trivy accepts an OCI directory, not Podman's OCI tar transport. Read only
	// offsets from this same verified root-owned descriptor; never extract layer
	// tar entries or reopen tenant-controlled source paths.
	layoutPath := filepath.Join(filepath.Dir(archive), "image.oci")
	if err := materializeArchiveLayout(ctx, stable, layoutPath, layout, ownerUID, ownerGID); err != nil {
		return ociimage.ErrScanFailed
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(layoutPath)
		}
	}()
	if stable.Close() != nil {
		return ociimage.ErrScanFailed
	}
	directory, err := os.Open(filepath.Dir(archive))
	if err != nil {
		return ociimage.ErrScanFailed
	}
	defer directory.Close()
	if directory.Sync() != nil {
		return ociimage.ErrScanFailed
	}
	complete = true
	return nil
}

type verifiedArchiveLayout struct {
	index, layout []byte
	blobs         map[string]archiveBlob
}

// Only called with a fully verified plan and the same immutable archive FD.
// The transaction parent is root-owned and not tenant-writable. A conflicting
// layout (including a symlink) is never adopted, replaced or cleaned up.
func materializeArchiveLayout(ctx context.Context, archive *os.File, target string, layout verifiedArchiveLayout, uid, gid uint32) error {
	if ctx.Err() != nil || os.Mkdir(target, 0o700) != nil {
		return ociimage.ErrScanFailed
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(target)
		}
	}()
	directories := make([]*os.File, 0, 3)
	defer func() {
		for _, directory := range directories {
			_ = directory.Close()
		}
	}()
	for _, name := range []string{"", "blobs", "blobs/sha256"} {
		directoryPath := filepath.Join(target, name)
		if name != "" && os.Mkdir(directoryPath, 0o700) != nil {
			return ociimage.ErrScanFailed
		}
		// #nosec G304 -- fixed directories below the exclusively created private root-owned layout, never archive-derived paths.
		directory, err := os.OpenFile(directoryPath, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return ociimage.ErrScanFailed
		}
		directories = append(directories, directory)
		if directory.Chown(int(uid), int(gid)) != nil || directory.Chmod(0o700) != nil {
			return ociimage.ErrScanFailed
		}
	}
	for digest, blob := range layout.blobs {
		if !ociimage.ValidDigest(digest) || blob.offset < 0 || blob.size < 0 || blob.size > ociimage.MaximumImageArchiveBytes {
			return ociimage.ErrScanFailed
		}
		if err := writeArchiveLayoutFile(ctx, filepath.Join(target, "blobs", "sha256", digest[7:]),
			io.NewSectionReader(archive, blob.offset, blob.size), blob.size, uid, gid); err != nil {
			return err
		}
	}
	for _, metadata := range []struct {
		name    string
		content []byte
	}{{"oci-layout", layout.layout}, {"index.json", layout.index}} {
		if err := writeArchiveLayoutFile(ctx, filepath.Join(target, metadata.name),
			bytes.NewReader(metadata.content), int64(len(metadata.content)), uid, gid); err != nil {
			return err
		}
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if directories[index].Sync() != nil {
			return ociimage.ErrScanFailed
		}
	}
	if ctx.Err() != nil {
		return ociimage.ErrScanFailed
	}
	complete = true
	return nil
}

func writeArchiveLayoutFile(ctx context.Context, name string, input io.Reader, size int64, uid, gid uint32) error {
	// #nosec G304 -- fixed metadata basenames or validated SHA256 names in newly created private root-owned directories; exclusive no-follow creation.
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ociimage.ErrScanFailed
	}
	defer file.Close()
	if file.Chown(int(uid), int(gid)) != nil || file.Chmod(0o600) != nil {
		return ociimage.ErrScanFailed
	}
	n, err := io.Copy(file, io.LimitReader(archiveContextReader{ctx, input}, size+1))
	if err != nil || n != size || file.Sync() != nil || file.Close() != nil {
		return ociimage.ErrScanFailed
	}
	return nil
}

func ownedArchive(info os.FileInfo, uid, gid uint32) bool {
	status, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 && status.Nlink == 1 &&
		status.Uid == uid && status.Gid == gid && info.Size() > 0 && info.Size() <= ociimage.MaximumImageArchiveBytes
}

type archiveContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader archiveContextReader) Read(content []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(content)
}

type archiveCounter struct {
	reader io.Reader
	offset int64
}

func (reader *archiveCounter) Read(content []byte) (int, error) {
	n, err := reader.reader.Read(content)
	reader.offset += int64(n)
	return n, err
}

type archiveBlob struct{ offset, size int64 }
type archiveDescriptor struct {
	MediaType string          `json:"mediaType"`
	Digest    string          `json:"digest"`
	Size      int64           `json:"size"`
	URLs      []string        `json:"urls,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// verifyImageArchive accepts only the bounded single-image OCI export produced
// by the fixed Podman profile. It never extracts files. Every blob is hashed;
// the config ImageID and ordered uncompressed layer hashes bind scanner bytes
// to the inspected image, rather than trusting attacker-editable index labels.
func verifyImageArchive(ctx context.Context, file *os.File, imageDigest string) error {
	var layout verifiedArchiveLayout
	return verifyImageArchiveLayout(ctx, file, imageDigest, &layout)
}

func verifyImageArchiveLayout(ctx context.Context, file *os.File, imageDigest string, result *verifiedArchiveLayout) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	counter := &archiveCounter{reader: archiveContextReader{ctx, io.LimitReader(file, ociimage.MaximumImageArchiveBytes+1)}}
	reader := tar.NewReader(counter)
	blobs := map[string]archiveBlob{}
	seen := map[string]bool{}
	var index, layout []byte
	for entries := 0; ; entries++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || entries >= maximumArchiveEntries || header.Size < 0 || header.Size > ociimage.MaximumImageArchiveBytes {
			return ociimage.ErrScanFailed
		}
		name := strings.TrimPrefix(header.Name, "./")
		if header.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" || path.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || seen[name] {
			return ociimage.ErrScanFailed
		}
		seen[name] = true
		for key := range header.PAXRecords {
			if strings.HasPrefix(key, "GNU.sparse") {
				return ociimage.ErrScanFailed
			}
		}
		if header.Typeflag == tar.TypeDir {
			if header.Size != 0 || name != "blobs" && name != "blobs/sha256" {
				return ociimage.ErrScanFailed
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return ociimage.ErrScanFailed
		}
		if name == "index.json" || name == "oci-layout" {
			if header.Size <= 0 || header.Size > maximumArchiveMetadata {
				return ociimage.ErrScanFailed
			}
			content, err := io.ReadAll(reader)
			if err != nil || int64(len(content)) != header.Size {
				return ociimage.ErrScanFailed
			}
			if name == "index.json" {
				index = content
			} else {
				layout = content
			}
			continue
		}
		digest := "sha256:" + strings.TrimPrefix(name, "blobs/sha256/")
		if !strings.HasPrefix(name, "blobs/sha256/") || !ociimage.ValidDigest(digest) {
			return ociimage.ErrScanFailed
		}
		blob := archiveBlob{offset: counter.offset, size: header.Size}
		hash := sha256.New()
		if n, err := io.Copy(hash, io.LimitReader(reader, header.Size+1)); err != nil || n != header.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != digest {
			return ociimage.ErrScanFailed
		}
		blobs[digest] = blob
	}
	// A second tar archive after the first end marker must not change what a
	// different parser sees. Only bounded zero padding is allowed after EOF.
	padding := make([]byte, 32<<10)
	for {
		n, err := counter.Read(padding)
		if counter.offset > ociimage.MaximumImageArchiveBytes {
			return ociimage.ErrScanFailed
		}
		for _, value := range padding[:n] {
			if value != 0 {
				return ociimage.ErrScanFailed
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	var layoutValue struct {
		Version string `json:"imageLayoutVersion"`
	}
	var indexValue struct {
		SchemaVersion int                 `json:"schemaVersion"`
		MediaType     string              `json:"mediaType"`
		Manifests     []archiveDescriptor `json:"manifests"`
	}
	if archiveJSON(layout, &layoutValue) != nil || layoutValue.Version != "1.0.0" || archiveJSON(index, &indexValue) != nil ||
		indexValue.SchemaVersion != 2 || indexValue.MediaType != "" && indexValue.MediaType != "application/vnd.oci.image.index.v1+json" || len(indexValue.Manifests) != 1 {
		return ociimage.ErrScanFailed
	}
	manifestDescriptor := indexValue.Manifests[0]
	if manifestDescriptor.MediaType != ociManifestType {
		return ociimage.ErrScanFailed
	}
	manifestContent, err := archiveMetadata(file, blobs, manifestDescriptor)
	if err != nil {
		return err
	}
	var manifest struct {
		SchemaVersion int                 `json:"schemaVersion"`
		MediaType     string              `json:"mediaType"`
		Config        archiveDescriptor   `json:"config"`
		Layers        []archiveDescriptor `json:"layers"`
	}
	if archiveJSON(manifestContent, &manifest) != nil || manifest.SchemaVersion != 2 || manifest.MediaType != "" && manifest.MediaType != ociManifestType ||
		manifest.Config.MediaType != ociConfigType || manifest.Config.Digest != imageDigest || len(manifest.Layers) > maximumArchiveLayers {
		return ociimage.ErrScanFailed
	}
	configContent, err := archiveMetadata(file, blobs, manifest.Config)
	if err != nil {
		return err
	}
	var config struct {
		RootFS struct {
			Type    string   `json:"type"`
			DiffIDs []string `json:"diff_ids"`
		} `json:"rootfs"`
	}
	if archiveJSON(configContent, &config) != nil || config.RootFS.Type != "layers" || len(config.RootFS.DiffIDs) != len(manifest.Layers) {
		return ociimage.ErrScanFailed
	}
	expanded := int64(0)
	for index, descriptor := range manifest.Layers {
		blob, exists := blobs[descriptor.Digest]
		if !exists || !validArchiveDescriptor(descriptor, blob) || !ociimage.ValidDigest(config.RootFS.DiffIDs[index]) {
			return ociimage.ErrScanFailed
		}
		var input io.Reader = archiveContextReader{ctx, io.NewSectionReader(file, blob.offset, blob.size)}
		var compressed *gzip.Reader
		switch descriptor.MediaType {
		case ociLayerType:
		case ociLayerType + "+gzip":
			compressed, err = gzip.NewReader(input)
			if err != nil {
				return ociimage.ErrScanFailed
			}
			input = compressed
		default:
			return ociimage.ErrScanFailed
		}
		hash := sha256.New()
		n, hashErr := io.Copy(hash, io.LimitReader(archiveContextReader{ctx, input}, maximumArchiveExpandedBytes-expanded+1))
		if compressed != nil {
			_ = compressed.Close()
		}
		expanded += n
		if hashErr != nil || expanded > maximumArchiveExpandedBytes || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != config.RootFS.DiffIDs[index] {
			return ociimage.ErrScanFailed
		}
	}
	*result = verifiedArchiveLayout{index: index, layout: layout, blobs: blobs}
	return nil
}

func validArchiveDescriptor(descriptor archiveDescriptor, blob archiveBlob) bool {
	return ociimage.ValidDigest(descriptor.Digest) && descriptor.Size >= 0 && descriptor.Size == blob.size && len(descriptor.URLs) == 0 && len(descriptor.Data) == 0
}

func archiveMetadata(file *os.File, blobs map[string]archiveBlob, descriptor archiveDescriptor) ([]byte, error) {
	blob, exists := blobs[descriptor.Digest]
	if !exists || !validArchiveDescriptor(descriptor, blob) || blob.size <= 0 || blob.size > maximumArchiveMetadata {
		return nil, ociimage.ErrScanFailed
	}
	return io.ReadAll(io.NewSectionReader(file, blob.offset, blob.size))
}

// Reject duplicate (including case-alias) keys before decoding, so the
// verifier and scanner cannot disagree on a security-relevant JSON value.
func archiveJSON(content []byte, target any) error {
	if len(content) == 0 || len(content) > maximumArchiveMetadata {
		return ociimage.ErrScanFailed
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := archiveJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ociimage.ErrScanFailed
	}
	if json.Unmarshal(content, target) != nil {
		return ociimage.ErrScanFailed
	}
	return nil
}

func archiveJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return ociimage.ErrScanFailed
	}
	token, err := decoder.Token()
	if err != nil {
		return ociimage.ErrScanFailed
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return ociimage.ErrScanFailed
			}
			key, ok := token.(string)
			if !ok {
				return ociimage.ErrScanFailed
			}
			// encoding/json also accepts Unicode SimpleFold aliases for ASCII
			// struct fields (for example long-s in "size"). This deliberately
			// bounded OCI export contract accepts ASCII object keys only.
			for _, character := range key {
				if character > 127 {
					return ociimage.ErrScanFailed
				}
			}
			key = strings.ToLower(key)
			if keys[key] {
				return ociimage.ErrScanFailed
			}
			keys[key] = true
			if err := archiveJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return ociimage.ErrScanFailed
		}
	case '[':
		for decoder.More() {
			if err := archiveJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return ociimage.ErrScanFailed
		}
	default:
		return ociimage.ErrScanFailed
	}
	return nil
}
