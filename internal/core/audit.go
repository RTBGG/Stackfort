// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode"

	"github.com/RTBGG/stackfort/internal/store"
)

type auditHashPayload struct {
	ID            string          `json:"id"`
	OccurredAt    string          `json:"occurredAt"`
	ActorID       string          `json:"actorId,omitempty"`
	SessionID     string          `json:"sessionId,omitempty"`
	SourceAddress string          `json:"sourceAddress,omitempty"`
	Action        string          `json:"action"`
	TargetType    string          `json:"targetType"`
	TargetID      string          `json:"targetId,omitempty"`
	AccountID     string          `json:"accountId,omitempty"`
	RequestID     string          `json:"requestId,omitempty"`
	OperationID   string          `json:"operationId,omitempty"`
	Result        string          `json:"result"`
	Details       json.RawMessage `json:"details"`
}

func (r *Repository) AppendAuditEvent(ctx context.Context, params AppendAuditEventParams) (AuditEvent, error) {
	now := r.timestamp()
	var event AuditEvent
	err := r.state.Write(ctx, func(executor store.Executor) error {
		created, err := r.appendAuditEventTx(ctx, executor, params, now)
		if err != nil {
			return err
		}
		event = created
		return nil
	})
	if err != nil {
		return AuditEvent{}, classifyDatabaseError(err)
	}
	return event, nil
}

func (r *Repository) appendAuditTx(
	ctx context.Context,
	executor store.Executor,
	params AppendAuditEventParams,
	now time.Time,
) error {
	_, err := r.appendAuditEventTx(ctx, executor, params, now)
	return err
}

func (r *Repository) appendAuditEventTx(
	ctx context.Context,
	executor store.Executor,
	params AppendAuditEventParams,
	now time.Time,
) (AuditEvent, error) {
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return AuditEvent{}, err
	}
	if err := validateOptionalID(params.SessionID, "sessionId"); err != nil {
		return AuditEvent{}, err
	}
	if err := validateOptionalID(params.AccountID, "accountId"); err != nil {
		return AuditEvent{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return AuditEvent{}, err
	}
	action, err := validateAction(params.Action, "action", 120)
	if err != nil {
		return AuditEvent{}, err
	}
	targetType, err := validateAction(params.TargetType, "targetType", 80)
	if err != nil {
		return AuditEvent{}, err
	}
	targetID, err := validateOptionalText(params.TargetID, "targetId", 128)
	if err != nil {
		return AuditEvent{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return AuditEvent{}, err
	}
	sourceAddress := strings.TrimSpace(params.SourceAddress)
	if sourceAddress != "" {
		address, err := netip.ParseAddr(sourceAddress)
		if err != nil {
			return AuditEvent{}, fmt.Errorf("%w: sourceAddress must be an IP address", ErrInvalidInput)
		}
		sourceAddress = address.String()
	}
	if params.Result != AuditSuccess && params.Result != AuditFailure && params.Result != AuditDenied {
		return AuditEvent{}, fmt.Errorf("%w: unsupported audit result", ErrInvalidInput)
	}
	detailsJSON, details, err := encodeAuditDetails(params.Details)
	if err != nil {
		return AuditEvent{}, err
	}
	id, err := r.newID()
	if err != nil {
		return AuditEvent{}, err
	}

	previousHash := make([]byte, sha256.Size)
	var storedPrevious []byte
	err = executor.QueryRowContext(ctx, `
		SELECT event_hash
		FROM audit_events
		ORDER BY sequence DESC
		LIMIT 1`).Scan(&storedPrevious)
	if err == nil {
		copy(previousHash, storedPrevious)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return AuditEvent{}, err
	}

	payload := auditHashPayload{
		ID:            string(id),
		OccurredAt:    formatTime(now),
		ActorID:       optionalIDString(params.ActorID),
		SessionID:     optionalIDString(params.SessionID),
		SourceAddress: sourceAddress,
		Action:        action,
		TargetType:    targetType,
		TargetID:      targetID,
		AccountID:     optionalIDString(params.AccountID),
		RequestID:     requestID,
		OperationID:   optionalIDString(params.OperationID),
		Result:        string(params.Result),
		Details:       json.RawMessage(detailsJSON),
	}
	eventHash, err := hashAuditPayload(previousHash, payload)
	if err != nil {
		return AuditEvent{}, err
	}

	result, err := executor.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, occurred_at, actor_identity_id, session_id, source_address,
			action, target_type, target_id, account_id, request_id, operation_id,
			result, details_json, previous_hash, event_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(id),
		formatTime(now),
		nullableID(params.ActorID),
		nullableID(params.SessionID),
		nullableString(sourceAddress),
		action,
		targetType,
		nullableString(targetID),
		nullableID(params.AccountID),
		nullableString(requestID),
		nullableID(params.OperationID),
		string(params.Result),
		detailsJSON,
		previousHash,
		eventHash,
	)
	if err != nil {
		return AuditEvent{}, err
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return AuditEvent{}, err
	}
	return AuditEvent{
		Sequence:      sequence,
		ID:            id,
		OccurredAt:    now,
		ActorID:       params.ActorID,
		SessionID:     params.SessionID,
		SourceAddress: sourceAddress,
		Action:        action,
		TargetType:    targetType,
		TargetID:      targetID,
		AccountID:     params.AccountID,
		RequestID:     requestID,
		OperationID:   params.OperationID,
		Result:        params.Result,
		Details:       details,
		PreviousHash:  append([]byte(nil), previousHash...),
		EventHash:     append([]byte(nil), eventHash...),
	}, nil
}

// VerifyAuditChain checks every visible event against its predecessor. An
// exported checkpoint is still required to detect removal of the newest tail.
func (r *Repository) VerifyAuditChain(ctx context.Context) error {
	return r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT sequence, id, occurred_at, actor_identity_id, session_id,
			       source_address, action, target_type, target_id, account_id,
			       request_id, operation_id, result, details_json,
			       previous_hash, event_hash
			FROM audit_events
			ORDER BY sequence`)
		if err != nil {
			return err
		}
		defer rows.Close()

		expectedPrevious := make([]byte, sha256.Size)
		for rows.Next() {
			var sequence int64
			var id, occurredAt, action, targetType, result, detailsJSON string
			var actorID, sessionID, sourceAddress, targetID, accountID, requestID, operationID sql.NullString
			var previousHash, eventHash []byte
			if err := rows.Scan(
				&sequence,
				&id,
				&occurredAt,
				&actorID,
				&sessionID,
				&sourceAddress,
				&action,
				&targetType,
				&targetID,
				&accountID,
				&requestID,
				&operationID,
				&result,
				&detailsJSON,
				&previousHash,
				&eventHash,
			); err != nil {
				return err
			}
			if len(previousHash) != sha256.Size || subtle.ConstantTimeCompare(previousHash, expectedPrevious) != 1 {
				return fmt.Errorf("audit chain previous hash mismatch at sequence %d", sequence)
			}
			payload := auditHashPayload{
				ID:            id,
				OccurredAt:    occurredAt,
				ActorID:       actorID.String,
				SessionID:     sessionID.String,
				SourceAddress: sourceAddress.String,
				Action:        action,
				TargetType:    targetType,
				TargetID:      targetID.String,
				AccountID:     accountID.String,
				RequestID:     requestID.String,
				OperationID:   operationID.String,
				Result:        result,
				Details:       json.RawMessage(detailsJSON),
			}
			expectedHash, err := hashAuditPayload(previousHash, payload)
			if err != nil {
				return err
			}
			if len(eventHash) != sha256.Size || subtle.ConstantTimeCompare(eventHash, expectedHash) != 1 {
				return fmt.Errorf("audit chain event hash mismatch at sequence %d", sequence)
			}
			expectedPrevious = append(expectedPrevious[:0], eventHash...)
		}
		return rows.Err()
	})
}

func encodeAuditDetails(value map[string]any) (string, map[string]any, error) {
	encoded, err := encodeObject(value, maxAuditDetailsBytes)
	if err != nil {
		return "", nil, err
	}
	details, err := decodeObject(encoded)
	if err != nil {
		return "", nil, err
	}
	if err := rejectAuditSecrets(details, "details"); err != nil {
		return "", nil, err
	}
	return encoded, details, nil
}

func rejectAuditSecrets(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalizedKey := strings.Map(func(character rune) rune {
				if unicode.IsLetter(character) || unicode.IsDigit(character) {
					return unicode.ToLower(character)
				}
				return -1
			}, key)
			for _, forbidden := range auditForbiddenPieces {
				if strings.Contains(normalizedKey, forbidden) {
					return fmt.Errorf("%w: audit field %s.%s may contain secret material", ErrInvalidInput, path, key)
				}
			}
			if err := rejectAuditSecrets(child, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := rejectAuditSecrets(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func hashAuditPayload(previousHash []byte, payload auditHashPayload) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode audit hash payload: %w", err)
	}
	digest := sha256.New()
	if _, err := digest.Write(previousHash); err != nil {
		return nil, err
	}
	if _, err := digest.Write(encoded); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
}

func optionalIDString(value *ID) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
