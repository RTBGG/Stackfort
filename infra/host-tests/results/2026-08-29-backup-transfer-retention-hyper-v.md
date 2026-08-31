# K-007 backup transfer/retention Hyper-V qualification

- Date: 2026-08-29
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `f74b295009ec2d0e2edcb35fd790b3edebe95a053fc7ec6e090845617b0fa85f`
- Production agent/helper SHA-256:
  `27cc4974e26e19a5ab86f7de4fa1af3433529c9871c7308d8a92f95694b9aa18`

The binaries were built once from the final K-007 tree and reused unchanged on
all three guests. Each run created a fresh derived hosting identity and used the
separately built production agent for payload creation and restore.

| Guest | Result | Test duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 0.48 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 0.35 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 0.25 s |

Each guest additionally verified:

- full and single-range download only after complete HMAC, SHA-256, tar grammar,
  entry-count, and content-size verification;
- exact-offset resumable upload into root-owned staging and publication with a
  new host-authenticated manifest;
- permanent exact-UUID deletion and immediate measured-count update;
- apparent-size reservation and independent backup repository quota rejection;
- existing K-006 restore, tamper rejection, cross-account isolation, ownership,
  modes, and cleanup invariants.

Both qualification markers were observed. All three VMs were `Off` afterward.
