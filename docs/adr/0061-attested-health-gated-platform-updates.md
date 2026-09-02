# ADR 0061: Attested, health-gated platform updates with exact rollback

- Status: accepted
- Date: 2026-09-02
- Extends: [ADR 0060](0060-immutable-stable-beta-release-discovery.md)

## Context

Release discovery alone cannot safely mutate a hosting server. An update can
replace the API and agent that initiated it, migrate the panel database, change
native NGINX/Coraza/Vinyl packages, and temporarily interrupt hosted traffic.
The control path must survive those restarts, reject mutable or substituted
artifacts, and restore the exact prior release if any migration or health gate
fails.

Running an updater inside the unprivileged API would couple recovery to the
process being replaced. Allowing the API to submit arbitrary root commands
would also defeat the local agent's closed protocol boundary.

## Decision

1. Keep functional updates manual. The default-on stable/beta check discovers
   releases, but never installs one automatically.
2. Accept only the exact newer immutable release currently persisted by release
   discovery. Require platform-manage permission, CSRF protection, a recently
   authenticated administrator session, and an audit event before contacting
   the privileged agent.
3. Give the agent two closed operations: inspect the bounded updater status and
   start `stackfort-update@<canonical-version>.service`. The protocol accepts
   only `X.Y.Z` and `X.Y.Z-beta.N`, carries no account ID, executable, argument
   list, environment, path, or shell input, and binds the start to the persisted
   audit-event UUID.
4. Run a separate root-owned `stackfort-updater` oneshot. A short unit delay
   lets the API return HTTP 202 before the updater stops the control services.
   The browser reconnects through the ordinary authenticated status endpoint.
5. Fetch the exact current and target tags from `RTBGG/Stackfort`. Refuse
   redirects outside GitHub, drafts, mutable releases, tag/prerelease
   mismatches, duplicate or incomplete assets, missing GitHub SHA-256 digests,
   non-canonical asset URLs, oversized responses, and unsafe archives.
6. Compare the archive against both GitHub's asset digest and `SHA256SUMS`.
   Query the public repository attestation endpoint by that digest, accept only
   bounded Snappy bundles from GitHub's pinned production bundle store, and
   verify them locally with the release-bundled, checksum-pinned GitHub CLI.
   Pin the repository, exact tag source ref, release workflow, and GitHub-hosted
   runner policy without requiring a server-side GitHub token. Store only a
   fully inspected release beneath the root-owned updater directory and persist
   a durable provenance record.
7. Use a root-owned, mode-0600, atomically replaced and directory-synced journal
   outside the panel database. Fence it to the exact current/target source
   digest pair and serialize updates with a non-blocking file lock.
8. Apply these journaled stages in order: stop control services, create a
   consistent SQLite snapshot, transition exact native packages, activate the
   release payload, reconcile system configuration, run the target database
   migration, start services, and pass the full installation health gate.
9. On any stage or post-mutation journal failure, use an independent bounded
   rollback context. Stop services, restore the SQLite snapshot, restore the
   exact current native packages, payload, configuration, and NGINX state, then
   start and health-check the prior release.
10. Treat an interrupted `applying`, `rolling_back`, or `rollback_failed`
    journal as recovery work. Never continue blindly from the last stage. A
    deliberate invocation of the same immutable target rolls back first; a
    different target is rejected until recovery is terminal. Recovery accepts
    the running updater only when its embedded version is one side of the
    journal's verified current/target pair, then restores from the journal's
    original current release even if the target updater payload was already
    activated.

## Consequences

- Updating does not widen the API or agent into a generic root execution
  surface.
- The API and agent may be replaced or disconnected without losing the update
  transaction or its recovery evidence.
- Database migration is forward-only within the target activation, while the
  preserved SQLite snapshot provides exact transaction rollback.
- Both current and target releases are freshly verified before mutation, so
  rollback does not trust an unverified local payload.
- Native package changes can move forward or backward between the two verified
  manifests. Package-level failure does not erase the outer transaction's
  rollback base.
- Functional updates remain amd64-only until native arm64 artifacts and upgrade
  matrices are qualified.
- A host with a non-terminal journal intentionally fails closed. Operators can
  inspect the root-owned journal and systemd unit without exposing raw paths or
  error output to the browser.

## References

- <https://cli.github.com/manual/gh_attestation_verify>
- <https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline>
- <https://docs.github.com/en/rest/repos/attestations>
- <https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases>
