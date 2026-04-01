# Plan 04 - Hardening, Betrieb und Lieferfaehigkeit

Status: Umgesetzt, wartet auf Review

Zuletzt aktualisiert: 2026-04-01

## Ziel

Die bereits funktionale Gateway-Komponente soll fuer Dauerbetrieb, Diagnose, saubere Lieferung und kontrollierte Sicherheitsprozesse gehaertet werden.

## Abhaengigkeit

Plan 03 muss abgeschlossen und reviewt sein.

## Schwerpunkte

* Fehler- und Close-Code-Semantik finalisieren
* Observability und Auditierung vervollstaendigen
* Secret-Handling und Betriebsgrenzen absichern
* Build-, Test- und Auslieferungspfade reproduzierbar machen

## Arbeitspakete

### 1. Fehler- und Beendigungsmodell finalisieren

Umgesetzt:

* Mapping zwischen Backend-Fehlern, WebSocket-Fehlern und SSH-Fehlern vereinheitlicht
* Session-Endgruende um `idle_timeout` und `session_limit_reached` erweitert
* konsistente WebSocket-Fehler-/Close-Pfade fuer:
  * Protokollverletzungen
  * SSH-Lese-/Schreibfehler
  * Browser-Disconnect
  * Inaktivitaets-Timeout
  * Queue-Ueberlauf
  * Server-Shutdown
* Session-Limit-Ueberschreitung liefert jetzt einen expliziten WebSocket-Fehler (`session_limit_reached`) statt eines stillen Scheiterns

### 2. Zeitlimits und Ressourcenhaertung

Umgesetzt:

* neue Konfiguration fuer:
  * `GATEWAY_HTTP_READ_HEADER_TIMEOUT`
  * `GATEWAY_SESSION_IDLE_TIMEOUT`
  * `GATEWAY_SESSION_MAX_CONCURRENT`
  * `GATEWAY_SESSION_OUTBOUND_QUEUE_DEPTH`
  * `GATEWAY_WEBSOCKET_MAX_MESSAGE_BYTES`
* WebSocket-Read-Limit wird direkt am Gorilla-Connection-Objekt gesetzt
* Session-Registry erzwingt maximale Parallelitaet
* Idle-Watchdog beendet haengende Sitzungen deterministisch
* Queue-Tiefe ist konfigurierbar statt fest verdrahtet

### 3. Audit und Observability

Umgesetzt:

* konkreter `internal/audit.LoggerSink`
* strukturierte Session-Start-/End-Logs mit Session-ID, Ziel-IP, SSH-Account, Dauer und Endegrund
* Audit-Ereignisse sind als zaehlbare strukturierte Logs verfuegbar
* optionale Audit-Felder aus dem Backend-Validierungspayload werden vorbereitet extrahiert:
  * `pin`
  * `mitarbeiteraccount`
* Entscheidung fuer diesen Plan:
  * keine separate Metrik- oder Prometheus-Schnittstelle
  * countbare strukturierte Ereignislogs reichen fuer Plan 04 als Mindest-Observability

### 4. Secret- und Deployment-Pfad

Umgesetzt:

* Beispiel-Environment-Datei ohne Geheimnisse unter `deploy/systemd/gateway.env.example`
* dokumentierter Secret-Mount-Pfad fuer echte Deployments
* README dokumentiert jetzt explizit, dass die SSH-Schluessel in einen externen Secret-Store bzw. Secret-Mount gehoeren

Hinweis:

* der bestehende Default-Pfad fuer lokale Entwicklung (`secrets/...`) bleibt fuer das Repo-MVP bestehen
* die Betriebsdokumentation zieht den produktiven Pfad bewusst auf einen Secret-Mount um

### 5. Betriebsartefakte

Umgesetzt:

* primaerer Lieferpfad fuer Plan 04 ist `systemd`
* Unit-Datei unter `deploy/systemd/rook-servicechannel-gateway.service`
* README enthaelt jetzt:
  * Verify-/Start-Hinweise
  * Health-Checks
  * typische Fehlerindikatoren
  * lokalen E2E-Testpfad

### 6. Test- und Freigabepfad

Umgesetzt:

* neues Testpaket unter `tests/e2e/`
* reproduzierbarer lokaler End-to-End-Pfad mit:
  * echtem Grant-HTTP-Client gegen Mock-Backend
  * echtem SSH-Client gegen Test-SSHD
  * WebSocket-Client als Browser-Ersatz
* Negativtests fuer:
  * Backend-Ausfall
  * SSH-Ausfall
  * Inaktivitaets-Timeout
* `Makefile` um `test-e2e` und `verify` erweitert
* CI-Entscheidung fuer diesen Plan:
  * noch keine CI-Pipeline im Repo
  * `make verify` ist der verbindliche lokale Freigabepfad bis eine spaetere CI-Einbindung explizit priorisiert wird

## Erwartete Dateien oder Bereiche

* `internal/audit/...`
* `tests/e2e/...`
* Deployment-Artefakte passend zur Zielplattform
* Betriebsdokumentation im Repo

## Validierung

* `go test ./...`
* `go build ./...`
* `make test-e2e`

## Exit-Kriterien

* Gateway ist nicht nur funktional, sondern betrieblich nachvollziehbar
* Secret-Handling ist fuer echte Nutzung vorbereitet
* Freigabepfad ist reproduzierbar
* Betriebsgrenzen und Fehlerbilder sind dokumentiert

## Fortschrittspflege

Stand nach Umsetzung:

* finale Timeouts und Limits:
  * Header-Read-Timeout konfigurierbar
  * Session-Idle-Timeout konfigurierbar
  * Maximalzahl paralleler Sessions konfigurierbar
  * WebSocket-Message-Groesse konfigurierbar
  * Queue-Tiefe pro Session konfigurierbar
* tatsaechliche Deployment-Form:
  * `systemd`
* Entscheidung zu Metriken, Audit und CI:
  * Audit ueber strukturierte Logger-Sink-Ereignisse
  * keine separate Metrik-Schnittstelle in diesem Plan
  * keine CI-Pipeline in diesem Plan; `make verify` ist der lokale Freigabepfad

## Offene Punkte

* Host-Key-Verifikation ist weiterhin bewusst nicht gehaertet; dieser bekannte MVP-Kompromiss bleibt als Folgearbeit offen
* Ob zusaetzliche Rate-Limits oder Auth-Layer vor dem Gateway noetig werden
* Ob spaeter eine separate Metrik-Schnittstelle noetig wird
* Wie stark der Gateway fuer mehrere gleichzeitige Team-Sitzungen dimensioniert werden muss

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Review von Plan 04 durchfuehren.
2. Rueckmeldungen in Code, README, Plan und `spec`-Statusdatei nachziehen.
3. Danach wieder anhalten statt stillschweigend neue Arbeitspakete zu eroeffnen.
