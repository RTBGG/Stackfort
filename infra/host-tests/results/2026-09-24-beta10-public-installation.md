# Beta.10 immutable publication and public GitHub installation

Result: **passed**, 2026-09-24. This is the first public experimental release,
not production approval or independent security review. The
[maintainer decision](../../../docs/release-evidence/2026-09-24-beta10-publication-approval.md)
authorizes only fresh disposable Debian 13 amd64 test servers, community support,
and destructive full-OS reinstallation for removal.

## Publication and unchanged bytes

- [Public release](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.10):
  ID395551154, published at `2026-09-24T10:07:03Z`, draft=false,
  prerelease=true, immutable=true.
- [Tag workflow35452066167](https://github.com/RTBGG/Stackfort/actions/runs/35452066167),
  attempt2, promotion job107586003320: all steps passed, including exact retained
  artifact promotion, live readiness, predecessor inventory, final provenance,
  publication and `gh release verify`.
- Source remains `0096faed0ae77aef3326ce629db7591a433bb37f`; annotated tag object
  remains `cd6c2319f18fd1f34ba43057bc79958cd1be8ffd`.
  No rebuild, tag move, gate waiver or replacement of published assets occurred.
- Readiness pins evidence commit `fa0a1da4806c23794345b72498c6dd516ac44a93`,
  actual maintainer approval and the
  [prior exact-candidate qualification](2026-09-19-beta10-candidate-qualification.md).
  Live CI35451311504 and Security35451311533 attempt1 inventories passed.
  No predecessor existed; the first-release upgrade matrix has zero cells.
- Original build35451346082 attempt1/artifact10586044694 remains authoritative.
  Its ZIP SHA-256 is
  `4d68a705d86a58df3d1c8519c01e0d4c39cc8142bb3b184859c0a0a4ff061896`.

All **14 public release assets** were actually downloaded over HTTPS on the
Windows qualification host, with exact sizes and SHA-256 checked against GitHub
metadata. All10 original build files were additionally compared against retained
bytes and remained identical. GitHub exposes the six carrier package/sidecar
names with `.beta.10` where originals contain `~beta.10`; bytes and internal
package versions are unchanged. This does not affect the archive/bootstrap;
the manual guide documents original-name restoration before carrier checksums.

| Input | SHA-256 |
| --- | --- |
| `stackfort-0.1.0-beta.10-linux-amd64.tar.gz` | `8e7ce111d6cefda172e74bd7a71380c181e66a726d8f157f6bd1af8944e93263` |
| `SHA256SUMS` | `2f46582e7f806aff11f0d1706df4f35e9fe98c42af9528160fdfa46e470c5b2e` |
| Public `build-attestation.jsonl` | `88709e156b0f29966a60196df6159b4e5885e7cec8b8dba852aa0026dd3b6f6b` |
| Bootstrap from public `main` and frozen source | `21977a1a0cb79b0768a23da3e669697f8451a51179643e49b17e270dfbffbe65` |
| Archived installer | `40331c7ee783aba3f4ca9597f0d2881afa45a83c92ee0359d7583c41064f30b2` |

The new tag-attempt attestation differs from the earlier unpublished attempt1
attestation as expected; archived product bytes do not.

## Fresh public installation

Dedicated Hyper-V VM `4361f439-15e9-4f9e-a690-9a8e44b6cbd3` used the independent
Debian OS disks from the [completed removal test](2026-09-19-beta10-os-removal.md).
Exact disk IDs/attachments, MAC and strict SSH identity were checked; the
duplicate-identity rescue VM remained off. No checkpoint was restored or deleted.
Fresh preflight passed at `2026-09-24T10:06:26Z`: Debian13 amd64, error-free
cloud-init, plain writable ext4 without project/quota features, and no Stackfort
state, hosting data, managed identities/units or platform listeners.

The unchanged external onboarding driver
(`48f7d3ec51ee43c55b29bbf6d186ca407549955266592963c7d0e33b05febbc9`)
and installed-API helper
(`49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196`)
ran with `Transport=public-github`.

Fresh invocation fetched the bootstrap at the frozen source commit and explicitly
selected Beta.10, equal to that script's default. Independently downloaded
`main` bytes matched exactly. An empty environment excluded fixture switches.
The bootstrap downloaded the actual public archive/checksums/attestation from
GitHub and verified genuine tag provenance. The driver also stages hash-pinned
reference files in the guest, but the public branch neither configures nor
consumes them as a fallback. No installed binary was repaired or replaced.

Real controlling-terminal fresh-disposable/reinstallation-risk, reboot and
`SAVED` acknowledgements completed. The original setup code stayed in process
memory, was redeemed after installation, rejected on reuse, and allowed original
administrator login. Raw credentials, cookies and terminal transcripts were not
retained.

Operation: `120f43ab-db34-4ebb-b587-98989ccb87e7`.

| Boot | Identity |
| --- | --- |
| Fresh OS | `e51181d2-e427-43c7-bc97-06d2f06a3cc1` |
| Automatic quota conversion/install | `719451e5-3ada-4842-87d8-31ef60e26918` |
| Ordinary subsequent reboot | `8f9085a1-12aa-4fc9-8b84-9589e9a7d756` |

Installed API checks passed at `2026-09-24T10:12:12Z`:

- package/account provisioning and idempotent domain creation;
- static/PHP upload, download and actual web serving;
- database/database-user wizard;
- document-root backup/download/restore and content verification;
- FastCGI enable/disable, MISS/HIT, cookie bypass and purge;
- WAF off/detection/blocking, including blocking before a warmed cache.

The same public bootstrap rerun passed live admission without reconversion,
setup-code reissue or reboot. Original session, package/account/domains, file and
backup digests, database inventory, static/PHP serving, WAF and FastCGI persisted
both after rerun and ordinary reboot. DHCP changed across boots; fixed SSH
identity plus exact operation/storage/setup bindings authenticated the new
address without changing the original TLS/cookie authority. Full driver result:
passed at `2026-09-24T10:12:56Z`.

Finally the actual README pipe from public `main`, without explicit version or
fixture settings, passed on the completed installation at
`2026-09-24T10:13:53Z`: journal-pinned Beta.10, live admission success, exit0,
no setup reissue or changed boot. This was a completed-install rerun; the fresh
test used the byte-identical commit-pinned bootstrap.

## Retained evidence and limits

Ignored raw receipts remain under `infra/host-tests/work/publication-beta10-20260924/`:

| Receipt | SHA-256 |
| --- | --- |
| `release.json` | `6f3398662c4f756f68f31f276bd329dc1016d0986c6ba11b3ca900ef45829d00` |
| `original-byte-comparison.json` | `61c0b86baa49bc7e37218d7347e36abdfe8af12e56c53f065664cbb86a465cd4` |
| `fresh-preflight.json` | `b575675f2da2f619b29cf1277d4367f2ddd74fa6c980eb193e8f89261cc5acaf` |
| `public-onboarding-result.json` | `bd812f4c1ab2bc9706b84f3909b183a4a3e2b8e9199915ff2c529c29c084de37` |
| `main-readme-rerun.json` | `edf6dc2c5c348dd3d536c7e3aea77bd5e689665888007086dcacd441de7542f4` |

This public-transport run does not repeat the earlier exact-candidate resource,
isolation, rootless OCI, process-loss or removal tests; their reports remain
separate. Nor does it claim a new benchmark, visual EN/DE browser review,
phpMyAdmin/SQL credential test, full-account/database backup, public OCI workflow,
globally routed IPv6, I/O-rate proof, independent audit or production suitability.
It covers only the documented Debian13 amd64 GPT/ext4/GRUB profile; Ubuntu/Rocky
native conversion and arbitrary provider images remain unqualified. Shared-root
capacity exhaustion remains an explicit experimental limitation. Removal requires
complete OS reinstallation destroying all server data; no in-place uninstall or
successful in-place recovery is promised.
