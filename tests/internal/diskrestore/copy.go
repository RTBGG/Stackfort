// SPDX-License-Identifier: AGPL-3.0-or-later

// Package diskrestore supports the disposable whole-disk restore tests only.
// It neither discovers devices nor grants permission to overwrite one.
package diskrestore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// CopyBlank requires a destination independently verified to be entirely zero.
// Skipping zero ranges is correct ONLY under that prerequisite. Source size and
// digest are checked again while copying; failure leaves a failed partial target.
func CopyBlank(dst io.WriterAt, src io.Reader, size int64, digest string) (written int64, err error) {
	if size <= 0 || size%512 != 0 || size > 64<<30 {
		return 0, errors.New("invalid disk size")
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return 0, errors.New("invalid digest")
	}
	buffer, zero := make([]byte, 1<<20), make([]byte, 1<<20)
	h := sha256.New()
	for offset := int64(0); offset < size; {
		length := int64(len(buffer))
		if length > size-offset {
			length = size - offset
		}
		chunk := buffer[:length]
		if _, err := io.ReadFull(src, chunk); err != nil {
			return written, err
		}
		_, _ = h.Write(chunk)
		if !bytes.Equal(chunk, zero[:length]) {
			n, err := dst.WriteAt(chunk, offset)
			written += int64(n)
			if err != nil {
				return written, err
			}
			if n != len(chunk) {
				return written, io.ErrShortWrite
			}
		}
		offset += length
	}
	var extra [1]byte
	if n, err := io.ReadFull(src, extra[:]); n != 0 || !errors.Is(err, io.EOF) {
		return written, errors.New("source grew or trailing read failed")
	}
	if !bytes.Equal(h.Sum(nil), decoded) {
		return written, errors.New("source changed while restoring")
	}
	return written, nil
}

func CheckBlank(src io.Reader, size int64) error {
	if size <= 0 || size%512 != 0 || size > 64<<30 {
		return errors.New("invalid disk size")
	}
	buffer, zero := make([]byte, 1<<20), make([]byte, 1<<20)
	for remaining := size; remaining > 0; {
		length := int64(len(buffer))
		if remaining < length {
			length = remaining
		}
		if _, err := io.ReadFull(src, buffer[:length]); err != nil {
			return err
		}
		if !bytes.Equal(buffer[:length], zero[:length]) {
			return errors.New("replacement disk is not blank")
		}
		remaining -= length
	}
	return nil
}
