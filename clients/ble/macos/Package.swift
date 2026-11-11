// swift-tools-version:5.9
// deck-ble: the Bluetooth helper agents deck's service starts on macOS. It
// speaks the line protocol in ../README.md on stdin and stdout.
import PackageDescription
import Foundation

// The helper carries its own Info.plist (bundle identifier and the Bluetooth
// usage text) in a __TEXT,__info_plist section, so macOS can ask for
// Bluetooth on its behalf.
let here = URL(fileURLWithPath: #filePath).deletingLastPathComponent().path
let infoPlist = here + "/Support/Info.plist"

let package = Package(
    name: "deck-ble",
    platforms: [.macOS(.v12)],
    products: [
        .executable(name: "deck-ble", targets: ["deck-ble"]),
    ],
    targets: [
        // The line protocol, the write queue and the link logic: no
        // CoreBluetooth here, so tests drive it with a fake radio.
        .target(name: "DeckBLECore"),
        .executableTarget(
            name: "deck-ble",
            dependencies: ["DeckBLECore"],
            linkerSettings: [
                .linkedFramework("CoreBluetooth"),
                .unsafeFlags(["-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__info_plist", "-Xlinker", infoPlist]),
            ]
        ),
        .testTarget(name: "DeckBLECoreTests", dependencies: ["DeckBLECore"]),
    ]
)
