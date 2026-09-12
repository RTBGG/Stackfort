# Stackfort documentation

Stackfort is pre-beta. Use disposable hosts only; there is no supported public
release or production support window yet. Start with the guide for your task:

| I want to… | Read |
| --- | --- |
| Understand what is ready | [Project overview](../README.md), [roadmap](roadmap.md) |
| Prepare and install a test server | [Preflight](installer-preflight.md), [installation](installer-installation.md) |
| Set up and operate the panel | [Operations guide](operations.md) |
| Diagnose a failed action | [Troubleshooting](troubleshooting.md) |
| Understand backup coverage | [Backup and recovery boundaries](operations.md#backups-and-disaster-recovery) |
| Check for or install an update | [Update checks](update-channels-and-checks.md), [staged updates](staged-platform-updates.md) |
| Report a security issue privately | [Security policy](../SECURITY.md) |
| Contribute code, docs, or translations | [Contributing](../CONTRIBUTING.md), [development setup](../DEVELOPMENT.md) |
| Interpret or reproduce performance results | [Benchmark methodology and results](benchmarks.md) |
| Qualify a release | [Release checklist](release-checklist.md), [upgrade matrix](upgrade-matrix.md) |

## Feature references

These references explain implemented APIs, invariants, and tests, rather than
repeating the everyday operating instructions:

- Accounts: [administrator flows](administrator-phase1-flows.md),
  [account-owner flows](account-owner-phase1-flows.md),
  [resource control](account-resource-control.md).
- Hosting: [domains](static-domain-lifecycle.md),
  [PHP controls](account-php-controls.md),
  [TLS](acme-account-and-http01.md), [scheduled jobs](scheduled-jobs.md).
- Applications: [OCI foundation](oci-application-foundation.md),
  [image preparation](oci-image-preparation.md),
  [private resources](oci-private-resources.md),
  [deployment lifecycle](oci-deployment-lifecycle.md).
- Data: [files and local backups](local-file-backup-foundation.md),
  [MariaDB](account-database-lifecycle.md),
  [phpMyAdmin](phpmyadmin-signon.md), [panel persistence](persistence.md).
- Single-disk onboarding (not enabled): [native quota experiment](native-quota-prototype.md),
  [installation-state protocol](native-quota-installation-state.md),
  [journal-bound boot experiment](native-quota-boot-handoff.md),
  [durable release staging](native-quota-release-staging.md),
  [release-origin and boot binding](native-quota-release-origin.md),
  [post-boot installation continuation](native-quota-install-continuation.md),
  [service admission and reviewed recovery](native-quota-service-admission.md),
  [native installer operator commands](native-installer-operator.md),
  [real installer boot services](native-installer-runtime.md),
  [real installer offline preparation](native-installer-preparation.md),
  [native host eligibility and prerequisites](native-installer-host-eligibility.md),
  [native power-loss containment](native-installer-power-loss.md),
  [deterministic crash-image replay and restore](native-installer-crash-replay.md),
  [whole-disk rescue and restored-system boot](native-installer-whole-disk-recovery.md),
  [backup/reinstallation policy and read-only recovery handoff](native-installer-recovery-policy.md).
- Protection: [authentication](password-authentication-and-sessions.md),
  [MFA and recovery](totp-recovery-and-session-management.md),
  [WAF](waf-foundation.md), [cache](cache-foundation.md).

## Design and evidence

[Product specification](product-spec.md) · [Architecture](architecture.md) ·
[Security model](security.md) · [Architecture decisions](adr) ·
[Host qualification harness](../infra/host-tests/README.md) ·
[Dated qualification records](../infra/host-tests/results)

The product specification and security model include target requirements.
The roadmap and feature references distinguish implemented behavior from
remaining work. Dated results describe the tested commit/artifact, not every
subsequent build. Documentation is maintained in English; the interface has
English and German catalogs, with the final critical-workflow review still
listed separately in Phase 6.
