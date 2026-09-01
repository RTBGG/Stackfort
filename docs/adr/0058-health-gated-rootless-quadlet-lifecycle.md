# ADR 0058: Health-gate fixed rootless Quadlets before atomic domain routing

- Status: accepted
- Date: 2026-09-01
- Extends: [ADR 0054](0054-rootless-podman-account-runtime.md), [ADR 0055](0055-digest-pinned-bounded-oci-image-preparation.md), [ADR 0057](0057-account-private-oci-resources.md)

## Context

An approved image and private resources are not sufficient evidence that an
application is safe to route. General Compose/Podman options would reopen host
ports and privileged features, while marking a database row active before the
real workload is ready would let NGINX publish a broken or stale revision.
Secrets and logs also need narrow, non-durable boundaries.

## Decision

1. Persist one immutable loopback-only port allocation per application and
   render every Quadlet from a closed, revisioned spec. Publish only on
   `127.0.0.1`; fix the private network, digest image, hardening, labels,
   restart policy, secrets, and managed volumes.
2. Let the local agent own atomic root-only Quadlet replacement and fixed
   rootless Podman/systemd profiles. No engine socket or generic command surface
   is introduced.
3. Decrypt environment values only after the durable revision fence and pass
   them through the authenticated local request directly to fixed Podman stdin.
   Never place plaintext in durable work, arguments, logs, Quadlets, or replay
   state; clear transient buffers and remove derived secrets at retirement.
4. Require a successful loopback HTTP/TCP health probe before appending
   deployment evidence or making the revision routable. Restore the previous
   Quadlet and active state when candidate activation fails.
5. Route only `active` applications whose applied revision equals the current
   revision. Feed their persisted loopback allocations to the existing atomic
   NGINX activation pipeline. Refuse suspend/remove while an active route exists.
6. Provide revision-fenced deploy, suspend, resume, rollback, and remove
   operations plus bounded, sanitized journald reads from the derived unit.

## Consequences

- Customer workloads cannot bind a public port directly; NGINX remains the
  only public ingress and WAF/TLS policy stays ahead of the application.
- A deployment can be running before its later domain operation activates, but
  a domain can never point at an unverified or stale application revision.
- Secret plaintext crosses one authenticated local IPC boundary transiently;
  the agent cache retains only request digests and responses.
- Managed volume data intentionally survives workload removal. A future
  explicit retention/deletion workflow must authorize destructive data removal.
- L-006 must still prove reboot recovery, aggregate resource control, malicious
  workload behavior, and cross-account isolation on all three supported guests.

## References

- <https://docs.podman.io/en/stable/markdown/podman-systemd.unit.5.html>
- <https://docs.podman.io/en/stable/markdown/podman-secret-create.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-secret-rm.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-run.1.html>
