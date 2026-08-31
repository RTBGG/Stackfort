# Transactional NGINX site activation

F-003 turns the deterministic F-002 output into a crash-recoverable host
revision. The control API sends typed domain intent, never NGINX text or a
caller-selected path. The privileged agent renders again, validates a complete
candidate with the vendor binary, switches one symlink atomically, gracefully
reloads NGINX, probes the selected route locally, and commits or restores the
previous revision.

## Owned layout

```text
/etc/nginx/stackfort/
├── sites-enabled/
│   └── 00-current.conf
├── sites-current -> site-revisions/<active UUIDv7>
├── site-revisions/
│   └── <operation UUIDv7>/
│       ├── .stackfort-revision.json
│       └── account-<account UUIDv7>.conf
├── site-transactions/
│   └── <operation UUIDv7>/nginx.conf
├── site-activation.json
└── .site-activation.lock
```

The fixed `00-current.conf` includes only `sites-current/*.conf`. Each revision
is a complete global set of account includes: staging copies the existing
root-owned account files, replaces or removes the affected account file, and
writes a manifest containing typed identity, desired-revision, digest, and
count metadata. The candidate main configuration points directly at that
private revision, so `nginx -t` checks the complete proposed include graph
without changing the live pointer.

Directories are root-owned mode `0750`; generated configuration and manifests
are `0640`; the journal and non-blocking `flock` anchor are `0600`. Symlinks,
hard-linked files, non-root ownership, unexpected names, unsafe modes, malformed
UUIDs, and unknown JSON fields fail closed. Cleanup visits only an exact
validated UUID directory and removes regular root-owned files without recursive
path traversal.

## Activation protocol

One durable operation UUID is also the immutable host revision UUID. With the
exclusive activation lock held, the agent performs:

1. recover any existing journal before considering new intent;
2. render the typed snapshot and compare it with the active manifest and file
   digest for idempotent replay;
3. persist a `preparing` journal and build full revision and test roots;
4. persist `staged`, run the fixed candidate `nginx -t`, then persist
   `validated`;
5. atomically rename a relative symlink to promote the candidate and persist
   `promoted`;
6. ask systemd for a graceful `nginx.service` reload, verify the unit remains
   loaded, active, and enabled, and persist `reloaded`;
7. connect to `127.0.0.1:80` with one validated domain in `Host`; the rejecting
   default returns no HTTP status, while the intended route must return one;
8. persist `healthy`, remove the private test root and journal, and leave the
   new immutable revision active.

Every journal replacement, pointer replacement, and material removal is
followed by a parent-directory sync. A candidate syntax error occurs before
promotion. Any post-promotion error restores the previous pointer, checks that
configuration, reloads it, verifies service state, and only then removes the
failed revision and journal.

## Restart recovery and convergence

The journal records `preparing`, `staged`, `validated`, `promoted`, `reloaded`,
`healthy`, or `recovering`. On agent restart, the next activation first compares
the journal with the live pointer. A candidate that may have become visible is
rolled back and the previous configuration is revalidated/reloaded before the
incomplete tree is removed. Pre-promotion staging is removed directly. An
unexpected pointer is a conflict, not an invitation to guess.

API recovery uses the existing leased operation state machine. The operation
payload stores an immutable typed domain snapshot and desired-state revision;
it does not reread mutable domain rows on retry. If the API restarts after the
agent committed, the same operation UUID matches the active manifest and the
agent performs only baseline validation and the local health probe. Migration
011 makes `(account_id, operation_id)` unique in applied-state history, while
`RecordAppliedStateRevision` returns the matching active record on an exact
replay. A different digest or desired revision with the same operation fails as
a conflict.

Successful current and prior immutable revisions are retained for now. Bounded
retention and administrator-directed rollback are later operational features;
F-003 never deletes an unrelated or recursively discovered path.

## Verification

Unit tests inject failures into candidate validation, validation journaling,
promotion, reload, reload journaling, service inspection, health checking,
health journaling, and commit. Each converges to the previous valid revision.
They also cover exact replay after an agent cache restart, interrupted-promotion
recovery, strict operation payloads, stable worker failure codes, and idempotent
applied-state recording.

The disposable-host test performs real first activation, replay with a fresh
agent activator, a manually interrupted `promoted` transaction, recovery, a
second activation, real vendor configuration tests, graceful reloads, local
HTTP probes, and final pointer/journal checks on Debian 13, Ubuntu 26.04, and
Rocky Linux 10.

Upstream references:

- [NGINX command-line parameters](https://nginx.org/en/docs/switches.html)
- [NGINX configuration reload behavior](https://nginx.org/en/docs/control.html)
- [NGINX beginner's guide: reloading configuration](https://nginx.org/en/docs/beginners_guide.html)

