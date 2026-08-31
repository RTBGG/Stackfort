# Localization foundation

H-002 establishes English as Stackfort's source locale and requires a complete
German catalog. Locale selection remains limited to the typed `en` and `de`
identifiers; presentation uses the explicit `en-US` and `de-DE` BCP 47 tags.

## Formatting contract

`web/src/formatting.ts` is the only application-level formatting boundary for
localized measurements. It provides pure, typed helpers for:

- dates and times through `Intl.DateTimeFormat`;
- numbers and percentages through `Intl.NumberFormat`;
- binary byte quantities using 1024-based `B` through `PiB` suffixes;
- byte rates as a localized byte quantity per second; and
- rounded seconds as localized day/hour/minute/second duration lists.

API timestamps remain UTC. A valid ISO timestamp is presented in the browser's
current time zone unless the caller explicitly supplies another time zone.
Invalid dates, negative measurements, and non-finite values raise `RangeError`
instead of rendering misleading output. Components convert unavailable or
invalid backend values to translated state labels.

The application shell uses this boundary for domain counts, progress, and build
timestamps. Later resource and usage screens must reuse it for storage, traffic,
limits, operation ages, and service timestamps.

## Catalog integrity

`i18n.test.ts` treats the English leaf-key set as canonical. Every supported
catalog must have exactly that structure, contain no empty messages, and retain
the same named interpolation placeholders as English. This catches both missing
translations and translations that would fail at runtime.

`npm run check:i18n` parses every Vue single-file component with Vue's compiler.
It rejects alphabetic literal template text and static values in critical
`alt`, `aria-description`, `aria-label`, `placeholder`, and `title` attributes.
Backend data and decorative non-alphabetic symbols may remain expressions or
literals; user-facing prose and labels must resolve through translation keys.

CI runs the literal check as a named web step. The repository-level
`scripts/verify.sh` command runs the same gate locally.

## Verification

Unit tests cover both locales for dates, numbers, percentages, bytes, rates,
durations, and invalid input. The browser critical flow loads a fixed UTC build
timestamp and switches between German and English. It verifies translated UI,
locale-specific date order, separators, percent spacing, and the browser-local
time-zone conversion.
