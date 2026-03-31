# Plan 02 - Browser-WebSocket und Sitzungssteuerung

Status: Entwurf fuer Review

Zuletzt aktualisiert: initial angelegt

## Ziel

Die Browser-seitige Echtzeitverbindung soll fachlich korrekt angenommen und als neue Gateway-Sitzung orchestriert werden. Dieser Plan deckt Handshake, Protokollrahmen, Session-Lifecycle und Reconnect-Verhalten ab.

## Abhaengigkeit

Plan 01 muss abgeschlossen und reviewt sein.

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

Mindestens diese Nachrichtentypen gemaess Draft beruecksichtigen:

* `authorize`
* `authorized`
* `input`
* `output`
* `resize`
* `error`
* `close`

Festlegung fuer die Umsetzung:

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

Bei Umsetzung dieses Plans laufend nachziehen:

* final gewaehltes Fehlerformat fuer Handshake und Laufzeit
* finale Session-Statusfelder
* beschlossene Queue-Groessen oder Zeitlimits

## Offene Punkte

* Welche WebSocket-Close-Codes final verwendet werden sollen
* Ob `authorize` und `authorized` trotz Header-basiertem Grant als explizite Kontrollnachrichten benoetigt bleiben
* Ob spaeter Heartbeats/Pings explizit modelliert werden muessen

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Status aktualisieren.
2. Das tatsaechliche Nachrichtenmodell im Plan festschreiben.
3. Dann stoppen und Review abwarten.
