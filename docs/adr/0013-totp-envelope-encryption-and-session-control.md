# ADR 0013: TOTP envelope encryption and identity session control

Status: Accepted

## Context

TOTP requires a server-retrievable shared secret, while recovery codes and login
challenges do not. A database disclosure must not directly reveal reusable
factors, and a password-authenticated request must not become a full session
before the configured second factor succeeds. Factor replacement and session
revocation also need race-resistant identity scope.

## Decision

1. Use a 160-bit, HMAC-SHA-1, six-digit, 30-second TOTP profile with a one-step
   verification window and a persisted highest-used counter.
2. Envelope-encrypt every setup and active TOTP secret with AES-256-GCM, a
   per-record 256-bit data key, and a filesystem-held 256-bit host master key.
   Bind purpose, record, identity, and key version as associated data.
3. Generate ten 128-bit recovery codes, return them once, store SHA-256 digests
   only, and consume them transactionally.
4. Store only digests of five-minute MFA login challenges. Do not create or
   rotate the browser session until the second factor succeeds.
5. Persist the factor's replay counter, challenge consumption, recovery-code
   use, session creation, and relevant audit event in serialized transactions.
6. Scope self-service session reads and mutations to the server-derived
   identity subject. Require recent authentication for mutation and revoke all
   sessions after factor activation, replacement, or removal.

## Consequences

- SQLite or a state backup alone does not disclose a usable TOTP secret.
- The master key is mandatory recovery material and needs an independent secure
  backup and later key-rotation tooling.
- HMAC-SHA-1 maximizes authenticator compatibility; its use is narrowly limited
  to the standardized keyed OTP construction.
- A user must save recovery codes at activation because plaintext cannot be
  reconstructed later.
- Changing factor state intentionally signs the identity out everywhere.
- TOTP remains phishing-susceptible; phishing-resistant WebAuthn/passkeys can be
  added as a separate factor type later.

