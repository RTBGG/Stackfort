// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package updateapply

func syncDirectoryEntry(string) error { return nil }
func persistReleaseTree(string) error { return nil }
