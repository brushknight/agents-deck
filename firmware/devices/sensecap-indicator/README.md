# SenseCAP Indicator: agents deck auf dem 4-Zoll-Touchdisplay

480×480, 4×4-Kacheln, bis zu 64 Agents. Gemeinsames `panel-core` mit dem Waveshare Touch LCD 4: Figuren, Hüte, Statusfarben, Detailkarten, Freigaben und Seitenwechsel. Der Port ersetzt die Hardwaretreiber. Der RP2040 wird nicht geflasht. Sensorik gehört nicht zu dieser Anwendung.

## Hardware

| Funktion | Anbindung |
|---|---|
| ESP32-S3 | 8 MB Flash, OPI-PSRAM |
| ST7701S RGB | DE 18, VSYNC 17, HSYNC 16, PCLK 21; 12 MHz |
| RGB-Daten | R: 4 3 2 1 0; G: 10 9 8 7 6 5; B: 15 14 13 12 11 |
| LCD-Init | SCK 41, MOSI 48, CS über TCA9535 IO4, LCD-Reset IO5 |
| I2C | SDA 39, SCL 40; TCA9535 0x20 oder 0x39 |
| FT6336 | GX 0x48; DX 0x38; Touch-Reset TCA9535 IO7 |
| Hintergrundlicht | GPIO45, PWM |
| Obere Taste | GPIO38, aktiv tief; zurück oder nächste Seite |
| USB | CH340 an UART0, 115200 Baud |

GX benötigt die ST7701S-Initialisierung; DX wird anhand der Touchadresse erkannt und überspringt sie. GX wurde beim ursprünglichen Bring-up erkannt. DX ist noch nicht physisch validiert. Pinquelle: [Seeed BSP](https://github.com/Seeed-Solution/SenseCAP_Indicator_ESP32/tree/main/components/bsp).

## Bauen und flashen

PlatformIO verwenden, zum Beispiel `uvx --from platformio pio` anstelle von `pio`. Befehle ab Repositorywurzel. `<port>` durch den mit `agentctl link ports` identifizierten SenseCAP-Port ersetzen.

```sh
pio run -d firmware/devices/sensecap-indicator -e sensecap-indicator
agentctl link rm <port>
pio run -d firmware/devices/sensecap-indicator -e sensecap-indicator -t upload --upload-port <port>
agentctl link add <port> --baud 115200
agentctl link
agentctl devices
```

Das Entfernen der Portregistrierung verhindert konkurrierenden Zugriff beim Flashen. Danach wieder registrieren. Keine anderen angeschlossenen Boards flashen. Hello meldet `0.2.0+<git-hash>`, bei uncommittierten Quellen zusätzlich `-dirty`; Archive ohne Git melden `unknown`.

| Build | Verwendung |
|---|---|
| `sensecap-indicator` | Standard, USB; Arduino-ESP32 2.0.17 |
| `debug` | USB mit Diagnoseausgaben zwischen Deck-Link-Frames |
| `touch-diagnostic` | Isolierter Touchtest ohne Agent-Aktionen |
| `idf5` im Unterordner `idf5` | Optionales WLAN mit TLS 1.3; experimentell |

Bei serieller Überwachung DTR und RTS nicht umschalten: Die Leitungen steuern Reset und Bootmodus. Die PlatformIO-Monitorkonfiguration setzt beide auf 0. Daemon vor exklusiver Überwachung vom Port trennen.

## Touchabnahme

Diagnosebuild wie oben mit `-e touch-diagnostic` flashen. Alle vier orangefarbenen Ziele antippen: Grüner Punkt und Koordinaten müssen mit dem Finger übereinstimmen. Obere Taste muss `down`/`up` wechseln. Danach Standardfirmware wiederherstellen und Kacheln in allen vier Ecken, Zurückgeste, Seitenwechsel und eine simulierte Freigabe prüfen. Niemals produktive Freigaben für Tests bestätigen.

Standard: X unverändert, Y invertiert. Falls die tatsächliche Panelvariante abweicht, `TOUCH_INVERT_X` bzw. `TOUCH_INVERT_Y` als Buildflag setzen. Hardwareabnahme im [Prüfprotokoll](VALIDATION.md) führen.

## Betrieb und Wiederherstellung

Scope `act` erlaubt Antworten, Fokus, Unterbrechen und Verschieben. Stoppen, Entfernen und Fortsetzen benötigen `admin`; dieser Port erweitert Berechtigungen nicht automatisch. Dimmen nach 30 Sekunden Inaktivität ist Teil des gemeinsamen UI.

USB ist die bevorzugte Rückfalloption. Bei Problemen dieselbe Standardfirmware erneut mit 115200 Baud flashen. Falls nötig den Bootmodus gemäss Seeed-Anleitung aktivieren. Eine vorher gesicherte Werks-Firmware kann mit esptool an Adresse 0 zurückgespielt werden; Grösse und Prüfsumme des Backups vorher kontrollieren. Backups können Zugangsdaten enthalten und gehören nicht ins Repository.

Der Pixeltakt bleibt 12 MHz: Beim ursprünglichen Bring-up verursachten 18 MHz ein instabiles Bild. Nicht ohne Bildprüfung erhöhen. Display-Scanrate und UI-Animationsrate sind unterschiedliche Grössen.

## Entwicklungsstand

[Backlog](BACKLOG.md), [Spezifikation](SPECIFICATION.md) und [Prüfprotokoll](VALIDATION.md). Physische Touch- und WLAN-Abnahmen werden getrennt von erfolgreichen Builds ausgewiesen. Für die Upstream-Einreichung: Hardwareport, Dokumentation und Änderungen am gemeinsamen Gesten-UI liegen in getrennten Commits und können separat geprüft werden.

## Optionales WLAN

```sh
uvx --from platformio pio run -d firmware/devices/sensecap-indicator/idf5 -e idf5
# Flashen wie beim USB-Build, aber mit -d .../idf5 -e idf5.
agentctl link wifi <board> --ssid <network>
```

Das Passwort wird interaktiv abgefragt. Erst per USB verbinden und das Gerätezertifikat provisionieren lassen. Danach USB-Datenverbindung trennen und separat mit Strom versorgen. WLAN muss über TLS 1.3 mit Clientzertifikat verbinden. USB übernimmt bei erneuter Verbindung zum Daemon. Ohne erfolgreiche Abnahme (falsches Passwort, Neustart, Verbindungsverlust, USB-Rückkehr und Bildruhe) bleibt dieser Build experimentell. Der optionale Bounce-Buffer ist mit `SENSECAP_BOUNCE_PX=0` standardmässig deaktiviert; Aktivierung nur nach visueller Prüfung.

## Kacheln verschieben

Eine belegte Kachel mindestens 600 ms ruhig halten, dann zur Zielzelle ziehen und loslassen. Orange Umrandungen markieren Quelle und Ziel. Belegte Ziele werden vom Daemon getauscht; Loslassen ausserhalb der Zellen bricht ab. Kurzes Tippen öffnet weiterhin die Karte, schnelles Wischen wechselt die Seite. Verschieben über Seitengrenzen ist in dieser Version nicht vorgesehen.

Beim ersten IDF5-Build können sich Core und der interne pioarduino-Unterprozess über SCons-Versionen widersprechen. Ein Versionspin allein reicht nicht, da pioarduino einen eigenen Core installiert. Das Repository enthält dafür bereits den gemeinsamen Vorbereitungsschritt (ab Repositorywurzel):

```sh
PLATFORMIO_CORE_DIR="$HOME/.platformio-pioarduino" uv run --no-project --with platformio sh .github/scripts/pioarduino-scons.sh firmware/devices/sensecap-indicator/idf5 idf5
uvx --from platformio pio run -d firmware/devices/sensecap-indicator/idf5 -e idf5
```

Siehe auch [allgemeine Build-Fehlerbehebung](../../../docs/flashing.md#troubleshooting) und [Upstream-Issue 556](https://github.com/pioarduino/platform-espressif32/issues/556). Die explizite IDF5-Partitionstabelle behält die NVS- und App-Offsets des USB-Builds bei.
