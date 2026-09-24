# Beta.10 experimental publication approval

Recorded on 2026-09-24 at 09:58:29 UTC following RTBGG's explicit confirmation
in the project conversation:

> Ich bestätige hiermit die Veröffentlichung von **Beta.10 als experimentelle Test-Beta**.

This responds to the candidate-specific request after the technical results were
reported. That request explicitly covered fresh Debian13 amd64 disposable test
servers only, no production or important data, no independent security review,
community-only support, and removal exclusively by complete OS reinstallation.
The maintainer's earlier community-support and destructive-removal decisions
remain in force. This records an actual user decision, not an agent-generated
human approval or a claim of independent review.

## Exact approved bytes

- Version/tag: `0.1.0-beta.10` / `v0.1.0-beta.10`.
- Candidate source: `0096faed0ae77aef3326ce629db7591a433bb37f`.
- Archive: `stackfort-0.1.0-beta.10-linux-amd64.tar.gz`.
- Archive SHA-256:
  `8e7ce111d6cefda172e74bd7a71380c181e66a726d8f157f6bd1af8944e93263`.
- Original build35451346082, attempt1, artifact10586044694; artifact ZIP SHA-256
  `4d68a705d86a58df3d1c8519c01e0d4c39cc8142bb3b184859c0a0a4ff061896`.

The [candidate qualification](../../infra/host-tests/results/2026-09-19-beta10-candidate-qualification.md)
and [same-target OS removal result](../../infra/host-tests/results/2026-09-19-beta10-os-removal.md)
record the actual tests and their limitations. Their publication-pending status
is the historical status on2026-09-19, before this approval. The original source,
tag and tested archive must not be replaced or rebuilt under this approval.

## Support and deployment decision

RTBGG approves this exact candidate as an **experimental beta**, not a stable or
production release. Fresh-default native one-line installation is qualified only
for Debian13 amd64 on the documented compatible GPT/ext4/GRUB profile. Existing
workloads, production use and important data are not permitted. Ubuntu/Rocky
native one-line conversion and other storage layouts are not newly qualified.

No independent security review has been performed. Experimental beta for fresh
disposable test servers only; not for production or important data.

Support for this candidate is community-only through GitHub issues and private
security reporting, maintained by RTBGG and any future voluntary contributors.
There is no guaranteed response time, fix commitment, SLA or promised support
end date. The candidate's `SECURITY.md` remains the digest-bound support policy.

**No in-place uninstaller is available.** Removal requires complete operating-
system reinstallation and irreversibly removes all server data, configuration
and services. Removing the passive release package does not remove Stackfort.
The full-system-reprovision method and its destructive scope are explicitly part
of this candidate's support and publication decision; no recovery, backup or
secure-erasure guarantee is given.

## Execution boundary

The maintainer authorizes publication through the existing exact-candidate
readiness/upgrade/immutable-release gates and subsequent public-download/native
installation verification. All technical gates remain required. This decision
does not declare that assets have already been published, bypass a failed gate,
approve a different candidate, or waive the final public transport check.
