# K-003 staged file-write Hyper-V qualification

- Date: 2026-08-29
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `ece26c96a57c61eafc388d74377a74d32a041b8dd113eabd0e38b357f7893930`
- Production agent/helper SHA-256:
  `2bcd32afe4e5314722d25c302da7a6674f93417e89bdd0b2dc179da880c1de0c`

The integration and agent binaries were built once from the final K-003 tree.
Every guest ran the production agent's hidden file-write helper mode after the
parent test dropped it to a fresh disposable hosting account UID/GID.

| Guest | Result | Duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 0.23 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 0.18 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 0.16 s |

Each run verified:

- staging in the fixed hidden `.stackfort-uploads` directory;
- exact-offset chunks and status/resume through a fresh helper process;
- final size and SHA-256 verification followed by atomic activation;
- uploaded content and mode `0640` at the requested destination;
- descriptor-relative empty-directory and empty-file creation with modes
  `0750` and `0640`;
- no-replace conflict behavior that preserves the existing destination; and
- explicit cancellation that removes the incomplete staging records.

All wrappers observed `STACKFORT_QUALIFICATION file-manager-write=passed`.
All three VMs were Off after qualification.
