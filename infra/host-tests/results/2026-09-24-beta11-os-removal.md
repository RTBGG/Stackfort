# Beta.11 same-target OS-reprovision removal

**Passed: 2026-09-24T12:17:17Z.** Complete OS replacement on the same disposable
GPT/UEFI VM that ran the exact active Beta.11. Not passive-package removal,
checkpoint rollback, new-VM substitution or secure erasure. No separate MBR
OS-reprovision result is claimed.

## Bound target and candidate

- Hyper-V ID before/after: `55729cf8-11e3-4744-be5b-ddde3824c0e4`.
- DMI before/after: `a5db08d7-6c4e-47e0-a381-9bd594b2731a`.
- Version/source: `0.1.0-beta.11`, `ec117da052b18e051870a224b89bee4b0ddf64e7`.
- Original build: run `35991617289`, attempt 1, artifact `10804961581`.
- Archive SHA-256: `d6fec6d5a894d0c120aef5d83829b335a8875b68a181c45a7ad2bb9d139d1304`.
- Operation: `a46df87b-325b-4197-b997-3ebc75279dda`.
- Previous boot: `ae346f47-7ebd-49a0-aedd-19a0808091a7`.
- Fresh OS boot: `30f9566a-acca-4204-8a6b-d272a9653739`.

Full onboarding/API smoke, reboot, host/resource and real OCI tests passed before
the deliberate process-loss quarantine. Exact runtime and unchanged durable
record hashes were checked again before graceful shutdown and disk replacement.

## Independent distribution media and attachment

The retained Debian 13 genericcloud image was previously obtained over official
Debian HTTPS and verified against published SHA512SUMS. Preparation rechecked
both complete-image digests; no detached-signature verification is claimed:

- SHA-256: `85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`.
- SHA-512: `8ea9faae810043a0b35b0149f05014f26705c2339ffb11ead308f33e844a87cc3ef46ec81d5262b38817b6a88af404874d48a5857ebe072ef6a31dfb6e371f50`.

Fresh standalone 50-GiB system and 64-MiB public-key-only seed VHDX files were
prepared under `work/native-reprovision-e4525cf871b3434591686cfbe03e61a8/`.
Neither has a parent or copies an installed disk or old seed. Initial pins:

| Disk | VHD identifier | SHA-256 before first boot |
| --- | --- | --- |
| System | `d5b41bbf-8aec-d340-94f5-e7bff401dcdb` | `fee10dbffbe6038022db95c7d44a0a72d35be98f54b77406b2716c00f38dc4c0` |
| Seed | `5043d667-aebd-4c5e-aea5-8a6b3fcc4f4a` | `d08094fccf1120809fed27706abfbebd41d33de1f0522571e934b54bc5bd3e8b` |

Exact powered-off VM identity, old disk chains, new IDs/hashes and SCSI slots
were checked before/after attachment. Both old disks were detached, not deleted.
The attachment receipt retains their full graph. Before-fault checkpoint
`c0ca3bac-fdb2-48d7-9527-b509de2761b3` and quarantined checkpoint
`ac1ff1aa-ac1d-455e-b0b6-c058b5f0e3eb` remain available. Detached evidence is not
erased data and is not accessible to the fresh guest. The public Beta.10 VM and
user VPS were untouched.

## Fresh OS verification

The new public SSH key came from Hyper-V KVP bound to this instance nonce, image,
DMI and boot. Strict SSH used a separate known-hosts file, without reading a
private key or accepting an unauthenticated host key. Read-only checks passed:
cloud-init done without errors; Debian 13 amd64; plain ext4 without quota/project
features; no Stackfort state/configuration, binaries, hosting data, managed unit
files/services/tenant identities or platform TCP/UDP listeners. The independent
lab SSH operator `stackfort-test` belongs to the fresh seed, not the platform.

Redacted receipt `work/candidates/35993227841-attempt1/os-removal-gpt.json`:
SHA-256 `6027cd257e68b7247675f04f7018bcfa589757b4a8f37aba30cb779b0522423d`.
The independent attachment receipt proves disk replacement; guest path absence
alone does not. Provider-specific reinstall dashboards are not tested.
