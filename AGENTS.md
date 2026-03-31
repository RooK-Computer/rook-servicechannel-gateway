# AGENTS.md

Dieses Repository enthaelt die Umsetzungsplanung fuer den Browser-Terminal-Gateway des RooK-Servicechannels.

## Schnellstart fuer frische Agenten

1. Lies zuerst `plans/README.md`.
2. Lies danach den naechsten noch nicht umgesetzten Detailplan unter `plans/`.
3. Ziehe fuer fachlichen Kontext mindestens diese Spezifikationen heran:
   * `spec/docs/architecture/servicechannel-concept.md`
   * `spec/implementation/05-browser-terminal-gateway-status.md`
   * `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
   * `spec/openapi/05-gateway-console-ssh.openapi.yaml`
   * `spec/openapi/06-backend-gateway-terminal-grant.openapi.yaml`
4. Arbeite immer nur den naechsten freigegebenen Plan ab.
5. Wenn ein Plan fachlich abgeschlossen ist: relevante Statusdateien aktualisieren, ausdruecklich auch `spec/implementation/05-browser-terminal-gateway-status.md` nachziehen, den Planstatus pflegen und dann anhalten. Nicht selbststaendig mit dem naechsten Plan weitermachen, bevor ein Mensch reviewt hat.

## Harte Arbeitsregeln

* Die Plaene sind absichtlich sequentiell geschnitten. Keine parallele Umsetzung mehrerer Plaene ohne explizite Freigabe.
* Nach jedem abgeschlossenen Plan ist ein Review-Gate verpflichtend.
* Laufende oder unterbrochene Arbeit muss so dokumentiert werden, dass ein frischer Agent ohne Chat-Historie uebernehmen kann.
* Repo-lokale Plaene und die gemeinsame Statuspflege im `spec`-Submodule muessen synchron bleiben.
* Keine Secrets committen. Das Verzeichnis `secrets/` ist lokal fuer sensible Artefakte vorgesehen und per Root-`.gitignore` ausgeschlossen.
* Der aktuell erzeugte Gateway-Schluessel liegt lokal unter:
  * `secrets/gateway_ssh_ed25519`
  * `secrets/gateway_ssh_ed25519.pub`
* Diese Dateien sind nur als Startpunkt fuer lokale Entwicklung gedacht. Vor echter Nutzung muessen sie in einen externen Secret-Store uebernommen und danach lokal nur noch kontrolliert bereitgestellt werden.

## Erwartete Zielrichtung fuer die Implementierung

* Empfohlene Sprache: Go
* Rolle des Dienstes:
  * WebSocket-Upgrade fuer das Browser-Terminal
  * Online-Validierung des Terminal-Grants gegen das Backend
  * serverseitiger SSH-Aufbau zur Konsole ueber das VPN
  * PTY/Shell-Bridging zwischen Browser und Konsole
* Wichtige fachliche Leitplanken:
  * Browser sieht die Konsole nie direkt
  * Browser-Reconnect erzeugt immer eine neue Gateway-Sitzung
  * derselbe Grant darf nur im vom Backend erlaubten Grace-Window erneut validiert werden
  * Support-Ende, Revocation, Timeout oder Reboot muessen Browser- und SSH-Seite sauber schliessen

## Resume-Protokoll

Wenn du eine begonnene Arbeit uebernimmst:

1. `git status` lesen und unerwartete Fremdaenderungen pruefen.
2. `plans/README.md` auf Status, Reihenfolge und Review-Gate lesen.
3. Im betroffenen Detailplan die Abschnitte `Fortschrittspflege`, `Offene Punkte` und `Naechste Uebergabe` lesen.
4. Erst danach aendern, implementieren oder testen.

## Minimaler Handover nach jedem Arbeitsblock

Jeder Agent soll im bearbeiteten Plan mindestens diese Punkte aktualisieren:

* `Status`
* `Zuletzt aktualisiert`
* `Fortschrittspflege`
* `Offene Punkte`
* `Naechste Uebergabe`

Wenn neue Entscheidungen getroffen werden, muessen sie direkt im betreffenden Plan dokumentiert werden und nicht nur in der Chat-Antwort.
