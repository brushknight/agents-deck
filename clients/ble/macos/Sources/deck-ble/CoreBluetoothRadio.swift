import CoreBluetooth
import DeckBLECore
import Foundation

/// CoreBluetoothRadio is the Radio on macOS: a CBCentralManager on the
/// helper's queue. macOS itself runs pairing: when the helper turns on TX
/// notifications and the board answers "insufficient authentication", macOS
/// pairs, shows its own window for the code, and keeps the bond. The helper
/// never sees the code.
final class CoreBluetoothRadio: NSObject, Radio, CBCentralManagerDelegate, CBPeripheralDelegate {
    weak var engine: Engine?
    private var central: CBCentralManager!
    private var peripherals: [UUID: CBPeripheral] = [:]
    private var rx: [UUID: CBCharacteristic] = [:]
    private var tx: [UUID: CBCharacteristic] = [:]
    private var closing: Set<UUID> = []
    private let service = CBUUID(string: DeckGATT.service)
    private let rxID = CBUUID(string: DeckGATT.rx)
    private let txID = CBUUID(string: DeckGATT.tx)
    private let infoID = CBUUID(string: DeckGATT.info)

    init(queue: DispatchQueue) {
        super.init()
        central = CBCentralManager(delegate: self, queue: queue, options: [CBCentralManagerOptionShowPowerAlertKey: false])
    }

    // MARK: Radio

    func scan(_ on: Bool) {
        if on {
            central.scanForPeripherals(withServices: [service], options: [CBCentralManagerScanOptionAllowDuplicatesKey: false])
        } else {
            central.stopScan()
        }
    }

    func open(_ id: UUID) -> Bool {
        var p = peripherals[id]
        if p == nil {
            p = central.retrievePeripherals(withIdentifiers: [id]).first
        }
        guard let p = p else { return false }
        peripherals[id] = p
        p.delegate = self
        closing.remove(id)
        switch p.state {
        case .connected:
            if let info = p.services?.first(where: { $0.uuid == service })?.characteristics?.first(where: { $0.uuid == infoID }) {
                p.readValue(for: info)
            } else {
                p.discoverServices([service])
            }
        case .connecting:
            break
        default:
            // No timeout: a board out of range is connected to when it comes back.
            central.connect(p, options: nil)
        }
        return true
    }

    func subscribe(_ id: UUID) {
        guard let p = peripherals[id], let c = tx[id] else {
            engine?.radioClosed(id, .notADeck)
            return
        }
        p.setNotifyValue(true, for: c)
    }

    func close(_ id: UUID) {
        guard let p = peripherals[id] else {
            engine?.radioClosed(id, .cancelled)
            return
        }
        closing.insert(id)
        if p.state == .disconnected {
            engine?.radioClosed(id, .cancelled)
            return
        }
        central.cancelPeripheralConnection(p)
    }

    func forget(_ id: UUID) {
        peripherals[id] = nil
        rx[id] = nil
        tx[id] = nil
        closing.remove(id)
    }

    func canSend(_ id: UUID) -> Bool {
        guard let p = peripherals[id], let c = rx[id] else { return false }
        if !c.properties.contains(.writeWithoutResponse) { return pendingWrite[id] == nil }
        return p.canSendWriteWithoutResponse
    }

    func maxWrite(_ id: UUID) -> Int {
        guard let p = peripherals[id] else { return 20 }
        let type: CBCharacteristicWriteType = (rx[id]?.properties.contains(.writeWithoutResponse) ?? true) ? .withoutResponse : .withResponse
        return p.maximumWriteValueLength(for: type)
    }

    /// Boards whose RX only takes writes with response: one write in flight.
    private var pendingWrite: [UUID: Bool] = [:]

    func send(_ id: UUID, _ data: Data) {
        guard let p = peripherals[id], let c = rx[id] else { return }
        if c.properties.contains(.writeWithoutResponse) {
            p.writeValue(data, for: c, type: .withoutResponse)
        } else {
            pendingWrite[id] = true
            p.writeValue(data, for: c, type: .withResponse)
        }
    }

    // MARK: CBCentralManagerDelegate

    func centralManagerDidUpdateState(_ c: CBCentralManager) {
        let s: RadioState
        switch c.state {
        case .poweredOn: s = .on
        case .poweredOff: s = .off
        case .unauthorized: s = .unauthorized
        case .unsupported: s = .unsupported
        case .resetting: s = .resetting
        default: s = .unknown
        }
        engine?.radioState(s)
    }

    func centralManager(_ c: CBCentralManager, didDiscover p: CBPeripheral, advertisementData: [String: Any], rssi: NSNumber) {
        if peripherals[p.identifier] == nil {
            peripherals[p.identifier] = p
        }
        let name = (advertisementData[CBAdvertisementDataLocalNameKey] as? String) ?? p.name ?? ""
        let r = rssi.intValue == 127 ? 0 : rssi.intValue // 127: not available
        engine?.radioFound(p.identifier, name: name, rssi: r)
    }

    func centralManager(_ c: CBCentralManager, didConnect p: CBPeripheral) {
        p.delegate = self
        p.discoverServices([service])
    }

    func centralManager(_ c: CBCentralManager, didFailToConnect p: CBPeripheral, error: Error?) {
        engine?.radioClosed(p.identifier, problem(error, fallback: .timeout), detail: error?.localizedDescription ?? "")
    }

    func centralManager(_ c: CBCentralManager, didDisconnectPeripheral p: CBPeripheral, error: Error?) {
        let id = p.identifier
        rx[id] = nil
        tx[id] = nil
        pendingWrite[id] = nil
        if closing.remove(id) != nil {
            engine?.radioClosed(id, .cancelled)
            return
        }
        engine?.radioClosed(id, problem(error, fallback: .gone), detail: error?.localizedDescription ?? "")
    }

    // MARK: CBPeripheralDelegate

    func peripheral(_ p: CBPeripheral, didDiscoverServices error: Error?) {
        guard error == nil, let s = p.services?.first(where: { $0.uuid == service }) else {
            failed(p, .notADeck, error)
            return
        }
        p.discoverCharacteristics([rxID, txID, infoID], for: s)
    }

    func peripheral(_ p: CBPeripheral, didDiscoverCharacteristicsFor s: CBService, error: Error?) {
        let cs = s.characteristics ?? []
        guard error == nil,
              let r = cs.first(where: { $0.uuid == rxID }),
              let t = cs.first(where: { $0.uuid == txID }),
              let i = cs.first(where: { $0.uuid == infoID }) else {
            failed(p, .notADeck, error)
            return
        }
        rx[p.identifier] = r
        tx[p.identifier] = t
        p.readValue(for: i)
    }

    func peripheral(_ p: CBPeripheral, didUpdateValueFor c: CBCharacteristic, error: Error?) {
        if let error = error {
            if c.uuid == infoID { failed(p, problem(error, fallback: .error), error) }
            return
        }
        guard let v = c.value else { return }
        if c.uuid == infoID {
            engine?.radioInfo(p.identifier, v)
        } else if c.uuid == txID {
            engine?.radioReceived(p.identifier, v)
        }
    }

    func peripheral(_ p: CBPeripheral, didUpdateNotificationStateFor c: CBCharacteristic, error: Error?) {
        guard c.uuid == txID else { return }
        if let error = error {
            // Pairing failed, was cancelled or timed out: the board keeps TX
            // closed to anyone who isn't paired with a passkey.
            failed(p, problem(error, fallback: .pairingFailed), error)
            return
        }
        if c.isNotifying {
            engine?.radioSubscribed(p.identifier)
        }
    }

    func peripheralIsReady(toSendWriteWithoutResponse p: CBPeripheral) {
        engine?.radioReadyToSend(p.identifier)
    }

    func peripheral(_ p: CBPeripheral, didWriteValueFor c: CBCharacteristic, error: Error?) {
        pendingWrite[p.identifier] = nil
        if let error = error {
            failed(p, problem(error, fallback: .error), error)
            return
        }
        engine?.radioReadyToSend(p.identifier)
    }

    // MARK: errors

    /// failed reports a problem and hangs up.
    private func failed(_ p: CBPeripheral, _ why: LinkProblem, _ error: Error?) {
        let id = p.identifier
        engine?.radioClosed(id, why, detail: error?.localizedDescription ?? "")
        if p.state != .disconnected {
            central.cancelPeripheralConnection(p)
        }
        closing.insert(id) // the disconnect that follows was already reported
    }

    private func problem(_ error: Error?, fallback: LinkProblem) -> LinkProblem {
        guard let error = error as NSError? else { return fallback }
        if error.domain == CBATTErrorDomain {
            switch CBATTError.Code(rawValue: error.code) {
            case .insufficientAuthentication, .insufficientEncryption, .insufficientEncryptionKeySize, .insufficientAuthorization:
                return .pairingFailed
            default:
                return fallback
            }
        }
        if error.domain == CBErrorDomain {
            switch CBError.Code(rawValue: error.code) {
            case .peerRemovedPairingInformation: return .pairingRemoved
            case .encryptionTimedOut: return .pairingTimeout
            case .connectionTimeout: return .timeout
            case .peripheralDisconnected: return .gone
            case .unknownDevice: return .unknownPeripheral
            default: return fallback
            }
        }
        return fallback
    }
}
