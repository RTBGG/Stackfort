# ADR 0042: Account-credential file-download stream

- Status: accepted and implemented by K-002
- Date: 2026-08-28

## Context

Serving file content through the existing 64-KiB JSON RPC would require
buffering, weaken its bounded control-plane contract, and encourage a generic
privileged path API. Reading an already validated descriptor in the root agent
would also bypass the account's actual Unix read permissions. Downloads must
survive beyond the ordinary 10/30-second HTTP write deadlines while still
terminating promptly after a browser disconnect.

## Decision

1. Keep `hosting.files.list` metadata-only and add a distinct typed streaming
   endpoint on the same peer-credential-authenticated Unix socket.
2. Accept only a complete managed identity, canonical account-relative file
   path, and an optional closed single-range union. Reject absolute paths,
   traversal, multi-range syntax, and unknown JSON fields.
3. Start the same root-owned agent binary in a hidden helper mode with the
   account UID/GID, no supplementary groups, and a parent-death signal. The
   helper must independently validate its effective identity.
4. Open every directory with `O_PATH|O_DIRECTORY|O_NOFOLLOW`, enforce trusted
   ancestor/account ownership and one filesystem device, then open the final
   component with `O_RDONLY|O_NOFOLLOW|O_NONBLOCK`. Stream only a same-device,
   account-owned regular file.
5. Send one bounded JSON metadata frame from helper to agent followed by the
   exact raw byte count. Helper logs never share this stdout channel.
6. Permit at most four concurrent streams, 4 GiB per response, and 30 minutes
   per stream. Support one RFC-style byte range so larger files can be
   retrieved in bounded pieces.
7. Bind the API-to-agent request and helper process to the browser request
   context. Remove write deadlines only from a validated active stream.
8. Derive `Content-Disposition` from the already validated final filename and
   expose download actions only for listed regular files.

## Consequences

- File bytes are copied with constant memory and never cross a JSON boundary.
- The account's kernel-enforced traversal/read permissions remain authoritative
  even though the parent agent is privileged.
- Symlinks, FIFOs, sockets, devices, ownership conflicts, and cross-device
  paths cannot be downloaded through the supported operation.
- A single response is bounded; clients needing more than 4 GiB must use
  sequential range requests. Multipart ranges are intentionally absent.
- K-003 can reuse the separation principle, but upload staging and mutation
  auditing require a new write-specific contract rather than extending this
  read-only helper.
