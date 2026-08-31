# ADR 0022: Typed, context-specific NGINX rendering

Status: accepted

## Context

Domain names, document roots, redirect URLs, headers, and NGINX-owned dynamic
values occupy different grammar contexts. Quoting alone is not a complete
injection boundary. In particular, a backslash before `$` survives in generated
text but is removed before the NGINX rewrite module resolves variables.

Configuration also needs a stable digest for desired/applied revisions. Map or
server order that depends on repository iteration would create false changes
and unnecessary reloads.

## Decision

1. Render one complete account include from typed domain records in a pure,
   side-effect-free package. Keep staging, process validation, activation, and
   rollback outside the renderer.
2. Revalidate every persisted invariant at the renderer boundary and fail the
   whole result on an invalid, cross-account, conflicting, oversized, or not-yet
   supported target.
3. Expose no raw directive, header name/value, variable, path, or server-name
   source field. Generate grammar only from fixed templates and closed enums.
4. Apply encoding by context. Normalize hostnames and paths before rendering,
   NGINX-quote fixed values, URL-encode user-originated `$` as `%24` inside
   redirect literals, and append only renderer-owned dynamic variables.
5. Sort canonical hostnames and header enums before emission. Assign required
   map variables short collision-free ordinals only after sorting, and bind the
   output digest to the exact returned bytes.
6. Bound the input to 10,000 domains and output to 4 MiB. Skip suspended and
   removed routes; fail closed on PHP and OCI until their typed renderers exist.
7. Require adversarial unit tests and a real vendor `nginx -t` test on every
   supported distribution.

## Consequences

- Account users can select product behavior but cannot write NGINX source.
- Corrupt private state is detected again before it can become a service
  candidate.
- Equivalent intent does not cause configuration churn due to collection order.
- A literal `$` in a redirect target may be emitted in percent-encoded form,
  representing it as URL data without creating an NGINX variable expression.
- New headers, target types, or variables require a reviewed enum/template and
  tests; adding an arbitrary-string escape hatch is not compatible with this
  decision.
- F-003 treats the returned bytes and digest as an immutable candidate, then
  independently validates the full include graph and handles activation
  atomically as specified by ADR 0023.

## Rejected alternatives

- Accepting expert-mode raw NGINX snippets would bypass schema validation and
  make isolation, upgrades, and recovery dependent on tenant-authored source.
- Applying a generic shell/HTML/string escaper would target the wrong grammar.
- Escaping a literal dollar as `\$` is rejected because real NGINX packages
  still compile the following name as a variable in a `return` expression.
- Rendering directly into the active include directory would combine pure
  translation with privileged mutation and preclude safe staging.
- Preserving repository iteration order would make digests and reload decisions
  nondeterministic.
