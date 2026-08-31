# ADR 0029: Centralized Intl formatting and template literal gate

Status: Accepted

## Context

Matching English and German message keys does not prevent components from
hard-coding labels or formatting server values with inconsistent separators,
units, and time zones. UI workflows will soon display quotas, traffic rates,
operation durations, and UTC timestamps, so this boundary must exist before
those screens multiply.

## Decision

1. Keep `en` and `de` as persisted application identifiers and map them to the
   explicit `en-US` and `de-DE` presentation locales.
2. Centralize dates, numbers, percentages, binary bytes, byte rates, and
   durations in pure typed helpers built on the platform `Intl` APIs.
3. Reject invalid dates, negative measurements, and non-finite values at the
   formatting boundary; components own translated unavailable-state labels.
4. Require exact catalog leaf-key and interpolation-placeholder parity with the
   English source catalog.
5. Parse Vue templates in CI and reject alphabetic literal text plus literal
   critical accessibility/form attributes.
6. Display UTC API timestamps in the user's browser time zone by default.

## Consequences

- Future screens share stable unit, separator, duration, and time-zone behavior.
- Translation omissions and common hard-coded UI strings fail before merge.
- Deliberately non-localized backend values remain permitted as template
  expressions rather than broad scanner exceptions.
- Additional locales require one typed identifier, one locale tag, a structurally
  complete catalog, and formatter test coverage.
