// SPDX-License-Identifier: AGPL-3.0-or-later
export const panelHostnameMessages = {
  "en": {
    "eyebrow": "Management endpoint",
    "title": "Panel domain",
    "body": "Use your own domain or subdomain for Stackfort with automatic Let’s Encrypt HTTPS.",
    "prerequisites": "First point every A/AAAA record to this server. Public TCP ports 80 and 443 must be reachable. Do not add this hostname as a hosted website. Sign in again if your administrator login is more than five minutes old.",
    "fallback": "The IP-based HTTPS access on port 8443 remains available. The panel uses a separate root-managed ACME account. Use its existing contact email when reconfiguring. Failed issuance attempts also trigger a one-hour cooldown.",
    "hostname": "Panel hostname",
    "hostnameExample": "panel.example.com",
    "confirm": "I confirm changing the panel address. An existing custom address may be replaced. I will need to sign in again at the new address.",
    "issue": "Set up domain and certificate",
    "terms": "Read terms",
    "endpoint": "Custom panel address",
    "disabled": "Not configured",
    "renewal": "Automatic certificate renewal",
    "pending": "Issuing and checking the certificate. This can take several minutes. Do not submit another request.",
    "success": "The panel address is ready. Open the link above and sign in again; this tab remains on the current address.",
    "failed": "The panel status could not be loaded.",
    "recovery": "An interrupted change requires administrator recovery using the server console before another request."
  },
  "de": {
    "eyebrow": "Verwaltungszugang",
    "title": "Panel-Domain",
    "body": "Nutze eine eigene Domain oder Subdomain für Stackfort mit automatischem HTTPS von Let’s Encrypt.",
    "prerequisites": "Richte zuerst alle A-/AAAA-Einträge auf diesen Server. TCP-Port 80 und 443 müssen öffentlich erreichbar sein. Lege diesen Hostnamen nicht als gehostete Website an. Melde dich erneut an, wenn deine Administrator-Anmeldung länger als fünf Minuten zurückliegt.",
    "fallback": "Der IP-Zugang über HTTPS auf Port 8443 bleibt erhalten. Das Panel nutzt ein separates, root-verwaltetes ACME-Konto. Verwende bei Änderungen dessen bisherige Kontakt-E-Mail. Auch fehlgeschlagene Ausstellungen lösen eine einstündige Wartezeit aus.",
    "hostname": "Panel-Hostname",
    "hostnameExample": "panel.example.com",
    "confirm": "Ich bestätige die Änderung der Panel-Adresse. Eine bestehende eigene Adresse kann ersetzt werden. Unter der neuen Adresse muss ich mich erneut anmelden.",
    "issue": "Domain und Zertifikat einrichten",
    "terms": "Bedingungen lesen",
    "endpoint": "Eigene Panel-Adresse",
    "disabled": "Nicht eingerichtet",
    "renewal": "Automatische Zertifikatserneuerung",
    "pending": "Das Zertifikat wird ausgestellt und geprüft. Dies kann einige Minuten dauern. Bitte keine weitere Anfrage stellen.",
    "success": "Die Panel-Adresse ist bereit. Öffne den Link oben und melde dich erneut an; dieser Tab bleibt auf der bisherigen Adresse.",
    "failed": "Der Panel-Status konnte nicht geladen werden.",
    "recovery": "Eine unterbrochene Änderung muss vor einer erneuten Anfrage über die Serverkonsole wiederhergestellt werden."
  }
} as const
