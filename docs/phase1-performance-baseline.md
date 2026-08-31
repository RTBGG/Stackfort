# Phase 1 performance baseline

Status: Recorded on 2026-08-25

These measurements are a small, repeatable regression baseline for the
installed static-domain slice. They are not production capacity claims.

## Method

- Hyper-V host CPU: Intel Core Ultra 7 270K Plus
- Each guest: 2 vCPU and 4 GiB startup RAM
- Client: the qualification Go process inside the same guest
- Transport: loopback HTTP/1.1 with connection reuse, no TLS, no external
  network, no Vinyl Cache
- Warm-up: 32 requests per target
- Static probe: 4,000 requests at concurrency 8
- API probe: 2,000 requests at concurrency 8
- Regression floor: at least 100 requests/s and p99 no greater than 1 second
- Qualified archive SHA-256:
  `0c63f3e46bffc4da1f74a09bb17ea9cbe7c125d36bd376470b8fa2dab9250001`

The static target is an account-owned file served by the managed NGINX
configuration. The API target is `GET /api/v1/health` on the installed,
systemd-sandboxed control API. Each response status and body-read result is
validated; failed requests fail the test instead of being omitted.

## Results

| Guest | Target | Requests/s | p50 (µs) | p95 (µs) | p99 (µs) |
| --- | --- | ---: | ---: | ---: | ---: |
| Debian 13, kernel 6.12.101, NGINX 1.26.3 | Static NGINX | 62,010 | 57 | 396 | 855 |
| Debian 13, kernel 6.12.101 | Control API | 29,574 | 175 | 672 | 1,255 |
| Ubuntu 26.04 LTS, kernel 7.0.0-29, NGINX 1.28.3 | Static NGINX | 84,564 | 41 | 330 | 913 |
| Ubuntu 26.04 LTS, kernel 7.0.0-29 | Control API | 38,953 | 101 | 710 | 1,394 |
| Rocky Linux 10.2, kernel 6.12.0-211.16.1, NGINX 1.26.3 | Static NGINX | 67,978 | 52 | 390 | 937 |
| Rocky Linux 10.2, kernel 6.12.0-211.16.1 | Control API | 42,271 | 80 | 434 | 1,619 |

All six probes passed the intentionally broad regression floor. The very short
64/67 ms (Debian), 47/51 ms (Ubuntu), and 58/47 ms (Rocky) sample durations and
tiny loopback responses explain the high rates; scheduler noise can materially
change individual runs. Future comparisons must retain the request counts,
concurrency, VM shape, target payload, and artifact identity, and should use a
longer external load generator before making sizing or web-server/cache
decisions.

Raw records are emitted as one-line JSON prefixed with
`STACKFORT_PERFORMANCE` by `tests/integration/host_nginx_linux_test.go` and are
required by the Hyper-V installer harness. See
[Phase 1 qualification](phase1-qualification.md) for the functional matrix.
