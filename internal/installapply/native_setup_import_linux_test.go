// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	debugbuild "debug/buildinfo"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This explicit qualification uses a real freshly built API command, not a
// mock response or installed-payload replacement. Only private mounts hide the
// installed binary/database. The current host's service account is read-only.
func TestDisposableNativeSetupImportRealAPI(t *testing.T) {
	if os.Getenv("STACKFORT_NATIVE_SETUP_IMPORT_TEST") != "1" {
		t.Skip("requires explicit real-API qualification fixture and disposable private mount namespace")
	}
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" || os.Geteuid() != 0 {
		t.Fatal("real setup import qualification requires an explicitly disposable root host")
	}
	fixturePath := os.Getenv("STACKFORT_TEST_NATIVE_SETUP_API")
	fixtureSHA := os.Getenv("STACKFORT_TEST_NATIVE_SETUP_API_SHA256")
	if !filepath.IsAbs(fixturePath) || filepath.Clean(fixturePath) != fixturePath || !pinDigestPattern.MatchString(fixtureSHA) {
		t.Fatal("real API qualification needs its exact absolute fixture path and SHA-256")
	}
	if os.Getenv("STACKFORT_NATIVE_SETUP_IMPORT_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, "/usr/bin/unshare", "--mount", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeSetupImportRealAPI$")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_SETUP_IMPORT_CHILD=1")
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	nativePackageTestNamespace(t)
	uid, gid, err := serviceIdentity()
	if err != nil {
		t.Fatal("existing service identity must be valid:", err)
	}
	for _, target := range []string{"/usr/local/bin/stackfort-api", "/var/lib/stackfort"} {
		info, err := os.Lstat(target)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("fixed namespace mount target must already exist without a symlink", target, err)
		}
		if target == "/usr/local/bin/stackfort-api" && !info.Mode().IsRegular() || target == "/var/lib/stackfort" && !info.IsDir() {
			t.Fatal("unexpected mount target type", target)
		}
	}
	apiFile, err := os.Open(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	defer apiFile.Close()
	var apiStat unix.Stat_t
	if err := unix.Fstat(int(apiFile.Fd()), &apiStat); err != nil || apiStat.Mode&unix.S_IFMT != unix.S_IFREG || apiStat.Uid != 0 || apiStat.Gid != 0 || apiStat.Nlink != 1 || apiStat.Size <= 0 || apiStat.Size > 128<<20 {
		t.Fatal("unsafe real API fixture", err)
	}
	build, err := debugbuild.Read(apiFile)
	if err != nil || build.Path != "github.com/RTBGG/stackfort/cmd/stackfort-api" {
		t.Fatal("fixture is not the production stackfort-api build", err)
	}
	if _, err := apiFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	apiCopy := filepath.Join(temporary, "stackfort-api")
	copied, err := os.OpenFile(apiCopy, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(copied, hash), io.LimitReader(apiFile, (128<<20)+1))
	closeErr := copied.Close()
	if copyErr != nil || closeErr != nil || n != apiStat.Size || hex.EncodeToString(hash.Sum(nil)) != fixtureSHA {
		t.Fatal("real API fixture digest mismatch", copyErr, closeErr)
	}
	if err := os.Chmod(apiCopy, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDirectory := filepath.Join(temporary, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(stateDirectory, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(apiCopy, "/usr/local/bin/stackfort-api", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Unmount("/usr/local/bin/stackfort-api", 0); err != nil {
			t.Error(err)
		}
	}()
	if err := unix.Mount("", "/usr/local/bin/stackfort-api", "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(stateDirectory, "/var/lib/stackfort", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Unmount("/var/lib/stackfort", 0); err != nil {
			t.Error(err)
		}
	}()
	if _, err := os.Lstat("/var/lib/stackfort/stackfort.db"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("test database was not initially absent")
	}
	// No usable setup bearer token is generated for this isolated adapter test.
	digest := admissionDigest([]byte("isolated actual native setup import adapter fixture"))
	started := time.Now().UTC()
	first, err := nativeImportSetupDigest(t.Context(), digest)
	if err != nil {
		t.Fatal("actual runuser/API import failed:", err)
	}
	if first.AlreadyRegistered || first.CreatedAt.Before(started.Add(-time.Second)) || first.ExpiresAt.Sub(first.CreatedAt) != time.Hour {
		t.Fatal("invalid initial real registration", first)
	}
	assertNativeSetupDatabaseOwnership(t, uid, gid)
	second, err := nativeImportSetupDigest(t.Context(), digest)
	if err != nil || !second.AlreadyRegistered || second.ID != first.ID || second.CreatedAt != first.CreatedAt || second.ExpiresAt != first.ExpiresAt {
		t.Fatal("actual import retry renewed or duplicated capability", second, err)
	}
	if _, err := nativeImportSetupDigest(t.Context(), strings.Repeat("f", 64)); err == nil {
		t.Fatal("foreign active digest replaced actual registered capability")
	}
	assertNativeSetupDatabaseOwnership(t, uid, gid)
	// Immutable read-only connection after the API exits cannot create WAL/SHM
	// files or make a root-owned write. The actual command performed all writes.
	database, err := sql.Open("sqlite", "file:/var/lib/stackfort/stackfort.db?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count, audits int
	if err := database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM bootstrap_capabilities").Scan(&count); err != nil || count != 1 {
		t.Fatal("actual import was not idempotent", count, err)
	}
	if err := database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM audit_events WHERE action = 'bootstrap.capability_created'").Scan(&audits); err != nil || audits != 1 {
		t.Fatal("actual import duplicated its audit", audits, err)
	}
	var storedID string
	var storedDigest []byte
	if err := database.QueryRowContext(t.Context(), "SELECT id, token_hash FROM bootstrap_capabilities").Scan(&storedID, &storedDigest); err != nil {
		t.Fatal(err)
	}
	wanted, _ := hex.DecodeString(digest)
	if storedID != string(first.ID) || !bytes.Equal(storedDigest, wanted) {
		t.Fatal("database does not contain the exact imported hash identity")
	}
	assertNativeSetupDatabaseOwnership(t, uid, gid)
	t.Logf("NATIVE_SETUP_REAL_API fixture_sha256=%s service_uid=%d service_gid=%d capability_count=1 creation_audit_count=1 unchanged_expiry=true", fixtureSHA, uid, gid)
}

func assertNativeSetupDatabaseOwnership(t *testing.T, uid, gid int) {
	t.Helper()
	for _, name := range []string{"stackfort.db", "stackfort.db-wal", "stackfort.db-shm"} {
		var stat unix.Stat_t
		err := unix.Lstat("/var/lib/stackfort/"+name, &stat)
		if errors.Is(err, unix.ENOENT) && name != "stackfort.db" {
			continue
		}
		if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Uid != uint32(uid) || stat.Gid != uint32(gid) || stat.Nlink != 1 {
			t.Fatal("API did not create private service-owned SQLite state", name, err)
		}
	}
}
