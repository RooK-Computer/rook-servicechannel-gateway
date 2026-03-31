# Gateway-Implementierungsplaene

Status: Entwurf erstellt, wartet auf Review

## Ziel dieser Planmappe

Diese Planmappe zerlegt die Gateway-Umsetzung in reviewbare, fortsetzbare Arbeitspakete. Sie ist so strukturiert, dass frische Agenten oder Menschen auch nach Unterbrechungen sauber weiterarbeiten koennen.

## Verbindliche Arbeitsreihenfolge

1. `plans/01-runtime-grundgeruest-und-backend-validierung.md`
2. `plans/02-browser-websocket-und-sitzungssteuerung.md`
3. `plans/03-ssh-bridge-und-terminaldatenpfad.md`
4. `plans/04-hardening-betrieb-und-lieferfaehigkeit.md`

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
| 01 | Runtime-Grundgeruest und Backend-Validierung | Entwurf | keine | Pflicht |
| 02 | Browser-WebSocket und Sitzungssteuerung | Entwurf | Plan 01 | Pflicht |
| 03 | SSH-Bridge und Terminaldatenpfad | Entwurf | Plan 02 | Pflicht |
| 04 | Hardening, Betrieb und Lieferfaehigkeit | Entwurf | Plan 03 | Pflicht |

## Fortschrittspflege

Bei spaeterer Umsetzung hier nachziehen:

* welcher Plan gerade in Arbeit ist
* welcher Plan im Review haengt
* welche Entscheidungen alle Folgeplaene beeinflussen
* ob `spec/implementation/05-browser-terminal-gateway-status.md` bereits zum letzten Meilenstein nachgezogen wurde
