import Foundation
import XCTest
@testable import DeckBLECore

/// FakeRadio stands in for CoreBluetooth: it records what the engine asks and
/// lets a test decide when writes fit.
final class FakeRadio: Radio {
    var scanning = false
    var known: Set<UUID> = []
    var opened: [UUID] = []
    var subscribed: [UUID] = []
    var infoReads: [UUID] = []
    var closed: [UUID] = []
    var forgotten: [UUID] = []
    var sent: [Data] = []
    var room = Int.max // writes that fit before canSend turns false
    var mtu = 182

    func scan(_ on: Bool) { scanning = on }
    func open(_ id: UUID) -> Bool {
        guard known.contains(id) else { return false }
        opened.append(id)
        return true
    }
    func subscribe(_ id: UUID) { subscribed.append(id) }
    func readInfo(_ id: UUID) { infoReads.append(id) }
    func close(_ id: UUID) { closed.append(id) }
    func forget(_ id: UUID) { forgotten.append(id) }
    func canSend(_ id: UUID) -> Bool { room > 0 }
    func maxWrite(_ id: UUID) -> Int { mtu }
    func send(_ id: UUID, _ data: Data) {
        sent.append(data)
        room -= 1
    }
}

final class EngineTests: XCTestCase {
    var radio: FakeRadio!
    var engine: Engine!
    var lines: [[String: Any]] = []
    var later: [() -> Void] = []
    let board = UUID(uuidString: "6B1F2C3D-0000-4000-8000-0000000000A1")!

    override func setUp() {
        radio = FakeRadio()
        radio.known = [board]
        lines = []
        later = []
        engine = Engine(radio: radio) { [unowned self] l in
            let obj = try! JSONSerialization.jsonObject(with: Data(l.utf8)) as! [String: Any]
            XCTAssertFalse(l.contains("\n"), "one event per line")
            self.lines.append(obj)
        }
        engine.schedule = { [unowned self] _, block in self.later.append(block) }
        engine.start()
        engine.radioState(.on)
        lines.removeAll()
    }

    func events(_ ev: String) -> [[String: Any]] { lines.filter { $0["ev"] as? String == ev } }

    func testReadyLine() {
        let r = Event.ready.line()
        XCTAssertTrue(r.contains("\"ev\":\"ready\""))
        XCTAssertTrue(r.contains("\"protocol\":1"))
        XCTAssertTrue(r.contains("\"window\":4096"))
    }

    func testParseCommands() throws {
        XCTAssertEqual(try Command.parse(#"{"op":"scan","on":true}"#), .scan(on: true))
        XCTAssertEqual(try Command.parse(#"{"op":"connect","id":"\#(board.uuidString)"}"#), .connect(id: board))
        XCTAssertEqual(try Command.parse(#"{"op":"write","id":"\#(board.uuidString)","data":"AAEC"}"#),
                       .write(id: board, data: Data([0, 1, 2])))
        XCTAssertEqual(try Command.parse(#"{"op":"forget","id":"\#(board.uuidString.lowercased())"}"#), .forget(id: board))
        for bad in ["", "[]", "{}", #"{"op":"fly"}"#, #"{"op":"connect","id":"x"}"#, #"{"op":"write","id":"\#(board.uuidString)","data":"!!"}"#,
                    #"{"op":"scan"}"#] {
            XCTAssertThrowsError(try Command.parse(bad), bad)
        }
    }

    func testBadLineIsAnErrorNotACrash() {
        engine.handle(line: "not json")
        XCTAssertEqual(events("error").count, 1)
        engine.handle(line: "   ")
        XCTAssertEqual(lines.count, 1, "blank lines are ignored")
    }

    func testInfoParsing() {
        let i = BoardInfo.parse(Data("board=t4s3-panel;id=AA:BB:CC:3F:A9:C1;fw=0.4.0;paired=0".utf8))
        XCTAssertEqual(i.board, "t4s3-panel")
        XCTAssertEqual(i.boardID, "AA:BB:CC:3F:A9:C1")
        XCTAssertEqual(i.firmware, "0.4.0")
        XCTAssertEqual(i.paired, false)
        XCTAssertNil(BoardInfo.parse(Data("junk".utf8)).paired)
    }

    func testScanProbesInfoOnce() {
        engine.handle(.scan(on: true))
        XCTAssertTrue(radio.scanning)
        engine.radioFound(board, name: "deck-3fa9c1", rssi: -58)
        XCTAssertEqual(radio.opened, [board], "a found board is opened to read Info")
        engine.radioInfo(board, Data("board=round;id=b1;fw=0.4.0;paired=0".utf8))
        let found = events("found")
        XCTAssertEqual(found.count, 2)
        XCTAssertEqual(found[1]["board"] as? String, "round")
        XCTAssertEqual(found[1]["boardId"] as? String, "b1")
        XCTAssertEqual(found[1]["paired"] as? Bool, false)
        XCTAssertEqual(found[1]["name"] as? String, "deck-3fa9c1")
        XCTAssertEqual(radio.closed, [board], "the probe hangs up after reading Info")
        engine.radioFound(board, name: "deck-3fa9c1", rssi: -60)
        XCTAssertEqual(radio.opened.count, 1, "Info is read once per scan")
        engine.handle(.scan(on: false))
        XCTAssertFalse(radio.scanning)
    }

    func testProbeTimeout() {
        engine.handle(.scan(on: true))
        let other = UUID()
        radio.known.insert(other)
        engine.radioFound(board, name: "a", rssi: -50)
        engine.radioFound(other, name: "b", rssi: -50)
        XCTAssertEqual(radio.opened, [board], "one probe at a time")
        later.removeFirst()()
        XCTAssertEqual(radio.closed, [board])
        XCTAssertEqual(radio.opened, [board, other])
    }

    func testScanRefusedWhenOff() {
        engine.radioState(.off)
        lines.removeAll()
        engine.handle(.scan(on: true))
        XCTAssertFalse(radio.scanning)
        XCTAssertEqual(events("error").first?["message"] as? String, RadioState.off.message)
    }

    func connect(paired: Bool) {
        engine.handle(.connect(id: board))
        XCTAssertEqual(radio.opened.last, board)
        engine.radioInfo(board, Data("board=round;id=b1;fw=0.4.0;paired=\(paired ? 1 : 0)".utf8))
        XCTAssertEqual(radio.subscribed, [board])
    }

    /// TX is on (the link is encrypted now): Info is read again over it, and
    /// "connected" carries that answer.
    func confirm(_ info: String = "board=round;id=b1;fw=0.4.0;paired=1") {
        engine.radioSubscribed(board)
        XCTAssertEqual(radio.infoReads.last, board, "Info read again once TX is on")
        engine.radioInfo(board, Data(info.utf8))
    }

    func testConnectAskForPasskeyThenConnected() {
        connect(paired: false)
        XCTAssertEqual(events("passkey").count, 1, "a board in setup makes macOS ask for its code")
        XCTAssertEqual(events("connected").count, 0, "not connected before pairing finishes")
        engine.radioSubscribed(board)
        XCTAssertEqual(events("connected").count, 0, "not before Info is read over the encrypted link")
        engine.radioInfo(board, Data("board=round;id=b1;fw=0.4.0;paired=1".utf8))
        let c = events("connected")
        XCTAssertEqual(c.count, 1)
        XCTAssertEqual(c[0]["mtu"] as? Int, 182)
        XCTAssertEqual(c[0]["boardId"] as? String, "b1")
        XCTAssertEqual(c[0]["paired"] as? Bool, true, "the second read, after pairing")
        XCTAssertEqual(events("passkey").count, 1, "asked once")
    }

    func testConnectedReportsTheEncryptedInfo() {
        connect(paired: true)
        confirm("board=round;id=b2;fw=0.4.0;paired=1")
        XCTAssertEqual(events("connected").first?["boardId"] as? String, "b2", "what the board says over the encrypted link")
        XCTAssertEqual(radio.subscribed, [board], "subscribed once")
    }

    func testSubscribedTwiceReadsInfoOnce() {
        connect(paired: true)
        engine.radioSubscribed(board)
        engine.radioSubscribed(board)
        XCTAssertEqual(radio.infoReads, [board])
    }

    func testBondedBoardNeedsNoPasskey() {
        connect(paired: true)
        XCTAssertEqual(events("passkey").count, 0)
    }

    func testPairingFailure() {
        connect(paired: false)
        engine.radioClosed(board, .pairingFailed, detail: "Authentication is insufficient.")
        let d = events("disconnected")
        XCTAssertEqual(d.count, 1)
        XCTAssertEqual(d[0]["code"] as? String, "pairing-failed")
        XCTAssertTrue((d[0]["reason"] as! String).hasPrefix("pairing failed"))
        engine.handle(.write(id: board, data: Data([1])))
        XCTAssertEqual(events("error").count, 1, "no writes after the link is gone")
    }

    func testUnknownPeripheral() {
        let stranger = UUID()
        engine.handle(.connect(id: stranger))
        XCTAssertEqual(events("disconnected").first?["code"] as? String, "unknown-peripheral")
    }

    func testWritesAreCutToTheMTUAndWaitForRoom() {
        connect(paired: true)
        radio.mtu = 20
        let frame = Data((0..<50).map { UInt8($0) })
        engine.handle(.write(id: board, data: frame))
        XCTAssertTrue(radio.sent.isEmpty, "nothing goes out before TX is subscribed")
        radio.room = 2
        confirm()
        XCTAssertEqual(radio.sent.map(\.count), [20, 20], "cut to the largest write, until the radio is full")
        XCTAssertEqual(events("sent").last?["n"] as? Int, 40)
        radio.room = 5
        engine.radioReadyToSend(board)
        XCTAssertEqual(radio.sent.map(\.count), [20, 20, 10])
        XCTAssertEqual(radio.sent.reduce(Data(), +), frame, "the stream arrives whole and in order")
        XCTAssertEqual(events("sent").last?["n"] as? Int, 10)
        // Two writes in a row are one stream: no framing of our own.
        engine.handle(.write(id: board, data: Data([0xAA])))
        engine.handle(.write(id: board, data: Data([0xBB, 0x00])))
        XCTAssertEqual(radio.sent.suffix(2), [Data([0xAA]), Data([0xBB, 0x00])])
    }

    func testReceivedDataIsBase64() {
        connect(paired: true)
        confirm()
        engine.radioReceived(board, Data([0x01, 0x02, 0x00]))
        XCTAssertEqual(events("data").first?["data"] as? String, "AQIA")
    }

    func testDisconnectAndForget() {
        connect(paired: true)
        confirm()
        engine.handle(.disconnect(id: board))
        XCTAssertEqual(radio.closed, [board])
        engine.radioClosed(board, .cancelled)
        XCTAssertEqual(events("disconnected").first?["code"] as? String, "cancelled")
        engine.handle(.forget(id: board))
        XCTAssertEqual(radio.forgotten, [board])
        XCTAssertEqual(events("forgotten").count, 1)
    }

    func testBluetoothOffEndsLinks() {
        connect(paired: true)
        confirm()
        engine.radioState(.off)
        XCTAssertEqual(events("disconnected").first?["code"] as? String, "bluetooth-off")
        XCTAssertEqual(events("state").last?["state"] as? String, "off")
    }

    func testHelloBeforeTheSubscriptionIsConfirmed() {
        connect(paired: true)
        engine.radioSubscribed(board)
        engine.radioReceived(board, Data([0x05, 0x01, 0x00])) // the board's Hello, early
        XCTAssertEqual(events("data").count, 0, "nothing before connected")
        engine.radioInfo(board, Data("board=round;id=b1;fw=0.4.0;paired=1".utf8))
        XCTAssertEqual(lines.suffix(2).map { $0["ev"] as? String }, ["connected", "data"], "kept and passed on after connected")
        XCTAssertEqual(events("data").first?["data"] as? String, "BQEA")
    }

    func testRefusedWriteIsSentAgainInOrderAndCountedOnce() {
        connect(paired: true)
        radio.mtu = 20
        confirm()
        let frame = Data((0..<30).map { UInt8($0) })
        radio.room = 1
        engine.handle(.write(id: board, data: frame))
        XCTAssertEqual(radio.sent.map(\.count), [20])
        XCTAssertEqual(events("sent").last?["n"] as? Int, 20)
        // The board says "insufficient resources" (ATT 0x11) for that write.
        engine.radioWriteRefused(board, radio.sent.removeLast())
        radio.room = 5
        XCTAssertEqual(later.count, 1, "sent again after a moment")
        later.removeFirst()()
        XCTAssertEqual(radio.sent.reduce(Data(), +), frame, "the same bytes, in order")
        let total = events("sent").compactMap { $0["n"] as? Int }.reduce(0, +)
        XCTAssertEqual(total, 30, "each byte reported once")
    }

    func testConnectDuringProbeKeepsTheConnection() {
        engine.handle(.scan(on: true))
        engine.radioFound(board, name: "deck-1", rssi: -40)
        engine.handle(.connect(id: board))
        XCTAssertEqual(radio.opened, [board], "the probe's connection becomes the link")
        engine.radioInfo(board, Data("board=round;id=b1;fw=1;paired=0".utf8))
        XCTAssertEqual(radio.closed, [], "not hung up")
        XCTAssertEqual(radio.subscribed, [board])
    }
}
