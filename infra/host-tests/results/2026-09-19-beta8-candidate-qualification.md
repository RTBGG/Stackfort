# Beta.8 candidate qualification — 2026-09-19

Status: **native installation and original setup passed; first domain activation
failed; not publishable**. The frozen beta.8 tag and selected bytes stay unchanged.

The [beta.7 installation failure](2026-09-19-beta7-candidate-qualification.md)
identified missing Vinyl runtime compiler/header dependencies. The next source
declares those dependencies and adds three compiler-free container checks.

## Rejected pre-selection source

Source `a27492d25d04028e7d2a0898e4d3e9b822d3df3c` was submitted to
[build run 35440648878](https://github.com/RTBGG/Stackfort/actions/runs/35440648878).
All three new Vinyl minimal-runtime jobs passed: Debian 13, Ubuntu 26.04 and
Rocky Linux 10. These checks install the exact package into a fresh container
without a compiler, then compile its managed VCL. They are not complete host
installation or service qualification.

This source was **not selected or tagged**. Its
[CI run](https://github.com/RTBGG/Stackfort/actions/runs/35440589715) failed because
the Linux-only Rocky transaction test still expected the former dependency
command. Windows tests do not exercise Linux-tagged files. The test expectation
now includes the required compiler and C headers; the fixed three Vinyl tests
were cross-compiled and executed successfully on Linux using only mocked
package commands and temporary test fixtures, without repairing beta.7.

The [Security run](https://github.com/RTBGG/Stackfort/actions/runs/35440589733)
also flagged the report's public executable SHA-256 as a generic API key.
The value was independently verified against the retained beta.7 archive, not
obtained from a credential store. The historical exception is limited to that
exact commit/file/rule/line fingerprint. The current label avoids the ambiguous
wording; no rule or report directory is excluded from scanning.

## Second pre-selection source: checks passed, documentation stale

Source `8035d8044029355dc3a8b6f1799c021f11b0f815` passed
[CI](https://github.com/RTBGG/Stackfort/actions/runs/35441008430),
[Security](https://github.com/RTBGG/Stackfort/actions/runs/35441008422) and
[original build 35441057414](https://github.com/RTBGG/Stackfort/actions/runs/35441057414),
all on attempt 1. The full seven-artifact inventory and aggregate artifact
`10583299321` (313,140,612 bytes) passed download/integrity inspection.
ZIP SHA-256: `7bc78871d86e7a3f537a1a3b0d897fe006f6a05d3aeae6beff73322a99be2886`;
TAR SHA-256: `8174012320606e9319699113236d1c702ff4de83680087f0a4cf761e8d974988`.
The strict extractor verified all ten files, checksums, source/version, SBOM
and archived/standalone installer equality. The corrected Vinyl revision and
independent WAF-member digests were also inspected.

Before committing a selection or creating a tag, review found stale beta.6
version labels in `SECURITY.md` and the installer command README. This would
contradict exact-candidate support disclosure. The uncommitted proposed
selection was retained with this run's ignored diagnostic files, not committed
as a promotion. No tag or native installation was started from this source.
The labels are corrected and a documentation regression now requires current
support/quick-start versions to agree with the canonical bootstrap default;
synthetic stale, mixed, missing and ambiguous versions are rejected.

A new original build from the corrected source was required to pass CI and
Security before selection, as recorded below. Neither superseded run was rerun,
substituted into a selection, or treated as a qualified candidate. Those two
pre-selection builds provide no beta.8 installation or publication result.

## Corrected retained candidate

Frozen source: `e217f9fbe6f62491f2142391af8799c5231255a1`.
[Original build 35441790427](https://github.com/RTBGG/Stackfort/actions/runs/35441790427),
[CI 35441754938](https://github.com/RTBGG/Stackfort/actions/runs/35441754938) and
[Security 35441754895](https://github.com/RTBGG/Stackfort/actions/runs/35441754895)
all passed on attempt 1 before the selection commit. The build includes all six
native-package jobs and all three compiler-free Vinyl runtime checks. Slow Rocky
repository downloads delayed its jobs but did not require a retry or bypass.

- Aggregate artifact: `10583803548`, 313,145,063 bytes.
- ZIP SHA-256: `fdf0bc28a3058dbe930d84082c784c0a6d116d35db45829088401c41aa36d903`.
- TAR SHA-256: `b5a7f7a39c3bc6f132d9aafc46d1a024a781aad4e8aa8955b4089aa3326a7da0`.
- Installer SHA-256: `48003e5bc856608ea7bdfe010440d9987f71c4af18130b33515add611e60d14d`.
- Control-plane executable SHA-256: `bff777e41819c2e08fa675db721f973798353a154b40d40963a735af4f55bc89`.
- Agent SHA-256: `905a8c50e1c51231d91b47206757bb7006662175abf955e56169703dc4f93dd5`.
- Bootstrap SHA-256: `6bd01feab35e2306dd404467c4264cbe590ced22060ebc487b45761bd6deecae`.

Mechanical selection validation and strict extraction passed against the
complete seven-artifact API inventory. All ten payload files, checksums, carrier
sidecars, SPDX metadata and source/version bindings were verified. The archived
and standalone installer are identical. A separate read-only inspection checked
the Debian Vinyl revision `9.0.1-2sf1` and the WAF package/module/library/inventory
digests against the authenticated TAR and its component manifest.

The selection is not publication approval. No beta.8 tag-qualified installation,
setup redemption, live product test or release is claimed by these build checks.

## Prepared fresh target

The beta.7 failed state is retained with checkpoint
`bd6eb82c-e64d-4809-bc2b-1460cbfbe4a1`. The exact disposable VM was shut down
gracefully, its complete old disk chain recorded, and both system/seed attachments
replaced with independently prepared vendor-image disks. No disk or checkpoint
was deleted, restored or copied from the installed host. The rescue clone stays
off. This fresh baseline preparation is not post-install removal qualification.

The vendor Debian image remains SHA-256
`85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`,
authenticated using official HTTPS and published SHA512SUMS. The VM-bound public
KVP report authenticated the new SSH key before replacing its strict pin.
Cloud-init completed without errors. Secure Boot is on; the 50 GiB root is plain
GPT/ext4 with 256-byte inodes, approximately 47 GiB free and 3.24 million free
inodes, without project-quota features or Stackfort state/configuration.

Initial boot: `b1c8349b-386a-4ef3-aaac-76c12130d4ef`.
Fresh checkpoint: `63424f12-be04-483c-a58e-01003e09c886`
(`native-beta8-vendor-fresh-20260919`). It remains unchanged while the exact
candidate is built and validated; the live guest subsequently advanced as below.

## Exact tag and installation

Annotated tag `v0.1.0-beta.8` resolves to the frozen source, not the later
selection-record commit. [Tag run 35442457289](https://github.com/RTBGG/Stackfort/actions/runs/35442457289)
reused the original selected payload without rebuilding, created its tag-bound
attestation and retained unpublished artifact `10584680248`. Its readiness gate
then failed closed because the required readiness document was absent (HTTP 404);
publication was skipped, not successful.

The retained tag ZIP is 313,153,138 bytes, SHA-256
`7afb564279e7f64bacc55650e0e804c953339822b2a78e15418d8df109c4de71`.
All ten original files are byte-identical; the tag artifact adds the promotion
record and attestation. Attestation SHA-256:
`52362b45c7af820714c1948dbee87966a4695324c2f9e62e033e8d4dc5f435c2`;
checksums SHA-256:
`c903fe3293b7dba19316e1228d54884fd86e29a856fe80dcdb8bb118cb5223b6`.
Local extraction verified integrity; the unchanged archived installer performed
the actual cryptographic origin validation during onboarding.

The retained-fixture transport exercised the real public dispatcher, exact
interactive release/host review, disposable-server/reboot acknowledgement and
original setup-code `SAVED` acknowledgement. The driver kept the setup code and
credentials only in memory; no terminal transcript or raw credential response
was logged. This transport does not qualify public GitHub downloads.

Prerequisites completed. Automatic offline quota preparation reached `ready`
after the authorized reboot, boot ID `0142ddaa-9043-49cc-9837-e133fa456efa`.
The native installation then completed all nine stages, including the corrected
Vinyl package and service/health checks. Admission is `admitted` and
`stackfort-native-install.service` is `active/exited`, result `success`.
The hosting bind mount is ext4 with `prjquota`. No manual filesystem preparation
or replacement executable was needed. Installed installer, control-plane and
agent executable hashes match the independent selected archive hashes above.

Original setup redemption, refusal of repeated redemption, login and session
checks passed. The installed API smoke detected managed PHP, created its package
and hosting account, and confirmed successful account provisioning/host-ready.
It then failed the first static-domain operation:
`01a0b9a0-a6ae-7df4-ad94-7e8d256ed326`, account
`01a0b9a0-a292-79b9-8ed4-4cb0b7607621`, attempt 1,
error code `nginx.activation_rejected`. The driver exited unsuccessfully at
`domains-and-files`; no complete onboarding-success receipt was generated.
The original setup credentials were not recovered, renewed or replaced.

The live failure is preserved in checkpoint
`1d38a630-1a10-4a63-bdc3-1fd7c8282107`
(`native-beta8-domain-failure-20260919`). No snapshot was restored, fixture
retried, installed payload patched or service configuration repaired.

## Long-domain hash failure and source correction

NGINX 1.26.3 rejected the valid fixture hostname
`sf-candidate-900356f0971a-static.example.test` and its managed `www` alias:
`could not build server_names_hash`, requesting a bucket larger than 64 bytes.
An isolated unprivileged syntax-only probe reproduced this error with the
default bucket; 128 and 512 passed. A second, more complete diagnostic built
from the frozen source read only the failed operation's account/desired-state
records, rendered its exact candidate and redirected its include to a new
test-owned directory. The unchanged baseline renderer failed with the same
error; adding only `server_names_hash_bucket_size 512` passed with the installed
Coraza module present. No service was started/reloaded and the installed main
configuration digest stayed unchanged. The active baseline still passes `nginx -t`.

The diagnostic executable SHA-256 is
`4414f901f17833213c2b6ed52bfe91f0d9dc4f819f7ece14d217f1fd60274871`.
Its temporary files and the failure checkpoint remain available. This confirms
the configuration defect, not a successful domain activation on the candidate.

The subsequent source sets the shared main/candidate HTTP-context bucket to 512,
covering long DNS names and hash metadata rather than shortening the smoke
fixtures. NGINX documents [long-name bucket sizing](https://nginx.org/en/docs/http/server_names.html#optimization)
and [hash allocation](https://nginx.org/en/docs/hash.html); this correctness fix
does not claim a measured performance improvement or unlimited-domain capacity.

New regression coverage includes both root configurations, all three rendered
distribution variants, short names, the exact failed hostname, IDNA names and
a 249-byte base with a valid 253-byte `www` alias. The real-NGINX syntax test
also requires two virtual servers on one port and verifies that deliberately
undersized 64/256-byte buckets fail for the expected reason. It creates only
temporary test files and never starts/reloads NGINX. CI explicitly opts in
after installing NGINX; absence of the binary fails rather than silently skips.

The Linux regression executable, SHA-256
`83d1fdf74cf4563dc9c0df61c8eb244d4e3b06694033c2c792ca53ac37572a68`,
passed all 24 positive syntax subtests, both negative controls and the complete
baseline unit suite as an unprivileged user on the Debian guest. Distribution
variants here mean rendered settings tested by Debian's binary, not three
independent operating-system installation qualifications. Local Go tests,
`go vet` and documentation contracts also passed. Remote CI and a newly built
candidate remain separate requirements.

## Still required

The next candidate must include this fix and repeat fresh exact-tag onboarding,
complete domain/file/database/backup/cache/WAF product smoke, same-release rerun,
ordinary reboot/persistence, external listener/isolation/resource and OCI checks,
process-loss quarantine and same-target full-OS removal. These later phases did
not execute in this attempt. No final-candidate approval, public release or
successful public README one-line download is claimed.
