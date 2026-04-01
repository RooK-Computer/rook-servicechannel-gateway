# Plan 05 - nfpm-Debian-Paketierung und Installationspfad

Status: Umgesetzt, wartet auf Review

Zuletzt aktualisiert: 2026-04-01

## Ziel

Der Gateway soll als reproduzierbar baubares Debian-Paket ausgeliefert werden. Die Paketierung soll auf `nfpm` basieren und den bereits vorhandenen `systemd`-Pfad integrieren, ohne die spaetere Betriebsform unnoetig festzulegen oder die Konfigurierbarkeit des Dienstes einzuschraenken.

## Abhaengigkeit

Plan 04 muss reviewed/abgenommen sein.

## Schwerpunkte

* `nfpm` als offizieller `.deb`-Buildpfad fuer den Gateway einfuehren
* Binary, `systemd`-Unit und Konfigurationsartefakte paketierbar zusammenfuehren
* Konfigurierbarkeit ueber Environment-Datei und Secret-Mounts erhalten
* Installationsdetails offen halten, solange Zielumgebung und Rolloutprozess noch nicht endgueltig feststehen

## Arbeitspakete

### 1. Paketlayout und Metadaten festziehen

Umgesetzt:

* `nfpm.yaml` im Repo angelegt
* Paketname `rook-servicechannel-gateway`
* Versions- und Release-Felder bleiben ueber Build-Variablen injizierbar
* Debian-Abhaengigkeiten fuer den aktuellen Stand modelliert:
  * `systemd`
  * `adduser`
  * `ca-certificates`

### 2. Binary- und Artefaktablage definieren

Umgesetzt:

* Linux-Binary wird fuer den Paketbau nach `dist/package/rook-servicechannel-gateway` gebaut
* Paketinhalt:
  * `/usr/bin/rook-servicechannel-gateway`
  * `/lib/systemd/system/rook-servicechannel-gateway.service`
  * `/etc/rook-servicechannel-gateway/gateway.env`
* `systemd`-Unit wurde auf paketfaehige Pfade umgestellt
* Es werden bewusst keine Secrets oder ephemere Verzeichnisse wie `/run/secrets/...` ins Paket aufgenommen

### 3. Konfigurierbarkeit trotz systemd erhalten

Umgesetzt:

* Dienst bleibt ueber `/etc/rook-servicechannel-gateway/gateway.env` extern konfigurierbar
* Beispiel-Environment wird als `config|noreplace` paketiert
* Secret-Pfade bleiben reine Laufzeitkonfiguration und werden nicht mitgeliefert
* `README.md` dokumentiert jetzt explizit, wie Paketarchitektur, Version und externe Konfiguration ueberschrieben werden koennen

### 4. Installationsverhalten behutsam vorbereiten

Umgesetzt:

* `postinstall`, `preremove` und `postremove` als `nfpm`-Maintainer-Skripte angelegt
* `postinstall`:
  * legt bei Bedarf den Systemnutzer `rook-gateway` an
  * fuehrt `systemctl daemon-reload` aus
  * aktiviert oder startet den Dienst bewusst nicht
* `preremove` stoppt einen ggf. laufenden Dienst
* `postremove` fuehrt `systemctl daemon-reload` aus
* festgelegter Default fuer diesen Plan bleibt:
  * Paket installiert Binary und Betriebsartefakte
  * der `systemd`-Dienst bleibt standardmaessig deaktiviert
  * kein automatischer Start waehrend der Paketinstallation

### 5. Build- und Validierungspfad

Umgesetzt:

* `Makefile` erweitert um:
  * `make package`
  * `make package-inspect`
* Paketbau laeuft ueber `go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.46.0`
* lokaler Pruefpfad auf macOS ohne Debian-Toolchain:
  * Paket bauen
  * Paketinhalt per `ar`/`tar` inspizieren
* bewusste Abgrenzung:
  * echte Debian-Installations- und Laufzeittests sind fuer einen spaeteren Testlauf auf Debian vorgesehen

## Erwartete Dateien oder Bereiche

* `nfpm.yaml` oder aequivalente `nfpm`-Konfiguration
* `Makefile`
* `deploy/systemd/...`
* ggf. Packaging-Hilfsartefakte unter `packaging/` oder `deploy/`
* `README.md`
* `plans/README.md`
* `spec/implementation/05-browser-terminal-gateway-status.md`

## Validierung

* `make verify`
* `make package`
* `make package-inspect`

## Exit-Kriterien

* Der Gateway kann als `.deb` mit `nfpm` gebaut werden
* Binary, `systemd`-Unit und Beispielkonfiguration sind im Paket enthalten
* Die Laufzeitkonfiguration bleibt bewusst extern und ueberschreibbar
* Der Build-/Pruefpfad fuer das Paket ist im Repo dokumentiert

## Fortschrittspflege

Stand nach Umsetzung:

* finaler Paketname:
  * `rook-servicechannel-gateway`
* Zielpfade:
  * `/usr/bin/rook-servicechannel-gateway`
  * `/lib/systemd/system/rook-servicechannel-gateway.service`
  * `/etc/rook-servicechannel-gateway/gateway.env`
* Debian-Abhaengigkeiten:
  * `systemd`
  * `adduser`
  * `ca-certificates`
* Entscheidung zu Auto-Start/Auto-Enable:
  * Dienst bleibt nach Paketinstallation deaktiviert
  * kein automatischer Start
* Nutzeranlage:
  * `postinstall` legt bei Bedarf `rook-gateway` als Systemnutzer an
* lokale Validierung:
  * Paketbau und Inhaltspruefung laufen auf dem Mac
  * Installations-/Runtime-Tests auf Debian bleiben explizit Folgearbeit fuer einen Kollegen mit Debian-Entwicklungsumgebung

## Offene Punkte

* Ob spaeter ein automatisches Enable/Start-Verhalten gewuenscht ist
* Ob die Maintainer-/Maintainer-Mailadresse fuer das Paket angepasst werden soll
* Ob spaeter Paket-Signing oder Release-Automatisierung hinzugefuegt werden soll
* Echte Debian-Installations- und Laufzeittests stehen noch aus

## Naechste Uebergabe

Nach Abschluss dieses Plans:

1. Plan 05 reviewen.
2. Rueckmeldungen zu Paketlayout, Maintainer-Skripten und Installationsverhalten nachziehen.
3. Danach wieder anhalten statt stillschweigend den naechsten Plan zu beginnen.
