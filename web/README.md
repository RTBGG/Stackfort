# Web Interface

The interface uses Vue 3, TypeScript, Vite, and Vue I18n. English is the source
locale; both catalogs are checked, while the final manual EN/DE workflow review
remains a pre-beta gate. No untranslated literal
user-facing text should be introduced outside development-only screens.

From this directory:

```sh
npm ci
npm run dev
```

`npm run typecheck` explicitly checks both `tsconfig.app.json` and
`tsconfig.node.json`. Do not run plain `vue-tsc --noEmit` against the empty
solution configuration: it does not walk project references. Application source
and Vue templates remain strict. `skipLibCheck` skips dependency declaration
internals only: pinned vue-i18n 11.4.10 imports `GenericComponentInstance`, which
pinned Vue 3.5.42 does not export. Remove that workaround after upstream type
compatibility is restored; do not suppress application errors to work around it.

The development server listens on `127.0.0.1:5173` and proxies `/api` to the
local Stackfort API on `127.0.0.1:8080`. Run `npm run build` for type checking
and a production bundle, and `npm test` for translation parity and accessible
application-shell tests.

Run `npm run check:i18n` to reject critical literal text in Vue templates. Use
`src/formatting.ts` for locale-aware dates, numbers, percentages, binary byte
quantities, byte rates, and durations; do not format these values ad hoc in
components.

The shell provides keyboard navigation, visible focus states, a skip link,
semantic landmarks, responsive drawer focus management, English/German locale
switching, and reduced-motion support. See
`../docs/accessible-application-shell.md` for its behavior and verification
strategy.

The administrator console resolves one-time bootstrap, password/MFA login, and
session restoration before mounting protected views. It uses bounded authorized
APIs for packages, hosting accounts, operations, audit events, host services,
and account-scoped domain lifecycle work. Production authentication requires
HTTPS because Stackfort does not weaken its `Secure` host-only cookies for the
plain-HTTP development server.

After session discovery, `GET /api/v1/me` selects the administrator or
account-owner workspace from server-derived platform capability and active
memberships. The owner workspace exposes bounded account/package usage,
account-scoped domain/TLS actions, own profile editing, and identity-scoped
session revocation. Administrators can switch to it only for accounts where
they also hold an explicit membership.
