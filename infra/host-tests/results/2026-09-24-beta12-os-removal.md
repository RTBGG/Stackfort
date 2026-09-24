# Beta.12 same-target OS-reprovision removal

**Passed: 2026-09-24T15:06:39Z.** Complete OS replacement on the same disposable
GPT/UEFI VM that ran the exact active Beta.12. Not passive-package removal,
checkpoint rollback, new-VM substitution or secure erasure. No separate MBR
OS-reprovision result is claimed.

## Bound target and candidate

- Hyper-V ID before/after: `55729cf8-11e3-4744-be5b-ddde3824c0e4`.
- DMI before/after: `a5db08d7-6c4e-47e0-a381-9bd594b2731a`.
- Version/source: `0.1.0-beta.12`, `f4d1a7947af4ffbdc2cbf28d2f618c10833cefec`.
- Original build: run `36005061649`, attempt 1, artifact `10811230131`.
- Archive SHA-256: `446cf63a51994993b1b9cb337b0067967714122d41c6ff155720571f887268a9`.
- Operation: `8a5d5d5a-bc3b-4990-8944-95448056fbcc`.
- Previous boot: `be3be9af-73dc-4dfa-a15c-ff7033ad18fe`.
- Fresh OS boot: `a7dd89e1-ec3a-4bce-8d0a-32de3349f70a`.

Full onboarding/API smoke, rerun/reboot, host/resource and actual OCI tests passed
before deliberate process-loss quarantine. Exact runtime and unchanged durable
record hashes were checked again before graceful shutdown and disk replacement.

## Independent distribution media and attachment

The retained Debian 13 genericcloud image was previously obtained over official
Debian HTTPS and verified against published SHA512SUMS. Preparation rechecked
both complete-image digests; no detached-signature verification is claimed:

- SHA-256: `85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`.
- SHA-512: `8ea9faae810043a0b35b0149f05014f26705c2339ffb11ead308f33e844a87cc3ef46ec81d5262b38817b6a88af404874d48a5857ebe072ef6a31dfb6e371f50`.

Fresh standalone 50-GiB system and 64-MiB public-key-only seed VHDX files were
prepared under `work/native-reprovision-b477f35acf684fa48d4d85541df1f007/`.
Neither has a parent or copies an installed disk or old seed. Initial pins:

| Disk | VHD identifier | SHA-256 before first boot |
| --- | --- | --- |
| System | `18576d53-6d83-2f42-a267-1b4ceb207519` | `3f7a28ce3a1a0e65439e91f357ac22d29f482904375c0268c7ef8dea9c6dbebe` |
| Seed | `7275a9ae-f383-4629-a265-2c982337f680` | `c5e7cab559525ab76cc11f00abdbdd3b2e1c1a787f218ac83fff26a472988ef0` |

Exact powered-off VM identity, old disk chains, new IDs/hashes and SCSI slots
were checked before/after attachment. Both old disks were detached, not deleted.
The attachment receipt retains their full graph. Healthy Beta.12 checkpoint
`354857cd-9086-46e4-a548-65967e68fe94` and preserved older Beta.11 checkpoints
remain available, along with the final quarantined disk chain. Detached evidence
is not erased data and is not accessible to the fresh guest. The public Beta.10
VM and user VPS were untouched.

## Fresh OS verification

The new public SSH key came from Hyper-V KVP bound to the independent instance
nonce, image, DMI and boot. Strict SSH used a separate known-hosts file, without
reading a private key or accepting an unauthenticated host key. Read-only checks
passed: cloud-init done without errors; Debian 13 amd64; plain ext4 without quota
or project features; no Stackfort state/configuration, binaries, hosting data,
managed unit files/services/tenant identities or platform TCP/UDP listeners.
The independent lab SSH operator belongs to the new seed, not the platform.

Redacted receipts below are retained in `work/beta12-candidate-20260924/`:

| Receipt | SHA-256 |
| --- | --- |
| `removal-verification.json` | `4bc8f96c9ee23eab48b3a55d1ad8b0ddb1a4f5c5161d9602ed991fdcf29e7d26` |
| `removal-attachment.json` | `ec05d668d301d6bc46b4e779926a3740158d737e7a145114542ea036684540c0` |
| `reprovision-public-report.json` | `148d4424f51508aeefc017ae0cbc59ec5be9c50942640d7a3875becfe2b75f38` |

The independent attachment graph proves disk replacement; guest path absence
alone does not. Provider-specific reinstall dashboards are not tested. Real
removal requires complete OS reinstallation and destroys all server data; this
laboratory preserved detached evidence and makes no secure-erasure claim.
