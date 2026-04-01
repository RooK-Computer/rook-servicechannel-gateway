# Plan 02 - Browser-WebSocket und Sitzungssteuerung

Status: Umgesetzt, wartet auf Review

Zuletzt aktualisiert: 2026-04-01

## Ziel

Die Browser-seitige Echtzeitverbindung soll fachlich korrekt angenommen und als neue Gateway-Sitzung orchestriert werden. Dieser Plan deckt Handshake, Protokollrahmen, Session-Lifecycle und Reconnect-Verhalten ab.

## Abhaengigkeit

Plan 01 ist umgesetzt; die Umsetzung von Plan 02 ist auf dieser Basis erfolgt und wartet jetzt auf Review.

## Eingaben und Referenzen

* `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
* Ergebnisse und Interfaces aus Plan 01
* Architekturfluss aus `spec/docs/architecture/servicechannel-concept.md`

## Fachliche Leitplanken

* Grant kommt im Header des WebSocket-Handshakes
* Das Gateway validiert den Grant vor dem Konsolenaufbau
* Reconnect bedeutet immer neue Gateway-Sitzung
* Dasselbe Token kann nur innerhalb des backendseitigen Grace-Windows erneut funktionieren
* Browser und Konsole werden bei Revocation, Timeout oder Fehler sauber getrennt

## Arbeitspakete

### 1. WebSocket-Handshake produktiv machen

* Route `GET /gateway/terminal` auf echtes Upgrade umstellen
* Grant-Header extrahieren und auf Mindestanforderungen pruefen
* Grant gegen Backend validieren, bevor eine Sitzung aufgebaut wird
* Fehlerhafte Handshakes in ein konsistentes JSON-Fehlerformat ueberfuehren, soweit vor dem Upgrade noch HTTP moeglich ist

### 2. Gateway-Sitzungsmodell einziehen

Eine Sitzung braucht mindestens:

* technische Session-ID
* Browser-Connection-State
* Validierungsergebnis inklusive Ziel-IP
* Zeitpunkte fuer Start, letzte Aktivitaet und geordnetes Ende
* Grund fuer Session-Ende

Dieses Modell soll zentral gepflegt werden, nicht verteilt in Handlern.

### 3. Nachrichtenmodell umsetzen

Mindestens diese Nachrichtentypen gemaess aktualisiertem Draft beruecksichtigen:

* `input`
* `output`
* `resize`
* `error`
* `close`

Festlegung fuer die Umsetzung:

* Autorisierung ausschliesslich ueber den Handshake-Header, keine aktiven Laufzeitnachrichten `authorize` oder `authorized`
* Kontrollnachrichten als Text-Frames
* Terminaldaten optional auch als Binary-Frames
* strikte Validierung unbekannter oder unvollstaendiger Kontrollnachrichten

### 4. Read-/Write-Loops und Backpressure

* getrennte Lese- und Schreibroutinen
* begrenzte Queues pro Sitzung
* definierte Reaktion bei langsamem Browser-Client
* klare Besitzverhaeltnisse fuer das Schliessen der Verbindung

### 5. Reconnect- und Grace-Verhalten

* Browser-Abbruch fuehrt lokal zum Sitzungsende
* erneuter Browser-Aufbau nutzt immer eine neue Gateway-Sitzung
* derselbe Grant wird nur erneut verwendet, wenn das Backend ihn im Grace-Window nochmals akzeptiert
* keine clientseitige Wiederanbindung an alte Session-Objekte

### 6. Session-Cleanup

* alle Goroutinen sauber beenden
* Sitzung aus Registern entfernen
* Abschlussgrund protokollieren
* Folgeplan einen stabilen Hook fuer SSH-Abbau bereitstellen

## Erwartete Dateien oder Bereiche

* `internal/websocket/...`
* `internal/session/...`
* Erweiterungen in `internal/httpserver/...`
* Protokolltests unter `tests/...` oder paketnah

## Validierung

* Handshake-Tests fuer fehlenden, gueltigen und ungueltigen Grant
* Protokolltests fuer erlaubte und unerlaubte Nachrichtentypen
* Tests fuer Doppel-Schliessen, Browser-Abbruch und Queue-Ueberlauf
* manueller Test mit einfachem WebSocket-Client gegen Mock-Backend

## Exit-Kriterien

* Browser kann eine neue Gateway-Sitzung aufbauen
* Sitzungen haben klaren Lifecycle
* Handshake und Nachrichtenmodell sind testbar und dokumentiert
* Folgeplan kann die SSH-Bridge an klar definierte Session-Hooks anbinden

## Fortschrittspflege

Bei Umsetzung dieses Plans nachgezogen:

* finales Fehlerformat vor dem Upgrade: HTTP-JSON mit `code` und `message`
* Laufzeit-Protokollfehler nach dem Upgrade: `error`-Nachricht gefolgt von `close` und WebSocket-Close
* finale Session-Statusfelder im Code: Session-ID, Browser-State, Grant-Ergebnis inklusive Ziel-IP, Start-/Aktivitaets-/Endzeit und Abschlussgrund
* initiale Queue-Groesse: 16 Nachrichten pro Sitzung
* beschlossene Protokollaenderung: Header-Handshake ist alleinige Autorisierung, `authorize`/`authorized` sind entfernt

## Offene Punkte

* Welche WebSocket-Close-Codes final langfristig stabil bleiben sollen
* Wie spaeter Heartbeats/Pings explizit modelliert werden muessen
* Wie Plan 03 den Uebergang von Browser-Sitzung zu echter SSH-Bridge ohne zusaetzliche Initialnachricht am saubersten andockt

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Status in `plans/README.md` und in diesem Dokument aktualisieren.
2. Die konzeptionelle Protokollaenderung auch im `spec`-Submodul nachziehen, damit Backend- und Gateway-Team dieselben Annahmen nutzen.
3. Danach stoppen und Review abwarten.
