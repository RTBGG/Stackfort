# Contributing to Stackfort

Contributions to code, documentation, tests, and translations are welcome.
For a suspected vulnerability, use the [private security process](SECURITY.md)
instead of a public issue or pull request.

## Start small

1. Check the [roadmap](docs/roadmap.md) and existing
   [issues](https://github.com/RTBGG/Stackfort/issues). Discuss a substantial
   feature or architectural change before implementing it.
2. Fork the repository and create a focused branch from `main`.
3. Follow [DEVELOPMENT.md](DEVELOPMENT.md) for the pinned toolchains and local
   setup. Run host mutations only in explicitly disposable supported VMs.
4. Add regression tests and update the relevant guide. Explain changes to a
   trust boundary in an [architecture decision](docs/adr).
5. Open a pull request describing the problem, approach, tests actually run,
   and any remaining risks. Separate unrelated changes.

English is the source language for code and documentation; English and German
are welcome in issue reports. New UI messages need **both** English and German
translations, matching placeholders, and locale-aware formatting. Include
keyboard, focus, narrow-screen, and error/confirmation behavior in UI changes.

## Verification

From the repository root:

```sh
bash scripts/verify.sh
```

For documentation-only changes, the offline link gate can run separately:

```sh
node --test scripts/check-docs.test.mjs
node scripts/check-docs.mjs
git diff --check
```

The full check includes Go formatting/vet/tests, frontend types/tests/i18n/build,
dependency audit, and packaging locks. Linux additionally runs Go race tests
and the Unix-socket smoke test. CI adds security and reproducible-build gates.
Windows checks and successful cross-compilation do **not** qualify Linux host
behavior. State skipped tests explicitly; never describe a rehearsal as a
published-release qualification.

Changes to the installer, updater, NGINX/WAF, account isolation, quotas, or OCI
runtime need appropriate Debian 13, Ubuntu 26.04, and Rocky Linux 10 evidence.
See the [host harness](infra/host-tests/README.md) and
[benchmark methodology](docs/benchmarks.md). Do not run destructive tests on a
shared CI runner or an existing server with valuable data.

## Invariants to preserve

- The web/API process stays unprivileged. The agent accepts closed, typed
  intent, never arbitrary commands, shell strings, paths, units, or environments.
- Enforce authorization on the server, including ownership, CSRF, and recent
  authentication where required. Add denied and cross-account cases.
- Treat filenames, archives, redirects, upstream responses, and stored input
  as untrusted. Keep resource limits, secret redaction, and rollback paths.
- Never rewrite an applied SQL migration. Add a new checksum-locked migration
  and exercise historical-schema and recovery tests.
- Keep dependencies and GitHub Actions pinned. Native WAF/NGINX changes must
  preserve exact ABI locks and real-worker qualification on every target.
- Keep customer data, keys, database files, `.env` files, VM disks, and raw
  sensitive logs out of commits and artifacts. Use synthetic fixtures.

## License and review

Stackfort uses [AGPL-3.0-or-later](LICENSE). Submit only material you have the
right to contribute under the repository's license; preserve existing notices
and add the existing SPDX header style to new source files. Document third-party
licenses with the corresponding component. Do not add a CLA, sign-off
requirement, commercial dependency, or support promise as an incidental change.

Maintainers review scope, correctness, security, compatibility, and evidence
before merging. Passing CI is necessary, not a substitute for review or
supported-host qualification. Release publication follows the separate
[maintainer checklist](docs/release-checklist.md).
