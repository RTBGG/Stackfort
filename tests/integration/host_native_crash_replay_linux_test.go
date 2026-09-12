// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

// Test-only crash modelling on new regular files and their owned loop devices.
// No root-device conversion, repair, checkpoint restore or public command.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/tests/internal/writelog"
	"golang.org/x/sys/unix"
)

const crashImageBytes int64 = 256 << 20

type crashCase struct {
	Index             int    `json:"index"`
	Stage             string `json:"stage"`
	Entry             int    `json:"entry"`
	Kind              string `json:"kind"`
	Offset            int64  `json:"offset"`
	SHA256            string `json:"sha256"`
	FsckExit          int    `json:"fsckExit"`
	DiagnosticSHA256  string `json:"diagnosticSHA256"`
	ReadOnlyUnchanged bool   `json:"readOnlyUnchanged"`
}

// Newly allocated private directory only. Never adopt a path supplied by flags.
func crashFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
}

func crashHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func crashDigest(t *testing.T, path string) string {
	t.Helper()
	result, err := crashHash(path)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// A file-image restore rehearsal, NOT a generic disk restore utility. A verified
// complete backup and exclusive new regular-file destination are required. Never
// overwrites evidence; the caller keeps the damaged image and backup separately.
func crashRestoreImage(source, destination, digest string, size int64) error {
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) || size <= 0 || size > 1<<30 {
		return errors.New("invalid backup identity")
	}
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	input := os.NewFile(uintptr(fd), source)
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("backup geometry/type differs")
	}
	h := sha256.New()
	if _, err := io.Copy(h, input); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("backup checksum differs")
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	// Rehash the actual copied stream, so changes after the initial check cannot
	// be reported as success. A failed partial destination remains as evidence.
	h.Reset()
	copied, err := io.Copy(io.MultiWriter(out, h), input)
	if err != nil {
		return err
	}
	if copied != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("backup changed during copy")
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h.Reset()
	if _, err := io.Copy(h, out); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("restore verification differs")
	}
	parent, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func TestCrashRestoreImageGuards(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "backup")
	destination := filepath.Join(dir, "restore")
	imageWrite(t, source, []byte("complete backup"), 0600)
	digest := crashDigest(t, source)
	for _, tc := range []struct {
		hash string
		size int64
	}{{strings.Repeat("0", 64), 15}, {digest, 14}, {"", 15}} {
		if err := crashRestoreImage(source, destination, tc.hash, tc.size); err == nil {
			t.Fatal("invalid backup accepted")
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid backup touched target")
		}
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"truncated", []byte("complete backu")}, {"corrupted", []byte("damaged  backup")}} {
		bad := filepath.Join(dir, tc.name)
		imageWrite(t, bad, tc.data, 0600)
		if err := crashRestoreImage(bad, destination, digest, 15); err == nil {
			t.Fatal("damaged backup accepted")
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("damaged backup touched target")
		}
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := crashRestoreImage(fifo, destination, digest, 15); err == nil {
		t.Fatal("non-regular backup accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if err := crashRestoreImage(link, destination, digest, 15); err == nil {
		t.Fatal("symlink source accepted")
	}
	if err := os.Symlink(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := crashRestoreImage(source, destination, digest, 15); err == nil {
		t.Fatal("symlink destination accepted")
	}
	if crashDigest(t, source) != digest {
		t.Fatal("source changed")
	}
	destination = filepath.Join(dir, "new-restore")
	if err := crashRestoreImage(source, destination, digest, 15); err != nil {
		t.Fatal(err)
	}
	if err := crashRestoreImage(source, destination, digest, 15); err == nil {
		t.Fatal("existing target overwritten")
	}
	if crashDigest(t, destination) != digest {
		t.Fatal("restore differs")
	}
}

func TestDisposableNativeCrashReplay(t *testing.T) {
	requireNativeLab(t)
	if os.Getenv("STACKFORT_NATIVE_CRASH_REPLAY") != "1" {
		t.Skip("requires explicit scratch-image crash-replay opt-in")
	}
	if strings.TrimSpace(string(imageRead(t, "/sys/class/dmi/id/product_uuid"))) != "6365bd88-5141-4f15-b3f8-2ba9996baad2" {
		t.Fatal("wrong VM DMI")
	}
	var intent installapply.NativeRuntimeIntent
	if err := json.Unmarshal(imageRead(t, installapply.DefaultJournalDirectory+"/native-runtime-intent.json"), &intent); err != nil || intent.Offline == nil || !intent.Offline.PowerLossGuard {
		t.Fatal("requires completed guarded fixture", err)
	}
	for path, digest := range intent.Offline.Tools {
		if crashDigest(t, path) != digest {
			t.Fatal("tool differs from boot-qualified artifact", path)
		}
	}
	if imageCommand(t, "/usr/bin/systemctl", "is-active", "stackfort-native-install.service") != "active" {
		t.Fatal("fixture not normally admitted")
	}
	protected := map[string]string{}
	for _, path := range []string{"/etc/fstab", "/boot/grub/grub.cfg", installapply.DefaultJournalDirectory + "/native-runtime-intent.json", installapply.DefaultJournalDirectory + "/native-runtime-installer"} {
		protected[path] = crashDigest(t, path)
	}

	dir, err := os.MkdirTemp("/var/tmp", "stackfort-crash-lab-")
	if err != nil {
		t.Fatal(err)
	}
	// Evidence is deliberately retained on failures and success. No recursive delete.
	t.Log("CRASH_REPLAY evidence=" + dir)
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil || stat.Bavail*uint64(stat.Bsize) < 4<<30 {
		t.Fatal("requires 4 GiB scratch headroom", err)
	}
	commands := 0
	run := func(allowed []int, name string, args ...string) ([]byte, int) {
		commands++
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		output, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		imageWrite(t, filepath.Join(dir, fmt.Sprintf("command-%04d.log", commands)), append([]byte(fmt.Sprintf("%s %v\nexit=%d\n", name, args, code)), output...), 0600)
		if !slices.Contains(allowed, code) {
			t.Fatalf("%s %v exit=%d: %s", name, args, code, output)
		}
		return output, code
	}
	call := func(name string, args ...string) string {
		output, _ := run([]int{0}, name, args...)
		return strings.TrimSpace(string(output))
	}
	seed := filepath.Join(dir, "seed")
	if err := os.Mkdir(seed, 0700); err != nil {
		t.Fatal(err)
	}
	imageWrite(t, filepath.Join(seed, "sentinel.txt"), []byte("Stackfort pre-conversion recovery payload\n"), 0600)
	imageWrite(t, filepath.Join(seed, "payload.bin"), bytes.Repeat([]byte{0, 1, 0x7f, 0x80, 0xfe, 0xff}, 32768), 0640)
	baseline := filepath.Join(dir, "baseline.img")
	crashFile(t, baseline, crashImageBytes)
	call("/usr/sbin/mke2fs", "-F", "-t", "ext4", "-b", "4096", "-I", "256", "-O", "^quota,^project", "-E", "nodiscard,lazy_itable_init=0,lazy_journal_init=0", "-d", seed, baseline)
	run([]int{0, 1}, "/usr/sbin/e2fsck", "-f", "-p", baseline)
	before := call("/usr/sbin/tune2fs", "-l", baseline)
	for _, line := range strings.Split(before, "\n") {
		if strings.HasPrefix(line, "Filesystem features:") && (strings.Contains(line, "quota") || strings.Contains(line, "project")) {
			t.Fatal("baseline already converted")
		}
	}
	backupDigest := crashDigest(t, baseline)
	backup := filepath.Join(dir, "verified-backup.img")
	if err := crashRestoreImage(baseline, backup, backupDigest, crashImageBytes); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(backup, 0400); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(dir, "recording.img")
	if err := crashRestoreImage(backup, working, backupDigest, crashImageBytes); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "writes.img")
	crashFile(t, logPath, 64<<20)
	call("/usr/sbin/modprobe", "dm-log-writes")
	attach := func(path string) string {
		loop := call("/usr/sbin/losetup", "--find", "--show", "--nooverlap", path)
		if !regexp.MustCompile(`^/dev/loop[0-9]+$`).MatchString(loop) {
			t.Fatal("unexpected loop", loop)
		}
		if call("/usr/sbin/losetup", "--noheadings", "--raw", "--output", "BACK-FILE", loop) != path {
			t.Fatal("foreign loop backing")
		}
		return loop
	}
	dataLoop, logLoop := "", ""
	mapName := "sf-crash-" + filepath.Base(dir)[len("stackfort-crash-lab-"):]
	mapped := "/dev/mapper/" + mapName
	// Clean up only our exact mapping and loops, never force/deferred removal.
	mapLive := false
	dataLive, logLive := false, false
	t.Cleanup(func() {
		if mapLive {
			cmd := exec.Command("/usr/sbin/dmsetup", "remove", mapName)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("preserve busy mapping: %v %s", err, output)
				return
			}
		}
		for _, owned := range []struct {
			device, path string
			live         bool
		}{{dataLoop, working, dataLive}, {logLoop, logPath, logLive}} {
			if !owned.live {
				continue
			}
			out, err := exec.Command("/usr/sbin/losetup", "--noheadings", "--raw", "--output", "BACK-FILE", owned.device).Output()
			if err != nil || strings.TrimSpace(string(out)) != owned.path {
				t.Errorf("refuse foreign loop cleanup: %s", owned.device)
				continue
			}
			if err := exec.Command("/usr/sbin/losetup", "--detach", owned.device).Run(); err != nil {
				t.Error(err)
			}
		}
	})
	dataLoop = attach(working)
	dataLive = true
	logLoop = attach(logPath)
	logLive = true
	call("/usr/sbin/dmsetup", "create", mapName, "--table", fmt.Sprintf("0 %d log-writes %s %s", crashImageBytes/512, dataLoop, logLoop))
	mapLive = true
	if call("/usr/sbin/blockdev", "--getsize64", mapped) != strconv.FormatInt(crashImageBytes, 10) {
		t.Fatal("wrong mapped size")
	}
	var deviceStat unix.Stat_t
	if err := unix.Stat(mapped, &deviceStat); err != nil || deviceStat.Mode&unix.S_IFMT != unix.S_IFBLK {
		t.Fatal("invalid scratch mapping", err)
	}
	for _, line := range strings.Split(string(imageRead(t, "/proc/self/mountinfo")), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[2] == fmt.Sprintf("%d:%d", unix.Major(deviceStat.Rdev), unix.Minor(deviceStat.Rdev)) {
			t.Fatal("scratch mapping unexpectedly mounted")
		}
	}
	flush := func() {
		f, err := os.Open(mapped)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := f.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	mark := func(name string) { call("/usr/sbin/dmsetup", "message", mapName, "0", "mark", name) }
	for _, stage := range []struct {
		name, tool string
		args       []string
		codes      []int
	}{
		{"precheck", "/usr/sbin/e2fsck", []string{"-f", "-p", mapped}, []int{0, 1}},
		{"quota", "/usr/sbin/tune2fs", []string{"-O", "project,quota", "-Q", "prjquota", mapped}, []int{0}},
		{"postcheck", "/usr/sbin/e2fsck", []string{"-f", "-p", mapped}, []int{0, 1}},
	} {
		mark(stage.name + "-start")
		run(stage.codes, stage.tool, stage.args...)
		flush()
		mark(stage.name + "-flushed")
	}
	call("/usr/sbin/dmsetup", "status", mapName)
	call("/usr/sbin/dmsetup", "remove", mapName)
	mapLive = false
	call("/usr/sbin/losetup", "--detach", dataLoop)
	dataLive = false
	call("/usr/sbin/losetup", "--detach", logLoop)
	logLive = false
	logFile, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	entries, err := writelog.Decode(logFile, 64<<20, crashImageBytes)
	if err != nil {
		t.Fatal(err)
	}
	var marks []string
	for _, entry := range entries {
		if entry.Mark != "" {
			marks = append(marks, entry.Mark)
		}
	}
	if !slices.Equal(marks, []string{"precheck-start", "precheck-flushed", "quota-start", "quota-flushed", "postcheck-start", "postcheck-flushed", "dm-log-writes-end"}) {
		t.Fatal("incomplete or conflicting stage markers", marks)
	}
	encoded, _ := json.MarshalIndent(entries, "", "  ")
	imageWrite(t, filepath.Join(dir, "decoded-writes.json"), encoded, 0600)
	replay := filepath.Join(dir, "replay.img")
	if err := crashRestoreImage(backup, replay, backupDigest, crashImageBytes); err != nil {
		t.Fatal(err)
	}
	replayFile, err := os.OpenFile(replay, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer replayFile.Close()
	cases := []crashCase{}
	invalid, tears, writes, sectorCuts := 0, 0, 0, 0
	stage := "baseline"
	damagedDigest := ""
	check := func(entry int, kind string, offset int64) {
		if len(cases) >= 4096 {
			t.Fatal("case bound exceeded: no silent sampling")
		}
		if err := replayFile.Sync(); err != nil {
			t.Fatal(err)
		}
		digest := crashDigest(t, replay)
		out, code := run([]int{0, 4, 8, 12}, "/usr/sbin/e2fsck", "-f", "-n", replay)
		unchanged := crashDigest(t, replay) == digest
		if !unchanged {
			t.Fatal("read-only diagnostic changed image")
		}
		if code != 0 {
			invalid++
			if damagedDigest == "" {
				damagedDigest = digest
				if err := crashRestoreImage(replay, filepath.Join(dir, "damaged-evidence.img"), digest, crashImageBytes); err != nil {
					t.Fatal(err)
				}
			}
		}
		cases = append(cases, crashCase{len(cases), stage, entry, kind, offset, digest, code, nativeHash(out), unchanged})
		if len(cases)%25 == 0 {
			t.Logf("CRASH_REPLAY cases=%d inconsistent=%d stage=%s", len(cases), invalid, stage)
		}
	}
	check(-1, "initial-boundary", 0)
	for index, entry := range entries {
		if entry.Mark != "" {
			stage = entry.Mark
			continue
		}
		if len(entry.Data) == 0 {
			continue
		}
		old := make([]byte, len(entry.Data))
		if _, err := replayFile.ReadAt(old, entry.Offset); err != nil {
			t.Fatal(err)
		}
		if err := writelog.SectorPrefixes(old, entry.Data, func(size int, state []byte) error {
			if _, err := replayFile.WriteAt(state, entry.Offset); err != nil {
				return err
			}
			sectorCuts++
			check(index, "sector-prefix-boundary", entry.Offset+int64(size))
			_, err := replayFile.WriteAt(old, entry.Offset)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := writelog.Tears(old, entry.Data, func(sector int, suffix bool, state []byte) error {
			if _, err := replayFile.WriteAt(state, entry.Offset); err != nil {
				return err
			}
			kind := "half-sector-prefix"
			if suffix {
				kind = "half-sector-suffix"
			}
			tears++
			check(index, kind, entry.Offset+int64(sector*512))
			_, err := replayFile.WriteAt(old, entry.Offset)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := replayFile.WriteAt(entry.Data, entry.Offset); err != nil {
			t.Fatal(err)
		}
		writes++
		check(index, "complete-write-boundary", entry.Offset)
	}
	if writes == 0 || tears == 0 || invalid == 0 || damagedDigest == "" {
		t.Fatal("matrix did not exercise real inconsistent states")
	}
	if crashDigest(t, replay) != crashDigest(t, working) {
		t.Fatal("full replay differs from actual converted image")
	}
	run([]int{0}, "/usr/sbin/e2fsck", "-f", "-n", replay)
	verifyPayload := func(path, label string) {
		for _, name := range []string{"sentinel.txt", "payload.bin"} {
			destination := filepath.Join(dir, label+"-"+name)
			call("/usr/sbin/debugfs", "-R", "dump /"+name+" "+destination, path)
			if crashDigest(t, destination) != crashDigest(t, filepath.Join(seed, name)) {
				t.Fatal("payload differs", label, name)
			}
		}
	}
	verifyPayload(replay, "converted")
	// Incorrect/truncated backup metadata must fail without creating a target.
	for index, bad := range []struct {
		hash string
		size int64
	}{{strings.Repeat("0", 64), crashImageBytes}, {backupDigest, crashImageBytes - 512}} {
		destination := filepath.Join(dir, fmt.Sprintf("rejected-restore-%d.img", index))
		if err := crashRestoreImage(backup, destination, bad.hash, bad.size); err == nil {
			t.Fatal("bad backup accepted")
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("bad backup touched target")
		}
	}
	restored := filepath.Join(dir, "restored.img")
	if err := crashRestoreImage(backup, restored, backupDigest, crashImageBytes); err != nil {
		t.Fatal(err)
	}
	run([]int{0}, "/usr/sbin/e2fsck", "-f", "-n", restored)
	verifyPayload(restored, "restored")
	if crashDigest(t, restored) != backupDigest || crashDigest(t, backup) != backupDigest || crashDigest(t, filepath.Join(dir, "damaged-evidence.img")) != damagedDigest {
		t.Fatal("restore changed backup/evidence or differs")
	}
	for path, digest := range protected {
		if crashDigest(t, path) != digest {
			t.Fatal("protected host file changed", path)
		}
	}
	unique := map[string]bool{}
	for _, item := range cases {
		unique[item.SHA256] = true
	}
	result := map[string]any{"schemaVersion": 1, "scope": "scratch ext4 block-write replay; not root boot or hardware power loss", "kernel": call("/usr/bin/uname", "-r"), "imageBytes": crashImageBytes, "sectorBytes": 512, "blockBytes": 4096, "tearBytes": 256, "entries": len(entries), "writes": writes, "tears": tears, "sectorCuts": sectorCuts, "uniqueImageStates": len(unique), "cases": cases, "inconsistentCases": invalid, "backupSHA256": backupDigest, "damagedSHA256": damagedDigest, "convertedSHA256": crashDigest(t, working), "logSHA256": crashDigest(t, logPath), "tools": intent.Offline.Tools, "protectedHostFiles": protected, "fullReplayExact": true, "restoreExact": true, "payloadsVerified": true, "noMountOrRepair": true}
	encoded, err = json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, filepath.Join(dir, "result.json"), encoded, 0600)
	t.Logf("CRASH_REPLAY PASS cases=%d writes=%d sectorCuts=%d tears=%d inconsistent=%d uniqueStates=%d exactReplay=true exactRestore=true evidence=%s", len(cases), writes, sectorCuts, tears, invalid, len(unique), dir)
}
