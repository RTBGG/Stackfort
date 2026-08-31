# ADR 0027: Server-verified domain routing preview

Status: Accepted

## Context

The browser must preview canonical and customer redirects before activation.
Reimplementing URL concatenation or host selection in the browser can let the
preview diverge from NGINX, especially when a fixed target query is combined
with preserved source path/query data. Redirect-only domains also need an
explicit distinction between apex-only, `www`-only, and both source hosts.

## Decision

1. Persist one closed redirect host-mode enum: `apex_only`, `www_only`, or
   `both`; omitted legacy intent normalizes to `both`.
2. Reject wildcard subdomains unless both exact source hosts are selected.
3. Produce preview examples server-side after the same normalization and loop
   validation used by domain persistence.
4. Render unselected exact hosts as isolated non-customer 404 routes while
   retaining a fixed HTTP-01 exception when certificate intent requires it.
5. Construct NGINX redirect values from interleaved encoded literal segments
   and renderer-owned variables, keeping preserved path before the query.
6. Verify the preview contract with real 301/302 responses on every supported
   distribution, not only generated-source assertions.

## Consequences

- Browser code renders authoritative examples and does not reproduce routing
  rules.
- Narrow source-host scopes remain compatible with HTTP-01 certificate orders.
- Existing redirect records preserve their prior both-host behavior.
- A wildcard redirect cannot express a contradictory exact-host exclusion.
- Adding future redirect scopes requires a schema and typed protocol change
  rather than accepting free-form NGINX conditions.

