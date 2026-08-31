// SPDX-License-Identifier: AGPL-3.0-or-later

package agentrpc

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/buildinfo"
)

func TestRPCEventsContainOnlyValidatedCorrelationMetadata(t *testing.T) {
	t.Parallel()

	const secret = "payload-secret-must-not-be-logged"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion,
		RequestID:       "safe-request-id",
		IdempotencyKey:  "idempotency-key-must-not-be-logged",
		Operation:       "test.privileged-mutation",
		Correlation: &agentprotocol.AuditCorrelation{
			OperationID: "019c1234-5678-7abc-8def-0123456789ab",
			ActorKind:   agentprotocol.ActorIdentity,
			ActorID:     "019c1234-5678-7abc-8def-0123456789ac",
			AccountID:   "019c1234-5678-7abc-8def-0123456789ad",
		},
		Handshake: &agentprotocol.HandshakeRequest{
			MinimumVersion: 1, MaximumVersion: 1,
			ClientBuild: buildinfo.Info{Version: secret, Commit: secret, BuildDate: secret},
		},
	}

	logRPCCompleted(t.Context(), logger, request, 200, false)
	logRPCRejected(t.Context(), logger, request, 409, "idempotency_conflict")
	logs := output.String()
	for _, forbidden := range []string{secret, request.IdempotencyKey, "clientBuild", "handshake"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("agent event leaked %q: %s", forbidden, logs)
		}
	}
	for _, expected := range []string{
		eventRPCCompleted, eventRPCRejected, request.RequestID, string(request.Operation),
		request.Correlation.OperationID, request.Correlation.ActorID,
		request.Correlation.AccountID, "idempotency_conflict",
	} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("agent event omitted %q: %s", expected, logs)
		}
	}
	for index, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("decode log line %d: %v", index, err)
		}
		if decoded["event_kind"] != eventKindAudit {
			t.Fatalf("log line %d event kind = %#v", index, decoded["event_kind"])
		}
	}
}

func TestHandlerLogsReplayWithoutRequestPayload(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	request := handshakeRequest("log-request-1", "log-replay-key", 1, 1)
	handler := NewHandler(logger)
	first := rpcRequest(t, request)
	handler.ServeHTTP(httptest.NewRecorder(), first)
	request.RequestID = "log-request-2"
	handler.ServeHTTP(httptest.NewRecorder(), rpcRequest(t, request))

	logs := output.String()
	if strings.Count(logs, eventRPCCompleted) != 2 ||
		!strings.Contains(logs, `"replayed":false`) ||
		!strings.Contains(logs, `"replayed":true`) ||
		strings.Contains(logs, request.IdempotencyKey) || strings.Contains(logs, "clientBuild") {
		t.Fatalf("replay logs = %s", logs)
	}
}

func TestHTTPServerErrorLoggerDropsRawMessage(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := NewRedactedHTTPErrorLogger(slog.New(slog.NewJSONHandler(&output, nil)))
	logger.Print("panic containing raw-request-secret and a complete request fragment")
	logs := output.String()
	if strings.Contains(logs, "raw-request-secret") || strings.Contains(logs, "request fragment") ||
		!strings.Contains(logs, eventHTTPError) || !strings.Contains(logs, eventKindOperational) {
		t.Fatalf("redacted HTTP log = %s", logs)
	}
}

func TestMalformedRPCPayloadIsNeverLogged(t *testing.T) {
	t.Parallel()

	const secret = "malformed-body-secret"
	var output bytes.Buffer
	handler := NewHandler(slog.New(slog.NewJSONHandler(&output, nil)))
	request := httptest.NewRequest(http.MethodPost, agentprotocol.Endpoint, strings.NewReader(
		`{"protocolVersion":1,"requestId":"bad-request","idempotencyKey":"bad-key","operation":"command.run",`+
			`"password":"`+secret+`","payload":{"complete":"untrusted"}}`,
	))
	request.Header.Set("Content-Type", agentprotocol.MediaType)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || output.Len() != 0 ||
		strings.Contains(output.String(), secret) {
		t.Fatalf("status=%d logs=%s", recorder.Code, output.String())
	}
}
