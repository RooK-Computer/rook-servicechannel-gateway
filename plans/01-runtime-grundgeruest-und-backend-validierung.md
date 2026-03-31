# Plan 01 - Runtime-Grundgeruest und Backend-Validierung

Status: Entwurf fuer Review

Zuletzt aktualisiert: initial angelegt

## Ziel

Ein minimal lauffaehiger Gateway-Dienst soll entstehen, der Konfiguration laden, HTTP-Anfragen annehmen, WebSocket-Upgrades vorbereiten und Terminal-Grants online gegen das Backend validieren kann. Dieser Plan schafft die tragende Runtime-Basis fuer alle Folgeplaene.

## Warum dieser Plan zuerst kommt

Ohne belastbares Grundgeruest bleiben spaetere WebSocket-, SSH- und Cleanup-Themen unstrukturiert. Ausserdem haengt die gesamte Gateway-Funktion an der Backend-Validierung des Grants.

## Eingaben und Referenzen

* `spec/docs/architecture/servicechannel-concept.md`
* `spec/implementation/05-browser-terminal-gateway-status.md`
* `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
* `spec/openapi/06-backend-gateway-terminal-grant.openapi.yaml`
* Backend-Status in `spec/implementation/04-rook-backend-status.md`

## Zielbild fuer Plan 01

Der Dienst startet lokal reproduzierbar, lauscht auf einem konfigurierbaren HTTP-Port, stellt Health- und Readiness-Endpunkte bereit, kapselt die Backend-Kommunikation fuer `validateToken` und trennt Konfiguration, Transport und Fachlogik sauber.

## Technische Entscheidungen

* Sprache: Go
* HTTP-Server: Standardbibliothek plus schlanker Router, nur wenn spaeter wirklich noetig
* Konfiguration per Umgebungsvariablen und optionaler lokaler Datei fuer Entwicklung
* Strukturierte Logs ab Start, damit spaetere Sitzungs- und Fehlerpfade nachvollziehbar bleiben
* Kein direkter Code fuer SSH oder Terminal-Protokoll in diesem Plan ausser klaren Interfaces

## Arbeitspakete

### 1. Projektbootstrap

* `go.mod` anlegen
* Einstiegspunkt unter `cmd/gateway/main.go`
* interne Pakete fuer `config`, `httpserver`, `grants`, `shutdown`
* Makefile oder schlanke Task-Kommandos nur fuer bereits benoetigte Ablaufe (`go test`, `go build`)

### 2. Konfigurationsmodell festziehen

Mindestens diese Konfigurationswerte vorsehen:

* Listen-Adresse des HTTP-Servers
* Backend-Basis-URL
* Timeout fuer Backend-Validierung
* Header-Name fuer den Grant, initial `X-Rook-Terminal-Grant`
* Log-Level
* Pfade fuer lokale Secrets und spaetere SSH-Konfiguration

Fehlerhafte oder unvollstaendige Konfiguration soll den Start klar fehlschlagen lassen.

### 3. HTTP-Runtime aufsetzen

* `/healthz` fuer Prozess-Liveness
* `/readyz` fuer Grundbereitschaft inklusive Konfigurationscheck
* Platzhalter-Route fuer `GET /gateway/terminal`, die im ersten Schritt nur Upgrade-Voraussetzungen prueft
* Graceful Shutdown mit Context-Abbruch und sauberem Server-Stopp

### 4. Backend-Grant-Client implementieren

* HTTP-Client fuer `POST /api/gateway/1/validateToken`
* Request- und Response-Modelle aus `spec/openapi/06-backend-gateway-terminal-grant.openapi.yaml` ableiten
* Fehlerpfade klar unterscheiden:
  * Netzwerk-/Timeout-Fehler
  * formale Backend-Fehler
  * fachlich ungueltiger Grant
* Rueckgabemodell mindestens mit `ipAddress` und dem rohen Validierungsergebnis fuer Folgeplaene aufbauen

### 5. Schnittstellen fuer Folgeplaene vorbereiten

Die folgenden Interfaces sollen nach Plan 01 existieren, auch wenn die echte Implementierung spaeter kommt:

* Session-Manager
* WebSocket-Verbindung
* SSH-Bridge
* Audit-Sink

Ziel ist Entkopplung, nicht Scheinfunktionalitaet.

## Erwartete Dateien oder Bereiche

* `go.mod`
* `cmd/gateway/main.go`
* `internal/config/...`
* `internal/httpserver/...`
* `internal/grants/...`
* `internal/shutdown/...`
* erste Tests unter `internal/...` oder `tests/...`

## Validierung

* `go test ./...`
* lokaler Start mit absichtlich unvollstaendiger Konfiguration muss nachvollziehbar fehlschlagen
* lokaler Start mit gueltiger Basiskonfiguration muss Health-Endpoints bereitstellen
* Grant-Client muss gegen Mock-Backend positive und negative Faelle pruefen

## Exit-Kriterien

* Gateway startet reproduzierbar
* Konfiguration ist explizit und dokumentiert
* Health/Readiness existieren
* Backend-Grant-Validierung ist mit Tests abgesichert
* Folgeplaene koennen auf stabile Interfaces statt auf Ad-hoc-Code aufbauen

## Fortschrittspflege

Bei Umsetzung dieses Plans laufend nachziehen:

* welche Dateien angelegt wurden
* welche Konfigurationswerte final benannt wurden
* welche Fehlercodes oder HTTP-Status vom Backend tatsaechlich beobachtet wurden

## Offene Punkte

* Ob fuer den HTTP-Teil ausschliesslich die Standardbibliothek reicht oder spaeter doch ein Router gebraucht wird
* Ob das finale Fehlerformat des Backends noch erweitert wird
* Wie eng Readiness bereits an externe Abhaengigkeiten gekoppelt werden soll

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Status in `plans/README.md` und in diesem Dokument aktualisieren.
2. Falls noetig `spec/implementation/05-browser-terminal-gateway-status.md` verdichtet nachziehen.
3. Danach stoppen und Review abwarten.
