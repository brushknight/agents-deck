# Prüfprotokoll

## 2026-11-08: Ausgangsbasis und Aktualisierung

- Touchdiagnose, USB-Release und USB-Debug: PlatformIO-Build erfolgreich.
- Gemeinsames panel-core: vollständige Hosttests mit ASan/UBSan erfolgreich, einschliesslich Mehrfachauswahladapter.
- Deck Link Hosttest: 1625 Prüfungen erfolgreich.
- Aktuelle Quellen enthalten Option.selected und die [x]-Markierung, die dem zuvor installierten Oktober-Build fehlt.
- Installation erfolgt nach Integration der weiteren Prioritäten, damit ein konsistenter Build mit Versionskennung und Gestenfunktion auf das Gerät kommt.
- Physische Touchprüfung und visuelle Bildprüfung bleiben offen. Protokolltests ersetzen keine Hardwareabnahme.

## Integration und USB-Hardware

- Backend: `go vet ./...` und `go test ./...` erfolgreich.
- Neue Gestentests mit ASan/UBSan: Halteschwelle, kurzes Tippen, Wischen, Rechteentzug, Agent-ID nach Snapshot-Umsortierung, verschwundene Quelle, belegtes Ziel, zweite Seite, Abbruch ausserhalb des Gitters und Darstellung im Framebuffer erfolgreich. Vollständige panel-core-Suite erfolgreich.
- Deck-Link-Hosttest nach Move-Serialisierungstest: 1628 Prüfungen erfolgreich.
- SenseCAP USB, Debug und Touchdiagnose sowie Waveshare Touch LCD 4 USB bauen erfolgreich.
- USB-Firmware `0.2.0+87fcf39` installiert und per Hello bestätigt. Isolierter Deck-Link-Hardwarecheck bestanden: Protokoll, Hello, Feldabonnement, sieben Ping-Antworten und Wiederaufnahme nach Stördaten. Produktionsscope bleibt act.
- Der erste kurze Hardwarecheck wurde begonnen, während das Board noch mit dem bisherigen Daemon verbunden war; er erhielt keinen neuen Hello. Nach Ablauf des 15-Sekunden-Linktimeouts bestand der erneute Check. Für Wiederholungen nach `link rm` mindestens 22 Sekunden ohne Hostverkehr warten.
- Touchausrichtung, reale Gesten und Bildruhe sind weiterhin physisch abzunehmen. Es wurden keine produktiven Freigaben beantwortet oder Agents gestoppt.

## WLAN-Toolchain

- Arduino-3.x-Build verwendet eine explizite 8-MB-Partitionstabelle mit denselben Offsets wie USB, damit NVS erhalten bleibt.
- Ein alleiniger Core-Versionspin genügt wegen des internen pioarduino-Unterprozesses nicht. Der bestehende Repository-Helfer `.github/scripts/pioarduino-scons.sh` bereitet die gemeinsame SCons-Version vor; Ablauf im README.
- Netzwerkabnahme ohne USB (Zugangsdaten, TLS-Verbindung zum Mac, Wiederverbindung und Bildruhe) erfordert ein eingerichtetes WLAN und physische Prüfung. Ohne diese bleibt WLAN experimentell.

- WLAN-Releasebuild erfolgreich: 1269666 Bytes App, 77308 Bytes statisches RAM. TLS-1.3-Konfiguration und TLS-1.3-Symbole im tatsächlichen Firmware-ELF bestätigt.

- IDF5-Release und IDF5-Debug erfolgreich gebaut. IDF5-Release `0.2.0+37647e7` auf Hardware geflasht: startet, verbindet sich mit dem Daemon über USB und meldet WLAN-Unterstützung (`not set`, statt `not supported`). Das ist keine Prüfung einer tatsächlichen WLAN-/TLS-Verbindung; Netzwerk und Zertifikatsverbindung bleiben abzunehmen.
- Der Kurzweg `-t nobuild -t upload` lieferte beim pioarduino-Upload unvollständige esptool-Argumente und wurde verworfen. Für reproduzierbare Uploads den normalen Build-und-Upload-Befehl im README verwenden.
- Nach dem Starttest wird die USB-Standardfirmware wieder installiert; der Diagnosebuild bleibt ein bewusst separat zu flashendes Werkzeug.
