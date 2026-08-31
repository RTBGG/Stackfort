// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestValidateFileDownloadRequestEnforcesClosedRangeAndRelativeFile(t *testing.T) {
	t.Parallel()
	start, end := uint64(10), uint64(19)
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	valid := FileDownloadRequest{ProtocolVersion: WireVersion, RequestID: "request-1",
		Identity: hostingidentity.Spec{AccountID: accountID,
			Username: username, UID: hostingidentity.MinimumID, GID: hostingidentity.MinimumID,
			HomeDirectory: home},
		Path: "public_html/index.html", Range: &FileDownloadRange{Start: &start, EndInclusive: &end}}
	if err := ValidateFileDownloadRequest(valid); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	invalid := valid
	invalid.Path = "../etc/passwd"
	if err := ValidateFileDownloadRequest(invalid); err == nil {
		t.Fatal("traversal path accepted")
	}
	invalid = valid
	invalid.Range = &FileDownloadRange{Start: &start, SuffixLength: &end}
	if err := ValidateFileDownloadRequest(invalid); err == nil {
		t.Fatal("ambiguous range accepted")
	}
	maximum := ^uint64(0)
	zero := uint64(0)
	invalid = valid
	invalid.Range = &FileDownloadRange{Start: &zero, EndInclusive: &maximum}
	if err := ValidateFileDownloadRequest(invalid); err == nil {
		t.Fatal("overflowing range accepted")
	}
}

func TestDecodeFileDownloadFrameRejectsTrailingData(t *testing.T) {
	t.Parallel()
	valid := `{"totalSize":4,"offset":0,"length":4,"modifiedAt":"2026-08-28T12:00:00Z","partial":false}`
	frame, err := DecodeFileDownloadFrame(strings.NewReader(valid))
	if err != nil || frame.TotalSize != 4 || frame.ModifiedAt.Location() != time.UTC {
		t.Fatalf("frame=%#v err=%v", frame, err)
	}
	if _, err := DecodeFileDownloadFrame(strings.NewReader(valid + `{}`)); err == nil {
		t.Fatal("trailing helper frame data accepted")
	}
}
