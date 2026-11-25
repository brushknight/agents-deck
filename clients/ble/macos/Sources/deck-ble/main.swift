// deck-ble: agents deck's Bluetooth helper for macOS. The service (agentctl)
// starts it and talks to it with JSON lines on stdin and stdout
// (clients/ble/README.md). It exits when stdin closes.
//
//   deck-ble                  run the line protocol
//   deck-ble --authorization  print whether it may use Bluetooth, without asking
//   deck-ble --version
import CoreBluetooth
import DeckBLECore
import Foundation

let helperVersion = "1"

setvbuf(stdout, nil, _IOLBF, 0)

let args = CommandLine.arguments.dropFirst()
if args.contains("--version") {
    print("deck-ble \(helperVersion) (protocol \(protocolVersion))")
    exit(0)
}
if args.contains("--authorization") {
    // The class property only reads the setting: it never shows a prompt.
    let a: String
    switch CBManager.authorization {
    case .allowedAlways: a = "allowed"
    case .denied: a = "denied"
    case .restricted: a = "restricted"
    case .notDetermined: a = "not asked yet"
    @unknown default: a = "unknown"
    }
    print("Bluetooth for deck-ble: \(a)")
    exit(0)
}

// macOS asks for Bluetooth on behalf of the "responsible" process. Started by
// agentctl (a launchd agent with no Info.plist of its own), that would be
// agentctl, and macOS would refuse without asking. So the helper starts itself
// again as its own responsible process, and macOS reads the Info.plist built
// into this executable: the prompt and the switch in System Settings, Privacy
// & Security, Bluetooth then say "agents deck Bluetooth".
if !args.contains("--no-disclaim") && ProcessInfo.processInfo.environment["DECK_BLE_DISCLAIMED"] == nil {
    if let status = Disclaim.respawn() {
        exit(status)
    }
    // No way to disclaim on this macOS: carry on as we are.
}

let queue = DispatchQueue(label: "deck-ble")
let radio: CoreBluetoothRadio
let engine: Engine

func writeLine(_ s: String) {
    FileHandle.standardOutput.write(Data((s + "\n").utf8))
}

// Everything below runs on `queue`: the engine, the radio's callbacks.
var boot: (CoreBluetoothRadio, Engine)!
queue.sync {
    let r = CoreBluetoothRadio(queue: queue)
    let e = Engine(radio: r, emit: writeLine)
    e.schedule = { delay, block in queue.asyncAfter(deadline: .now() + delay, execute: block) }
    r.engine = e
    e.start()
    boot = (r, e)
}
(radio, engine) = boot

// stdin: one command per line; EOF ends the helper.
signal(SIGPIPE, SIG_IGN)
let reader = Thread {
    while let line = readLine(strippingNewline: true) {
        queue.async { engine.handle(line: line) }
    }
    queue.async {
        exit(0)
    }
}
reader.start()
dispatchMain()
