# Plan 06 - WebSocket-Autorisierung per Nachricht statt Header

Status: Reviewed/abgenommen

Zuletzt aktualisiert: 2026-04-02

## Ziel

Die Browser-Anbindung soll wieder mit echten Browser-WebSocket-APIs kompatibel werden. Dazu wird die Header-basierte Grant-Uebergabe aus dem Handshake zurueckgenommen und der Terminal-Grant wieder als erste fachliche Nachricht ueber die WebSocket-Verbindung uebertragen.

## Ausloeser

Die bisherige Richtung aus Plan 02 und den nachgezogenen `spec`-Artefakten setzt fuer die Browser-Autorisierung auf einen benutzerdefinierten Header im WebSocket-Handshake. Vor dem ersten Integrationstest wurde klar, dass Browser-WebSocket-APIs diesen Pfad praktisch nicht bereitstellen. Damit ist die aktuelle Autorisierungsentscheidung ein Integrationsblocker und muss vor weiteren Browser-Tests korrigiert werden.

## Abhaengigkeit

Plan 05 ist reviewed/abgenommen. Plan 06 ist eine gezielte Folgearbeit auf dem bereits umgesetzten Gateway-Stand und korrigiert die unbrauchbare Browser-Autorisierungsentscheidung aus Plan 02.

## Eingaben und Referenzen

* `plans/02-browser-websocket-und-sitzungssteuerung.md`
* `spec/docs/architecture/servicechannel-concept.md`
* `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
* `spec/openapi/06-backend-gateway-terminal-grant.openapi.yaml`
* `spec/models/events/04-browser-gateway-message-types.md`
* `spec/models/states/04-browser-gateway-session-state.md`
* `spec/schemas/gateway/04-browser-gateway-protocol-catalog.md`
* `spec/schemas/backend/06-backend-gateway-terminal-grant-catalog.md`
* `spec/implementation/05-browser-terminal-gateway-status.md`

## Fachliche Leitplanken

* Browser-Reconnect erzeugt weiterhin immer eine neue Gateway-Sitzung.
* Derselbe Grant darf weiterhin nur innerhalb des vom Backend erlaubten Grace-Windows erneut validiert werden.
* Vor erfolgreicher Autorisierung darf keine SSH-Verbindung zur Konsole aufgebaut werden.
* Vor erfolgreicher Autorisierung duerfen keine fachlichen Browser-Nachrichten ausser dem Autorisierungsschritt wirksam werden.
* Ungueltige, fehlende, doppelte oder verspaetete Autorisierung muss Browser- und Gateway-Seite sauber schliessen.
* Es sollen keine Browser-untypischen Workarounds wie benutzerdefinierte Handshake-Header als Pflichtpfad bestehen bleiben.

## Arbeitspakete

### 1. Handshake- und Protokollmodell zurueckbauen

* `GET /gateway/terminal` wieder ohne verpflichtenden Grant-Header akzeptieren
* `authorize` als initiale Browser-zu-Gateway-Nachricht wieder einfuehren
* festlegen, ob und in welcher Minimalform eine positive Bestaetigung `authorized` zurueckgegeben wird
* klar definieren, welche Nachrichten vor erfolgreicher Autorisierung erlaubt, ignoriert oder als Protokollfehler behandelt werden

### 2. Vorautorisierte Sitzungsphase sauber modellieren

* Session-Lifecycle um einen expliziten Vorautorisierungszustand erweitern
* Grant erst nach Eingang der Autorisierungsnachricht gegen das Backend validieren
* SSH-Aufbau, PTY-Initialisierung und weiterfuehrende Session-Hooks strikt hinter die erfolgreiche Autorisierung ziehen
* die bestehende zentrale Session-Verwaltung fuer unautorisierte Kurzzeitsitzungen weiterverwenden statt Parallelpfade einzufuehren

### 3. Fehler-, Timeout- und Cleanup-Pfade fuer die Autorisierung schaerfen

* definieren, wie das Gateway auf fehlende Autorisierung nach erfolgreichem Upgrade reagiert
* Protokollfehler fuer unbekannte oder zu fruehe Nachrichten in der Vorautorisierungsphase explizit abbilden
* doppelte `authorize`-Nachrichten, ungueltige Grants und Backend-Fehler sauber klassifizieren
* Browser-, Session- und ggf. bereits gestartete Hintergrundpfade deterministisch bereinigen

### 4. Tests und Integrationspfad auf Browser-Realitaet umstellen

* Handshake-Tests von Grant-Header auf nachgelagerte Autorisierungsnachricht umstellen
* Parser-/Protokolltests fuer `authorize` und ggf. `authorized` wieder aktivieren oder neu aufsetzen
* E2E-Pfad so anpassen, dass ein Browser-aehnlicher WebSocket-Client ohne benutzerdefinierte Header testbar ist
* den ersten echten Integrationstest explizit gegen den korrigierten Browser-Pfad vorbereiten

### 5. Konfiguration und Doku bereinigen

* Header-spezifische Konfiguration, Dokumentation und Hinweise auf obsolete Pfade pruefen
* Repo-README, Planhistorie und ggf. Deploy-Hinweise nur dort anpassen, wo die Autorisierungsentscheidung sichtbar beschrieben ist
* vermeiden, dass veraltete Header-Namen oder Testannahmen als aktive Wahrheit stehen bleiben

### 6. Gemeinsame Spezifikation synchron nachziehen

* OpenAPI-Draft, Nachrichtenkatalog, Sitzungszustand und Backend-Grant-Katalog wieder auf Nachricht-basierte Autorisierung ausrichten
* Komponentenuebergreifenden Status in `spec/implementation/05-browser-terminal-gateway-status.md` aktualisieren
* nach Umsetzung und Spezifikationsnachzug erneut am Review-Gate anhalten

## Erwartete Dateien oder Bereiche

* `internal/httpserver/...`
* `internal/websocket/...`
* `internal/session/...`
* `tests/...`
* `tests/e2e/...`
* `README.md`
* `plans/README.md`
* `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
* `spec/openapi/06-backend-gateway-terminal-grant.openapi.yaml`
* `spec/models/events/04-browser-gateway-message-types.md`
* `spec/models/states/04-browser-gateway-session-state.md`
* `spec/schemas/gateway/04-browser-gateway-protocol-catalog.md`
* `spec/schemas/backend/06-backend-gateway-terminal-grant-catalog.md`
* `spec/implementation/05-browser-terminal-gateway-status.md`

## Validierung

* `go test ./...`
* `go build ./...`
* `make test-e2e`
* gezielter Browser-kompatibler Integrationslauf ohne benutzerdefinierte WebSocket-Header

## Exit-Kriterien

* Ein Browser kann die WebSocket-Verbindung ohne benutzerdefinierten Auth-Header oeffnen.
* Der Terminal-Grant wird als Protokollnachricht uebertragen und erst dann validiert.
* Vor erfolgreicher Autorisierung wird keine SSH-Sitzung aufgebaut.
* Fehler- und Timeout-Verhalten der Vorautorisierungsphase sind testbar und dokumentiert.
* Repo-Planung und gemeinsame Spezifikation sind wieder synchron.

## Fortschrittspflege

Bei Umsetzung dieses Plans nachziehen:

* finale Form der `authorize`-Nachricht: JSON-Textnachricht `{"type":"authorize","token":"..."}` als zwingend erste Client-Nachricht
* `authorized` bleibt als explizite Erfolgsnachricht ohne zusaetzliche Terminal-Metadaten erhalten
* Vorautorisierungs-Timeout verwendet aktuell denselben Wert wie `GATEWAY_SESSION_IDLE_TIMEOUT`
* betroffene Konfigurations- und Dokumentationsstellen wurden in README, Repo-Plaenen und den Browser-/Grant-Artefakten im `spec`-Submodul nachgezogen

## Offene Punkte

* Ob fuer die Vorautorisierungsphase spaeter ein eigener kuerzerer Timeout statt Wiederverwendung von `GATEWAY_SESSION_IDLE_TIMEOUT` eingefuehrt werden soll
* Ob das Session-Limit langfristig auch unautorisierte Kurzzeitsitzungen mitzaehlen soll

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Rueckmeldungen aus dem ersten echten Browser-Integrationslauf gesammelt in Plan 07 weiterfuehren.
2. Idle-, Keepalive- und Session-Endgrund-Semantik nicht mehr hier, sondern im Folgeplan `plans/07-idle-keepalive-und-session-endgruende.md` nachziehen.
3. Danach erneut am Review-Gate des Folgeplans orientieren.
