# Beta.14 candidate preparation

Status: **unpublished candidate preparation**, not release approval or a passing
exact-archive installation qualification. The public installer still selects
Beta.13. No tag, release, automatic update or predecessor upgrade is authorized
by this document. Existing installations must not receive manually mixed API,
agent, installer or frontend binaries.

## Changes to qualify together

- Restore the Let's Encrypt registration event and observe its background
  operation; show actionable errors for a missing or incomplete ACME account.
- Replace duplicated database wizard numbers with descriptive English/German
  steps, and distinguish an absent account PHP pool from absent PHP software.
- Inspect the managed Debian/Ubuntu firewall unit and distinguish service-unit
  presence from running state; explain the inactive global PHP/Podman units.
- Add administrator-only panel domain/subdomain setup, explicit terms and origin
  change consent, bounded background issuance, automatic renewal and the
  existing port-8443 fallback. The panel uses its own root-managed ACME account.
- Permit panel-only management on a fully admitted native installation while
  retaining the generic installer's native-state prohibition, shared locks,
  current-boot admission and live quota checks.

No dependency upgrades, storage conversion changes, database migrations or
WAF/cache performance changes are intended. The new typed agent operations
require matched API and agent binaries from the same archive.

## Qualification sequence

1. Pass local unit, frontend, localization and documentation gates. Inspect the
   privileged boundary and test the installed agent sandbox on a disposable host.
2. Freeze one source commit on a candidate branch, run CI/security and manually
   build `0.1.0-beta.14` with the existing release workflow. Its output is an
   unpublished artifact, not an installable public release.
3. Record actual artifact IDs and digests. Obtain genuine exact-tag provenance
   through the [retained-candidate procedure](../packaging/releases/PROMOTION.md)
   before the real native onboarding test. Do not bypass origin verification.
4. Test the exact archive on fresh Debian 13 GPT/UEFI and MBR/BIOS fixtures,
   including setup, service state, ACME/panel workflows, ordinary rerun/reboot,
   security/isolation/quotas, WAF/cache, rootless OCI and failure containment.
   Private-CA tests must not be described as public Let's Encrypt issuance.
5. Complete same-target full OS removal qualification and retain artifact-bound
   evidence. Obtain candidate-specific publication/scope approval. Upgrade
   exclusions must be decided explicitly, not copied from an older beta.
6. Publish only after the applicable gates pass; verify public immutable assets
   before advancing the one-line selector. Never modify the Beta.13 release.

The [development report](../infra/host-tests/results/2026-09-26-panel-ui-fixes.md)
records useful regression evidence, but does not qualify a future release archive.
