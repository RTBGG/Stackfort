// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package updateapply

import "errors"

const (
	DefaultStateDirectory   = "/var/lib/stackfort-updater"
	DefaultJournalPath      = DefaultStateDirectory + "/update-state.json"
	DefaultReleaseDirectory = DefaultStateDirectory + "/releases"
	DefaultBackupDirectory  = DefaultStateDirectory + "/backups"
)

type FileStore struct{}
type Lock struct{}

func NewFileStore() *FileStore { return &FileStore{} }
func (*FileStore) AcquireLock() (*Lock, error) {
	return nil, errors.New("Stackfort updates require Linux")
}
func (*FileStore) Load() (Journal, bool, error) {
	return Journal{}, false, errors.New("Stackfort updates require Linux")
}
func (*FileStore) Save(Journal) error { return errors.New("Stackfort updates require Linux") }
func (*Lock) Close() error            { return nil }
