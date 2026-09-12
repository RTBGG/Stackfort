# OCI security review and bounded regression evidence

Date: 2026-09-12. Reviewed base: `6a74497f3b6324888c86f7e2b70bf189ad9353d4`, followed by the uncommitted OCI security fixes described below. This is a scoped engineering review, not an independent external security audit or a release qualification claim.

## Outcome

The reported OCI CodeQL paths were traced through their actual request validators, derived identifiers, filesystem boundaries and callers. No caller-controlled lexical path traversal was demonstrated at the cited sinks. This is **not** an automatic dismissal of the alerts or a statement that all OCI behavior was safe: the review found real redirect, activation-error and mutable build/scan-input defects, which were fixed and regression-tested.

No GitHub security alert was dismissed or suppressed by this review. Existing root-owned host directories remain part of the trust boundary; these checks do not protect against a malicious host administrator or arbitrary root filesystem corruption.

## Cited CodeQL paths

Line numbers below refer to the original reported source, before the fixes shifted some lines.

| Reported group | Guard and call-chain evidence | Assessment |
| --- | --- | --- |
| `hostocideployment/manager_linux.go`: #54 line 215, #55 line 245, #56 line 321, #57 line 333, #58 line 345, #60 line 362, #61 line 379, #67 line 358 | Public `Reconcile` calls `ocideployment.ValidateRequest`; `Normalize` requires `hostingidentity.Validate`, a canonical UUIDv7 application ID and bounded revision. `RenderQuadlet` validates again, then `hostingoci.ForIdentity` derives `/etc/containers/systemd/users/<UID>/stackfort-<32hex>.container`. UID is 200000–249999. `validateQuadletParent` requires that exact directory, root:root ownership, mode 0755 and no final symlink; files are root:root regular 0644. Temporary files are generated inside that directory. Removal is of the same rendered fixed basename. | No raw user-selected pathname reaches these sinks. `hostidentity.ensureAbsoluteDirectoryChain` creates/checks Quadlet ancestors descriptor-relative, rejects symlinks, non-root ownership and group/other write permission. The deployment manager still depends on this managed ancestor trust boundary. |
| `hostocideployment/manager_linux.go`: #64 line 431, #65 line 434 | Private `writeManifest` derives directories from constant `DeploymentStateRoot`, canonical account/application UUIDs, bounded decimal revision and a locally computed SHA-256 digest. State directories are root:root 0700; final relative manifest I/O uses `os.OpenRoot`. | No demonstrated request path injection. The installed root-owned `/var/lib/stackfort-agent` ancestor is trusted. |
| `hostocideployment/manager_linux.go`: #66 allocation at line 462 | Public `ReadLogs` calls `ValidateLogSpec` before `parseJournal`; `Tail` must be 1–500. The fixed journal execution profile checks the same bound independently. Command output is capped at 256 KiB, scanner lines at 64 KiB, and retained messages at 8 KiB. | The `make(..., 0, limit)` capacity is not an unbounded user allocation through this entry point. |
| `hostociresources/manager_linux.go`: #52/#53 lines 344/347 | Public `Reconcile` calls `ociresources.Validate`, which validates the complete derived hosting identity, canonical application UUID, revision, secret references and volume UUIDs. Manifest directories are constant `ArtifactRoot` plus those IDs. Root:root 0700 is checked; `os.OpenRoot` confines final manifest access. Tenant volumes use descriptor-relative `openat`/`mkdirat`, no-follow, same-device and exact ownership/mode checks. | No demonstrated lexical traversal at the cited state-directory sinks. |
| `hostociimage/manager_linux.go`: lines 108, 114, 156, 163, 165 | `Prepare` validates `ociimage.PrepareSpec` and independently calls `TransactionDirectory` for a canonical UUIDv7 operation ID. Transaction and archive names are fixed derivations under constant `TransactionRoot`, not arbitrary paths. Transactions are root-owned, mode 0711, and exclusively created. | Lexical containment was present, but ownership changes made the archive contents mutable. See the real defect and fix below. |
| `hostociimage/manager_linux.go`: lines 234, 260, 287, 297, 306, 309 | Source home must exactly equal `/srv/hosting/accounts/<canonical-account-UUID>`. `NormalizeSource` permits only normalized bounded relative build paths. Source access uses `os.OpenRoot`; final file opens use no-follow and regular-file checks. `WalkDir` rejects symlink/special entries, and each relative output must not be `..` or begin `../`. Output paths lie below the exclusively created transaction/context. | No demonstrated lexical escape. However, the former tenant ownership of the snapshot invalidated its immutability claim after validation. |
| `hostociimage/manager_linux.go`: lines 367, 408, 421, 428, 431, 443, 450 | Private manifest helpers receive only constant `ArtifactRoot`, validated account/application UUIDs and bounded decimal revision. Root-owned 0700 directories and 0600 regular, non-symlink, single-link manifest files are checked; reads are bounded. | No demonstrated request path injection. Private helpers are not generic public path APIs, and managed root ancestors are assumed. |

Relevant contract implementations are `internal/hostingidentity/spec.go`, `internal/hostingoci/spec.go`, `internal/ociapps/spec.go`, `internal/ocideployment/{spec,quadlet}.go`, `internal/ociimage/policy.go`, `internal/ociresources/spec.go` and the corresponding manager implementations. UUID checks require parse success, exact canonical string equality and version 7; they do not merely remove `../` substrings.

## Real defects fixed

### Privileged health-check redirects

The original HTTP probe started at loopback but used the default redirect-following HTTP client. A tenant-controlled response could direct a privileged probe toward a different host or port. The fix rejects redirects, pins the dialer to the original loopback endpoint, disables proxies and accepts only direct 2xx responses. Per-attempt cancellation, bounded body consumption and transport cleanup remain in place.

Tests cover 301/302/303/307/308 with another local endpoint, a relative redirect and a metadata-service URL. They assert exactly one original request and zero secondary requests. Direct 200/204 succeed; 300/304/400/500 fail.

### Discarded reload/restart errors

An inner `err` declaration discarded `daemon-reload` and restart failures. A healthy old application could then make the new deployment appear successful and persist a success manifest even though activation had failed. The fix propagates the command error, does not run the health probe after activation failure, and follows the existing restoration path.

The complete deploy regression runs in a child mount namespace with a private `/etc`. It simulates a healthy previously active deployment and separately fails reload/restart through a returned error and nonzero exit status. It checks failure output, no success manifest, skipped probe, restored previous Quadlet and the exact recovery command sequence. No live service manager or live host Quadlet is modified by this test.

### Mutable build snapshots and scanner inputs

Mode 0400/0500 did not make tenant-owned files immutable: their owner could chmod and change them after validation. Build inputs now stay root-owned and use the tenant's private group: regular files/Containerfile 0440, executable files 0550, directories 0550. Group access becomes visible only after descendants are finalized. Other tenants receive no content access. This retains the existing normalization that removes write permission; it does not promise arbitrary source metadata preservation.

Changing ownership of the exported `image.tar` also did not revoke an already-open writable tenant FD. `sealImageArchive` now opens the bounded single-link export without following symlinks, copies it to a newly created root-owned 0600 inode, syncs and validates that stable copy, and atomically replaces the scanner's fixed `image.tar` path. A retained tenant FD points to the old, unlinked inode. No scanner command argument or environment bypass was added.

The verifier hashes every blob, checks descriptor sizes, requires exactly one image manifest and binds its config digest to the image ID previously returned by the fixed Podman inspect profile (`{{.Id}}`). It additionally verifies each ordered layer's uncompressed SHA-256 against the config's `rootfs.diff_ids`; checking only index labels or compressed blob names would not provide that binding. This follows the OCI [content-addressed layout](https://github.com/opencontainers/image-spec/blob/main/image-layout.md), [manifest descriptors](https://github.com/opencontainers/image-spec/blob/main/manifest.md) and [configuration/ImageID and DiffID relationships](https://github.com/opencontainers/image-spec/blob/main/config.md).

The verifier does not extract files or fetch URLs. It rejects links, alternate Docker `manifest.json`, duplicate archive paths, duplicate/case-aliased JSON keys, non-ASCII object keys that could form Unicode field aliases, unsupported encodings and nonzero trailing archive data. Image replay manifest schema is now 2, so an old unchecked receipt cannot bypass the new preparation path.

### FIFO blocking and actual Podman ImageID format

Peer review identified a further availability defect: a tenant could make the Containerfile a FIFO, or race a context file into a FIFO, and block the privileged open/read before the regular-file rejection. Both source opens now use `O_NONBLOCK` and `O_NOFOLLOW`; Containerfile metadata is checked before reading any content. A bounded regression tests Containerfile/context FIFOs without a writer and requires prompt rejection.

The old image-inspect mock also differed from the producer. Podman's fixed `{{.Id}}` format emits a bare 64-character image ID, whereas the manager previously required a `sha256:` prefix. The manager now accepts only exact lowercase 64-hex output, optionally followed by one LF, then prefixes it for the internal digest contract. Prefixed, uppercase, whitespace-padded and multiline output is rejected. This follows the actual format documented in [Podman image inspect](https://docs.podman.io/en/stable/markdown/podman-image-inspect.1.html) and was subsequently confirmed by the actual producer smoke below.

## Limits and compatibility

- HTTP health endpoints must directly return 2xx; redirect-based checks are deliberately no longer accepted.
- Existing schema-1 image-preparation receipts are rejected, not silently upgraded. A new application revision/preparation is needed; no old state is deleted automatically.
- The bounded scanner export supports a single OCI image, SHA-256 blobs, uncompressed or gzip OCI layers, at most 128 layers and 1024 archive entries, 1 MiB per JSON metadata document, a 2 GiB exported archive and 4 GiB aggregate expanded layer bytes. Context cancellation is checked during copying/hashing.
- Zstd, alternate Docker archives, multi-image indexes, externally referenced layer blobs and non-ASCII metadata object keys fail closed. These are explicit compatibility limits, not claims about all valid OCI layouts.
- Retaining root ownership prevents chmod/write by the tenant; it does not revoke read access from that tenant, nor protect against root. Temporary source copies also require real free space; aggregate shared-root capacity reservations are a separate policy question.
- This review does not claim that an already-installed historical image was rescanned, nor that OCI image preparation is exposed through a complete public web/API feature.

## Validation performed

The parent operator executed the following hash-verified cross-compiled test binaries on the disposable native Debian host as root with `STACKFORT_DISPOSABLE_HOST_TEST=1`. Both completed with PASS and no skipped top-level tests. Logs were inspected locally after execution.

| Test binary | SHA-256 | Evidence |
| --- | --- | --- |
| `work/hostocideployment-redirect-fix.test` | `f3c326ef40f8ea3d228bf60708d231f613b8439c0ec74c7604e12462d9223e52` | `work/hostocideployment-redirect-fix.test.log`; direct HTTP behavior and isolated complete deploy failure regressions. |
| `work/hostociimage-immutable-fix-v1.test` | `ffe1cd544147fce0e9ccdd685373e89c4f4161afdff175fa500e34db4d8e6fda` | `work/hostociimage-immutable-fix.test.log`; 11 top-level tests, including real tenant credentials, foreign-group denial, chmod/write denial and retained writable FD isolation. The original qualified binary was preserved under the explicit `-v1` name. |
| `work/hostociimage-immutable-fix-v2.test` | `f3cb38e2a8557d0ff2e889532d6c3cd212e1227a0c674a5c0ffb3064ba83480a` | `work/hostociimage-immutable-fix-v2.test.log`; all 13 top-level tests PASS without skips, including the subsequent FIFO and actual-producer ImageID format regressions. |
| `work/hostociimage-producer-v1.test` | `1b5908477d7442ed12bacdfb52353f28bfb3fd1ddd328239a75db9a06a775234` | `work/hostociimage-producer-v1.test.log`; the actual `TestDisposableRealPodmanOCIArchiveProducer` passed without skips in 0.58 seconds, with both `STACKFORT_DISPOSABLE_HOST_TEST=1` and `STACKFORT_REAL_PODMAN_EXPORT_TEST=1`. |

The same final producer-v1 binary subsequently passed the complete hostociimage suite: all 14 top-level tests without skips, including the real producer and credential checks (`work/hostociimage-final-suite-v1.test.log`).

The image suite also tests compressed/uncompressed content-addressed OCI-layout fixtures, wrong config ImageID, blob tampering, a foreign layer with internally correct blob/manifest hashes but the wrong config DiffID, duplicate paths/keys, path escape, links, alternate Docker metadata and cancellation. Existing prepare/scan/replay and source-symlink tests continue to pass using the bounded layout fixture. These hash-binding unit fixtures are not a real runtime-produced image and do not substitute for a Podman/Trivy integration run.

The complete Windows `go test ./...` suite passed (`work/native-beta4-security-fixes-windows-go.log`). Pure hosting identity/runtime, OCI application/image/resource/deployment contract suites also passed locally. Windows results alone do not exercise Linux-only adapters.

The subsequent FIFO, real-producer ImageID normalization and focused gosec amendments passed the Linux rerun as the separately preserved `-v2` binary above. A Linux-target gosec 2.28 scan reported zero issues (`work/native-beta4-security-gosec-v2.json` and `.log`). The changes added an explicit tar-entry read bound and narrowly justified only the private derived archive path and root-owned/private-group read-only mode. This gosec result is not a GitHub CodeQL alert dismissal.

The separate actual producer test uses installed Podman in a freshly created private vfs store with fixed isolated `--root`, `--runroot`, temporary configuration and private mount/network namespaces. It builds `FROM scratch` plus `COPY` of one small fixture without network, `RUN` or container startup, reads the real bare `.Id`, saves an OCI archive, seals and re-verifies it, and removes only its own isolated image/store. It passed on native Debian and confirmed the current producer's config/layer binding and archive layout. It does not exercise rootless Buildah, production tenant-group snapshot access by Buildah, Trivy, or deployment admission.

## Outstanding qualification

The actual rootful Podman producer format smoke above is complete. A real final-installed-candidate rootless image build/save/Trivy-scan/deploy lifecycle remains pending. In particular, verify actual rootless builder access to root-owned tenant-group inputs and application behavior when a Containerfile copies the normalized permissions into an image that selects a non-root runtime user. The successful test fixtures, tenant credential checks and isolated rootful producer smoke do not substitute for that qualification.

Peer-agent review of the bounded snapshot/seal/identity work is complete, with no additional blocking code findings after the FIFO and ImageID-format fixes. Those fixes passed the Linux rerun, and the requested real producer smoke also passed. This engineering review is not final release approval or an external audit. No release or production-readiness claim is made here.
