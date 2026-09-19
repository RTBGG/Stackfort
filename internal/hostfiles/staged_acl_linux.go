// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const (
	accessACLAttribute  = "system.posix_acl_access"
	defaultACLAttribute = "system.posix_acl_default"
	maximumACLBytes     = 64 << 10
)

// Rename preserves an inode's ACL, not the destination's default ACL. Only live
// descriptor-bound POSIX ACLs are copied, never archive xattrs or supplied users.
// These helpers run as the account UID, not as a privileged ACL repair service.
func readDescriptorACL(descriptor int, attribute string) ([]byte, error) {
	buffer := make([]byte, maximumACLBytes)
	count, err := unix.Fgetxattr(descriptor, attribute, buffer)
	if errors.Is(err, unix.ENODATA) {
		return nil, nil
	}
	if err != nil || count <= 0 || count > len(buffer) {
		return nil, ErrUnavailable
	}
	return buffer[:count], nil
}

func setDescriptorACL(descriptor int, attribute string, acl []byte) error {
	var err error
	if len(acl) == 0 {
		err = unix.Fremovexattr(descriptor, attribute)
		if errors.Is(err, unix.ENODATA) {
			return nil
		}
	} else {
		err = unix.Fsetxattr(descriptor, attribute, acl, 0)
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// Seed only per-operation DEFAULT permissions. The staging access mode stays
// 0700, so web workers cannot traverse unfinished content. The kernel then
// propagates the destination policy to newly created descendants.
func seedStagingDefaultACL(staging, destination int) error {
	acl, err := readDescriptorACL(destination, defaultACLAttribute)
	if err != nil {
		return err
	}
	return setDescriptorACL(staging, defaultACLAttribute, acl)
}

func inheritStagedAccess(descriptor, parent int, mode uint32, directory bool) error {
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil {
		return ErrUnavailable
	}
	if (directory && status.Mode&unix.S_IFMT != unix.S_IFDIR) ||
		(!directory && (status.Mode&unix.S_IFMT != unix.S_IFREG || status.Nlink != 1)) {
		return ErrConflict
	}
	acl, err := readDescriptorACL(parent, defaultACLAttribute)
	if err != nil {
		return err
	}
	if err := setDescriptorACL(descriptor, accessACLAttribute, acl); err != nil {
		return err
	}
	if directory {
		if err := setDescriptorACL(descriptor, defaultACLAttribute, acl); err != nil {
			return err
		}
	}
	// chmod also bounds the ACL mask. Do not introduce world/set-ID/sticky bits.
	if unix.Fchmod(descriptor, mode&0o770) != nil || unix.Fsync(descriptor) != nil {
		return ErrUnavailable
	}
	return nil
}

// Restores replace whole directories. Retain live directory policy at matching
// paths, including custom/nested web roots; new descendants inherit their staged
// parent's policy. Never make the complete account readable by the web worker.
// New directory/file permissions remain normalized to 0750/0640; live directory
// permissions are retained, including any more restrictive private directories.
func restoreStagedDirectoryAccess(
	ctx context.Context, staged, live int, identity hostingidentity.Spec, device uint64,
	budget *fileOperationBudget, depth uint32,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > agentprotocol.MaximumFileOperationDepth || budget.entries >= budget.maximumEntries() {
		return ErrTooLarge
	}
	if _, err := validateManagedDirectoryDescriptor(staged, identity, device); err != nil {
		return err
	}
	budget.entries++
	if live >= 0 {
		status, err := validateManagedDirectoryDescriptor(live, identity, device)
		if err != nil {
			return err
		}
		for _, attribute := range []string{accessACLAttribute, defaultACLAttribute} {
			acl, err := readDescriptorACL(live, attribute)
			if err != nil {
				return err
			}
			if err := setDescriptorACL(staged, attribute, acl); err != nil {
				return err
			}
		}
		if unix.Fchmod(staged, status.Mode&0o770) != nil {
			return ErrUnavailable
		}
	}
	names, err := readManagedDirectoryNames(staged, int(budget.remainingEntries()+1)) // #nosec G115 -- fixed operation entry bound plus overflow sentinel.
	if err != nil {
		return err
	}
	for _, name := range names {
		if !hostingpath.ValidFilename(name) {
			return ErrConflict
		}
		if err := restoreStagedEntryAccess(ctx, staged, live, name, identity, device, budget, depth); err != nil {
			return err
		}
	}
	if unix.Fsync(staged) != nil {
		return ErrUnavailable
	}
	return nil
}

func restoreStagedEntryAccess(
	ctx context.Context, parent, liveParent int, name string, identity hostingidentity.Spec, device uint64,
	budget *fileOperationBudget, depth uint32,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if budget.entries >= budget.maximumEntries() {
		return ErrTooLarge
	}
	status, err := validateManagedEntryAt(parent, name, identity, device)
	if err != nil {
		return err
	}
	directory := status.Mode&unix.S_IFMT == unix.S_IFDIR
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	child, err := unix.Openat(parent, name, flags, 0)
	if err != nil {
		return classifyFileMutationError(err)
	}
	defer unix.Close(child)
	var actual unix.Stat_t
	if unix.Fstat(child, &actual) != nil || actual.Ino != status.Ino || actual.Dev != status.Dev ||
		actual.Mode&unix.S_IFMT != status.Mode&unix.S_IFMT || actual.Uid != identity.UID || actual.Gid != identity.GID ||
		(!directory && actual.Nlink != 1) {
		return ErrConflict
	}
	if !directory {
		budget.entries++
		return inheritStagedAccess(child, parent, 0o640, false)
	}
	liveChild := -1
	if liveParent >= 0 {
		liveStatus, statErr := validateManagedEntryAt(liveParent, name, identity, device)
		if statErr != nil && !errors.Is(statErr, ErrNotFound) {
			return statErr
		}
		if statErr == nil && liveStatus.Mode&unix.S_IFMT == unix.S_IFDIR {
			liveChild, err = unix.Openat(liveParent, name, flags, 0)
			if err != nil {
				return classifyFileMutationError(err)
			}
			defer unix.Close(liveChild)
			var held unix.Stat_t
			if unix.Fstat(liveChild, &held) != nil || held.Ino != liveStatus.Ino || held.Dev != liveStatus.Dev {
				return ErrConflict
			}
		}
	}
	if liveChild < 0 {
		if err := inheritStagedAccess(child, parent, 0o750, true); err != nil {
			return err
		}
	}
	return restoreStagedDirectoryAccess(ctx, child, liveChild, identity, device, budget, depth+1)
}
