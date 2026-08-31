// SPDX-License-Identifier: AGPL-3.0-or-later

package agentrpc

import (
	"context"
	"log"
	"log/slog"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

const (
	eventKindAudit       = "audit"
	eventKindOperational = "operational"
	eventKindSecurity    = "security"

	eventRPCCompleted = "agent.rpc.completed"
	eventRPCRejected  = "agent.rpc.rejected"
	eventPeerRejected = "agent.peer.rejected"
	eventPeerClose    = "agent.peer.close_failed"
	eventHTTPError    = "agent.http.internal_error"
)

func logRPCCompleted(
	ctx context.Context,
	logger *slog.Logger,
	request agentprotocol.Request,
	status int,
	replayed bool,
) {
	attributes := rpcEventAttributes(request, eventRPCCompleted)
	attributes = append(attributes,
		slog.Int("http_status", status),
		slog.Bool("replayed", replayed),
	)
	logger.LogAttrs(ctx, slog.LevelInfo, "agent RPC completed", attributes...)
}

func logRPCRejected(
	ctx context.Context,
	logger *slog.Logger,
	request agentprotocol.Request,
	status int,
	reasonCode string,
) {
	attributes := rpcEventAttributes(request, eventRPCRejected)
	attributes = append(attributes,
		slog.Int("http_status", status),
		slog.String("reason_code", reasonCode),
	)
	logger.LogAttrs(ctx, slog.LevelWarn, "agent RPC rejected", attributes...)
}

func rpcEventAttributes(request agentprotocol.Request, eventCode string) []slog.Attr {
	attributes := []slog.Attr{
		slog.String("event_kind", eventKindAudit),
		slog.String("event_code", eventCode),
		slog.String("request_id", request.RequestID),
		slog.String("operation", string(request.Operation)),
	}
	if request.Correlation == nil {
		return attributes
	}
	attributes = append(attributes,
		slog.String("operation_id", request.Correlation.OperationID),
		slog.String("actor_kind", string(request.Correlation.ActorKind)),
	)
	if request.Correlation.ActorID != "" {
		attributes = append(attributes, slog.String("actor_id", request.Correlation.ActorID))
	}
	if request.Correlation.AccountID != "" {
		attributes = append(attributes, slog.String("account_id", request.Correlation.AccountID))
	}
	return attributes
}

func logPeerRejected(logger *slog.Logger, reasonCode string, credentials *PeerCredentials) {
	attributes := []slog.Attr{
		slog.String("event_kind", eventKindSecurity),
		slog.String("event_code", eventPeerRejected),
		slog.String("reason_code", reasonCode),
	}
	if credentials != nil {
		attributes = append(attributes,
			slog.Int64("peer_pid", int64(credentials.PID)),
			slog.Uint64("peer_uid", uint64(credentials.UID)),
		)
	}
	logger.LogAttrs(context.Background(), slog.LevelWarn, "agent peer rejected", attributes...)
}

func logRejectedPeerCloseFailure(logger *slog.Logger) {
	logger.LogAttrs(context.Background(), slog.LevelWarn, "rejected agent peer close failed",
		slog.String("event_kind", eventKindSecurity),
		slog.String("event_code", eventPeerClose),
		slog.String("reason_code", "connection_close_failed"),
	)
}

// NewRedactedHTTPErrorLogger adapts net/http's unstructured ErrorLog hook to a
// stable event without copying panic values, request fragments, or raw errors.
func NewRedactedHTTPErrorLogger(logger *slog.Logger) *log.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return log.New(redactedHTTPErrorWriter{logger: logger}, "", 0)
}

type redactedHTTPErrorWriter struct {
	logger *slog.Logger
}

func (writer redactedHTTPErrorWriter) Write(content []byte) (int, error) {
	writer.logger.LogAttrs(context.Background(), slog.LevelError, "agent HTTP server error",
		slog.String("event_kind", eventKindOperational),
		slog.String("event_code", eventHTTPError),
	)
	return len(content), nil
}
