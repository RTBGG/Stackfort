# Beta.12 candidate qualification — 2026-09-24

Status: **unpublished candidate retained; complete installed-candidate
qualification and publication approval are pending**. No Beta.12 release or
default-bootstrap change is authorized by this record. Beta.11 is unchanged.

This candidate corrects the rejection of inactive optional CD-ROM fstab rows.
See the [source correction and exact provider-row reproduction](2026-09-24-beta12-optical-regression.md).
The user VPS was not accessed or changed.

## Frozen source and retained build

- Version: `0.1.0-beta.12`.
- Source: `f4d1a7947af4ffbdc2cbf28d2f618c10833cefec`.
- [CI run 36004640915](https://github.com/RTBGG/Stackfort/actions/runs/36004640915), attempt 1: all four required jobs passed.
- [Security run 36004640826](https://github.com/RTBGG/Stackfort/actions/runs/36004640826), attempt 1: all four required security jobs passed; only PR-specific dependency review was skipped.
- [Manual build 36005061649](https://github.com/RTBGG/Stackfort/actions/runs/36005061649), attempt 1: successful, including six native packages, three minimal-runtime cells and aggregate inventory/attestation.
- Original artifact: `10811230131`, `stackfort-0.1.0-beta.12`, 313,199,834 bytes.
- Original ZIP SHA-256: `8cf13cd7084cc1a21497a1017bbd45225cdf924347fa4ee95fc66e6906dca485`.
- Release archive SHA-256: `446cf63a51994993b1b9cb337b0067967714122d41c6ff155720571f887268a9`.

The downloaded ZIP matches the live GitHub artifact digest. Mechanical
selection is not a security audit, readiness approval or publication decision.
Original build retention expires on 2026-10-24; do not rerun or substitute its
pinned build attempt. Ignored working evidence is retained under
`infra/host-tests/work/beta12-candidate-20260924/`.

## Remaining release boundaries

Fresh native installation scope remains Debian 13 amd64 only. The Beta.11-only
fresh-install publication exception does not automatically apply to Beta.12.
No old upgrade, security, resource-isolation, OCI, failure-recovery or destructive
OS-reprovision removal evidence is relabeled as a Beta.12 result. Those
exact-candidate tests, the upgrade-support decision, candidate-specific approval,
publication and real public-download verification remain separate gates.
