# Host capability inspection

D-002 adds the first read-only host-agent operation:
`host.capabilities.inspect`. It reports what the current node can safely support
without turning a missing package, mount option, controller, or service into a
protocol failure.

## Result semantics

Every independently useful capability has one of four states:

| Status | Meaning |
| --- | --- |
| `available` | The feature or resource was detected and is currently usable. |
| `unavailable` | Stackfort knows the feature, but the current host state does not provide it. |
| `unsupported` | The detected platform or filesystem is outside the supported implementation. |
| `unknown` | A bounded probe could not establish the state safely. |

Every non-available state includes a stable `reasonCode`. The control API can
map that code to localized explanations and remediation without parsing agent
logs or free-form command output. Examples include
`project-quota-not-mounted`, `cgroup-controller-unavailable`,
`package-not-installed`, and `service-query-failed`.

The report contains:

- distribution ID/version, kernel release, and Go architecture;
- systemd-as-PID-1 state;
- cgroup v2 and the `cpu`, `memory`, `io`, and `pids` controllers;
- the filesystem and project-quota state for the fixed managed root
  `/srv/hosting`;
- AppArmor on Debian/Ubuntu or SELinux on Rocky Linux;
- TCP port availability for public ingress ports 80 and 443;
- twelve fixed package roles covering NGINX, PHP-FPM, MariaDB, Vinyl Cache,
  Podman, its rootless networking/storage helpers, subordinate-ID tooling, and
  Coraza;
- eight fixed systemd roles, including the Stackfort services and the
  distribution firewall service; and
- typed OCI readiness for rootless execution, Quadlet, networking, storage,
  rootful-socket isolation, image preparation, and the fixed bundled Trivy
  scanner.

## Probe boundary

The RPC request has an empty payload. It cannot select a path, port, package,
unit, executable, argument, or environment variable.

Kernel and platform state is read from bounded, fixed sources including
`/etc/os-release`, `/proc`, `/sys/fs/cgroup`, and mount information. The kernel
documents `cgroup.controllers` as the list of controllers available for a
cgroup to enable. See the
[Linux cgroup v2 documentation](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html).

Package and service probes use distribution-specific, fixed absolute binaries:

- Debian/Ubuntu: `/usr/bin/dpkg-query`;
- Rocky Linux: `/usr/bin/rpm`;
- service properties: `/usr/bin/systemctl show` for allowlisted units only.

They are executed directly without a shell, with a minimal fixed environment,
a one-second per-command timeout, and separate 32-KiB output bounds. The entire
inspection is capped at eight seconds. Command output is parsed into allowlisted
fields and is never returned verbatim. D-003 now routes these probes through the
shared profiled runner, including Linux process-group cleanup on timeout or
output exhaustion. This narrow read-only probe remains unavailable as a general
execution API. See [Safe external process runner](safe-external-process-runner.md).

The OCI scanner probe only checks the fixed
`/usr/local/libexec/stackfort-trivy` regular executable installed by the
verified release. Linux requires exact root ownership, mode `0755`, and one
link. The probe does not run the scanner, contact a registry, open a Podman
socket, or accept a path/version from the caller. Image preparation requires
both rootless/storage/socket readiness and scanner readiness; see
[Bounded OCI image preparation](oci-image-preparation.md).

systemd reports `LoadState`, `ActiveState`, `SubState`, and `UnitFileState`
separately. This follows systemd's model in which load and active state are
orthogonal. See the
[systemd D-Bus unit properties](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.systemd1.html).

Project quota is reported available only when the target's longest matching
mount is:

- XFS with `prjquota` or `pquota`; or
- ext4 with `prjquota`.

Other filesystems return `unsupported`; a supported filesystem without the
required mount option returns `unavailable`. The kernel describes project quota
mount modes in its [XFS documentation](https://docs.kernel.org/admin-guide/xfs.html)
and the general [quota subsystem](https://docs.kernel.org/filesystems/quota.html).
E-002 repeats this focused mount inspection immediately before its mutation and
returns the unavailable/unsupported capability instead of recording an applied
limit. See [Hosting filesystem layout and project quota](hosting-filesystem-layout-and-quota.md).

## Freshness and verification

`inspectedAt` records when the report began. Callers should use a new
idempotency key when they require a fresh report; replaying the same key can
return the bounded D-001 cache entry.

Pure fixture tests cover Debian 13, Ubuntu 26.04, and Rocky Linux 10. They cover
both ext4 and XFS quota states, AppArmor and SELinux, cgroup controllers,
occupied ports, missing packages, and missing systemd units. Malformed fixture
data must become a typed `unknown` or `unsupported` capability rather than a
whole-operation parsing error. Linux integration and smoke tests exercise the
typed operation through the authenticated Unix socket on a real host.

The Linux control API exposes the validated result as
`GET /api/v1/admin/host/capabilities`. It authenticates the browser session,
authorizes the server-derived subject for `platform.view`, and generates a fresh
agent idempotency key. The API also derives `managedPhpVersions`: the closed
native runtime list is non-empty only when the platform/systemd capability and
exact distribution package are available. Non-administrators are rejected before the agent call. An
unreachable agent becomes the stable browser error `host_agent_unavailable`;
socket and subprocess details are not returned.
