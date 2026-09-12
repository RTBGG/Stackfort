# Native setup capability: terminal delivery and post-reboot activation

Status: **interactive delivery, sealed binding and local post-install activation
are implemented** in the registered public `onboard` path. Focused unit/namespace
and shell-routing tests pass. The exact beta.4 tagged archive and its real public
fresh-host/reboot/setup flow are not yet qualified or published; older beta.3
laboratory payloads do not establish that evidence.

The [installation guide](installer-installation.md#interactive-native-setup)
describes the operator workflow. This document defines its secret/activation
boundary, not release approval or general recovery authority.

## Trust boundary and delivery

After authenticating the exact release/host review, `onboard` requires explicit
fresh-disposable/no-data and reboot confirmations through a verified controlling
root terminal. It then generates 32 cryptographically random bytes
and forms the bearer token as `sfb_` followed by their **unpadded base64url**
encoding. It displays that token only to an interactive, administrator-owned
terminal, before reboot, and requires `SAVED` acknowledgement. Piped stdin cannot
grant consent; no public noninteractive flag substitutes for those steps. The
implementation does not store the raw bytes/token, put either in
process arguments or environment variables, include them in a URL, or send them
to a service journal, diagnostic artifact or unattended log.

Persist only SHA-256 of the **complete 47-character displayed token string**,
not SHA-256 of the decoded random bytes. The installer owns the exclusive,
root-owned receipt that binds this digest to its host, operation and installation
intent. The setup commitment is sealed before preparation and its digest is bound
into the runtime intent and offline boot checks. Missing, orphaned or mismatched
setup evidence stops the public tag-release path. The core bootstrap schema
intentionally has no second operation journal.
Locally supplying a digest cannot establish its preimage's entropy: only the
trusted producer may call this operation.

Once installed-payload, service-identity and health checks pass, while admission
remains closed, the installer starts the capability's one-hour lifetime with
this fixed local invocation:

```text
/usr/sbin/runuser --user stackfort -- /usr/local/bin/stackfort-api bootstrap import-digest --ttl=1h
```

The caller must verify the executable and service identity, use a controlled
environment/state path, and supply exactly 64 lowercase ASCII hexadecimal
characters plus one LF, followed by EOF, on a private stdin pipe. No token or
digest is accepted as a positional argument or environment-variable input; the
command has no `--replace` option. Run the database command as its service UID/GID
to avoid root-owned SQLite WAL/SHM sidecars. This is the same private-database
authority as the existing local `bootstrap create` command; it is not a new
privileged service or HTTP endpoint.

Input is bounded to 66 bytes (the extra byte detects trailing input). Invalid
framing, extra flags, positional arguments, missing IO or out-of-range TTLs fail
before opening the database. Invalid option/input errors do not echo supplied
values. A stalled stdin is bounded by the existing CLI command context timeout.

## Registration and crash reconciliation

`core.RegisterBootstrapCapabilityDigest` accepts a typed
`BootstrapCapabilityDigest`, an optional TTL and an optional audit request ID.
The digest's default zero value is rejected. The core TTL defaults to 15 minutes
and permits 1 minute through 1 hour; the CLI defaults to 15 minutes and accepts
only that explicit range.

The existing serialized SQLite creation transaction checks that no platform
administrator exists and samples the registration time inside the transaction.
It stores only the digest. Registration uses the existing globally unique digest,
single active-capability index and immutable terminal-history constraints; there
is no schema migration.

Successful stdout is one JSON object followed by LF:

```json
{"id":"<UUIDv7>","createdAt":"<RFC3339Nano UTC>","expiresAt":"<RFC3339Nano UTC>","alreadyRegistered":false}
```

These are the fields of `core.RegisteredBootstrapCapability`. There is no token
or digest in the result or creation audit event. `alreadyRegistered` is `true`
only on an exact still-active digest retry. Such a retry returns the original ID,
creation time and expiry; it does not extend expiry, create another row, add an
audit event or reset persistent redemption rate limits. This permits bounded
reconciliation if power is lost after the database commit but before the
installer records its nonsecret activation metadata. The caller must bind that
metadata to its original sealed intent, rather than treating stdout as a new
installation authorization.

The native installer retains the canonical nonsecret registration receipt,
checks its operation/setup binding and exact one-hour duration, and never
renews it on normal boots or completed reruns. A saved receipt may legitimately
describe an expired or consumed code; live admission does not require the initial
setup code to remain active forever. If registration committed but receipt
creation failed, only the core's exact active-digest retry can reconcile it.

A different active digest conflicts. A previously expired, replaced or consumed
digest cannot be renewed or reactivated; historical uniqueness failures also
roll back any attempted invalidation of another expired row. An existing
administrator disables registration, including same-digest retries. After
expiration or lost terminal delivery, recovery requires the administrator's
existing explicit local `bootstrap create` workflow, not automatic replacement.

The unauthenticated HTTP surface remains unchanged: status and redemption only.
Imported capabilities use the existing single-use administrator-creation
transaction, persistent source/global request limits, bounded password
derivation and final expiry/administrator rechecks.

## Verification and remaining release qualification

Focused core/CLI tests cover canonical parsing, TTL bounds and transaction timing,
concurrent exact imports, immutable terminal replay, output-failure retries,
unchanged rate limits, normal redemption, administrator-exists refusal,
stdin limits/cancellation and malformed-input failures without state creation.
The existing create/explicit-replace, HTTP and bootstrap tests remain applicable.

Installer tests additionally cover terminal refusal/acknowledgement ordering,
output/close failures before arming, setup-bound preparation, canonical and unsafe
records, registration lifetime/idempotent receipts, legacy versus tag-origin
requirements, and supervised completed-rerun result validation. Namespace fixtures
use synthetic records and do not create a real administrator capability.

The [latest source-suite evidence](../infra/host-tests/results/2026-09-12-native-private-image-kernel-lifecycle.md#later-same-day-source-and-namespace-validation)
and [shell-routing evidence](../infra/host-tests/results/2026-09-12-release-readiness-validation.md#bootstrap-routing-follow-up)
are not the final public qualification. The exact tagged candidate must still
prove real terminal delivery, installation/reboot, API activation/redemption,
rerun behavior and applicable interruption/recovery boundaries end to end.
