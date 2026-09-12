// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

const nativeOfflineArtifactLimit = 512 << 20

// The existing small-record offline reader must not be used to buffer an entire
// kernel/initrd. This closed path selection streams debugfs stdout into a bounded
// digest; no mount or debugfs write mode is used.
func nativeOfflineArtifactDigest(ctx context.Context, device, path string, plan storageprep.Plan) (string, error) {
	if ctx == nil || ctx.Err() != nil || plan.Validate() != nil || filepath.Dir(device) != "/dev" {
		return "", errors.New("invalid offline artifact invocation")
	}
	switch path {
	case "/boot/vmlinuz-" + plan.Kernel, "/boot/initrd.img-" + plan.Kernel, "/boot/grub/grub.cfg":
	default:
		return "", errors.New("offline artifact path is not allowlisted")
	}
	var stat unix.Stat_t
	if err := unix.Lstat(device, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFBLK {
		return "", errors.Join(err, errors.New("offline artifact source is not a direct block device"))
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// #nosec G204 -- fixed debugfs, validated plan-derived boot path/direct device;
	// no shell or write flag. Caller independently verifies unmounted root identity.
	command := exec.CommandContext(ctx, "/usr/sbin/debugfs", "-D", "-R", "cat "+path, device)
	containNativeCommand(command)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	hash := sha256.New()
	output := &nativeDigestOutput{target: hash, limit: nativeOfflineArtifactLimit}
	var stderr nativeBootOutput
	command.Stdout, command.Stderr = output, &stderr
	if err := command.Run(); err != nil || ctx.Err() != nil || output.overflow || stderr.overflow || output.written == 0 {
		return "", errors.Join(err, ctx.Err(), errors.New("offline boot artifact could not be hashed safely"))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type nativeDigestOutput struct {
	target   io.Writer
	limit    int64
	written  int64
	overflow bool
}

func (output *nativeDigestOutput) Write(data []byte) (int, error) {
	if output.written > output.limit || int64(len(data)) > output.limit-output.written {
		output.overflow = true
		return 0, errors.New("offline boot artifact exceeds limit")
	}
	n, err := output.target.Write(data)
	output.written += int64(n)
	return n, err
}
