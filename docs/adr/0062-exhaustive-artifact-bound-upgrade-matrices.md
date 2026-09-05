# ADR 0062: Exhaustive artifact-bound upgrade matrices

- Status: accepted
- Date: 2026-09-05

## Context

Journal/runner simulations established transaction behavior, but cannot establish
that a previously installed binary can actually replace its own payload or that
its state remains compatible with a candidate. No Stackfort releases have been
published yet, so a claimed public release-to-release result would be premature.

## Decision

Maintain an explicit supported/retired predecessor catalog with immutable archive
digests. Generate every supported predecessor × supported distribution × required
scenario. Include skipped-version and beta-to-stable paths. Compare the catalog
with GitHub's complete public inventory before publishing; retirement requires a
documented decision and omission is an error.

Qualify the exact candidate archive with a driver compiled from each predecessor's
tagged production source. Install the predecessor on a clean disposable host and
use its actual updater runner for success, injected final-health failure, and
subprocess interruption after migration. Preserve keys and data, verify complete
installed health, and require durable terminal journals. Source archives in
release-candidate runs additionally pass the real public provenance stager.

Bind every evidence cell to source and target archive digests. Review evidence on
`main` after testing a fixed candidate commit, then tag that candidate commit. The
tag workflow resolves a fixed `main` SHA for evidence and checks it against the
rebuilt archive before publication. This separates the evidence commit from the
build input without weakening digest matching. A different archive needs new
qualification. Rehearsals never satisfy this gate.

Run a complementary migration test for every historical SQL prefix in ordinary
CI. Verify existing metadata, identities, credentials, roles, migration history,
integrity, foreign keys, idempotent reopen, and prior-schema-compatible snapshots.

## Findings incorporated

The real host rehearsal exposed issues hidden by transaction simulations:
systemd templates need an instance for `LoadState` inspection, and the fresh-host
payload writer correctly rejected changed existing files but had no update path.
The installer now inspects a fixed inactive updater instance. The update-only
payload path requires two freshly inspected root-owned source trees, accepts only
their exact installed content/metadata, atomically replaces approved files, and
removes only known obsolete tree entries with directory synchronization. Both
directions and mixed-state recovery use the same rules. Fresh installation retains
its conflict behavior. Unknown files, symlinks, and file/directory type changes are
refused. File/directory changes need an intermediate release or an explicit future
migration mechanism.

The clean Rocky checkpoint also exposed a missing Vinyl runtime prerequisite.
The installer now installs `jemalloc` from signed EPEL before the exact local RPM
transaction; retaining RPM's downgrade path allows restoration of the prior
native artifact. A mocked command-sequence test now requires this dependency
step, and the real clean-host upgrade rehearsal checks the result.

## Consequences

The first release's empty upgrade matrix is accepted only when the public
predecessor inventory is also empty. Subsequent releases require complete reviewed
evidence. Windows/Hyper-V orchestrates disposable Linux hosts; release publication
does not depend on a persistent privileged GitHub self-hosted runner. Evidence
authenticity relies on maintainer review and the repository's `main` write boundary;
test reports are not presented as independently attested execution proofs.

See [the operator procedure](../upgrade-matrix.md). Broader isolation, WAF/cache,
OCI, performance, and clean-install qualification remain separate release gates.
