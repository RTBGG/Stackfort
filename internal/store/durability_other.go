// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package store

func syncBackupDirectory(string) error { return nil }
