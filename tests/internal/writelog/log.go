// SPDX-License-Identifier: AGPL-3.0-or-later

// Package writelog is test-only support for the Linux dm-log-writes v1 format.
// It has no block-device writer and is not an installer/recovery backend.
package writelog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const SectorSize = 512

type Entry struct {
	Offset int64
	Flags  uint64
	Mark   string
	Data   []byte
}

// Decode supports only bounded, 512-byte-sector logs without DISCARD. Unknown
// formats are errors, not an invitation to guess what reached stable storage.
// Format reference: Linux v6.12 drivers/md/dm-log-writes.c (little-endian v1).
func Decode(r io.ReaderAt, logBytes, targetBytes int64) ([]Entry, error) {
	if logBytes < SectorSize || logBytes > 1<<30 || targetBytes <= 0 || targetBytes > 1<<30 || targetBytes%SectorSize != 0 {
		return nil, errors.New("unsupported log/target geometry")
	}
	head := make([]byte, SectorSize)
	if _, err := r.ReadAt(head, 0); err != nil {
		return nil, err
	}
	u64 := binary.LittleEndian.Uint64
	count := u64(head[16:24])
	if u64(head[:8]) != 0x6a736677736872 || u64(head[8:16]) != 1 || binary.LittleEndian.Uint32(head[24:28]) != SectorSize || count == 0 || count > 4096 {
		return nil, errors.New("unsupported or unbounded dm-log-writes header")
	}
	var entries []Entry
	pos := int64(SectorSize)
	for i := uint64(0); i < count; i++ {
		if pos > logBytes-SectorSize {
			return nil, io.ErrUnexpectedEOF
		}
		if _, err := r.ReadAt(head, pos); err != nil {
			return nil, err
		}
		pos += SectorSize
		sector, sectors, flags, extra := u64(head[:8]), u64(head[8:16]), u64(head[16:24]), u64(head[24:32])
		if flags & ^uint64(1|2|8|16) != 0 {
			return nil, errors.New("unsupported flags (including DISCARD)")
		}
		entry := Entry{Flags: flags}
		if flags&8 != 0 {
			if flags != 8 || sector != 0 || sectors != 0 || extra == 0 || extra > SectorSize-32 {
				return nil, errors.New("invalid mark")
			}
			mark := head[32 : 32+extra]
			for _, c := range mark {
				if c < 33 || c > 126 {
					return nil, errors.New("invalid mark text")
				}
			}
			entry.Mark = string(mark)
		} else {
			if extra != 0 || sector > (1<<30)/SectorSize || sector > uint64(targetBytes/SectorSize) || sectors > uint64(targetBytes/SectorSize)-sector || sectors > (8<<20)/SectorSize {
				return nil, errors.New("write outside bounded target")
			}
			if sectors == 0 && flags&1 == 0 {
				return nil, errors.New("empty non-flush record")
			}
			// Explicit sector bounds above keep both signed conversion and byte
			// multiplication within the supported 1-GiB target/8-MiB write sizes.
			length := int64(sectors) * SectorSize
			if length > logBytes-pos {
				return nil, io.ErrUnexpectedEOF
			}
			entry.Offset = int64(sector) * SectorSize
			entry.Data = make([]byte, length)
			if length != 0 {
				if _, err := r.ReadAt(entry.Data, pos); err != nil {
					return nil, err
				}
			}
			pos += length
		}
		if entry.Mark == "dm-log-writes-end" && i != count-1 {
			return nil, errors.New("early end marker")
		}
		entries = append(entries, entry)
	}
	if entries[len(entries)-1].Mark != "dm-log-writes-end" {
		return nil, errors.New("incomplete log: no terminal mark")
	}
	return entries, nil
}

// Tears models a prefix of complete sectors followed by a 256-byte old/new tear
// in each changed sector; later sectors remain old. Both half-sector directions
// are included. These are deliberately synthetic states, not a hardware claim.
func Tears(old, next []byte, visit func(sector int, suffix bool, data []byte) error) error {
	if len(old) == 0 || len(old) != len(next) || len(old)%SectorSize != 0 || len(old) > 8<<20 {
		return errors.New("invalid tear geometry")
	}
	for offset := 0; offset < len(old); offset += SectorSize {
		if bytes.Equal(old[offset:offset+SectorSize], next[offset:offset+SectorSize]) {
			continue
		}
		for _, suffix := range []bool{false, true} {
			state := bytes.Clone(old)
			copy(state[:offset], next[:offset])
			start := offset
			if suffix {
				start += SectorSize / 2
			}
			copy(state[start:start+SectorSize/2], next[start:start+SectorSize/2])
			if err := visit(offset/SectorSize, suffix, state); err != nil {
				return fmt.Errorf("sector %d: %w", offset/SectorSize, err)
			}
		}
	}
	return nil
}

// SectorPrefixes visits every internal 512-byte boundary of a write, including
// byte-identical states. This is separate from the weaker-atomicity tear model.
func SectorPrefixes(old, next []byte, visit func(bytesWritten int, data []byte) error) error {
	if len(old) == 0 || len(old) != len(next) || len(old)%SectorSize != 0 || len(old) > 8<<20 {
		return errors.New("invalid prefix geometry")
	}
	for size := SectorSize; size < len(old); size += SectorSize {
		state := bytes.Clone(old)
		copy(state[:size], next[:size])
		if err := visit(size, state); err != nil {
			return err
		}
	}
	return nil
}
