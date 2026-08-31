# Accessible application shell

H-001 provides the shared Vue application frame used by later administrator
and account-owner workflows. It deliberately does not imitate authentication
or resource mutations that belong to H-003 and H-004.

## Semantic structure

The shell contains one skip link, complementary sidebar, labelled primary
navigation, banner, live API status, language control, and main landmark. Each
navigation group has a visible heading. Navigation uses real buttons and marks
exactly one current page with `aria-current="page"`; decorative icons are
removed from the accessibility tree.

Changing the in-application page moves focus to the new level-one heading so a
keyboard or screen-reader user receives the same context change as a visual
user. The skip link performs an explicit focus transfer to the main landmark,
avoiding browser differences in fragment-only focus behavior.

English remains the source locale and every shell label has a German
counterpart. Locale changes update the document language and title and persist
only the supported locale identifier in local storage.

## Narrow layout and focus

At 900 CSS pixels and below, the sidebar becomes an off-canvas drawer. Opening
it:

- moves focus to its close control;
- makes the page frame inert;
- keeps Tab and Shift+Tab within the drawer;
- locks document scrolling; and
- exposes a labelled backdrop close action.

Escape or the close/backdrop action restores focus to the menu button. Selecting
a destination closes the drawer and moves focus to the new heading. At 320 CSS
pixels the header, language control, page content, and cards fit without
horizontal scrolling.

Motion transitions are reduced to effectively zero under
`prefers-reduced-motion: reduce`. All interactive controls have a minimum
44-pixel target in the narrow layout and a three-pixel high-contrast
`:focus-visible` indicator.

## Verification

`App.a11y.test.ts` mounts the real Vue shell in jsdom and runs Axe Core, excluding
only its layout-dependent color-contrast rule. It also checks landmarks, the
skip-link focus transfer, one-current-page invariant, mobile focus trapping,
Escape restoration, route-heading focus, document language, and locale
persistence. Translation structure tests require exact English/German key
parity.

The manual critical-flow review uses the in-app Chromium browser against the
Vite server. Desktop and 320-by-720 views were checked for readable contrast,
focus indication, content order, open/closed drawer layout, keyboard behavior,
language switching, overflow, and console warnings. The reviewed browser
reported no console errors or warnings.

