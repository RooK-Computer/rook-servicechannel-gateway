# Plan 07 - Idle, Keepalive und Session-Endgruende

Status: Umgesetzt, wartet auf Review

Zuletzt aktualisiert: 2026-04-02

## Ziel

Die Browser-Terminal-Laufzeit soll so nachgeschaerft werden, dass eine autorisierte Browser-Sitzung nicht mehr allein wegen fehlender Benutzereingaben endet und echte Transport- oder Infrastrukturabbrueche sauber von fachlicher Bedieninaktivitaet getrennt werden.

## Ausloeser

Der interaktive Integrationstest hat laut `spec/implementation/11-integrationsbefunde-und-folgearbeiten.md` fuer das Gateway vor allem zwei Folgearbeiten sichtbar gemacht:

* **Befund 4**: Das Browser-Terminal laeuft bei laengerer Inaktivitaet in einen Timeout.
* **Befund 5**: Der Gateway wirkt so, als beende er die Verbindung bei laengerer Nichtbenutzung, obwohl das fuer den Support-Fall unerwuenscht ist.

Die nachgezogene Spezifikationsrichtung ist bereits klarer als die aktuelle Runtime:

* fehlende Terminaleingaben oder ausbleibende Resize-Ereignisse beenden eine autorisierte Browser-Terminal-Sitzung nicht automatisch,
* Browser, Gateway und Zwischeninfrastruktur duerfen Keepalive-Mechanismen nutzen, um reine Transport-Idle-Abbrueche zu vermeiden,
* ein echter Browser-Disconnect beendet die Browser-Terminal-Sitzung, aber nicht automatisch die uebergeordnete Support-Sitzung.

## Abhaengigkeit

Plan 06 ist reviewed/abgenommen. Plan 07 ist die naechste gezielte Folgearbeit auf Basis des ersten echten Browser-Integrationslaufs.

## Eingaben und Referenzen

* `spec/implementation/11-integrationsbefunde-und-folgearbeiten.md`
* `spec/implementation/05-browser-terminal-gateway-status.md`
* `spec/docs/architecture/servicechannel-concept.md`
* `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
* `spec/openapi/06-backend-gateway-terminal-grant.openapi.yaml`
* `internal/httpserver/server.go`
* `internal/session/manager.go`
* `internal/websocket/conn.go`
* `internal/httpserver/server_test.go`
* `internal/session/manager_test.go`
* `tests/e2e/gateway_e2e_test.go`

## Fachliche Leitplanken

* Eine autorisierte Browser-Shell darf nicht allein wegen fehlender Tastatureingaben beendet werden.
* Fehlende `resize`-Ereignisse sind ebenfalls kein legitimer Endgrund fuer eine laufende Browser-Terminal-Sitzung.
* Die kurze Vorautorisierungsphase vor `authorize` darf weiterhin einen eigenen technischen Timeout haben.
* Browser-Disconnect, Infrastruktur-Disconnect und fachliche Session-Endgruende muessen unterscheidbar bleiben.
* Keepalive-/Ping-Mechanismen duerfen genutzt werden, um reine Transport-Idle-Abbrueche zu vermeiden.
* Die konsumierte Backend-Grant-API bleibt fuer diesen Plan bei `POST /api/gateway/1/validateToken`; die Aenderung betrifft primaer Browser-/Gateway-Laufzeitverhalten.
* Befund 6 aus dem Integrationsdokument bleibt fuer dieses Repo eine externe Abhaengigkeit: Er betrifft die uebergeordnete Support-Session und ist nicht mit der Browser-Terminal-Sitzung gleichzusetzen.

## Arbeitspakete

### 1. Idle- und Timeout-Semantik sauber trennen

* aktuellen Einsatz von `GATEWAY_SESSION_IDLE_TIMEOUT` in Vorautorisierungsphase und autorisierter Sitzung entkoppeln
* entscheiden, ob ein bestehender Wert umgedeutet oder in getrennte Konfigurationen aufgeteilt werden muss
* festlegen, welche Laufzeitgrenzen fuer eine autorisierte Sitzung ueberhaupt noch legitim sind

### 2. Keepalive-Strategie fuer Browser und Gateway festziehen

* WebSocket-Ping/Pong oder funktional gleichwertige Keepalive-Mechanismen fuer den Gateway-Pfad ausarbeiten
* technische Keepalive-Aktivitaet klar von fachlicher Benutzeraktivitaet trennen
* Verhalten bei fehlgeschlagenen Keepalives oder echten Disconnects sauber klassifizieren

### 3. Endgruende und Beobachtbarkeit nachschaerfen

* Session-Endgruende so nachziehen, dass Bedieninaktivitaet nicht laenger als direkter Abbruchgrund modelliert bleibt
* Logs und Tests so erweitern, dass echte Abbruchstellen entlang Browser, Proxy, Gateway und SSH besser eingrenzbar sind
* festhalten, wo der Gateway selbst aktiv schliesst und wo er nur fremde Disconnects weiterreicht

### 4. Tests und Integrationspfad anpassen

* bestehende Unit-, Handler- und E2E-Tests identifizieren, die das heutige `idle_timeout`-Verhalten absichern
* neue Tests fuer ruhende, aber weiter offene autorisierte Browser-Sitzungen definieren
* Vorautorisierungs-Timeout weiterhin gezielt testen, getrennt vom Verhalten laufender Sitzungen
* Integrationspfad so nachziehen, dass Keepalive- und Disconnect-Ursachen reproduzierbarer werden

### 5. Planung, Status und Doku synchron halten

* repo-lokale Plaene und `spec/implementation/05-browser-terminal-gateway-status.md` konsistent halten
* README und andere sichtbare Doku nur dort nachziehen, wo aktuelles Idle-Verhalten bereits als beabsichtigt beschrieben ist
* nach Umsetzung und Review wieder am Review-Gate anhalten

## Erwartete Dateien oder Bereiche

* `internal/httpserver/...`
* `internal/session/...`
* `internal/websocket/...`
* `tests/e2e/...`
* `README.md`
* `plans/README.md`
* `spec/implementation/05-browser-terminal-gateway-status.md`
* `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
* ggf. weitere Browser-/Gateway-Protokollartefakte im `spec`-Submodul

## Validierung

* `go test ./...`
* `go build ./...`
* `make test-e2e`
* gezielter Integrationslauf fuer ruhende Browser-Terminals und reproduzierbare Disconnect-/Keepalive-Beobachtung

## Exit-Kriterien

* Autorisierte Browser-Terminal-Sitzungen enden nicht mehr allein wegen fehlender Eingaben oder ausbleibender `resize`-Ereignisse.
* Vorautorisierungs-Timeout und Laufzeit autorisierter Sitzungen sind fachlich und technisch getrennt.
* Keepalive-/Disconnect-Verhalten ist testbar, beobachtbar und in Repo-Planung plus `spec` konsistent beschrieben.
* Die fuer Befund 4 und 5 relevanten Gateway-Folgearbeiten sind umgesetzt und sauber gegen Backend-/Agent-Themen abgegrenzt.

## Fortschrittspflege

Bei Umsetzung dieses Plans nachgezogen:

* aktive Browser-Terminal-Sitzungen werden nicht mehr allein wegen fehlender Eingaben oder ausbleibender `resize`-Ereignisse beendet
* die Vorautorisierungsphase verwendet jetzt `GATEWAY_SESSION_AUTHORIZE_TIMEOUT`; `GATEWAY_SESSION_IDLE_TIMEOUT` bleibt als Legacy-Fallback fuer diesen Wert erhalten
* das Gateway sendet serverseitige WebSocket-Keepalive-Pings im konfigurierbaren Intervall `GATEWAY_WEBSOCKET_KEEPALIVE_INTERVAL`
* ausbleibende Keepalive-Antworten fuehren zu `keepalive_timeout`; fehlende erste Client-Nachricht vor erfolgreicher Autorisierung fuehrt zu `authorize_timeout`
* die Testabdeckung wurde auf getrennten Autorisierungs-Timeout, ruhende aber offene Browser-Sitzung und den aktualisierten E2E-Idle-Fall nachgezogen

## Offene Punkte

* Ob fuer autorisierte Browser-Sitzungen neben Keepalive spaeter noch weitere Laufzeitgrenzen aus Betriebssicht benoetigt werden
* Welche WebSocket-Close-Codes fuer Keepalive-Fehler oder echte Disconnects langfristig stabil bleiben sollen
* Ob aus der Ursachenanalyse noch weiterer Nachzug im Browser-Gateway-Protokoll-Draft noetig wird

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Review von Plan 07 durchfuehren.
2. Rueckmeldungen aus Review und weiteren Integrationslaeufen in Runtime, README, Repo-Plaenen und `spec`-Artefakten konsistent nachziehen.
3. Danach stoppen und erst nach expliziter Freigabe einen weiteren Folgeplan beginnen.
