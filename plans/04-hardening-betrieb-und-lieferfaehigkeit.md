# Plan 04 - Hardening, Betrieb und Lieferfaehigkeit

Status: Entwurf fuer Review

Zuletzt aktualisiert: initial angelegt

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

* Mapping zwischen Backend-Fehlern, WebSocket-Fehlern und SSH-Fehlern festziehen
* finale Close-Codes dokumentieren
* klare Trennung zwischen Benutzerfehler, Infrastrukturfehler und Backend-Nichtverfuegbarkeit

### 2. Zeitlimits und Ressourcenhaertung

* Timeouts fuer Handshake, Backend-Validierung, SSH-Connect und Inaktivitaet
* Limits fuer parallele Sitzungen
* Limits fuer Nachrichtengroessen und Queue-Tiefen
* Schutz vor haengenden Sessions bei Browser- oder Netzabbruechen

### 3. Audit und Observability

* strukturierte Session-Logs
* Metriken oder mindestens zaehlbare Betriebsereignisse
* Korrelation ueber Session-ID
* vorbereitete Audit-Felder gemaess Gesamtarchitektur:
  * PIN
  * Mitarbeiteraccount
  * Ziel-IP
  * Start/Ende
  * Endegrund

### 4. Secret- und Deployment-Pfad

* klares Laden des privaten SSH-Schluessels aus Dateisystem oder Secret-Mount
* kein Fallback auf eingebettete Defaults
* dokumentierter Prozess zur Auslagerung des jetzt lokal erzeugten Schluessels in externen Secret-Store
* Beispielkonfiguration ohne echte Geheimnisse

### 5. Betriebsartefakte

Je nach gewaehlter Zielumgebung mindestens vorbereiten:

* `systemd`-Unit oder Container-Startkommando
* Beispiel-Environment-Datei ohne Geheimnisse
* Start-/Stop-/Health-Dokumentation
* Runbook fuer typische Fehlerfaelle

### 6. Test- und Freigabepfad

* vollstaendige Test-Suite fuer Unit- und Integrationstests
* reproduzierbarer lokaler End-to-End-Test mit Mock-Backend und Test-SSHD
* Entscheidung festziehen, ob und wie CI in diesem Repo aufgebaut wird

## Erwartete Dateien oder Bereiche

* `internal/audit/...`
* `tests/e2e/...`
* Deployment-Artefakte passend zur Zielplattform
* Betriebsdokumentation im Repo

## Validierung

* `go test ./...`
* End-to-End-Test ueber kompletten Pfad Browser -> Gateway -> Konsole
* absichtliche Negativtests fuer Backend-Ausfall, SSH-Ausfall, abgelaufenen Grant und Browser-Abbruch
* manueller Restart-Test mit sauberem Recovery

## Exit-Kriterien

* Gateway ist nicht nur funktional, sondern betrieblich nachvollziehbar
* Secret-Handling ist fuer echte Nutzung vorbereitet
* Freigabepfad ist reproduzierbar
* Betriebsgrenzen und Fehlerbilder sind dokumentiert

## Fortschrittspflege

Bei Umsetzung dieses Plans laufend nachziehen:

* finale Timeouts und Limits
* tatsaechliche Deployment-Form
* getroffene Entscheidungen zu Metriken, Audit und CI

## Offene Punkte

* Ob zusaetzliche Rate-Limits oder Auth-Layer vor dem Gateway noetig werden
* Ob Betriebsmetriken direkt durch das Deployment-Umfeld gesammelt werden
* Wie stark der Gateway fuer mehrere gleichzeitige Team-Sitzungen dimensioniert werden muss

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Alle Plan- und Statusdokumente synchronisieren.
2. Offene Restpunkte fuer Betrieb oder Sicherheit als eigene Folgearbeit ausweisen.
3. Danach wieder auf Review warten statt stillschweigend neue Arbeitspakete zu eroeffnen.
