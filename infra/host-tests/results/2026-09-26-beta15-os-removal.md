# Beta.15 same-target full OS reprovision removal

Passed at `2026-09-26T18:22:18Z`. This qualifies the explicitly disclosed
experimental-beta removal method, not an in-place uninstaller or secure erasure.

The active candidate was `0.1.0-beta.15`, source
`d93c4bfb9fb527e80a648eeb8482b159d0076b29`, archive
`b5ac6c02a8f13bcabb72b527c677844ba007fb8b2c8ff435a231ddad510ba617`, original
build `36260323807` attempt 1/artifact `10911968743`. Its real installation,
API/security/resources/OCI and failure-containment checks are in the
[candidate report](2026-09-26-beta15-candidate-qualification.md).

Before and after target: Hyper-V VM `55729cf8-11e3-4744-be5b-ddde3824c0e4`,
DMI `a5db08d7-6c4e-47e0-a381-9bd594b2731a`. The healthy candidate checkpoint
`b6e5950b-8a23-44a3-8995-9836cdf971df` and old system/seed differencing chains
were retained. After fault containment, the exact VM was gracefully stopped,
its exact old attachment graph recorded, and an independently prepared vendor
Debian system/seed pair attached. No checkpoint rollback, copied installed disk,
reset journal, passive-package uninstall or deletion substituted for removal.

## Independent media and attachment

Official HTTPS source: [Debian 13 generic cloud amd64](https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2).
The 340,983,808-byte QCOW2 matched the published current SHA512SUMS:

- SHA-256: `5754395abffb1d384d50f6d0945d46d1beb7be42a7e786e4fc4a6f27270ab16f`.
- SHA-512: `95e110dfcdbd0ed8a82a75ed9579802f9950cabf51a810dcc6388e81bc778188713878b9f28d583a0ea602fbf48b35996ae9ad37f584166d8fbd6489df248f53`.
- Retrieved SHA512SUMS file SHA-256: `4833d6c69d4d8b02a5cfe2415d02fef5125ed98999b8a9f2937b01146921c4cf`.

Authentication is official HTTPS plus published checksum, **not** detached
signature verification. The image became an independent 50-GiB dynamic VHDX,
ID `9707154b-82ee-9141-b689-a6c86a892a46`, initial SHA-256
`29492c5a37a3f8cb16f7ce93dc025820cf7b014ebed3ce55b02df8e06c066043`.
Fresh 64-MiB seed ID `8f67e123-be01-4412-a29e-2c97cb00c731`, SHA-256
`dd6f98b8068fc6ff94caa7a21c2dc80b374d2bbd6a4016d40a8600f6103d6793`.
The seed copied only the named lab SSH **public** key, not private credentials
or an old installed seed. It requested sudo/hyperv-daemons/curl/ca-certificates,
without package upgrades, and generated a fresh instance/nonce-bound public
host-key KVP report.

Fresh instance: `native-reprovision-16db9296585c44a48f4ab8871653601e`.
Fresh boot: `332efe43-49d6-49ae-975f-e9c5fb4c9c02`.
The KVP read independently checked that same VM/DMI, nonce, instance, image and
the exact active independent disk graph before appending the new public SSH pin.
Existing pins were preserved; strict SSH host-key checking was never disabled.

## Read-only clean-system verification

Cloud-init completed without errors. Debian 13 amd64 booted with plain ext4,
without project/quota features or mounts. All checked platform state, binaries,
configuration, WAF installation, managed service units and identities were absent.
`/srv/hosting` and the actual candidate API-created account directory were absent;
none of the platform's web/database/cache/test ports were listening. The only
lab operator, `stackfort-test`, came from the new independent seed.

This verifies removal from the active OS. Retained old VHDX/checkpoints still
contain recoverable test data and are not presented as securely erased. The
user's VPS and MBR failure fixture were not reprovisioned. A fresh-vendor checkpoint
`6f55414a-7686-4ebd-beb2-d89624afcc10` was saved afterward for the subsequent public
download installation test; it is not the removal mechanism.

| Retained record/helper | SHA-256 |
| --- | --- |
| preparation receipt | `9c6a6e7930da02042b80689a8c7b1cb5138b9e4da9ded252a56b3cd5b65d84ff` |
| exact attachment record | `077fd9af24651078017ad175654cad53d2af91694dcf1630658d760c73dc57b0` |
| bound public host-key receipt | `b43b76b0024dd6fcb1620bab1591f5a89b16b6a4e5bcfd5c7f6b6eca25fe73df` |
| clean-system verification | `7b91a5083fc8bf7a8df0428ae8e690ed676bbed7d5750557280ee6611ff6435c` |
| preparation helper | `a92e835c4d329112cf8eed88549641ccff93d57fe32deebbf76d3d1e9c7e3985` |
| attachment helper | `76464746c7d13fcd6854384f7302ff76fb614b8e20f9323fb7bf169bbe39751d` |
| read-only verification helper | `d20cb625c0a7674dd6eefc09c92af7f1f371e6b13129c209eb997a0b773b8ecc` |
