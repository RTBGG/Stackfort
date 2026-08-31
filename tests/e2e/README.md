# End-to-end tests

End-to-end tests cover administrator and account-user workflows in English and
German, including authentication, domain lifecycle, files, backup/restore,
database management, WAF, cache, and OCI deployments as they become available.

`browser-operation-fixture.mjs` is a localhost-only, synthetic account API for
manual real-browser review of the domain-operation progress bridge. Start it on
port 8080, start the Vite development server from `web`, and exercise domain
creation without real credentials, DNS, or a public ACME request. The fixture
requires the CSRF and idempotency headers emitted by the production browser
client and advances the operation through pending, running, and succeeded
states before returning the created domain. It also returns one active and one
retired non-secret certificate record for bilingual history review.
