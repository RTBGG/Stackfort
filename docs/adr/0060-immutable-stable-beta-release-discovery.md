# ADR 0060: Discover only complete immutable stable and beta releases

- Status: accepted
- Date: 2026-09-02
- Extends: [ADR 0059](0059-passive-native-release-carrier.md)

## Context

Phase 6 needs useful default-on update checks before the privileged staged
updater exists. Treating every tag, prerelease flag, or partially uploaded
GitHub Release as installable would create ambiguity and a supply-chain race.
Release discovery must not silently become functional automatic updating, nor
should it execute mutable branch content or accept an incomplete artifact set.

## Decision

1. Use canonical tags only: `vX.Y.Z` for stable and `vX.Y.Z-beta.N` for beta.
   The stable channel considers stable releases only. The beta channel considers
   stable and beta releases and selects the greatest semantic version.
2. Query the fixed public GitHub Releases API endpoint. Use a ten-second
   timeout, refuse redirects, bound the response to 2 MiB and 20 releases, send
   conditional ETag requests, and retain rate-limit reset state.
3. Reject drafts, tag/prerelease mismatches, mutable releases, invalid semantic
   versions, duplicate assets, and incomplete artifact inventories. A candidate
   must contain the amd64 archive, installer, passive DEB and RPM carriers, SPDX
   SBOM, and checksum manifest; every required asset must be uploaded, non-empty,
   and carry a GitHub `sha256` digest. Native package metadata sidecars must
   match the exact release-version-derived package names as well.
4. Enable stable-channel checks by default at a fixed six-hour interval. Retry
   failures after one hour and enforce a one-minute manual-check cooldown.
   Persist only bounded policy, timestamps, ETag, candidate metadata, and stable
   error codes.
5. Restrict status to platform administrators. Require CSRF protection for
   manual checks and require platform-manage permission, recent authentication,
   request/source metadata, and an audit event for policy changes.
6. Keep automatic functional updates disabled. Discovery never downloads,
   stages, verifies locally, activates, restarts, migrates, or rolls back a
   release.
7. Publish beta tags as prereleases that do not become `latest`. Immediately
   verify the channel, immutable state, and GitHub attestation after publication.
   If repository immutability was not enabled, remove the just-created mutable
   release and fail; do not require a long-lived administration token in CI.

## Consequences

- Administrators receive a low-frequency, data-minimized availability signal
  without granting a network response authority to mutate the host.
- Beta participation is explicit and reversible; changing channel clears its
  ETag and cached candidate so releases are re-evaluated under the new policy.
- A release with missing or still-changing assets remains invisible even when
  its tag exists.
- Update installation remains a separate milestone with local provenance/hash
  verification, staging, migration boundaries, health gates, and rollback.
- Repository maintainers must enable GitHub release immutability before the
  first tagged release. A missed setting produces a failed run and cleanup of
  the mutable release while preserving the pushed tag for a safe retry.

## References

- <https://docs.github.com/en/rest/releases/releases>
- <https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api>
- <https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases>
- <https://cli.github.com/manual/gh_release_create>
- <https://cli.github.com/manual/gh_release_verify>
