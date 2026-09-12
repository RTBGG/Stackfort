// SPDX-License-Identifier: AGPL-3.0-or-later

package writelog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func fixture() []byte {
	b := make([]byte, 4*SectorSize)
	put := func(at int, value uint64) { binary.LittleEndian.PutUint64(b[at:at+8], value) }
	put(0, 0x6a736677736872)
	put(8, 1)
	put(16, 2)
	binary.LittleEndian.PutUint32(b[24:28], SectorSize)
	put(512, 2)
	put(520, 1)
	copy(b[1024:1536], bytes.Repeat([]byte{0x79}, SectorSize))
	put(1552, 8)
	put(1560, uint64(len("dm-log-writes-end")))
	copy(b[1568:], "dm-log-writes-end")
	return b
}

func TestDecode(t *testing.T) {
	b := fixture()
	entries, err := Decode(bytes.NewReader(b), int64(len(b)), 4096)
	if err != nil || len(entries) != 2 || entries[0].Offset != 1024 || !bytes.Equal(entries[0].Data, b[1024:1536]) {
		t.Fatal(entries, err)
	}
	for _, tc := range []struct {
		name  string
		at    int
		value uint64
	}{
		{"magic", 0, 0}, {"version", 8, 2}, {"empty", 16, 0}, {"huge-count", 16, 4097},
		{"geometry", 24, 4096}, {"overflow-offset", 512, ^uint64(0)}, {"overflow-length", 520, ^uint64(0)},
		{"beyond-target", 512, 8}, {"discard", 528, 4}, {"unknown-flag", 528, 32}, {"extra-data", 536, 1},
		{"mark-length", 1560, 481}, {"mark-flags", 1552, 9}, {"mark-sector", 1536, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := fixture()
			binary.LittleEndian.PutUint64(bad[tc.at:tc.at+8], tc.value)
			if _, err := Decode(bytes.NewReader(bad), int64(len(bad)), 4096); err == nil {
				t.Fatal("accepted malformed log")
			}
		})
	}
	for _, size := range []int{0, 511, 512, 1024, 1535, 2047} {
		if _, err := Decode(bytes.NewReader(b[:size]), int64(size), 4096); err == nil {
			t.Fatalf("accepted truncated size %d", size)
		}
	}
	bad := fixture()
	copy(bad[1568:], "wrong-end-marker")
	if _, err := Decode(bytes.NewReader(bad), int64(len(bad)), 4096); err == nil {
		t.Fatal("accepted missing end")
	}
	bad = fixture()
	bad[1568] = 0
	if _, err := Decode(bytes.NewReader(bad), int64(len(bad)), 4096); err == nil {
		t.Fatal("accepted invalid mark text")
	}
	for _, geometry := range []int64{0, 513, 1 << 31} {
		if _, err := Decode(bytes.NewReader(b), int64(len(b)), geometry); err == nil {
			t.Fatal("accepted invalid target")
		}
	}
}

func TestDecodeMaximumBoundedOffset(t *testing.T) {
	const targetBytes = 1 << 30
	for _, tc := range []struct {
		name   string
		sector uint64
		valid  bool
	}{
		{"last-sector", targetBytes/SectorSize - 1, true},
		{"past-last-write", targetBytes / SectorSize, false},
		{"past-target", targetBytes/SectorSize + 1, false},
		{"signed-byte-overflow", 1 << 54, false},
		{"unsigned-byte-overflow", 1 << 55, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := fixture()
			binary.LittleEndian.PutUint64(data[512:520], tc.sector)
			entries, err := Decode(bytes.NewReader(data), int64(len(data)), targetBytes)
			if !tc.valid {
				if err == nil {
					t.Fatal("accepted out-of-bounds or overflowing offset")
				}
				return
			}
			if err != nil || len(entries) != 2 || entries[0].Offset != targetBytes-SectorSize {
				t.Fatalf("last bounded sector: entries=%v, err=%v", entries, err)
			}
		})
	}
}

func TestTears(t *testing.T) {
	old := bytes.Repeat([]byte{1}, 3*SectorSize)
	next := bytes.Clone(old)
	copy(next[:SectorSize], bytes.Repeat([]byte{2}, SectorSize))
	copy(next[2*SectorSize:], bytes.Repeat([]byte{3}, SectorSize))
	count := 0
	err := Tears(old, next, func(sector int, suffix bool, state []byte) error {
		count++
		offset := sector * SectorSize
		if sector == 1 || !bytes.Equal(state[:offset], next[:offset]) || !bytes.Equal(state[offset+SectorSize:], old[offset+SectorSize:]) {
			t.Fatal("incorrect prefix/suffix")
		}
		start := offset
		if suffix {
			start += SectorSize / 2
		}
		if !bytes.Equal(state[start:start+SectorSize/2], next[start:start+SectorSize/2]) {
			t.Fatal("wrong new half")
		}
		oldStart := offset
		if !suffix {
			oldStart += SectorSize / 2
		}
		if !bytes.Equal(state[oldStart:oldStart+SectorSize/2], old[oldStart:oldStart+SectorSize/2]) {
			t.Fatal("wrong old half")
		}
		return nil
	})
	if err != nil || count != 4 || old[0] != 1 || next[0] != 2 {
		t.Fatal(count, err)
	}
	if err := Tears(old, next[:512], func(int, bool, []byte) error { return nil }); err == nil {
		t.Fatal("accepted unequal inputs")
	}
	sentinel := errors.New("stop")
	if err := Tears(old, next, func(int, bool, []byte) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(fixture())
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = Decode(bytes.NewReader(b), int64(len(b)), 4096) })
}

func TestSectorPrefixes(t *testing.T) {
	old, next := make([]byte, 1536), bytes.Repeat([]byte{1}, 1536)
	count := 0
	if err := SectorPrefixes(old, next, func(size int, state []byte) error {
		count++
		if size != count*512 || !bytes.Equal(state[:size], next[:size]) || !bytes.Equal(state[size:], old[size:]) {
			t.Fatal("incorrect sector prefix")
		}
		return nil
	}); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if err := SectorPrefixes(old, next[:512], func(int, []byte) error { return nil }); err == nil {
		t.Fatal("invalid prefix accepted")
	}
	sentinel := errors.New("stop")
	if err := SectorPrefixes(old, next, func(int, []byte) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}
