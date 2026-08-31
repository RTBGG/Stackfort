-- SPDX-License-Identifier: AGPL-3.0-or-later

ALTER TABLE hosting_account_unix_identities
ADD COLUMN oci_runtime_reconciled_at TEXT;

CREATE TRIGGER hosting_account_oci_identity_range_insert
BEFORE INSERT ON hosting_account_unix_identities
FOR EACH ROW
WHEN NEW.uid > 249999 OR NEW.gid > 249999
BEGIN
    SELECT RAISE(ABORT, 'hosting account identity exceeds rootless OCI range');
END;

CREATE TRIGGER hosting_account_oci_runtime_history_immutable
BEFORE UPDATE OF oci_runtime_reconciled_at ON hosting_account_unix_identities
FOR EACH ROW
WHEN OLD.oci_runtime_reconciled_at IS NOT NULL
    AND NEW.oci_runtime_reconciled_at IS NOT OLD.oci_runtime_reconciled_at
BEGIN
    SELECT RAISE(ABORT, 'hosting account OCI runtime history is immutable');
END;
