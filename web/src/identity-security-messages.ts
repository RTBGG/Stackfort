// SPDX-License-Identifier: AGPL-3.0-or-later

export const identitySecurityMessages = {
  en: {
    title: 'Two-factor authentication', body: 'Protect your identity with a time-based authenticator. Changes require a sign-in within the last five minutes.',
    enabled: 'Two-factor authentication is enabled.', disabled: 'Two-factor authentication is not enabled.',
    remaining: '{count} unused recovery codes', enable: 'Set up authenticator', replace: 'Replace authenticator',
    currentFactor: 'Current authenticator or recovery code', replacementHint: 'Replacing the authenticator invalidates the old authenticator and all old recovery codes after confirmation.',
    secret: 'Manual setup key', setupHint: 'In your authenticator, add a time-based account named Stackfort and enter this key. Keep it private. Setup is not active until you confirm a code.',
    expires: 'Setup expires at {date}.', code: 'Six-digit code from the new authenticator', confirm: 'Confirm and enable', cancel: 'Cancel setup',
    expired: 'Setup expired. Start again with a new key.', stay: 'Stay on this page after confirmation to save your new recovery codes. All existing sessions will be signed out.',
    remove: 'Remove two-factor authentication', removeConfirm: 'I understand that removing this factor reduces account protection and signs out all sessions.',
    requestFailed: 'The security request could not be completed. Check your session and try again.',
    verificationFailed: 'The authenticator or recovery code is invalid, expired, or already used.',
    recoveryTitle: 'Save your recovery codes', recoveryBody: 'Your authenticator is active and all sessions have been signed out. These codes are displayed once. Each can replace your authenticator code for one sign-in.',
    recoveryWarning: 'Store them offline in a protected place. Do not share them. Leaving or reloading this page discards this display; codes cannot be shown again.',
    saved: 'I have saved my recovery codes securely.', login: 'Continue to sign in', signInAgain: 'Sign out and sign in again',
  },
  de: {
    title: 'Zwei-Faktor-Authentifizierung', body: 'Schütze deine Identität mit einer zeitbasierten Authenticator-App. Änderungen erfordern eine Anmeldung innerhalb der letzten fünf Minuten.',
    enabled: 'Die Zwei-Faktor-Authentifizierung ist aktiviert.', disabled: 'Die Zwei-Faktor-Authentifizierung ist nicht aktiviert.',
    remaining: '{count} unbenutzte Wiederherstellungscodes', enable: 'Authenticator einrichten', replace: 'Authenticator ersetzen',
    currentFactor: 'Aktueller Authenticator- oder Wiederherstellungscode', replacementHint: 'Nach der Bestätigung werden der bisherige Authenticator und alle bisherigen Wiederherstellungscodes ungültig.',
    secret: 'Schlüssel zur manuellen Einrichtung', setupHint: 'Füge in deiner Authenticator-App ein zeitbasiertes Konto namens Stackfort hinzu und trage diesen Schlüssel ein. Halte ihn geheim. Erst die Codebestätigung aktiviert die Einrichtung.',
    expires: 'Die Einrichtung läuft am {date} ab.', code: 'Sechsstelliger Code des neuen Authenticators', confirm: 'Bestätigen und aktivieren', cancel: 'Einrichtung abbrechen',
    expired: 'Die Einrichtung ist abgelaufen. Beginne mit einem neuen Schlüssel.', stay: 'Bleibe nach der Bestätigung auf dieser Seite, um die neuen Wiederherstellungscodes zu sichern. Alle bestehenden Sitzungen werden abgemeldet.',
    remove: 'Zwei-Faktor-Authentifizierung entfernen', removeConfirm: 'Ich verstehe, dass das Entfernen dieses Faktors den Kontoschutz verringert und alle Sitzungen abmeldet.',
    requestFailed: 'Die Sicherheitsanfrage konnte nicht abgeschlossen werden. Prüfe deine Sitzung und versuche es erneut.',
    verificationFailed: 'Der Authenticator- oder Wiederherstellungscode ist ungültig, abgelaufen oder bereits verwendet.',
    recoveryTitle: 'Wiederherstellungscodes sichern', recoveryBody: 'Dein Authenticator ist aktiv und alle Sitzungen wurden abgemeldet. Diese Codes werden einmalig angezeigt. Jeder Code ersetzt den Authenticator-Code für eine Anmeldung.',
    recoveryWarning: 'Bewahre die Codes offline an einem geschützten Ort auf und teile sie nicht. Beim Verlassen oder Neuladen dieser Seite geht diese Anzeige verloren; die Codes können nicht erneut angezeigt werden.',
    saved: 'Ich habe meine Wiederherstellungscodes sicher aufbewahrt.', login: 'Weiter zur Anmeldung', signInAgain: 'Abmelden und erneut anmelden',
  },
} as const
