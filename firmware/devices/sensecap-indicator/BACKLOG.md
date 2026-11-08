# SenseCAP Indicator Backlog

Stand: 2026-11-08. Basis: lokaler SenseCAP-Port und gemeinsames agents-deck-UI.
Status: offen, umgesetzt, geprüft oder externe Prüfung offen. Jede Priorität erhält einen eigenen Commit.

| ID | Priorität | Aufgabe | Abnahmekriterium | Status |
|---|---|---|---|---|
| SC-01 | P1 | Touchdiagnose und Hardwareabnahme | Eckpunkte, Achsen und Taste prüfbar; reale Abnahme separat protokolliert | Diagnose umgesetzt; physische Abnahme offen |
| SC-02 | P2 | Firmware aktualisieren | Aktuelles panel-core mit Mehrfachauswahlmarkierungen gebaut und auf USB-Gerät geprüft | Aktueller USB- und Debug-Build geprüft; Installation nach Gesamtintegration |
| SC-03 | P3 | Build identifizieren | Hello meldet Version und Git-Hash, Dirty-Builds sind markiert | offen |
| SC-04 | P4 | Port dokumentieren und PR vorbereiten | README, Pinbelegung, Flash- und Rückfallanleitung; überprüfbarer Upstream-Beitrag | offen |
| SC-05 | P5 | Optionales WLAN | Separater IDF5-Build, TLS 1.3 und Clientzertifikat; USB-Rückfall; Hardwareabnahme separat | offen |
| SC-06 | P6 | Kacheln verschieben | Halten und Ziehen sendet Move; Tippen und Wischen bleiben bedienbar; Tests für Berechtigungen und Abbruch | offen |

Keine automatische Erweiterung auf admin. Keine produktiven Agents für Tests stoppen oder Freigaben beantworten.
