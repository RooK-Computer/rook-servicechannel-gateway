# Gateway-Implementierungsplaene

Status: Plan 06 umgesetzt, wartet auf Review

## Ziel dieser Planmappe

Diese Planmappe zerlegt die Gateway-Umsetzung in reviewbare, fortsetzbare Arbeitspakete. Sie ist so strukturiert, dass frische Agenten oder Menschen auch nach Unterbrechungen sauber weiterarbeiten koennen.

## Verbindliche Arbeitsreihenfolge

1. `plans/01-runtime-grundgeruest-und-backend-validierung.md`
2. `plans/02-browser-websocket-und-sitzungssteuerung.md`
3. `plans/03-ssh-bridge-und-terminaldatenpfad.md`
4. `plans/04-hardening-betrieb-und-lieferfaehigkeit.md`
5. `plans/05-nfpm-debian-paketierung-und-installationspfad.md`
6. `plans/06-websocket-autorisierung-per-nachricht-statt-header.md`

Wichtig: Nach Abschluss eines Plans wird gestoppt und auf menschliches Review gewartet. Der naechste Plan beginnt erst nach expliziter Freigabe.

## Gemeinsame Annahmen

* Implementierung als separater Go-Dienst.
* Browser-Verbindung per WebSocket.
* Grant-Validierung online gegen das Backend ueber `POST /api/gateway/1/validateToken`.
* Zielkonsole wird aus dem validierten Grant ueber ihre VPN-IP bestimmt.
* SSH-Login erfolgt initial mit Account `pi`.
* Gateway-seitiger SSH-Key liegt lokal unter `secrets/gateway_ssh_ed25519(.pub)` und muss spaeter extern gesichert werden.
* Browser-Reconnect erzeugt immer eine neue Gateway-Sitzung; derselbe Grant kann nur innerhalb des vom Backend vorgesehenen Grace-Windows erneut akzeptiert werden.

## Zielstruktur fuer das spaetere Repository

Die folgende Struktur ist die Zielrichtung fuer die Implementierung und wird in Plan 01 konkretisiert:

```text
cmd/gateway/
internal/config/
internal/httpserver/
internal/grants/
internal/session/
internal/websocket/
internal/sshbridge/
internal/audit/
internal/shutdown/
tests/
plans/
secrets/
```

## Review- und Resume-Regeln

* Jeder Detailplan enthaelt eigene Exit-Kriterien.
* Jeder Detailplan enthaelt einen Abschnitt `Naechste Uebergabe`.
* Bei Unterbrechungen wird direkt im bearbeiteten Plandokument weiterprotokolliert.
* `AGENTS.md` und diese Datei sind der Einstiegspunkt fuer frische Agenten.
* Das gemeinsame Statusdokument `spec/implementation/05-browser-terminal-gateway-status.md` muss bei Planfortschritt immer mit aktualisiert werden, damit das `spec`-Submodule als Bindeglied synchron bleibt.

## Planstatus-Matrix

| Plan | Thema | Status | Abhaengigkeit | Review-Gate |
| --- | --- | --- | --- | --- |
| 01 | Runtime-Grundgeruest und Backend-Validierung | Reviewed/abgenommen | keine | Pflicht |
| 02 | Browser-WebSocket und Sitzungssteuerung | Reviewed/abgenommen | Plan 01 | Pflicht |
| 03 | SSH-Bridge und Terminaldatenpfad | Reviewed/abgenommen | Plan 02 | Pflicht |
| 04 | Hardening, Betrieb und Lieferfaehigkeit | Reviewed/abgenommen | Plan 03 | Pflicht |
| 05 | nfpm-Debian-Paketierung und Installationspfad | Reviewed/abgenommen | Plan 04 | Pflicht |
| 06 | WebSocket-Autorisierung per Nachricht statt Header | Umgesetzt, wartet auf Review | Plan 05 | Pflicht |

## Fortschrittspflege

Bei spaeterer Umsetzung hier nachziehen:

* Plan 06 ist umgesetzt und wartet jetzt auf Review.
* Der zuvor erkannte Browser-Blocker ist damit technisch adressiert:
  * `GET /gateway/terminal` fuehrt jetzt zuerst das WebSocket-Upgrade aus.
  * Der Browser uebergibt den Grant danach als erste `authorize`-Nachricht.
  * Das Gateway bestaetigt erfolgreiche Autorisierung explizit mit `authorized`, bevor der normale Terminal-Datenpfad aktiv wird.
* Bisherige Entscheidungen mit Auswirkung auf Folgeplaene:
  * HTTP-Runtime in Plan 01 mit Go-Standardbibliothek umgesetzt, ohne zusaetzlichen Router.
  * Konfiguration ueber Umgebungsvariablen plus optionale lokale `KEY=VALUE`-Datei fuer Entwicklung.
  * Readiness prueft in Plan 01 nur lokale Runtime-/Konfigurationsbereitschaft, nicht die Live-Erreichbarkeit des Backends.
  * `GET /gateway/terminal` fuehrt jetzt ein echtes WebSocket-Upgrade ohne benutzerdefinierten Auth-Header aus.
  * Die Browser-Autorisierung erfolgt ueber eine initiale `authorize`-Nachricht; erfolgreiche Freigabe wird mit `authorized` bestaetigt.
  * Sitzungen werden zentral verwaltet; Queue-Tiefe, Inaktivitaets-Timeout, Session-Limit und WebSocket-Message-Groesse sind jetzt konfigurierbar.
  * SSH-Bridge und PTY-Datenpfad sind jetzt produktiv angebunden.
  * Host-Key-Verifikation wird fuer den aktuellen MVP bewusst umgangen und muss in Plan 04 gezielt nachgehaertet werden.
  * Plan 04 setzt fuer Betriebs-Observability auf zaehlbare strukturierte Session- und Audit-Logs statt auf eine eigene Metrik-Schnittstelle.
  * Plan 04 liefert einen `systemd`-Pfad mit Beispiel-Environment und dokumentiertem lokalen `make verify`-/`make test-e2e`-Freigabepfad.
  * Plan 05 fuehrt die Debian-Paketierung mit `nfpm` ein, ohne die Laufzeitkonfiguration auf einen festen Installationsmodus zu verengen.
  * Das Debian-Paket installiert den Gateway, aktiviert oder startet den `systemd`-Dienst aber standardmaessig nicht.
  * Der Paketbuild ist so angelegt, dass er auf macOS ohne Debian-Toolchain gebaut und per Archivinspektion geprueft werden kann.
  * Das Session-Limit zaehlt derzeit autorisierte SSH-gestuetzte Sitzungen; die kurze Vorautorisierungsphase vor `authorize` wird ausserhalb des Session-Registers abgewickelt.
* `spec/implementation/05-browser-terminal-gateway-status.md` wurde fuer diesen Meilenstein nachgezogen.
