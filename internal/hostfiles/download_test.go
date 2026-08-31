// SPDX-License-Identifier: AGPL-3.0-or-later

package hostfiles

import (
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

func TestResolveDownloadRange(t *testing.T) {
	t.Parallel()
	start, end, suffix := uint64(3), uint64(6), uint64(4)
	for _, test := range []struct {
		name       string
		total      uint64
		rangeValue *agentprotocol.FileDownloadRange
		offset     uint64
		length     uint64
		partial    bool
		wantErr    error
	}{
		{name: "full", total: 10, offset: 0, length: 10},
		{name: "bounded", total: 10, rangeValue: &agentprotocol.FileDownloadRange{Start: &start, EndInclusive: &end}, offset: 3, length: 4, partial: true},
		{name: "suffix", total: 10, rangeValue: &agentprotocol.FileDownloadRange{SuffixLength: &suffix}, offset: 6, length: 4, partial: true},
		{name: "past end", total: 3, rangeValue: &agentprotocol.FileDownloadRange{Start: &start}, wantErr: ErrRange},
		{name: "empty range", total: 0, rangeValue: &agentprotocol.FileDownloadRange{SuffixLength: &suffix}, wantErr: ErrRange},
		{name: "oversized full", total: agentprotocol.MaximumFileDownloadBytes + 1, wantErr: ErrTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset, length, partial, err := resolveDownloadRange(test.total, test.rangeValue)
			if !errors.Is(err, test.wantErr) || offset != test.offset || length != test.length || partial != test.partial {
				t.Fatalf("got offset=%d length=%d partial=%v err=%v", offset, length, partial, err)
			}
		})
	}
}
