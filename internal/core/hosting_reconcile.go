// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

// HostingAccountHostReady is the control-plane gate before domain work may be
// queued. It never infers readiness from account status alone.
func (r *Repository) HostingAccountHostReady(ctx context.Context, accountID ID) (bool, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return false, err
	}
	var ready bool
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM hosting_accounts AS h
				JOIN hosting_account_unix_identities AS u ON u.account_id = h.id
				JOIN hosting_account_filesystems AS f ON f.account_id = h.id
				JOIN hosting_account_resources AS r ON r.account_id = h.id
				WHERE h.id = ? AND h.status = 'active'
				  AND u.lifecycle_state = 'reconciled' AND u.oci_runtime_reconciled_at IS NOT NULL
				  AND f.status = 'applied' AND f.capability_status = 'available'
				  AND r.status = 'applied' AND r.capability_status = 'available'
			)`, string(accountID)).Scan(&ready)
	})
	return ready, classifyDatabaseError(err)
}

func (r *Repository) ListHostingAccountsNeedingHostReconcile(
	ctx context.Context,
	limit int,
) ([]ID, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: reconciliation limit must be between 1 and 100", ErrInvalidInput)
	}
	accounts := make([]ID, 0, limit)
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT h.id
			FROM hosting_accounts AS h
			JOIN hosting_account_unix_identities AS u ON u.account_id = h.id
			JOIN hosting_account_filesystems AS f ON f.account_id = h.id
			JOIN hosting_account_resources AS r ON r.account_id = h.id
			WHERE h.status = 'active'
			  AND (u.lifecycle_state = 'allocated' OR u.oci_runtime_reconciled_at IS NULL OR
			       f.status = 'pending' OR r.status = 'pending')
			ORDER BY h.created_at, h.id
			LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var accountID ID
			if err := rows.Scan(&accountID); err != nil {
				return err
			}
			accounts = append(accounts, accountID)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return accounts, nil
}
