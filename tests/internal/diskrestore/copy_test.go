// SPDX-License-Identifier: AGPL-3.0-or-later

package diskrestore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

type target struct {
	data    []byte
	short   bool
	failure error
	calls   int
}

func (d *target) WriteAt(p []byte, offset int64) (int, error) {
	d.calls++
	if d.failure != nil {
		return 0, d.failure
	}
	if d.short {
		return 0, nil
	}
	return copy(d.data[offset:], p), nil
}
func sum(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }

func TestBlankCopy(t *testing.T) {
	src := make([]byte, 3<<20)
	src[0] = 1
	src[len(src)-1] = 2
	dst := &target{data: make([]byte, len(src))}
	n, err := CopyBlank(dst, bytes.NewReader(src), int64(len(src)), sum(src))
	if err != nil || n != 2<<20 || dst.calls != 2 || !bytes.Equal(src, dst.data) {
		t.Fatal(n, dst.calls, err)
	}
	if err := CheckBlank(bytes.NewReader(make([]byte, len(src))), int64(len(src))); err != nil {
		t.Fatal(err)
	}
	if err := CheckBlank(bytes.NewReader(src), int64(len(src))); err == nil {
		t.Fatal("nonblank accepted")
	}
	if err := CheckBlank(bytes.NewReader(src[:511]), 512); err == nil {
		t.Fatal("short disk accepted")
	}
}

func TestCopyFailures(t *testing.T) {
	src := bytes.Repeat([]byte{1}, 512)
	for _, tc := range []struct {
		name   string
		size   int64
		hash   string
		source []byte
	}{
		{"unaligned", 511, sum(src), src}, {"large", 65 << 30, sum(src), src}, {"digest", 512, "bad", src},
		{"short", 512, sum(src), src[:511]}, {"trailing", 512, sum(src), append(bytes.Clone(src), 0)},
		{"changed", 512, sum(make([]byte, 512)), src},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CopyBlank(&target{data: make([]byte, 512)}, bytes.NewReader(tc.source), tc.size, tc.hash); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	if _, err := CopyBlank(&target{short: true}, bytes.NewReader(src), 512, sum(src)); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	sentinel := errors.New("write failed")
	if _, err := CopyBlank(&target{failure: sentinel}, bytes.NewReader(src), 512, sum(src)); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := CheckBlank(bytes.NewReader(src), 0); err == nil {
		t.Fatal("invalid blank geometry accepted")
	}
}
