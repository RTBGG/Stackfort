# Update channels and release checks

Stackfort can discover eligible GitHub Releases without downloading or applying
them. Release checks are enabled by default on the stable channel. Functional
automatic updates remain disabled.

## Channels

| Channel | Accepted tags | Selection |
| --- | --- | --- |
| Stable | `vX.Y.Z` | Greatest stable semantic version |
| Beta | `vX.Y.Z`, `vX.Y.Z-beta.N` | Greatest stable or beta semantic version |

Numeric components are canonical: leading zeroes, `beta.0`, release candidates,
build metadata, and arbitrary tag suffixes are rejected. A beta sorts before the
stable release with the same base version.

## Candidate gate

A release is shown only when all of these conditions hold:

- it is published, non-draft, and immutable;
- its GitHub prerelease flag agrees with its tag;
- its tag matches the selected channel;
- it has one uploaded, non-empty amd64 archive and installer;
- it has uploaded, non-empty passive DEB and RPM carriers plus their metadata
  sidecars, all named for the exact release version;
- it has an uploaded, non-empty SPDX JSON SBOM and `SHA256SUMS`;
- every required asset reports a `sha256` digest, and asset names are unique.

The fixed endpoint is
`https://api.github.com/repos/RTBGG/Stackfort/releases?per_page=20`.
Stackfort refuses redirects, uses a ten-second timeout and ETags, and accepts at
most 20 releases in a 2-MiB JSON response. It does not send account, domain, or
customer data and does not require a GitHub token for the public repository.

## Schedule and failure behavior

- Normal automatic interval: six hours.
- Failure retry: one hour.
- Manual-check cooldown: one minute.
- GitHub rate-limit reset: honored before another request, capped at 24 hours
  so an invalid header cannot suppress checks indefinitely.

The last known good candidate is retained after a transient failure. A channel
change clears the old ETag and candidate so the next check evaluates the new
policy from a clean state. Disabling automatic checks does not disable the
administrator's manual check.

## Administrator interface and API

The **Updates** page shows the installed version, selected channel, latest
eligible immutable release, last success, next scheduled check, and a bounded
error state. Platform administrators can check immediately. Saving a channel or
automatic-check change requires a recently authenticated platform-manage
session and creates `platform.update_policy_changed` in the audit chain.

| Method | Route | Authorization |
| --- | --- | --- |
| `GET` | `/api/v1/admin/updates` | Platform view |
| `PATCH` | `/api/v1/admin/updates/policy` | Platform manage, recent authentication, CSRF |
| `POST` | `/api/v1/admin/updates/check` | Platform view, CSRF |
| `POST` | `/api/v1/admin/updates/apply` | Platform manage, recent authentication, CSRF |

The policy body is exactly:

```json
{
  "channel": "stable",
  "automaticChecks": true
}
```

## Maintainer publication prerequisite

Before pushing the first release tag, enable **Settings → Releases → Release
immutability** in the GitHub repository. The tagged workflow verifies the
published release immediately; if it is unexpectedly mutable, the workflow
removes that release and fails without deleting the tag, so it can be retried
after enabling the setting. This avoids storing a separate administration token
in Actions. Stable tags publish as the latest release; beta tags publish as
prereleases and never replace the latest stable release.

The discovery result alone is not proof that a release has been locally
downloaded or verified. Explicit manual activation performs local
attestation/hash verification, staging, migration, health checking, and exact
rollback independently; discovery never triggers it automatically. See
[Staged platform updates](staged-platform-updates.md).
