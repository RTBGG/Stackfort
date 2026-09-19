# Beta.9 candidate qualification — 2026-09-19

Status: **source preparation; no selected build, host qualification or publication**.

The [beta.8 attempt](2026-09-19-beta8-candidate-qualification.md) completed native
installation and original setup, then failed its first domain activation because
the NGINX server-name hash bucket could not hold the valid managed hostname.
Fix source `10627d77baa5ff139f5ce54f9ba8fea09d81bb81` passed
[CI](https://github.com/RTBGG/Stackfort/actions/runs/35443533379) and
[Security](https://github.com/RTBGG/Stackfort/actions/runs/35443534674), both on
attempt 1. CI includes the new real-NGINX syntax checks and reproducible builds.
This does not qualify an archive built from subsequent source.

Beta.9 retains that fix and updates the bootstrap/support/quick-start version
references together. The user independently reproduced the public beta.8
`SHA256SUMS` HTTP 404; the Releases API inventory was empty. No public download
or successful installation is inferred from our retained-fixture tests.

The bootstrap now explains which release asset is unavailable on HTTP 404 and
that installation has not started. Other transfer failures retain curl's exit
status and HTTP status. There is no fallback version, unverified transport or
verification bypass. The default selection still requires published assets.

The extended shell regression tests use a private mount namespace, fixed-path
synthetic state and a private PATH download shim; they perform no network call
or installed-host mutation. Missing checksum/archive/attestation, HTTP 500 and
DNS/transport failure all abort before the fixture installer. Successful mocked
downloads still exercise the ordinary checksum and handler path. All eight
qualification markers passed on the existing Debian VM without repairing its
failed product state. The exact tested bootstrap SHA-256 is
`582b1c4124444200f6148b333e977696a3ad2fee88b457d5fe387313f4ca00e8`;
shell-test SHA-256 is
`5fffcf29525abe7bde431268c3a91a8360c4f0892cd013de673a281f701e663b`.
Documentation contracts and 237 Markdown files / 859 local links also passed.

## Prepared target and remaining work

A new independent vendor-image system/seed pair is prepared but not yet attached.
It is separate from the reserved final-removal pair. Its 50 GiB system disk ID
is `02a6e8b8-3664-b34c-9d7b-a1d094b76340`, initial SHA-256
`1977b77523b37cb3266b9c660c968da5dd0ec3b34c23e1747b93b9e5ad3ee115`.
The 64 MiB seed ID is `71c313ff-749f-45cb-9b92-6187963080af`, SHA-256
`43c418361d7e14a66078483fc52876ea377b60c5d6a3c2de34e28426e6afa188`.
The failed beta.8 disk chain and checkpoint remain intact, with no reset or
credential recovery. No candidate installation/removal result is claimed here.

Required next: exact-source CI/security, one retained original native build,
mechanical selection and tag attestation, fresh exact-tag onboarding/product
smoke, rerun/reboot, host/security/isolation/OCI/failure/removal qualification,
actual candidate-specific publication approval and the publication gates.
Only then can the public README download path be verified on a fresh host.
