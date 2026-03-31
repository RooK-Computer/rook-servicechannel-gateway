# Plan 03 - SSH-Bridge und Terminaldatenpfad

Status: Entwurf fuer Review

Zuletzt aktualisiert: initial angelegt

## Ziel

Nach erfolgreicher Browser- und Session-Orchestrierung soll das Gateway die echte Shell-Verbindung zur Konsole aufbauen und den Datenstrom zwischen Browser-Terminal und PTY der Konsole robust weiterleiten.

## Abhaengigkeit

Plan 02 muss abgeschlossen und reviewt sein.

## Eingaben und Referenzen

* `spec/openapi/05-gateway-console-ssh.openapi.yaml`
* Ziel-IP aus der Backend-Grant-Validierung
* lokaler Schluessel unter `secrets/gateway_ssh_ed25519(.pub)`

## Fachliche Leitplanken

* Verbindung ausschliesslich serverseitig ueber SSH im VPN
* Login initial als Account `pi`
* PTY mindestens mit `TERM=xterm-256color`
* Rows und Columns werden vom Browser uebernommen
* Resizes muessen an die Konsole propagiert werden

## Arbeitspakete

### 1. SSH-Konfigurationsmodell festziehen

Mindestens diese Parameter brauchen Konfiguration:

* Pfad zum privaten Schluessel
* SSH-Username
* Connect-Timeout
* optionaler Known-Hosts-Pfad
* Verhalten bei Host-Key-Pruefung

Sichere Default-Richtung:

* Host-Key-Pruefung standardmaessig aktiv
* ein expliziter unsicherer Dev-Modus nur per opt-in und klar markiert

### 2. SSH-Client und Session-Aufbau

* SSH-Client aus Go-Bibliothek aufsetzen
* Verbindung zur vom Backend gelieferten VPN-IP aufbauen
* Shell-Session mit PTY anfordern
* UTF-8-faehige Locale, soweit auf der Gegenseite verfuegbar, beruecksichtigen

### 3. Browser-zu-PTY-Weiterleitung

* `input` vom Browser an STDIN der SSH-Session leiten
* `output` von SSH an Browser rueckfuehren
* Binary- und Text-Frames sauber behandeln
* Session-Abbruch bei I/O-Fehlern klar klassifizieren

### 4. Resize-Propagation

* `resize`-Nachrichten validieren
* PTY-Groesse nur mit plausiblen Werten aendern
* Fehler bei Resize nicht verschlucken, sondern als kontrollierten Sitzungsfehler behandeln oder explizit an den Browser melden

### 5. SSH- und Browser-Cleanup koppeln

* Browser-Ende schliesst SSH-Session
* SSH-Ende schliesst Browser-Verbindung mit nachvollziehbarem Grund
* keine haengenden Reader/Writer/Goroutinen

### 6. Audit- und Diagnosepunkte vorbereiten

Mindestens vorbereiten:

* Session-ID
* PIN oder Session-Referenz, soweit vom Backend verfuegbar
* Mitarbeiterkonto, sobald vom Backend oder Handshake verfuegbar
* Ziel-IP
* Endegrund

Falls die Daten im aktuellen Grant-Response noch fehlen, muessen Platzhalter-Interfaces vorgesehen werden statt Ad-hoc-Logging.

## Umgang mit den lokalen SSH-Secrets

Die aktuell lokal erzeugten Dateien sind:

* `secrets/gateway_ssh_ed25519`
* `secrets/gateway_ssh_ed25519.pub`

Vorgehen fuer spaetere Umsetzung:

1. Public Key auf den Konsolen fuer den vorgesehenen Account autorisieren.
2. Private Key vor echter Inbetriebnahme in einen externen Secret-Store uebernehmen.
3. Lokale Entwicklungsumgebung nur ueber bewusst bereitgestellte Secret-Dateien betreiben.
4. Niemals Schluesselmaterial in Beispielkonfigurationen oder Tests einbetten.

## Erwartete Dateien oder Bereiche

* `internal/sshbridge/...`
* Erweiterungen in `internal/session/...`
* ggf. `internal/audit/...`
* Integrationstests unter `tests/...`

## Validierung

* Tests fuer SSH-Konfigurationsfehler
* Tests fuer Resize-Validierung
* Integrationstest gegen lokale Test-SSHD-Instanz oder Container
* manueller End-to-End-Test: WebSocket -> Gateway -> SSH -> Shell-Ausgabe

## Exit-Kriterien

* Gateway kann ueber VPN-IP eine Shell oeffnen
* Browser und Konsole tauschen interaktive Terminaldaten aus
* Resize funktioniert
* Fehler und Cleanup sind fuer beide Seiten sauber gekoppelt

## Fortschrittspflege

Bei Umsetzung dieses Plans laufend nachziehen:

* tatsaechliche Known-Hosts-Strategie
* finale SSH-Library und Schluessel-Ladepfade
* beobachtete Fehlerbilder beim PTY-Aufbau

## Offene Punkte

* Ob das Backend zusaetzliche Audit-Felder im Grant-Response liefern wird
* Wie Host-Key-Verteilung fuer Konsolen praktisch organisiert wird
* Ob spaeter mehrere Ziel-Accounts noetig werden

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Secret-Handling und Host-Key-Strategie im Plan konkret nachziehen.
2. Status aktualisieren.
3. Dann stoppen und Review abwarten.
