// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestBackupDownloadContractIsClosedAndRangeBound(t *testing.T) {
	t.Parallel()
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	start, end := uint64(10), uint64(19)
	request := BackupDownloadRequest{ProtocolVersion: WireVersion, RequestID: "backup-download-test",
		Identity: hostingidentity.Spec{AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
			GID: hostingidentity.MinimumID, HomeDirectory: home},
		BackupID: "019c1234-5678-7abc-8def-0123456789c6",
		Range:    &FileDownloadRange{Start: &start, EndInclusive: &end}}
	encoded, _ := json.Marshal(request)
	decoded, err := DecodeBackupDownloadRequest(bytes.NewReader(encoded))
	if err != nil || decoded.BackupID != request.BackupID {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}
	request.BackupID = "../manifest.json"
	if ValidateBackupDownloadRequest(request) == nil {
		t.Fatal("non-UUID backup selector accepted")
	}
}

func TestBackupDownloadErrorRejectsFileNamespaceCodes(t *testing.T) {
	t.Parallel()
	response := FileDownloadErrorResponse{ProtocolVersion: WireVersion, RequestID: "backup-download-test",
		Error: ResponseError{Code: ErrorBackupIntegrity, Message: "integrity failed"}}
	encoded, _ := json.Marshal(response)
	if _, err := DecodeBackupDownloadErrorResponse(bytes.NewReader(encoded), response.RequestID); err != nil {
		t.Fatalf("valid backup error: %v", err)
	}
	response.Error.Code = ErrorFileNotFound
	encoded, _ = json.Marshal(response)
	if _, err := DecodeBackupDownloadErrorResponse(bytes.NewReader(encoded), response.RequestID); err == nil {
		t.Fatal("file-namespace error accepted on backup endpoint")
	}
}
