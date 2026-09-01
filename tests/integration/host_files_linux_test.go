// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostfiles"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
)

func TestDisposableHostFileManagerNavigation(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	identity := disposableIdentity(t, availableManagedID(t, 589_000))
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID,
	}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}

	browser := hostfiles.NewBrowser()
	root, err := browser.List(t.Context(), agentprotocol.FileListRequest{Identity: identity, Limit: 100})
	if err != nil {
		t.Fatalf("list account root: %v", err)
	}
	wantRoot := map[string]bool{"applications": true, "backups": true, "domains": true, "logs": true, "public_html": true, "tmp": true}
	for _, entry := range root.Entries {
		if entry.Type != agentprotocol.FileEntryDirectory || !wantRoot[entry.Name] {
			t.Fatalf("unexpected account-root entry: %#v", entry)
		}
		delete(wantRoot, entry.Name)
	}
	if len(wantRoot) != 0 || root.Next != "" {
		t.Fatalf("missing root entries=%#v next=%q", wantRoot, root.Next)
	}

	pageRoot := filepath.Join(identity.HomeDirectory, "public_html", "paged")
	if err := runAs(identity, "/usr/bin/mkdir", pageRoot); err != nil {
		t.Fatalf("create paged directory: %v", err)
	}
	for index := 0; index < 105; index++ {
		name := filepath.Join(pageRoot, fmt.Sprintf("entry-%03d.txt", index))
		if err := runAs(identity, "/usr/bin/touch", name); err != nil {
			t.Fatalf("create paged entry %d: %v", index, err)
		}
	}
	unsafeName := filepath.Join(pageRoot, "line\nbreak")
	if err := runAs(identity, "/usr/bin/touch", unsafeName); err != nil {
		t.Fatalf("create non-round-trippable entry: %v", err)
	}

	seen := make(map[string]struct{}, 105)
	cursor := ""
	var omitted uint64
	for page := 0; page < 20; page++ {
		listing, err := browser.List(t.Context(), agentprotocol.FileListRequest{
			Identity: identity, Path: "public_html/paged", Cursor: cursor, Limit: 10,
		})
		if err != nil {
			t.Fatalf("list page %d: %v", page, err)
		}
		if len(listing.Entries) > 10 {
			t.Fatalf("page %d exceeded limit: %d", page, len(listing.Entries))
		}
		for _, entry := range listing.Entries {
			if entry.Type != agentprotocol.FileEntryRegular || entry.Name == "line\nbreak" {
				t.Fatalf("unsafe or unexpected paged entry: %#v", entry)
			}
			if _, duplicate := seen[entry.Name]; duplicate {
				t.Fatalf("duplicate paged entry %q", entry.Name)
			}
			seen[entry.Name] = struct{}{}
		}
		omitted += listing.OmittedEntries
		if listing.Next == "" {
			break
		}
		cursor = listing.Next
	}
	if len(seen) != 105 || omitted != 1 {
		t.Fatalf("paged entries=%d omitted=%d", len(seen), omitted)
	}

	escape := filepath.Join(identity.HomeDirectory, "public_html", "escape")
	if err := runAs(identity, "/usr/bin/ln", "-s", "/etc", escape); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}
	publicHTML, err := browser.List(t.Context(), agentprotocol.FileListRequest{
		Identity: identity, Path: "public_html", Limit: 100,
	})
	if err != nil {
		t.Fatalf("list public_html: %v", err)
	}
	foundSymlink := false
	for _, entry := range publicHTML.Entries {
		if entry.Name == "escape" {
			foundSymlink = entry.Type == agentprotocol.FileEntrySymlink
		}
	}
	if !foundSymlink {
		t.Fatal("escape symlink was not reported as a non-followed symlink")
	}
	_, err = browser.List(t.Context(), agentprotocol.FileListRequest{
		Identity: identity, Path: "public_html/escape", Limit: 10,
	})
	if !errors.Is(err, hostfiles.ErrConflict) && !errors.Is(err, hostfiles.ErrNotFound) {
		t.Fatalf("symlink traversal error=%v", err)
	}
	_, err = browser.List(t.Context(), agentprotocol.FileListRequest{
		Identity: identity, Path: "../etc", Limit: 10,
	})
	if !errors.Is(err, hostfiles.ErrInvalid) {
		t.Fatalf("path traversal error=%v", err)
	}
	forged := identity
	forged.AccountID = disposableIdentity(t, availableManagedID(t, identity.UID+1)).AccountID
	_, err = browser.List(t.Context(), agentprotocol.FileListRequest{Identity: forged, Limit: 10})
	if !errors.Is(err, hostfiles.ErrInvalid) {
		t.Fatalf("forged redundant identity error=%v", err)
	}
	t.Log("STACKFORT_QUALIFICATION file-manager-navigation=passed")
}

func TestDisposableHostFileArchiveOperations(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	helper := os.Getenv("STACKFORT_AGENT_HELPER")
	if helper == "" {
		t.Fatal("STACKFORT_AGENT_HELPER must name the separately built stackfort-agent binary")
	}
	identity := disposableIdentity(t, availableManagedID(t, 585_000))
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID,
	}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}
	root := filepath.Join(identity.HomeDirectory, "public_html")
	source := filepath.Join(root, "archive-source")
	createManagedTestDirectory(t, source, identity.UID, 0o750)
	createManagedTestDirectory(t, filepath.Join(source, "nested"), identity.UID, 0o750)
	writeManagedTestFile(t, filepath.Join(source, "index.txt"), []byte("stackfort archive"), identity.UID, 0o640)
	writeManagedTestFile(t, filepath.Join(source, "nested", "empty.txt"), nil, identity.UID, 0o640)
	correlation := &agentprotocol.FileAuditCorrelation{
		AuditEventID: "019c1234-5678-7abc-8def-0123456789af",
		ActorID:      "019c1234-5678-7abc-8def-0123456789b0",
		SessionID:    "019c1234-5678-7abc-8def-0123456789b1",
		AccountID:    identity.AccountID, RequestID: "qualification-file-archives",
	}
	writer := hostfiles.NewWriterWithExecutable(helper)
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-archive-create-zip", Action: agentprotocol.FileWriteArchiveCreate,
		Identity: identity, OperationID: "019c1234-5678-7abc-8def-0123456789d0",
		SourceDirectory: "public_html", SourceName: "archive-source", Directory: "public_html", Name: "archive-source.zip",
		ArchiveFormat: agentprotocol.FileArchiveZIP, Correlation: correlation}
	result, err := writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || !result.Completed || result.EntryCount != 4 || result.SizeBytes == 0 {
		t.Fatalf("create zip result=%#v error=%v", result, err)
	}
	zipReader, err := zip.OpenReader(filepath.Join(root, "archive-source.zip"))
	if err != nil || len(zipReader.File) != 4 {
		t.Fatalf("open created zip entries=%d error=%v", len(zipReader.File), err)
	}
	_ = zipReader.Close()

	request.RequestID, request.OperationID, request.Name, request.ArchiveFormat =
		"qualification-archive-create-tar-gzip", "019c1234-5678-7abc-8def-0123456789d1", "archive-source.tar.gz", agentprotocol.FileArchiveTarGzip
	result, err = writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || result.EntryCount != 4 || result.SizeBytes == 0 {
		t.Fatalf("create tar.gz result=%#v error=%v", result, err)
	}

	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-archive-extract-zip", Action: agentprotocol.FileWriteArchiveExtract,
		Identity: identity, OperationID: "019c1234-5678-7abc-8def-0123456789d2",
		SourceDirectory: "public_html", SourceName: "archive-source.zip", Directory: "public_html", Name: "zip-restored",
		ArchiveFormat: agentprotocol.FileArchiveZIP, Correlation: correlation}
	result, err = writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || result.EntryCount != 4 || result.SizeBytes != uint64(len("stackfort archive")) {
		t.Fatalf("extract zip result=%#v error=%v", result, err)
	}
	assertArchiveRestored(t, filepath.Join(root, "zip-restored", "archive-source"))

	request.RequestID, request.OperationID, request.SourceName, request.Name, request.ArchiveFormat =
		"qualification-archive-extract-tar-gzip", "019c1234-5678-7abc-8def-0123456789d3",
		"archive-source.tar.gz", "tar-restored", agentprotocol.FileArchiveTarGzip
	result, err = writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || result.EntryCount != 4 {
		t.Fatalf("extract tar.gz result=%#v error=%v", result, err)
	}
	assertArchiveRestored(t, filepath.Join(root, "tar-restored", "archive-source"))

	createManagedTestDirectory(t, filepath.Join(root, "existing-target"), identity.UID, 0o750)
	request.RequestID, request.OperationID, request.SourceName, request.Name, request.ArchiveFormat =
		"qualification-archive-extract-conflict", "019c1234-5678-7abc-8def-0123456789d4",
		"archive-source.zip", "existing-target", agentprotocol.FileArchiveZIP
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("extract no-replace error=%v", err)
	}

	writeZIPFixture(t, filepath.Join(root, "traversal.zip"), identity.UID, func(archive *zip.Writer) {
		entry, createErr := archive.Create("../escape.txt")
		if createErr != nil {
			t.Fatal(createErr)
		}
		_, _ = entry.Write([]byte("escape"))
	})
	assertArchiveRejected(t, writer, identity, correlation, "019c1234-5678-7abc-8def-0123456789d5",
		"traversal.zip", "traversal-output", agentprotocol.FileArchiveZIP, hostfiles.ErrConflict)
	if _, err := os.Stat(filepath.Join(identity.HomeDirectory, "escape.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zip traversal escaped extraction root: %v", err)
	}

	writeZIPFixture(t, filepath.Join(root, "symlink.zip"), identity.UID, func(archive *zip.Writer) {
		header := &zip.FileHeader{Name: "passwd-link", Method: zip.Store}
		header.SetMode(os.ModeSymlink | 0o777)
		entry, createErr := archive.CreateHeader(header)
		if createErr != nil {
			t.Fatal(createErr)
		}
		_, _ = entry.Write([]byte("/etc/passwd"))
	})
	assertArchiveRejected(t, writer, identity, correlation, "019c1234-5678-7abc-8def-0123456789d6",
		"symlink.zip", "symlink-output", agentprotocol.FileArchiveZIP, hostfiles.ErrConflict)

	writeZIPFixture(t, filepath.Join(root, "duplicate.zip"), identity.UID, func(archive *zip.Writer) {
		for _, content := range []string{"first", "second"} {
			entry, createErr := archive.Create("duplicate.txt")
			if createErr != nil {
				t.Fatal(createErr)
			}
			_, _ = entry.Write([]byte(content))
		}
	})
	assertArchiveRejected(t, writer, identity, correlation, "019c1234-5678-7abc-8def-0123456789d7",
		"duplicate.zip", "duplicate-output", agentprotocol.FileArchiveZIP, hostfiles.ErrConflict)

	writeZIPFixture(t, filepath.Join(root, "bomb.zip"), identity.UID, func(archive *zip.Writer) {
		entry, createErr := archive.CreateHeader(&zip.FileHeader{Name: "expanded.bin", Method: zip.Deflate})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, copyErr := io.CopyN(entry, zeroArchiveReader{}, 128<<20); copyErr != nil {
			t.Fatal(copyErr)
		}
	})
	assertArchiveRejected(t, writer, identity, correlation, "019c1234-5678-7abc-8def-0123456789d8",
		"bomb.zip", "bomb-output", agentprotocol.FileArchiveZIP, hostfiles.ErrTooLarge)

	writeTarGzipHardlinkFixture(t, filepath.Join(root, "hardlink.tar.gz"), identity.UID)
	assertArchiveRejected(t, writer, identity, correlation, "019c1234-5678-7abc-8def-0123456789d9",
		"hardlink.tar.gz", "hardlink-output", agentprotocol.FileArchiveTarGzip, hostfiles.ErrConflict)

	unsafe := filepath.Join(root, "archive-unsafe-link")
	if err := os.Symlink("/etc/passwd", unsafe); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(unsafe, int(identity.UID), int(identity.GID)); err != nil {
		t.Fatal(err)
	}
	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-archive-create-symlink", Action: agentprotocol.FileWriteArchiveCreate,
		Identity: identity, OperationID: "019c1234-5678-7abc-8def-0123456789da",
		SourceDirectory: "public_html", SourceName: "archive-unsafe-link", Directory: "public_html", Name: "unsafe.zip",
		ArchiveFormat: agentprotocol.FileArchiveZIP, Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("archive symlink source error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "unsafe.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected archive source exposed destination: %v", err)
	}

	listing, err := hostfiles.NewBrowser().List(t.Context(), agentprotocol.FileListRequest{Identity: identity, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listing.Entries {
		if entry.Name == agentprotocol.ReservedFileOperationDirectory {
			t.Fatal("archive staging leaked into file-manager listing")
		}
	}
	stagingEntries, err := os.ReadDir(filepath.Join(identity.HomeDirectory, agentprotocol.ReservedFileOperationDirectory))
	if err != nil || len(stagingEntries) != 0 {
		t.Fatalf("archive staging was not cleaned after success/failure paths: entries=%d error=%v", len(stagingEntries), err)
	}
	t.Log("STACKFORT_QUALIFICATION file-manager-archives=passed")
}

func assertArchiveRestored(t *testing.T, root string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "index.txt"))
	empty, emptyErr := os.Stat(filepath.Join(root, "nested", "empty.txt"))
	if err != nil || string(content) != "stackfort archive" || emptyErr != nil || empty.Size() != 0 ||
		empty.Mode().Perm() != 0o640 {
		t.Fatalf("restored root=%s content=%q read=%v empty=%v stat=%v", root, content, err, empty, emptyErr)
	}
}

func assertArchiveRejected(
	t *testing.T, writer *hostfiles.Writer, identity hostingidentity.Spec, correlation *agentprotocol.FileAuditCorrelation,
	operationID, sourceName, destinationName string, format agentprotocol.FileArchiveFormat, want error,
) {
	t.Helper()
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-reject-" + destinationName, Action: agentprotocol.FileWriteArchiveExtract,
		Identity: identity, OperationID: operationID, SourceDirectory: "public_html", SourceName: sourceName,
		Directory: "public_html", Name: destinationName, ArchiveFormat: format, Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, want) {
		t.Fatalf("extract %s error=%v want=%v", sourceName, err, want)
	}
	if _, err := os.Stat(filepath.Join(identity.HomeDirectory, "public_html", destinationName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected archive %s exposed destination: %v", sourceName, err)
	}
}

type zeroArchiveReader struct{}

func (zeroArchiveReader) Read(target []byte) (int, error) {
	clear(target)
	return len(target), nil
}

func writeZIPFixture(t *testing.T, name string, owner uint32, build func(*zip.Writer)) {
	t.Helper()
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	build(archive)
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(name, int(owner), int(owner)); err != nil {
		t.Fatal(err)
	}
}

func writeTarGzipHardlinkFixture(t *testing.T, name string, owner uint32) {
	t.Helper()
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "passwd", Typeflag: tar.TypeLink, Linkname: "/etc/passwd"}); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(tarWriter.Close(), gzipWriter.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(name, int(owner), int(owner)); err != nil {
		t.Fatal(err)
	}
}

func TestDisposableHostFileManagerDownload(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	helper := os.Getenv("STACKFORT_AGENT_HELPER")
	if helper == "" {
		t.Fatal("STACKFORT_AGENT_HELPER must name the separately built stackfort-agent binary")
	}
	identity := disposableIdentity(t, availableManagedID(t, 588_000))
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID,
	}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}

	content := []byte("0123456789-stackfort-download")
	managedPath := filepath.Join(identity.HomeDirectory, "public_html", "site data.txt")
	writeManagedTestFile(t, managedPath, content, identity.UID, 0o640)
	downloader := hostfiles.NewDownloaderWithExecutable(helper)
	request := agentprotocol.FileDownloadRequest{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "qualification-download-full",
		Identity: identity, Path: "public_html/site data.txt",
	}
	download, err := downloader.Open(t.Context(), request)
	if err != nil {
		t.Fatalf("open full download: %v", err)
	}
	full, readErr := io.ReadAll(download.Body)
	closeErr := download.Body.Close()
	if readErr != nil || closeErr != nil || string(full) != string(content) || download.Partial ||
		download.Length != uint64(len(content)) {
		t.Fatalf("full download=%#v bytes=%q read=%v close=%v", download, full, readErr, closeErr)
	}

	start, end := uint64(11), uint64(19)
	request.RequestID = "qualification-download-range"
	request.Range = &agentprotocol.FileDownloadRange{Start: &start, EndInclusive: &end}
	download, err = downloader.Open(t.Context(), request)
	if err != nil {
		t.Fatalf("open range download: %v", err)
	}
	partial, readErr := io.ReadAll(download.Body)
	closeErr = download.Body.Close()
	if readErr != nil || closeErr != nil || string(partial) != string(content[start:end+1]) ||
		!download.Partial || download.Offset != start {
		t.Fatalf("range download=%#v bytes=%q read=%v close=%v", download, partial, readErr, closeErr)
	}

	escape := filepath.Join(identity.HomeDirectory, "public_html", "passwd-link")
	if err := runAs(identity, "/usr/bin/ln", "-s", "/etc/passwd", escape); err != nil {
		t.Fatalf("create file escape symlink: %v", err)
	}
	request.RequestID, request.Path, request.Range = "qualification-download-symlink", "public_html/passwd-link", nil
	if _, err := downloader.Open(t.Context(), request); !errors.Is(err, hostfiles.ErrConflict) && !errors.Is(err, hostfiles.ErrNotFound) {
		t.Fatalf("symlink download error=%v", err)
	}

	unreadable := filepath.Join(identity.HomeDirectory, "public_html", "unreadable.txt")
	writeManagedTestFile(t, unreadable, []byte("secret"), identity.UID, 0)
	request.RequestID, request.Path = "qualification-download-permissions", "public_html/unreadable.txt"
	if _, err := downloader.Open(t.Context(), request); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("account permission error=%v", err)
	}

	oversized := filepath.Join(identity.HomeDirectory, "public_html", "oversized.bin")
	writeManagedTestFile(t, oversized, nil, identity.UID, 0o640)
	if err := os.Truncate(oversized, int64(agentprotocol.MaximumFileDownloadBytes+1)); err != nil {
		t.Fatalf("create sparse oversized file: %v", err)
	}
	request.RequestID, request.Path = "qualification-download-limit", "public_html/oversized.bin"
	if _, err := downloader.Open(t.Context(), request); !errors.Is(err, hostfiles.ErrTooLarge) {
		t.Fatalf("oversized download error=%v", err)
	}
	zero, thirtyOne := uint64(0), uint64(31)
	request.RequestID = "qualification-download-limited-range"
	request.Range = &agentprotocol.FileDownloadRange{Start: &zero, EndInclusive: &thirtyOne}
	download, err = downloader.Open(t.Context(), request)
	if err != nil {
		t.Fatalf("open bounded range from oversized file: %v", err)
	}
	if _, err := io.Copy(io.Discard, download.Body); err != nil {
		t.Fatalf("read bounded oversized range: %v", err)
	}
	if err := download.Body.Close(); err != nil {
		t.Fatalf("close bounded oversized range: %v", err)
	}

	cancellable := filepath.Join(identity.HomeDirectory, "public_html", "cancel.bin")
	writeManagedTestFile(t, cancellable, nil, identity.UID, 0o640)
	if err := os.Truncate(cancellable, 64<<20); err != nil {
		t.Fatalf("create sparse cancellation file: %v", err)
	}
	request.RequestID, request.Path, request.Range = "qualification-download-cancel", "public_html/cancel.bin", nil
	cancelContext, cancel := context.WithCancel(t.Context())
	download, err = downloader.Open(cancelContext, request)
	if err != nil {
		t.Fatalf("open cancellation download: %v", err)
	}
	cancel()
	if err := download.Body.Close(); err == nil {
		t.Fatal("cancelled partial stream closed without reporting truncation")
	}
	t.Log("STACKFORT_QUALIFICATION file-manager-download=passed")
}

func TestDisposableHostFileManagerWrite(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	helper := os.Getenv("STACKFORT_AGENT_HELPER")
	if helper == "" {
		t.Fatal("STACKFORT_AGENT_HELPER must name the separately built stackfort-agent binary")
	}
	identity := disposableIdentity(t, availableManagedID(t, 587_000))
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID,
	}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}

	writer := hostfiles.NewWriterWithExecutable(helper)
	correlation := &agentprotocol.FileAuditCorrelation{
		AuditEventID: "019c1234-5678-7abc-8def-0123456789af",
		ActorID:      "019c1234-5678-7abc-8def-0123456789b0",
		SessionID:    "019c1234-5678-7abc-8def-0123456789b1",
		AccountID:    identity.AccountID,
		RequestID:    "qualification-file-write",
	}
	content := []byte("stackfort-staged-upload")
	digest := sha256.Sum256(content)
	wantSHA256 := hex.EncodeToString(digest[:])
	uploadID := "019c1234-5678-7abc-8def-0123456789b2"
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-upload-initiate", Action: agentprotocol.FileWriteInitiate,
		Identity: identity, UploadID: uploadID, Directory: "public_html", Name: "uploaded.txt",
		SizeBytes: uint64(len(content)), ExpectedSHA256: wantSHA256, Correlation: correlation}
	result, err := writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || result.ReceivedBytes != 0 || result.Name != "uploaded.txt" {
		t.Fatalf("initiate result=%#v error=%v", result, err)
	}
	root, err := hostfiles.NewBrowser().List(t.Context(), agentprotocol.FileListRequest{Identity: identity, Limit: 100})
	if err != nil {
		t.Fatalf("list root with staged upload: %v", err)
	}
	for _, entry := range root.Entries {
		if entry.Name == agentprotocol.ReservedFileUploadDirectory {
			t.Fatal("internal upload staging leaked into file-manager listing")
		}
	}

	first := uint64(9)
	request.Action, request.Correlation = agentprotocol.FileWriteChunk, nil
	request.Directory, request.Name, request.SizeBytes, request.ExpectedSHA256 = "", "", 0, ""
	request.RequestID, request.Offset, request.ChunkLength = "qualification-upload-chunk-1", 0, first
	result, err = writer.Execute(t.Context(), request, strings.NewReader(string(content[:first])))
	if err != nil || result.ReceivedBytes != first {
		t.Fatalf("first chunk result=%#v error=%v", result, err)
	}
	// A fresh writer process must resume from the descriptor-derived on-disk offset.
	writer = hostfiles.NewWriterWithExecutable(helper)
	request.Action, request.RequestID, request.Offset = agentprotocol.FileWriteStatus, "qualification-upload-status", 0
	request.ChunkLength = 0
	result, err = writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || result.ReceivedBytes != first {
		t.Fatalf("resumed status result=%#v error=%v", result, err)
	}
	request.Action, request.RequestID, request.Offset = agentprotocol.FileWriteChunk, "qualification-upload-chunk-2", first
	request.ChunkLength = uint64(len(content)) - first
	result, err = writer.Execute(t.Context(), request, strings.NewReader(string(content[first:])))
	if err != nil || result.ReceivedBytes != uint64(len(content)) {
		t.Fatalf("second chunk result=%#v error=%v", result, err)
	}
	request.Action, request.RequestID, request.Correlation = agentprotocol.FileWriteComplete, "qualification-upload-complete", correlation
	request.Offset, request.ChunkLength = 0, 0
	request.Directory, request.Name, request.SizeBytes, request.ExpectedSHA256 =
		"public_html", "uploaded.txt", uint64(len(content)), wantSHA256
	result, err = writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || !result.Completed || result.SHA256 != wantSHA256 {
		t.Fatalf("complete result=%#v error=%v", result, err)
	}
	uploadedPath := filepath.Join(identity.HomeDirectory, "public_html", "uploaded.txt")
	uploaded, readErr := os.ReadFile(uploadedPath)
	status, statErr := os.Stat(uploadedPath)
	if readErr != nil || statErr != nil || string(uploaded) != string(content) {
		t.Fatalf("uploaded=%q read=%v stat=%v", uploaded, readErr, statErr)
	}
	if status.Mode().Perm() != 0o640 {
		t.Fatalf("uploaded mode=%v", status.Mode())
	}

	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-create-directory", Action: agentprotocol.FileWriteCreateDirectory,
		Identity: identity, Directory: "public_html", Name: "assets", Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	request.RequestID, request.Action, request.Directory, request.Name =
		"qualification-create-file", agentprotocol.FileWriteCreateFile, "public_html/assets", "empty.css"
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if status, err := os.Stat(filepath.Join(identity.HomeDirectory, "public_html", "assets", "empty.css")); err != nil || status.Mode().Perm() != 0o640 || status.Size() != 0 {
		t.Fatalf("created file status=%v error=%v", status, err)
	}

	// No-replace activation leaves an existing visible file untouched.
	conflictID := "019c1234-5678-7abc-8def-0123456789b3"
	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-conflict-initiate", Action: agentprotocol.FileWriteInitiate,
		Identity: identity, UploadID: conflictID, Directory: "public_html", Name: "uploaded.txt",
		SizeBytes: 1, Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("initiate conflict fixture: %v", err)
	}
	request.Action, request.Correlation, request.RequestID, request.Directory, request.Name, request.SizeBytes =
		agentprotocol.FileWriteChunk, nil, "qualification-conflict-chunk", "", "", 0
	request.ChunkLength = 1
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("x")); err != nil {
		t.Fatalf("write conflict fixture: %v", err)
	}
	request.Action, request.Correlation, request.RequestID = agentprotocol.FileWriteComplete, correlation, "qualification-conflict-complete"
	request.Directory, request.Name, request.SizeBytes, request.ChunkLength = "public_html", "uploaded.txt", 1, 0
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("no-replace completion error=%v", err)
	}
	request.Action, request.RequestID, request.Directory, request.Name, request.SizeBytes =
		agentprotocol.FileWriteCancel, "qualification-conflict-cancel", "", "", 0
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("cancel conflict fixture: %v", err)
	}
	if after, err := os.ReadFile(uploadedPath); err != nil || string(after) != string(content) {
		t.Fatalf("existing target changed after conflict: %q error=%v", after, err)
	}
	t.Log("STACKFORT_QUALIFICATION file-manager-write=passed")
}

func TestDisposableHostFileManagerOperations(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	helper := os.Getenv("STACKFORT_AGENT_HELPER")
	if helper == "" {
		t.Fatal("STACKFORT_AGENT_HELPER must name the separately built stackfort-agent binary")
	}
	identity := disposableIdentity(t, availableManagedID(t, 586_000))
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	filesystem := hostfilesystem.NewReconciler()
	if _, err := filesystem.Reconcile(t.Context(), hostingstorage.Spec{Identity: identity, ProjectID: identity.UID}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}
	root := filepath.Join(identity.HomeDirectory, "public_html")
	assets := filepath.Join(root, "assets")
	createManagedTestDirectory(t, assets, identity.UID, 0o750)
	writeManagedTestFile(t, filepath.Join(root, "rename.txt"), []byte("relocate"), identity.UID, 0o640)
	writeManagedTestFile(t, filepath.Join(assets, "style.css"), []byte("body{}"), identity.UID, 0o640)
	correlation := &agentprotocol.FileAuditCorrelation{
		AuditEventID: "019c1234-5678-7abc-8def-0123456789af",
		ActorID:      "019c1234-5678-7abc-8def-0123456789b0",
		SessionID:    "019c1234-5678-7abc-8def-0123456789b1",
		AccountID:    identity.AccountID,
		RequestID:    "qualification-file-operations",
	}
	writer := hostfiles.NewWriterWithExecutable(helper)
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-rename", Action: agentprotocol.FileWriteRename, Identity: identity,
		SourceDirectory: "public_html", SourceName: "rename.txt", Directory: "public_html", Name: "renamed.txt",
		Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	request.RequestID, request.Action = "qualification-move", agentprotocol.FileWriteMove
	request.SourceName, request.Directory, request.Name = "renamed.txt", "public_html/assets", "moved.txt"
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("move: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(assets, "moved.txt")); err != nil || string(content) != "relocate" {
		t.Fatalf("moved content=%q error=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("move source still exists: %v", err)
	}

	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-copy", Action: agentprotocol.FileWriteCopy, Identity: identity,
		OperationID: "019c1234-5678-7abc-8def-0123456789c0", SourceDirectory: "public_html", SourceName: "assets",
		Directory: "public_html", Name: "assets-copy", Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("copy directory: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "assets-copy", "style.css")); err != nil || string(content) != "body{}" {
		t.Fatalf("copied content=%q error=%v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(assets, "style.css")); err != nil || string(content) != "body{}" {
		t.Fatalf("copy changed source: %q error=%v", content, err)
	}
	request.OperationID, request.RequestID = "019c1234-5678-7abc-8def-0123456789c1", "qualification-copy-conflict"
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("copy no-replace error=%v", err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "unsafe-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(filepath.Join(root, "unsafe-link"), int(identity.UID), int(identity.GID)); err != nil {
		t.Fatal(err)
	}
	request.OperationID, request.RequestID = "019c1234-5678-7abc-8def-0123456789c2", "qualification-copy-symlink"
	request.SourceName, request.Name = "unsafe-link", "unsafe-copy"
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("symlink copy error=%v", err)
	}

	trashID := "019c1234-5678-7abc-8def-0123456789c3"
	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-trash", Action: agentprotocol.FileWriteTrash, Identity: identity,
		TrashID: trashID, SourceDirectory: "public_html", SourceName: "assets-copy", Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("trash directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "assets-copy")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trashed source remains visible: %v", err)
	}
	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-trash-list", Action: agentprotocol.FileWriteTrashList, Identity: identity}
	result, err := writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || len(result.TrashEntries) != 1 || result.TrashEntries[0].TrashID != trashID ||
		result.TrashEntries[0].Name != "assets-copy" || result.TrashEntries[0].Type != agentprotocol.FileEntryDirectory {
		t.Fatalf("trash listing=%#v error=%v", result, err)
	}
	listing, err := hostfiles.NewBrowser().List(t.Context(), agentprotocol.FileListRequest{Identity: identity, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listing.Entries {
		if entry.Name == agentprotocol.ReservedFileTrashDirectory || entry.Name == agentprotocol.ReservedFileOperationDirectory {
			t.Fatalf("internal operation directory leaked: %s", entry.Name)
		}
	}
	createManagedTestDirectory(t, filepath.Join(root, "assets-copy"), identity.UID, 0o750)
	request.Action, request.RequestID, request.TrashID, request.Correlation =
		agentprotocol.FileWriteTrashRestore, "qualification-trash-restore", trashID, correlation
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("restore no-replace error=%v", err)
	}
	if err := os.Remove(filepath.Join(root, "assets-copy")); err != nil {
		t.Fatalf("remove restore conflict fixture: %v", err)
	}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("restore directory: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "assets-copy", "moved.txt")); err != nil || string(content) != "relocate" {
		t.Fatalf("restored content=%q error=%v", content, err)
	}

	purgeID := "019c1234-5678-7abc-8def-0123456789c4"
	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-trash-purge-fixture", Action: agentprotocol.FileWriteTrash, Identity: identity,
		TrashID: purgeID, SourceDirectory: "public_html", SourceName: "assets-copy", Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("trash purge fixture: %v", err)
	}
	request.Action, request.RequestID, request.SourceDirectory, request.SourceName =
		agentprotocol.FileWriteTrashPurge, "qualification-trash-purge", "", ""
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); err != nil {
		t.Fatalf("purge directory: %v", err)
	}
	request.Action, request.RequestID, request.TrashID, request.Correlation =
		agentprotocol.FileWriteTrashList, "qualification-trash-empty", "", nil
	result, err = writer.Execute(t.Context(), request, strings.NewReader(""))
	if err != nil || len(result.TrashEntries) != 0 {
		t.Fatalf("trash after purge=%#v error=%v", result, err)
	}

	quotaSource := filepath.Join(root, "quota-source.bin")
	writeManagedTestFile(t, quotaSource, make([]byte, 1_200<<10), identity.UID, 0o640)
	if _, err := filesystem.Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID, ByteLimit: 2 << 20,
	}); err != nil {
		t.Fatalf("apply copy quota: %v", err)
	}
	request = agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "qualification-copy-quota", Action: agentprotocol.FileWriteCopy, Identity: identity,
		OperationID: "019c1234-5678-7abc-8def-0123456789c5", SourceDirectory: "public_html", SourceName: "quota-source.bin",
		Directory: "public_html", Name: "quota-copy.bin", Correlation: correlation}
	if _, err := writer.Execute(t.Context(), request, strings.NewReader("")); !errors.Is(err, hostfiles.ErrQuota) {
		t.Fatalf("quota copy error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "quota-copy.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quota failure exposed destination: %v", err)
	}
	t.Log("STACKFORT_QUALIFICATION file-manager-operations=passed")
}

func writeManagedTestFile(t *testing.T, name string, content []byte, owner uint32, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(name, content, mode); err != nil {
		t.Fatalf("write managed test file %s: %v", filepath.Base(name), err)
	}
	if err := os.Chown(name, int(owner), int(owner)); err != nil {
		t.Fatalf("own managed test file %s: %v", filepath.Base(name), err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatalf("protect managed test file %s: %v", filepath.Base(name), err)
	}
}

func createManagedTestDirectory(t *testing.T, name string, owner uint32, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(name, mode); err != nil {
		t.Fatalf("create managed test directory %s: %v", filepath.Base(name), err)
	}
	if err := os.Chown(name, int(owner), int(owner)); err != nil {
		t.Fatalf("own managed test directory %s: %v", filepath.Base(name), err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatalf("protect managed test directory %s: %v", filepath.Base(name), err)
	}
}
