# SenseCAP Indicator Spezifikation

## Ziel und Rahmen

Der SenseCAP Indicator dient als 480×480-Agent-Dashboard. Originales panel-core und Deck Link bleiben gemeinsam mit Waveshare nutzbar. USB ist der stabile Standard; WLAN wird optional und zunächst experimentell angeboten. Pixeltakt bleibt 12 MHz. RP2040 und Sensoranwendungen sind ausserhalb des Umfangs.

## Anforderungen

- SC-01: Separater Diagnosebuild zeigt vier Eckziele und gemessene Touchkoordinaten. Er sendet keine Agent-Aktionen. Kalibrierung über explizite X/Y-Invertierungsflags. Physische Touchabnahme darf ohne tatsächliche Bedienung nicht als bestanden gelten.
- SC-02: Firmware aus aktuellem Originalcode bauen. Mehrfachauswahl zeigt ausgewählte Optionen mit [x]. Board verbindet sich nach Flashen wieder mit dem Daemon; Datenanzeige wird über den Protokollstatus geprüft. Flash nur auf dem registrierten SenseCAP-Port.
- SC-03: Buildversion enthält semantische Version und kurzen Git-Hash. Uncommittierte Änderungen werden mit dirty markiert. Keine absoluten Pfade, Gerätekennungen oder Zugangsdaten einchecken.
- SC-04: Dokumentation enthält Hardwareanbindung, Buildvarianten, sichere serielle Überwachung, Wiederherstellung, bekannte Grenzen und Abnahmestand. Upstream-Beitrag trennt Hardwareport von Änderungen am gemeinsamen UI.
- SC-05: Arduino-ESP32 3.x/ESP-IDF 5, TLS 1.3 und vorhandene Deck-Link-Zertifikatsprüfung verwenden. Keine unverschlüsselte Netzwerkalternative. USB hat Vorrang. Keine WLAN-Zugangsdaten im Repository. Ohne physische WLAN-Abnahme bleibt der Build experimentell.
- SC-06: Auf der 4×4-Startseite aktiviert mindestens 600 ms stationäres Halten das Verschieben, ab Scope act. Anschliessendes Ziehen und Loslassen über einer anderen Zelle sendet genau einen vorhandenen Deck-Link-Move-Befehl mit nullbasiertem Zielslot. Schnelle Bewegung bleibt Wischen; kurzes Tippen öffnet die Karte. Quelle anhand Agent-ID verfolgen; bei verschwundener Quelle oder verweigerten Rechten abbrechen. Keine optimistische Änderung des Fleet-Modells. Visuelle Rückmeldung für Quelle und Ziel. Seitenwechsel während des Ziehens gehört nicht zur ersten Version.

## Textdarstellung

SC-07: Alle Bitmap-Schriftgrössen enthalten den druckbaren Latin-1-Zeichensatz einschliesslich ä ö ü Ä Ö Ü. Die deutsche Kombination aus A/O/U/a/o/u und U+0308 wird vor Breitenberechnung, Kürzung und Darstellung in die entsprechende Umlautglyphe überführt. Andere Unicode-Zeichen ausserhalb des vorhandenen Zeichensatzes behalten die bisherige Ersatzdarstellung. Vorhandene Glyphen und Zellgrössen dürfen sich nicht verändern.

## Fokus auf dem Mac

SC-08: Ein Codex-Thread, dessen Provider-ID einer laufenden Orca-Chat-Session gehört, aktiviert über die Orca-CLI deren konkreten Workspace und bringt Orca nach vorne. Die Zuordnung liest ausschliesslich Session-/Thread-IDs aus dem Orca-Journal im Read-only-Modus und bestätigt aktive Ownership über worktree ps. Keine heuristische Ordner-/Titelzuordnung. Nicht zugehörige Codex-App-Threads behalten ihren bisherigen Fokusweg. Bei mehrdeutiger Ownership oder fehlgeschlagener Orca-Aktivierung Fehler melden statt eine andere App zu öffnen. Der aktuelle CLI-Vertrag erlaubt keine Auswahl eines bestimmten Chat-Tabs; bei mehreren Chats bleibt die dortige Auswahl bestehen. Gerätesoftware und Berechtigung bleiben unverändert.

## Validierung

Hosttests für panel-core mit ASan/UBSan und Deck Link; PlatformIO-Builds für USB, Diagnose und IDF5 sowie Waveshare als Regression; Go vet und Tests für Backend. Auf Hardware: Version, Verbindung und Protokollzustand. Physische Prüfungen (Touch, Bildruhe, WLAN ohne USB) getrennt dokumentieren. Gerätescope bleibt act.
