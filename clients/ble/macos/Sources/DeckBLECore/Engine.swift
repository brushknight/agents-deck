import Foundation

/// The radio as the engine sees it: CoreBluetooth in the helper, a fake in
/// tests. Every call and callback happens on one queue.
public protocol Radio: AnyObject {
    /// Look for boards advertising the Deck Link service (or stop).
    func scan(_ on: Bool)
    /// Connect, find the Deck Link service and its characteristics, and read
    /// Info (Engine.radioInfo). false: this computer doesn't know the
    /// peripheral (never seen in a scan, never paired).
    func open(_ id: UUID) -> Bool
    /// Turn on TX notifications. On a board that needs pairing, this is what
    /// makes macOS ask for the code (Engine.radioSubscribed or radioClosed).
    func subscribe(_ id: UUID)
    /// Disconnect (Engine.radioClosed follows with .cancelled).
    func close(_ id: UUID)
    /// Drop every reference to the peripheral.
    func forget(_ id: UUID)
    /// Whether a write without response fits now (canSendWriteWithoutResponse).
    func canSend(_ id: UUID) -> Bool
    /// The largest write without response (maximumWriteValueLength).
    func maxWrite(_ id: UUID) -> Int
    /// Write to RX without response.
    func send(_ id: UUID, _ data: Data)
}

/// Engine is the helper's logic between the line protocol and the radio:
/// scanning and reading boards' Info, links, the write queue.
public final class Engine {
    public let radio: Radio
    /// Where event lines go (stdout in the helper).
    public let emit: (String) -> Void
    /// Runs a block later on the engine's queue (DispatchQueue.asyncAfter in
    /// the helper; tests run it by hand).
    public var schedule: (TimeInterval, @escaping () -> Void) -> Void = { _, _ in }

    /// How long reading a board's Info during a scan may take.
    public var probeTimeout: TimeInterval = 10

    public private(set) var state: RadioState = .unknown
    public private(set) var scanning = false

    struct Link {
        var info: BoardInfo?
        var subscribed = false
        var outbox = Data()
    }
    var links: [UUID: Link] = [:]
    var probeQueue: [UUID] = []
    var probing: UUID?
    var probed: Set<UUID> = []
    var seen: [UUID: (name: String, rssi: Int)] = [:]
    var probeGen = 0

    public init(radio: Radio, emit: @escaping (String) -> Void) {
        self.radio = radio
        self.emit = emit
    }

    func send(_ e: Event) { emit(e.line()) }

    /// Start: the first line the service reads.
    public func start() {
        send(.ready)
    }

    // MARK: lines from the service

    public func handle(line: String) {
        let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return }
        let cmd: Command
        do {
            cmd = try Command.parse(trimmed)
        } catch let e as ProtocolError {
            send(.error(id: nil, e.message))
            return
        } catch {
            send(.error(id: nil, "\(error)"))
            return
        }
        handle(cmd)
    }

    public func handle(_ cmd: Command) {
        switch cmd {
        case .state:
            send(.state(state))
            send(.scanning(scanning))
        case .scan(let on):
            if on && state != .on {
                send(.error(id: nil, state.message))
                send(.scanning(false))
                return
            }
            scanning = on
            radio.scan(on)
            if on {
                probed.removeAll()
                seen.removeAll()
            } else {
                probeQueue.removeAll()
            }
            send(.scanning(on))
        case .connect(let id):
            if state != .on {
                send(.disconnected(id: id, .bluetoothOff, detail: state.message))
                return
            }
            if let l = links[id] {
                if l.subscribed { send(.connected(id: id, mtu: radio.maxWrite(id), info: l.info)) }
                return // already on its way
            }
            links[id] = Link()
            probeQueue.removeAll { $0 == id }
            send(.connecting(id: id))
            if probing == id { return } // the probe's connection carries on as the link
            if !radio.open(id) {
                links[id] = nil
                send(.disconnected(id: id, .unknownPeripheral, detail: ""))
            }
        case .write(let id, let data):
            guard links[id] != nil else {
                send(.error(id: id, "not connected to that board"))
                return
            }
            links[id]!.outbox.append(data)
            pump(id)
        case .disconnect(let id):
            if links[id] != nil {
                radio.close(id) // radioClosed reports it
            } else {
                send(.disconnected(id: id, .cancelled, detail: ""))
            }
        case .forget(let id):
            if links[id] != nil || probing == id { radio.close(id) }
            links[id] = nil
            probeQueue.removeAll { $0 == id }
            probed.remove(id)
            radio.forget(id)
            send(.forgotten(id: id))
        }
    }

    // MARK: the write queue

    /// pump sends what is queued for a link while the radio takes it, cut to
    /// the largest write it allows, and tells the service how much went out.
    func pump(_ id: UUID) {
        guard var l = links[id], l.subscribed else { return }
        var n = 0
        while !l.outbox.isEmpty && radio.canSend(id) {
            let size = max(20, radio.maxWrite(id)) // 20: the smallest ATT payload
            let chunk = l.outbox.prefix(size)
            radio.send(id, Data(chunk))
            l.outbox.removeFirst(chunk.count)
            n += chunk.count
        }
        links[id] = l
        if n > 0 { send(.sent(id: id, n)) }
    }

    // MARK: callbacks from the radio

    public func radioState(_ s: RadioState) {
        let was = state
        state = s
        if s != was { send(.state(s)) }
        if s != .on {
            for id in links.keys.sorted(by: { $0.uuidString < $1.uuidString }) {
                send(.disconnected(id: id, .bluetoothOff, detail: s.message))
            }
            links.removeAll()
            probeQueue.removeAll()
            probing = nil
            if scanning {
                scanning = false
                send(.scanning(false))
            }
        }
    }

    public func radioFound(_ id: UUID, name: String, rssi: Int) {
        guard scanning else { return }
        let first = seen[id] == nil
        seen[id] = (name, rssi)
        if first || links[id] != nil || probed.contains(id) {
            send(.found(id: id, name: name, rssi: rssi, info: nil))
        }
        if links[id] == nil && !probed.contains(id) && probing != id && !probeQueue.contains(id) {
            probeQueue.append(id)
            nextProbe()
        }
    }

    func nextProbe() {
        guard probing == nil, scanning, !probeQueue.isEmpty else { return }
        let id = probeQueue.removeFirst()
        probing = id
        probed.insert(id)
        probeGen += 1
        let gen = probeGen
        if !radio.open(id) {
            probing = nil
            nextProbe()
            return
        }
        schedule(probeTimeout) { [weak self] in
            guard let self = self, self.probing == id, self.probeGen == gen else { return }
            if self.links[id] == nil { self.radio.close(id) }
            self.probing = nil
            self.nextProbe()
        }
    }

    public func radioInfo(_ id: UUID, _ raw: Data) {
        let info = BoardInfo.parse(raw)
        if links[id] != nil {
            links[id]!.info = info
            if info.paired != true { send(.passkey(id: id)) }
            radio.subscribe(id)
            if probing == id { probing = nil; nextProbe() }
            return
        }
        if probing == id {
            let s = seen[id] ?? ("", 0)
            send(.found(id: id, name: s.name, rssi: s.rssi, info: info))
            radio.close(id)
            probing = nil
            nextProbe()
        }
    }

    public func radioSubscribed(_ id: UUID) {
        guard links[id] != nil else { return }
        links[id]!.subscribed = true
        send(.connected(id: id, mtu: radio.maxWrite(id), info: links[id]!.info))
        pump(id)
    }

    public func radioReceived(_ id: UUID, _ data: Data) {
        guard let l = links[id], l.subscribed, !data.isEmpty else { return }
        send(.data(id: id, data))
    }

    public func radioReadyToSend(_ id: UUID) {
        pump(id)
    }

    public func radioClosed(_ id: UUID, _ why: LinkProblem, detail: String = "") {
        if links[id] != nil {
            links[id] = nil
            send(.disconnected(id: id, why, detail: detail))
        }
        if probing == id {
            probing = nil
            nextProbe()
        }
    }
}
