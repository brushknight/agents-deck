import Foundation

// The line protocol between agents deck's service and a Bluetooth helper:
// one JSON object per line, UTF-8, both ways. ../../README.md is the full
// list; this file is its Swift side.

/// The GATT table both sides agree on (docs/protocol.md, Bluetooth).
public enum DeckGATT {
    public static let service = "7E3A0001-5D2C-4F1B-9A6E-A9E17D5C0DE0"
    public static let rx = "7E3A0002-5D2C-4F1B-9A6E-A9E17D5C0DE0"   // deck to board: write, write without response
    public static let tx = "7E3A0003-5D2C-4F1B-9A6E-A9E17D5C0DE0"   // board to deck: notify
    public static let info = "7E3A0004-5D2C-4F1B-9A6E-A9E17D5C0DE0" // read, no pairing needed
}

/// Protocol version the helper speaks (in "ready").
public let protocolVersion = 1

/// Bytes the service may have in flight before it waits for "sent".
public let writeWindow = 4096

/// The longest line either side accepts.
public let maxLine = 64 * 1024

/// A command from the service.
public enum Command: Equatable {
    case scan(on: Bool)
    case connect(id: UUID)
    case write(id: UUID, data: Data)
    case disconnect(id: UUID)
    case forget(id: UUID)
    case state
}

public struct ProtocolError: Error, Equatable {
    public let message: String
    public init(_ m: String) { message = m }
}

/// Command.parse reads one line from the service.
extension Command {
    public static func parse(_ line: String) throws -> Command {
        guard line.utf8.count <= maxLine else { throw ProtocolError("line longer than \(maxLine) bytes") }
        guard let raw = line.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: raw) as? [String: Any] else {
            throw ProtocolError("not a JSON object")
        }
        guard let op = obj["op"] as? String else { throw ProtocolError("no \"op\"") }
        func id() throws -> UUID {
            guard let s = obj["id"] as? String, let u = UUID(uuidString: s) else {
                throw ProtocolError("\(op) needs \"id\", a peripheral identifier")
            }
            return u
        }
        switch op {
        case "scan":
            guard let on = obj["on"] as? Bool else { throw ProtocolError("scan needs \"on\": true or false") }
            return .scan(on: on)
        case "connect":
            return .connect(id: try id())
        case "write":
            let u = try id()
            guard let s = obj["data"] as? String, let d = Data(base64Encoded: s) else {
                throw ProtocolError("write needs \"data\" in base64")
            }
            return .write(id: u, data: d)
        case "disconnect":
            return .disconnect(id: try id())
        case "forget":
            return .forget(id: try id())
        case "state":
            return .state
        default:
            throw ProtocolError("unknown op \"\(op)\"")
        }
    }
}

/// What the board says about itself in its Info characteristic:
/// "board=<board>;id=<board id>;fw=<version>;paired=<0|1>".
public struct BoardInfo: Equatable {
    public var board = ""
    public var boardID = ""
    public var firmware = ""
    public var paired: Bool?

    public init() {}

    public static func parse(_ data: Data) -> BoardInfo {
        var i = BoardInfo()
        let s = String(decoding: data.prefix(512), as: UTF8.self)
        for part in s.split(separator: ";") {
            let kv = part.split(separator: "=", maxSplits: 1).map { String($0).trimmingCharacters(in: .whitespaces) }
            guard kv.count == 2 else { continue }
            let v = String(kv[1].prefix(64))
            switch kv[0] {
            case "board": i.board = v
            case "id": i.boardID = v
            case "fw": i.firmware = v
            case "paired": i.paired = (v == "1")
            default: break
            }
        }
        return i
    }
}

/// The adapter's state, in the protocol's words.
public enum RadioState: String {
    case on, off, unauthorized, unsupported, resetting, unknown

    public var message: String {
        switch self {
        case .on: return "Bluetooth is on"
        case .off: return "Bluetooth is off: turn it on in Control Centre or System Settings"
        case .unauthorized: return "agents deck may not use Bluetooth: allow it in System Settings, Privacy & Security, Bluetooth"
        case .unsupported: return "this Mac has no Bluetooth Low Energy"
        case .resetting: return "Bluetooth is restarting"
        case .unknown: return "Bluetooth is starting"
        }
    }
}

/// Why a link ended or failed, in the protocol's words.
public enum LinkProblem: String {
    case pairingFailed = "pairing-failed"     // the code was wrong, or pairing was cancelled
    case pairingRemoved = "pairing-removed"   // the board forgot this Mac
    case pairingTimeout = "pairing-timeout"   // nobody typed the code in time
    case timeout                              // out of range, switched off
    case gone                                 // the board hung up
    case cancelled                            // the service asked
    case unknownPeripheral = "unknown-peripheral"
    case notADeck = "not-a-deck"              // no Deck Link service or characteristics
    case bluetoothOff = "bluetooth-off"
    case error

    public var message: String {
        switch self {
        case .pairingFailed: return "pairing failed: the code typed didn't match the board's, or pairing was cancelled"
        case .pairingRemoved: return "the board no longer knows this computer: put it back in setup from its screen, remove it in System Settings, Bluetooth if it is listed there, and add it again"
        case .pairingTimeout: return "pairing timed out: type the code shown on the board when macOS asks for it"
        case .timeout: return "the board stopped answering: it is out of range or switched off"
        case .gone: return "the board disconnected"
        case .cancelled: return "disconnected"
        case .unknownPeripheral: return "this computer doesn't know that board: scan for it first"
        case .notADeck: return "that device isn't a Deck Link board (no Deck Link service)"
        case .bluetoothOff: return "Bluetooth is off"
        case .error: return "Bluetooth error"
        }
    }
}

/// Events to the service. Each encodes to one line.
public enum Event: Equatable {
    case ready
    case state(RadioState)
    case scanning(Bool)
    case found(id: UUID, name: String, rssi: Int, info: BoardInfo?)
    case connecting(id: UUID)
    case passkey(id: UUID)
    case connected(id: UUID, mtu: Int, info: BoardInfo?)
    case data(id: UUID, Data)
    case sent(id: UUID, Int)
    case disconnected(id: UUID, LinkProblem, detail: String)
    case forgotten(id: UUID)
    case error(id: UUID?, String)

    public func line() -> String {
        var o: [String: Any] = [:]
        func info(_ i: BoardInfo?) {
            guard let i = i else { return }
            o["board"] = i.board
            o["boardId"] = i.boardID
            o["firmware"] = i.firmware
            if let p = i.paired { o["paired"] = p }
        }
        switch self {
        case .ready:
            o = ["ev": "ready", "protocol": protocolVersion, "helper": "deck-ble/macos", "window": writeWindow]
        case .state(let s):
            o = ["ev": "state", "state": s.rawValue, "message": s.message]
        case .scanning(let on):
            o = ["ev": "scanning", "on": on]
        case .found(let id, let name, let rssi, let i):
            o = ["ev": "found", "id": id.uuidString, "name": name, "rssi": rssi]
            info(i)
        case .connecting(let id):
            o = ["ev": "connecting", "id": id.uuidString]
        case .passkey(let id):
            o = ["ev": "passkey", "id": id.uuidString,
                 "message": "Type the 6-digit code shown on the board into the window macOS opens."]
        case .connected(let id, let mtu, let i):
            o = ["ev": "connected", "id": id.uuidString, "mtu": mtu]
            info(i)
        case .data(let id, let d):
            o = ["ev": "data", "id": id.uuidString, "data": d.base64EncodedString()]
        case .sent(let id, let n):
            o = ["ev": "sent", "id": id.uuidString, "n": n]
        case .disconnected(let id, let p, let detail):
            o = ["ev": "disconnected", "id": id.uuidString, "code": p.rawValue, "reason": detail.isEmpty ? p.message : p.message + " (" + detail + ")"]
        case .forgotten(let id):
            o = ["ev": "forgotten", "id": id.uuidString]
        case .error(let id, let m):
            o = ["ev": "error", "message": m]
            if let id = id { o["id"] = id.uuidString }
        }
        let d = (try? JSONSerialization.data(withJSONObject: o, options: [.sortedKeys, .withoutEscapingSlashes])) ?? Data("{}".utf8)
        return String(decoding: d, as: UTF8.self)
    }
}
