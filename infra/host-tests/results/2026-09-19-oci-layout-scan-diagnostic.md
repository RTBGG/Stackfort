# Installed-broker OCI layout scan diagnostic

Date: 2026-09-19. Result: **PASS for the modified diagnostic installation only**.
This is not public one-line installation, exact-release qualification, a security
audit, or publication approval. Public GitHub release inventory remained empty
when checked on this date. Failed beta.4/beta.5 tags remain unchanged.

## Defect and correction

The preceding diagnostic built a rootless image successfully but received
`oci-image-scan-failed`. Trivy 0.74.0 tried to read the OCI TAR as a Docker
archive, then as a directory containing `index.json`. Its supported OCI input
is a layout directory, not Podman's `oci-archive` transport.

Source commit `b57ff51c14cc69e21e46bda78e81f0c89fbe8574` retains the fresh
privileged archive copy, strict outer archive checks, blob hashes, inspected
config ImageID binding, ordered layer diffIDs and existing resource bounds.
Only after full verification does it copy the outer OCI files from that same
open descriptor into an exclusively created `image.oci` directory. Directories
are private `0700`, files `0600`; layer TAR contents are never unpacked onto the
host. Existing destination files, directories or symlinks fail closed without
being adopted or removed. The scanner profile now receives that fixed layout
path. Scanner version, vulnerability policy, timeout and privileges are unchanged.

See the [input contract](https://github.com/aquasecurity/trivy/blob/v0.74.0/pkg/fanal/image/archive.go)
and [product design](../../../docs/oci-image-preparation.md).

## Preserved host and scope

- VM: `stackfort-native-quota-debian-13`, ID
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`.
- DMI UUID: `6365bd88-5141-4f15-b3f8-2ba9996baad2`.
- The VM was initially off. A new checkpoint
  `native-beta6-before-trivy-layout-20260919`, ID
  `81b76f62-a8f3-4d12-b7ed-41f77825b338`, was created before starting it.
  The checkpoint's immediate name lookup lagged; subsequent inventory verified
  its ID and creation time. Older checkpoints and disks were retained.
- Diagnostic boot ID: `cf0edd14-5130-407e-b53a-5b96ff4c0e03`.
  Starting this already modified host is **not** a normal-reboot release test.
- The rescue clone and unrelated VMs remained off. Existing SSH host-key
  verification and the exact VM/MAC checks remained enabled.
- The prior agent binary was backed up under the new private diagnostic
  directory before replacement; the installed agent was inactive beforehand.
- Only the agent executable changed. It was built from the exact source export,
  labelled `dev-beta6-oci-layout-diagnostic`, then started through the existing
  diagnostic systemd unit. The old broker drop-ins and hosting-root mode
  correction described in the [earlier failure report](2026-09-12-beta5-onboarding-agent-sandbox-failure.md)
  remain in effect; neither represents an authenticated release payload.
- `/srv/hosting` and its native bind source remained root-owned mode `0711`.
  The control API was not brought online for this test.
- After the successful workflow and post-check, the same VM was shut down
  gracefully and verified off, restoring its initial power state. The modified
  diagnostic installation, prior binary backup and old failure evidence remain.

## Actual results

1. Windows `go test ./...` passed. Linux/amd64 cross-compilation and `go vet`
   passed for the changed packages and the integration helper.
2. Linux archive/manager tests passed, including exact compressed/uncompressed
   OCI byte copying, retained writable-FD isolation, malformed archives,
   conflicting paths and failed-materialization cleanup. The opted-in snapshot
   test actually ran as UID/GID200000 and200001 and could not access the private
   scanner archive/layout. The separately opted-in rootful producer test was
   skipped, not counted as a pass.
3. The first snapshot test attempt failed before the child could execute:
   the uploaded test ELF was mode `0700`. Only that hash-verified test ELF was
   changed to `0755`; the enclosing staging directory stayed `0700`. The unchanged
   test binary then passed, including both unprivileged children. No product
   permission or test assertion was relaxed.
4. Seven pure RPC-helper test groups passed on Linux before any new account
   mutation, including framing, peer-UID parsing and bounded cleanup behavior.
5. The real installed-agent RPC workflow passed in 17.13 seconds. Podman was
   `5.4.2+ds1-2+b2`, the unchanged bundled scanner was Trivy `0.74.0`:
   - Fresh fixture UID/GID `249944`, account
     `01a0b914-6fde-7c88-abe2-e207d5dbf131`, application
     `01a0b914-6fde-7c96-8907-6b17e11397f4`.
   - Identity, project quota, account resource limits and rootless runtime
     reconciliation all returned HTTP200 through the actual peer-authenticated
     agent socket, using the installed control-service UID.
   - A scratch image executed a real `RUN` after `USER 1000:1000`, without
     instruction networking or a base-image pull. Export, archive verification,
     layout materialization and the actual vulnerability scan returned HTTP200
     in approximately 15 seconds. No scanner mock, offline database bypass or
     vulnerability exception was used.
   - Accepted config ImageID:
     `sha256:fbe07727dec481604a1bed96b72b6234000c8e74b122e12cb03721be5185154e`.
     Bound source digest:
     `sha256:8a8b07bc3657fb1d2cb9841b9bf0a5bbf04968abcb384bb17481a275c9c1e6d7`.
   - An unchanged-source replay returned HTTP200; changed-source reuse of the
     same revision returned HTTP409 as required.
   - Private resources and deployment succeeded. Actual loopback HTTP health,
     fixture content and UID/GID1000 inside the container were verified. The
     host process mapped to subordinate UID/GID3274130983 and remained under
     the exact delegated account cgroup. Bounded application-log retrieval passed.
   - Successful cleanup removed only this new application's container, image,
     private network, account runtime, state, home and identity. Quota reset used
     the production closed execution profile as the root test operator, not an
     installed-agent quota-reset RPC. Old failed fixtures and shared Trivy cache
     were retained. Post-check independently confirmed the new home, runtime
     directory and passwd identity were absent.
6. Linux-targeted gosec over `./cmd/... ./internal/... ./tests/...` passed:
   312 files, 80,852 lines, zero findings. This is automated analysis, not an
   independent security review. Documentation/link validation also passed.

The workflow is a diagnostic socket integration test. It does not exercise
browser setup, API-persisted operations, public NGINX routing, WAF/cache, external
IPv6 reachability, resource pressure, full OS reprovision removal, or the public
GitHub download/attestation/install path.

## Retained artifact identity

All 1,053 tracked raw Git blobs extracted for the v6 helper matched their source
commit object IDs. Four ignored integration overlay files and the unchanged
fixture ELF were added. Relative to v5, only the image helper's source label and
fresh UID changed; RPC framing, cleanup policy and fixture bytes are unchanged.
The ignored diagnostic artifacts are retained locally, not release assets.

| Artifact | SHA-256 |
| --- | --- |
| Exact source TAR | `289d455739aa365cf82a4faa010fa813a9af19202d26e9a69f7d455672d5ebcf` |
| Diagnostic agent | `285191173c195291bd1f1c6b0a3b387edc8635f83842e248af26496e013780b2` |
| Backed-up previous diagnostic agent | `760ac45bdddfb6737c84d7f2b1c27bf6d94c600c8d6a93e32ee52a35a82903c6` |
| Unchanged installed Trivy | `d89bcc6510a267f11b773398cbf1be5520ce39f9e8b6633178c4487f05b7d791` |
| v6 RPC test ELF | `4c35d55fb50488bb2d422bdf99ca51598a2761dd67807a31292f0f10ce8c4173` |
| Image helper overlay | `823f401091e05db83477e8bf31c1a87217d2ddd435b382eb00fbd848b6990b6f` |
| Unchanged fixture ELF | `65cd0e5327e39b3ecfdb6276543baddd436d9d9c11f4b101a7f98da9d974e8d0` |
| Actual RPC workflow log | `a8f220a83bfca21e6f99e6ff3299801d777f02c7ea8f48b4ed86d933ed6b39ab` |
| Seven pure helper groups log | `34e7ba6da9df1067996ec71c39f87b4f4a8c303515c7b587fa160d369e8f4472` |
| Linux archive test ELF | `713c9bf7827ad1e3906a222b3357ff5649ab198f49dcfa26179e298d0f7f4e11` |
| Initial test-executable permission failure log | `ee1a623f8f30b9d95aebffd8176f10a4cfa0e349b83248bb0842c5c56bb3a001` |
| Successful Linux archive tests log | `3e7f0e5b303f352ab81c851b491ba0152b66c3f77e9f7034857a99d2a90b1fbd` |
| Product gosec JSON | `a7f6dcd555f2d8b3f64af08999dd2af25b031f70ebcd32a8e3c2d664bd6cbe7f` |
| Product gosec log | `7de2bccaf0029682fffcd92c4b5b6d73673fd605c25bd97f6060bb8e29044f28` |

## Next gate

Keep the remaining resource/isolation and release qualification gates. Build a
new immutable beta.6 candidate with this correction and repeat the complete
native onboarding/rerun/reboot/security/removal sequence on fresh vendor OS media.
The modified host and this diagnostic pass must not be promoted to a fresh-host
installation result. Exact-candidate publication approval remains separate.
