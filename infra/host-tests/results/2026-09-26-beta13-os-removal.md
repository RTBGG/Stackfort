# Beta.13 same-target OS-reprovision removal

**Passed: 2026-09-26T11:54:13Z.** Complete independent OS replacement on the
same GPT/UEFI VM that ran the exact active Beta.13. Not checkpoint rollback,
passive-package removal, new-VM substitution or secure erasure.

- Hyper-V ID before/after: `55729cf8-11e3-4744-be5b-ddde3824c0e4`.
- DMI before/after: `a5db08d7-6c4e-47e0-a381-9bd594b2731a`.
- Candidate: `0.1.0-beta.13`, source `991f7df6b27093588a69d0dbea2e3eab7a7ceaae`.
- Original build: run `36235431432`, attempt 1, artifact `10904191959`.
- Archive SHA-256: `b56104eebc6f535d88d9b3de17bcf95efa32c97dc88997b1e992619725c2ea91`.
- Installed operation: `9cd68871-99f0-4ced-a41b-063fa6fceb21`.
- Previous boot: `0a66862e-1890-4845-8c14-9395b876c6ba`.
- Fresh OS boot: `8aefd50b-f37f-4568-b801-2f1907dadebb`.

Onboarding/API/rerun/reboot, security/resources, real OCI and process-loss
containment passed first. Exact installer and unchanged durable records were
verified again before graceful shutdown. Healthy checkpoint
`968920b6-57b6-47c5-af5a-57e71043b6f9`, older checkpoints and the final
quarantined disk chain remain preserved.

## Independent media and disks

The retained official Debian 13 genericcloud image was originally obtained over
Debian HTTPS and checked against published SHA512SUMS. Both complete-image
hashes were rechecked during preparation; no detached-signature claim.

- SHA-256: `85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`.
- SHA-512: `8ea9faae810043a0b35b0149f05014f26705c2339ffb11ead308f33e844a87cc3ef46ec81d5262b38817b6a88af404874d48a5857ebe072ef6a31dfb6e371f50`.

New independent 50-GiB system and 64-MiB seed VHDX files were prepared under
`work/native-reprovision-49926486970242338cdb4f70eef957f8/`. No installed disk,
snapshot or old seed was copied. Pre-boot identities:

| Disk | VHD ID | SHA-256 |
| --- | --- | --- |
| System | `0d0f195b-d6f1-954b-bef5-b7347bb89c3b` | `039590eee7723e4870cae49b39ef53ac316ae8e89d0be29b9219058d20b69102` |
| Seed | `d388d5ae-bb82-47a2-9997-a2589361a68a` | `c093b766eacaf9d1844bc585049f9d5979e274674027ced80881864bd533f5ee` |

The guarded attachment checked powered-off VM identity, exact old disk chains,
new hashes/IDs and SCSI slots. Old disks were detached and retained, not deleted.
They are inaccessible to the new guest. The user VPS was untouched.

## Fresh OS verification

Hyper-V KVP independently authenticated the new public SSH key, bound to instance
nonce, DMI, image and boot. A separate strict known-hosts file was used.
Cloud-init completed without errors. Read-only checks confirmed Debian 13 amd64,
fresh plain ext4 without quota/project features, absence of Stackfort state,
binaries, hosting data (including the actual smoke account), managed identities,
service/unit files and platform TCP/UDP listeners. The new SSH operator is from
the seed, not a retained hosting account.

| Retained receipt | SHA-256 |
| --- | --- |
| `removal-verification.json` | `8adbeebf96f1470ded066fc732fb1da0a7ef35fdfdecf5517dd107bf0296caee` |
| `removal-attachment.json` | `e127fd0a041a2dddfec9a6809a74a19fa9d2e063bc2920adb2e0232a5009b64b` |
| `reprovision-preparation.json` | `66c4c0ae03cb75a7a10913135b2843023f9a70eeb48a121aa9e3db38da28517e` |
| `reprovision-public-report.json` | `faea8ea2308f388fda63bd884d045478e01c96dcadd339a1c059b7438181304f` |

Receipts are under `work/beta13-candidate-20260926/`; the full old/new disk graph
is also retained in the prepared directory's exclusive attachment record.
No separate MBR removal or provider-dashboard test is claimed. Actual removal
requires full OS reinstallation and destroys all server data.
