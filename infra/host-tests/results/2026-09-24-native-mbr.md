# 2026-09-24 — primary MBR/BIOS native storage follow-up

Result: **MBR storage conversion, quota/isolation and normal-boot containment
PASS; full laboratory platform installation FAIL; no new public release.**
This must not be used as an exact-candidate release qualification.

## Scope and artifacts

- Base checkout: `91d723be91617437ef0f94c41cae2e737fd77459`, plus the MBR changes
  accompanying this record; Go 1.26.6, Linux/amd64, CGO disabled.
- New disposable Hyper-V Generation 1 VM: `stackfort-native-mbr-debian-13`,
  ID `1f5e665a-fddd-4cd2-bc55-44255b01963d`, 2 CPUs, fixed 8 GiB RAM for the
  second attempt, 50 GiB disk. BIOS, not UEFI/Secure Boot.
- Guest DMI: `91d127c9-40e6-ab48-9528-5085e8888729` (Generation 1 SMBIOS byte
  order differs from the Hyper-V BIOSGUID representation).
- Primary active type `0x83` MBR partition; PARTUUID `7c92ab10-01`;
  ext4 UUID `ee61a52a-e72b-43c3-8bda-1c23067ba4b0`; 256-byte inodes.
- Debian 13 system files from the separately provisioned official generic-cloud
  image; kernel `6.12.107+deb13-cloud-amd64`. The new blank target was formatted
  and GRUB BIOS installed as fixture construction, not by the Stackfort installer.
- Real installer SHA-256:
  `6ca910b2e9d43b5f8d1992862403f335ebbd8473765ed22fea9e0da92879fbaf`.
- Original boot integration test SHA-256:
  `4a01b6f8845b01edf0afbd5db5776a6d769bc45d4c489abc5389f640ffcfbe11`.
- Later, separately scoped failure/storage test SHA-256:
  `f107423ea5e90217d4f171c307c81337b99a16452c4dd135eb11da19fca2efb1`.
- Retained, authenticated **beta.3 laboratory payload**, not Beta.10 or a new
  candidate. Source digest
  `48babc7785a3025828ea89735959a107d2a95bbec0e66486a3dee3e1ce30caa8`.
  Its contents/attestation and production origin-verification policy were not
  changed. The current local installer was separately sealed by the normal
  preparation API. No test executable entered the initramfs boot path.

The user VPS, original GPT/Beta.10 VM, release assets, tag and bootstrap pin were
not changed. New fixture SSH keys were established through trusted provisioning
and strict host-key checks, not accepted from an unverified network scan.

## Results

| Check | Result |
| --- | --- |
| Canonical GPT/primary-MBR parsing and journal round trip | PASS |
| MBR metadata, BIOS-only policy, type/flag/number drift | PASS |
| Foreign disk/partition/GPT substitution, malformed IDs, logical partitions | Rejected |
| Recovery consent, fstab, GRUB and one-shot identity binding | PASS |
| Actual read-only MBR `blkid` probes and negative substitutions | PASS |
| Windows `go test ./...` and focused `go vet` | PASS |
| Linux `internal/installapply` and `internal/storageprep` tests with repository testdata | PASS |
| Fresh MBR eligibility, prerequisite installation, preparation | PASS |
| Real GRUB arm and repeated-arm no-op | PASS |
| Real unmounted-root conversion and storage finalization | PASS, both attempts |
| Full install with beta.3 payload | FAIL, WAF/NGINX version mismatch |
| Live byte/inode quotas, cross-account/symlink isolation, cgroup memory/PID limits | PASS |
| Normal BIOS reboot, live storage revalidation, same quota/isolation probes | PASS |
| Failed package installation not retried/admitted after reboot | PASS; gate closed |
| Read-only existing GPT layout with new installer | PASS for layout; full host blocked by dynamic RAM |
| Full new candidate, UI/ingress, MBR removal and upgrade qualification | NOT RUN |

Cross-built Linux unit tests require the repository `testdata` directory.
Invocations without those files failed; after supplying the unmodified fixtures,
both full packages passed. Missing-fixture failure logs remain retained, not
reclassified as product failures.

## Actual boot sequence

Second attempt operation: `c9035ee4-a435-4d8b-b647-351ae9e0a18e`.
Previous boot: `ec593dd1-32fe-4364-a63f-a38a525087af`.
Conversion boot: `23c610b0-79d6-42fd-959f-4371a6daabb3`.
Normal boot: `5a4b84f0-ef4d-4997-ae0a-027580b5e52d`.
Manifest SHA-256:
`57eb4e8bf735c1352f91933119e0f9abe4ef8e0292132b6d01072ab42e92e0a6`.

The real installer recorded `recovery-latch-flushed`, authorized the unmounted
root, ran precheck fsck, quota feature activation and postcheck fsck, then wrote
current-boot proof. Storage reached `ready` with exactly one arm attempt. The
normal runtime verified quotas and the hosting bind mount before installing
platform packages.

APT supplied NGINX `1.26.3-3+deb13u9`; beta.3's unchanged WAF package requires
`1.26.3-3+deb13u7`. APT refused the mismatch at `waf-native-package`, and the
installer kept admission `recovery-required`, attempt 1, with its gate closed.
There was no downgrade, replacement payload, dependency relaxation, journal
reset or in-place repair. The checkout already locks current `deb13u9`; a new
matching release artifact still needs its own complete tests.

Separate storage tests then exercised real account byte/inode hard limits,
cross-account denial, symlink containment, PID enforcement and cgroup OOM
enforcement. After a normal reboot the original kernel/initrd booted without a
conversion token or RAM proof. Storage verification passed again, but package
admission refused an unapproved retry. The original failed WAF stage and
admission attempt both remained at 1; the quota/isolation probes passed again.

## Failed first attempt and retained evidence

Attempt 1 also completed MBR conversion, but Hyper-V dynamic memory reduced
Linux usable RAM to about 1 GiB after boot. The subsequent installer memory gate
correctly stopped before platform installation. The VM was gracefully shut
down and checkpoint `mbr-attempt1-dynamic-memory-preflight-failure` retained.
Only this new lab VM's own clean checkpoint was restored. Fixed 8 GiB RAM and
checkpoint `mbr-static8g-before-conversion` preceded attempt 2. The wrapper now
rejects dynamic memory. This checkpoint restore is not OS-removal qualification.

Local, git-ignored evidence under `infra/host-tests/work/`:

- `mbr-lab-20260924/`: fixture construction, read-only MBR/GPT reports,
  live partition probes and Linux test logs.
- `mbr-native-boot-Prepare-20260924T104415Z`,
  `mbr-native-boot-Arm-20260924T104530Z`,
  `mbr-native-boot-Validate-20260924T104546Z`: first attempt.
- `mbr-native-boot-Prepare-20260924T104839Z`,
  `mbr-native-boot-Arm-20260924T105147Z`,
  `mbr-native-boot-Validate-20260924T105203Z`: second attempt, including the
  unsuccessful full-install result and preserved boot evidence archive.
- `mbr-native-boot-StorageAfterPackageFailure-20260924T105603Z` and
  `mbr-native-boot-NormalBootAfterPackageFailure-20260924T105616Z`: explicitly
  separate storage/failure-containment checks, not full-install success.
- `mbr-final-tests-20260924T105947Z/linux-tests-with-fixtures.log`: final Linux
  unit packages and actual-device MBR probe after normal boot.

Final inspection reports `/dev/sda1 ext4 rw,relatime,prjquota,errors=remount-ro`
and the same DOS PARTUUID/type/active flag. Both newly created lab VMs were
gracefully powered off. The final MBR disk state is retained in checkpoint
`mbr-storage-ready-package-failure-normal-boot`, ID
`7ff4b24d-9e97-4cd8-a75f-36e2a8156655`; no failed attempt or original checkpoint
was deleted.

The [implementation and release boundaries](../../../docs/native-installer-mbr.md)
remain mandatory. Public Beta.10 still rejects the user's MBR layout; these
development tests alone do not make the public one-line installer MBR-capable.
