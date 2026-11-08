# SenseCAP Indicator Backlog

Stand: 2026-11-08. Basis: lokaler SenseCAP-Port und gemeinsames agents-deck-UI.
Status: offen, umgesetzt, geprüft oder externe Prüfung offen. Jede Priorität erhält einen eigenen Commit.

| ID | Priorität | Aufgabe | Abnahmekriterium | Status |
|---|---|---|---|---|
| SC-01 | P1 | Touchdiagnose und Hardwareabnahme | Eckpunkte, Achsen und Taste prüfbar; reale Abnahme separat protokolliert | Diagnose umgesetzt; physische Abnahme offen |
| SC-02 | P2 | Firmware aktualisieren | Aktuelles panel-core mit Mehrfachauswahlmarkierungen gebaut und auf USB-Gerät geprüft | geprüft und installiert; USB-Protokollcheck bestanden |
| SC-03 | P3 | Build identifizieren | Hello meldet Version und Git-Hash, Dirty-Builds sind markiert | geprüft; Version und Commit-Hash per Hardware-Hello bestätigt |
| SC-04 | P4 | Port dokumentieren und PR vorbereiten | README, Pinbelegung, Flash- und Rückfallanleitung; überprüfbarer Upstream-Beitrag | Dokumentation geprüft; Upstream-Entwurf vorbereitet |
| SC-05 | P5 | Optionales WLAN | Separater IDF5-Build, TLS 1.3 und Clientzertifikat; USB-Rückfall; Hardwareabnahme separat | Release/Debug gebaut; Start und WLAN-Fähigkeit auf Hardware geprüft; WLAN-Verbindung offen |
| SC-06 | P6 | Kacheln verschieben | Halten und Ziehen sendet Move; Tippen und Wischen bleiben bedienbar; Tests für Berechtigungen und Abbruch | implementiert; Hosttests erfolgreich; physische Prüfung offen |

Keine automatische Erweiterung auf admin. Keine produktiven Agents für Tests stoppen oder Freigaben beantworten.
