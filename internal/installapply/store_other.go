// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import "errors"

type FileStore struct{}
type Lock struct{}

func NewFileStore() *FileStore { return &FileStore{} }
func (*FileStore) AcquireLock() (*Lock, error) {
	return nil, errors.New("Stackfort installation requires Linux")
}
func (*FileStore) Load() (Journal, bool, error) {
	return Journal{}, false, errors.New("Stackfort installation requires Linux")
}
func (*FileStore) Save(Journal) error { return errors.New("Stackfort installation requires Linux") }
func (*Lock) Close() error            { return nil }
