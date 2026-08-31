# K-002 secure file-download Hyper-V qualification

- Date: 2026-08-28
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `316ee666c1db14f3954f8c620a0f8d8105220abd5d39f13ef93cffeb12f01152`
- Production agent/helper SHA-256:
  `10ef7c64b7ad8b05a71d1d58b4a2f1cf10f209d94e950a3044104ff0b0319c7f`

The integration and agent binaries were built once from the final K-002 tree.
Every guest ran the production agent's hidden helper mode after the parent test
dropped it to a fresh disposable hosting account UID/GID.

| Guest | Result | Duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 0.17 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 0.10 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 0.22 s |

Each run verified:

- exact full-file streaming and a bounded single range;
- no-follow rejection of a symlink to `/etc/passwd`;
- denial of a mode-`0000` file while executing as the account identity;
- rejection of an un-ranged sparse file above the 4-GiB response ceiling;
- successful 32-byte range streaming from that larger file; and
- cancellation and termination of an unfinished 64-MiB sparse-file stream.

All wrappers observed `STACKFORT_QUALIFICATION file-manager-download=passed`.
All three VMs were Off after qualification.
