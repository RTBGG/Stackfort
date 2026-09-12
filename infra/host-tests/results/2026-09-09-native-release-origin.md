# Release-origin-bound native boot — 2026-09-09

Outcome: **actual candidate attestation verification, durable source staging and
the Debian one-shot boot manifest are now connected under the shared installer
lock**. The final integration executable passes all eight origin scenarios and
the real conversion/resume and subsequent normal-boot quota/OCI suites.

This is laboratory qualification, not a public release, performance benchmark,
real package-installation continuation or arbitrary power-loss qualification.
See the [design and reproduction boundary](../../../docs/native-quota-release-origin.md).

## Environment and authenticated inputs

- Base commit: `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2`, plus working-tree changes.
- Exact Hyper-V VM: `stackfort-native-quota-debian-13`, ID
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`; 2 vCPU, 8 GiB RAM, 50 GiB system
  disk and 64 MiB seed disk. No separate hosting disk or loop image.
- Debian 13, kernel `6.12.107+deb13-cloud-amd64`, plain GPT/ext4 root and GRUB.
- Successful sequence began from explicitly restored offline checkpoint
  `native-quota-before-conversion`, ID `9e62e0ac-89ad-4851-8ae2-e6b936e40eae`.
  This is a pre-conversion lab fixture, not full fresh-provider-image onboarding.
- Unchanged final integration executable SHA-256:
  `888808bcc35f46f7342076b351f7ebf95239bf5c2bd674c0f42b69a251386488`.

The retained beta.3 candidate came from workflow run `34182191221`, attempt 1.
The actual verified source/signer commit is
`5282946bec1f865de7222128a6a5d0d8a656f34c`, source ref `refs/heads/main` and
certificate workflow identity
`https://github.com/RTBGG/Stackfort/.github/workflows/release.yml@refs/heads/main`.
Verification requires GitHub's OIDC issuer and rejects self-hosted signing
runners. This matches the explicit `lab-candidate` policy only; it does not
qualify the strict tag-release policy.

| Input | SHA-256 |
| --- | --- |
| Candidate `stackfort-0.1.0-beta.3-linux-amd64.tar.gz` (117,210,432 bytes) | `3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0` |
| Retained `build-attestation.jsonl` | `b929c9d6a8e15726f53381bb35e2bc1912ab13908b2fadffe5c73e45178d8344` |
| Independently downloaded upstream GitHub CLI 2.99.0 Linux-amd64 archive | `ed4960225d2833e04a61590d9fa2b5773d147f3aa375459e5466a40c102f3832` |
| Approved verifier binary extracted from that archive, also matching the candidate's bundled CLI | `d0a901528411dfc2295253ba4183b99788f99c5901e3a6dda9172089c42d3b85` |

The upstream archive matched the existing build script's pin. The binary pin
was derived independently of the candidate and is enforced before execution.
The [official upstream release](https://github.com/cli/cli/releases/tag/v2.99.0)
and [verification options](https://cli.github.com/manual/gh_attestation_verify)
document the selected tool and verification interface.

Initial verification was allowed outbound HTTPS for public trust-root
discovery. Post-boot verification rechecks retained bytes and sealed receipts;
it does not rerun online signature verification. The candidate installer,
package payloads and application services were not executed or installed.

## Results

| Check | Result |
| --- | --- |
| Ordinary Windows `go test ./...` and `go vet ./...` | Pass |
| Linux installapply root unit suite | 46 top-level tests pass, no skips |
| Linux integration build and targeted installapply/storageprep/integration vet | Pass |
| Authentic candidate → retained source → manifest → protocol resume | Pass |
| Candidate incorrectly requested as a tag release | Rejected before journal/boot authorization |
| Altered signed DSSE payload in a parseable bundle | Verifier rejects before journal/boot authorization |
| Extraction changed with unchanged file length | Rejected at archive-content binding |
| Verifier replaced by a different static executable | Independent binary pin rejects before execution |
| Retained source changed after arming | Terminal `release-invalid`, no backend resume |
| Sealed manifest truncated after arming | Terminal `release-invalid`, no backend resume |
| Retained attestation bundle removed after arming | Terminal `release-invalid`, no backend resume |
| Real `Prepare -WithRelease` → `Arm` → reboot → `Validate` | Pass, `ready`, exactly one arm attempt |
| Subsequent reboot → `NormalBoot` | Pass, no preparation token/helper execution, journal unchanged |
| Project quotas, tenant isolation, OCI private resources and deployment lifecycle | Pass on both checked boots |
| Rootless subordinate-UID container writes | Quota enforced; 16 MiB append succeeds after raising quota, on both boots |
| Kernel, normal initrd and main GRUB configuration | Remain byte-identical |
| Production installer guard with a valid release-bound `ready` journal | Still rejects entry |

The eight origin scenarios run in private mount namespaces, with a protocol-only
backend for their simulated boot transitions. They are separate from the real
boot sequence; the negative cases do not simulate power loss or a metadata
failure. The final replay uses the exact same executable as all four successful
boot stages. The actual host journal is unchanged before/after that replay.

The real boot operation is `32a9ab81-3201-4b5f-92e4-f3148ee66b61`. Its canonical
release/host/boot manifest SHA-256 is
`1cde68a4eb1a0d99bf7c96c672962cade1d3803b555ca9cff3cee44a93b750fe`;
source digest is
`48babc7785a3025828ea89735959a107d2a95bbec0e66486a3dee3e1ce30caa8`.
The conversion boot ID is `0a0a4d6c-330c-48ee-9c7b-293a11453a83`.

After preparation, the original `/var/tmp/stackfort-origin-lab` was renamed to
`/var/tmp/stackfort-origin-lab-retired`, preserving the evidence but making the
expected input path absent. Both boot stages succeeded without restoring it.
The late namespace replay temporarily maps that retired fixture inside a
private mount namespace only; the original host path stays absent.

The early log explicitly confirms the origin/source/boot receipt checks before
the first filesystem write, followed by an unmounted-target conversion. The
post-boot resumer rechecks the retained full tree, archive, bundle and manifest
before reaching `ready`. The subsequent normal boot has no early helper log.
The final journal SHA-256 is
`cb952cb1387fcf308ba772dafd468bea2c3341526aac7d7f49f7c74a8a57000f`.

## Development failures, not qualifying results

Earlier local/namespace tests exposed and corrected:

1. An inherited `bytes.Buffer` method could bypass the verifier output limit.
   The buffer is now a named field with only the bounded writer exposed.
2. GitHub CLI rejects simultaneous explicit certificate identity and signer
   workflow flags. The fixed invocation uses the exact workflow-and-ref
   certificate identity, without the mutually exclusive workflow flag.
3. The wrong-extraction fixture initially hit the length gate instead of the
   expected content-hash gate. It now changes bytes while retaining file length.
4. The first real preparation rejected the lab manifest because the new strict
   reader expected a trailing newline while the existing writer emits none.
   The reader now matches that exact canonical format. This stopped before
   journal creation or boot arming; a fresh checkpoint restore preceded the
   successful sequence. No journal edit or forced resume was used.

The first preparation's evidence remains in
`native-journal-Prepare-20260909T064541Z`, archive SHA-256
`efefd3025aceb6e5c4d466c5cd502e592722a54b43bf56c5e952974a1079c325`.
Pre-conversion logs showing the synthetic consumer blocked by missing quota
proof are expected fixture behavior, not evidence of a successful resume.

## Retained evidence and final state

Paths below are relative to ignored `infra/host-tests/work/`. Boot directories
contain the exact executable, `tests.log`, `boot.log` and `evidence.tar.gz`.
The archives retain source/origin/manifest/journal data and boot evidence;
large original/retired initrds and the helper are omitted from the archive but
their hashes and the separate executable/checkpoint remain available.

| Stage or log | Evidence archive or log SHA-256 |
| --- | --- |
| `native-journal-Prepare-20260909T064809Z/evidence.tar.gz` | `e6e942bff2a3df5a6cfae945d625ce07c5e1b1051297cd8625a83c48815e7a21` |
| `native-journal-Arm-20260909T065025Z/evidence.tar.gz` | `c135d97d1f513e9e6c88c7e7af910f7c8141835983d7d855d2a9ece608f10b23` |
| `native-journal-Validate-20260909T065116Z/evidence.tar.gz` | `3a07c5ea7181d3bec6c04d6d099b33045638538a9adb6a25d6a1bed1d20f6bab` |
| `native-journal-NormalBoot-20260909T065245Z/evidence.tar.gz` | `3a07c5ea7181d3bec6c04d6d099b33045638538a9adb6a25d6a1bed1d20f6bab` |
| `origin-binding-boot-binary-linux.log` (all eight scenarios, final boot binary) | `0636e3f5bb86406b9f7599bd9fdc65b9de5b4a2270853f864e82a91dec06fb76` |
| `origin-unit-qualified-final-linux.log` | `4943abc0b8cddc0bb7743f8b19137803687353f65a7fc4914fcfd1532f5f5a95` |
| `origin-bootstrap-retired.log` | `9863c205f6af2f1015d3dcd113e9c5d836fe4c67189adac319feedc686ef7f76` |

Linux unit executable `origin-installapply.test` SHA-256 is
`4efc765c05f11301f36781bd3c20319bc3559061a762f9d2ae3319cb12054a49`.
The final boot validation test logs have hashes
`1896c7aaf39331f83255ce53c8bb5a3e62973a16c49cd93de924bce27f74d21e`
and `abbbb3d87f51a749b672becdcdb0fc541712fe93221c83cd684e7f3d806b2e5c`
for conversion and normal boot respectively.

The VM was shut down normally and saved as the additional offline checkpoint
`native-origin-qualified-success`, ID `24367aa5-b086-45ec-96d0-ca8024d1356b`.
Older checkpoints are preserved. All five test VMs are off. No customer VPS
change, commit, push, public release or production installer activation occurred.

## Next gates

Connect the retained authenticated source to real package and service
continuation through a reviewed production coordinator. Qualify tag-release
selection, fresh-server eligibility, interruption/recovery, external
package/kernel coordination, capacity reserve and ordinary multi-OS onboarding.
The root-state trust assumption and power-loss limitations remain; none of the
results justify exposing automatic native-root conversion in the public installer.
