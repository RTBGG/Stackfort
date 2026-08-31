# ADR 0028: Accessible shell focus and navigation

Status: Accepted

## Context

Stackfort needs one responsive shell for later administrator and account-owner
workflows. A visually hidden mobile menu, fake anchor links, or browser-dependent
fragment focus would make keyboard navigation unreliable. Adding a client-side
router before real routes exist would add a dependency without improving H-001.

## Decision

1. Use native landmarks, labelled sections, buttons, select controls, and a
   first-focusable skip link.
2. Keep H-001 destinations in typed in-memory state; H-003/H-004 may replace
   that state with real routes while preserving the same focus contract.
3. Move focus to the page H1 after an in-app destination change and explicitly
   focus main content from the skip link.
4. Treat the narrow sidebar as a modal drawer with inert background, contained
   Tab order, Escape close, and focus restoration.
5. Apply supported locale changes to document language/title immediately.
6. Combine automated semantic checks with real-browser desktop/mobile review;
   jsdom is not treated as evidence for visual contrast or responsive layout.

## Consequences

- Later flows inherit predictable keyboard and screen-reader behavior.
- Placeholder destinations remain honest application-shell states, not fake
  working resource screens.
- A future router must retain page-heading focus and current-page semantics.
- Visual regression and authenticated-flow tests can build on the same browser
  harness without changing the accessibility boundary.

