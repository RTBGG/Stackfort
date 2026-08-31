-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE TABLE stackfort_metadata (
    key TEXT PRIMARY KEY NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) WITHOUT ROWID, STRICT;
