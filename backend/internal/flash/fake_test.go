package flash

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// fakeBoard is a simulated ESP32-S3 on a serial port: its reset circuit
// (DTR/RTS), the boot log, and the ROM bootloader with a flash array.
type fakeBoard struct {
	mu     sync.Mutex
	notify chan struct{}
	out    []byte // board to host
	closed bool

	readTimeout time.Duration
	hostBaud    int
	chipBaud    int

	// Reset wiring: classic two-transistor auto-reset, or the S3's own
	// USB-Serial-JTAG.
	usbJTAG          bool
	dtr, rts         bool
	inReset, latched bool
	download         bool // in the ROM bootloader
	pending          *time.Timer
	resets           int

	// What the board is.
	neverDownload bool   // reset works, but it always starts the app (no auto-reset wiring)
	silent        bool   // not even a boot log
	chipID        uint32 // GET_SECURITY_INFO chip id
	noSecInfo     bool   // a chip without GET_SECURITY_INFO (ESP32): magic register instead
	magic         uint32
	secureDL      bool
	refuseDefl    bool // FLASH_DEFL_BEGIN fails
	corrupt       bool // flip a byte before MD5
	regs          map[uint32]uint32
	flash         []byte
	flashID       uint32

	// Write state.
	writeOffset, writeSize, blocks, nextSeq int
	defl                                    bytes.Buffer
	deflating                               bool
	setParams                               uint32

	dec    slipDecoder
	ops    []byte
	onOp   func(op byte)
	failOp byte // answer this command with an error
}

func newFakeBoard() *fakeBoard {
	b := &fakeBoard{
		notify:   make(chan struct{}, 1),
		hostBaud: 115200, chipBaud: 115200,
		chipID:  s3ChipID,
		magic:   0x00000009,
		flash:   bytes.Repeat([]byte{0xFF}, 4<<20),
		flashID: 0x1640EF, // 4 MB
		regs: map[uint32]uint32{
			efuseMACReg:     0x33445566,
			efuseMACReg + 4: 0x00001122,
			uartDevBufNo:    0,
		},
	}
	return b
}

var _ serial.Port = (*fakeBoard)(nil)

func (b *fakeBoard) emit(p []byte) {
	b.out = append(b.out, p...)
	select {
	case b.notify <- struct{}{}:
	default:
	}
}

func (b *fakeBoard) Read(p []byte) (int, error) {
	b.mu.Lock()
	timeout := b.readTimeout
	b.mu.Unlock()
	var deadline <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		deadline = t.C
	}
	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return 0, os.ErrClosed
		}
		if len(b.out) > 0 {
			n := copy(p, b.out)
			b.out = b.out[n:]
			b.mu.Unlock()
			return n, nil
		}
		b.mu.Unlock()
		select {
		case <-b.notify:
		case <-deadline:
			return 0, serial.ErrTimeout
		}
	}
}

func (b *fakeBoard) Write(p []byte) (int, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, os.ErrClosed
	}
	var ops []byte
	if b.download && b.hostBaud == b.chipBaud {
		for _, c := range p {
			if f, ok := b.dec.feed(c); ok && len(f) >= 8 && f[0] == 0 {
				ops = append(ops, f[1])
				b.ops = append(b.ops, f[1])
				b.handle(f)
			}
		}
	}
	hook := b.onOp
	b.mu.Unlock()
	if hook != nil {
		for _, op := range ops {
			hook(op)
		}
	}
	return len(p), nil
}

func (b *fakeBoard) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	return nil
}

func (b *fakeBoard) SetReadTimeout(d time.Duration) error {
	b.mu.Lock()
	b.readTimeout = d
	b.mu.Unlock()
	return nil
}

func (b *fakeBoard) SetBaud(baud int) error {
	b.mu.Lock()
	b.hostBaud = baud
	b.mu.Unlock()
	return nil
}

func (b *fakeBoard) SetDTR(v bool) error { return b.lines(&b.dtr, v) }
func (b *fakeBoard) SetRTS(v bool) error { return b.lines(&b.rts, v) }

func (b *fakeBoard) lines(line *bool, v bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	*line = v
	if b.usbJTAG {
		// USB-Serial-JTAG: DTR alone latches "boot to download", RTS alone
		// holds the chip in reset; leaving reset boots.
		if b.dtr && !b.rts {
			b.latched = true
		}
		if !b.dtr && b.rts {
			b.inReset = true
			return nil
		}
		if b.inReset {
			b.boot(b.latched)
		}
		return nil
	}
	// Classic: EN low when RTS without DTR; IO0 low when DTR without RTS.
	// EN rises slowly (an RC on the board), so the chip samples IO0 a
	// moment after EN is let go.
	if b.rts && !b.dtr {
		b.inReset = true
		if b.pending != nil {
			b.pending.Stop()
			b.pending = nil
		}
		return nil
	}
	if b.inReset && b.pending == nil {
		b.pending = time.AfterFunc(5*time.Millisecond, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.pending = nil
			if b.inReset && !(b.rts && !b.dtr) {
				b.boot(b.dtr && !b.rts)
			}
		})
	}
	return nil
}

func (b *fakeBoard) boot(download bool) {
	b.inReset, b.latched = false, false
	b.resets++
	b.chipBaud = 115200
	b.deflating = false
	b.dec = slipDecoder{}
	if b.silent {
		b.download = false
		return
	}
	if download && !b.neverDownload {
		b.download = true
		b.emit([]byte("ESP-ROM:esp32s3-20210327\r\nrst:0x1 (POWERON),boot:0x0 (DOWNLOAD(USB/UART0))\r\nwaiting for download\r\n"))
		return
	}
	b.download = false
	b.emit([]byte("ESP-ROM:esp32s3-20210327\r\nrst:0x1 (POWERON),boot:0x8 (SPI_FAST_FLASH_BOOT)\r\napp says hello\r\n"))
}

// reply sends a response: value, data, then the ROM's 4 status bytes.
func (b *fakeBoard) reply(op byte, val uint32, data []byte, errCode byte) {
	status := []byte{0, 0, 0, 0}
	if errCode != 0 {
		status[0], status[1] = 1, errCode
	}
	body := append(append([]byte(nil), data...), status...)
	pkt := make([]byte, 8, 8+len(body))
	pkt[0], pkt[1] = 1, op
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(body)))
	binary.LittleEndian.PutUint32(pkt[4:], val)
	b.emit(slipEncode(append(pkt, body...)))
}

func (b *fakeBoard) handle(f []byte) {
	op := f[1]
	n := int(binary.LittleEndian.Uint16(f[2:]))
	chk := binary.LittleEndian.Uint32(f[4:])
	data := f[8:]
	if len(data) != n {
		b.reply(op, 0, nil, invalidMessage)
		return
	}
	word := func(i int) uint32 { return binary.LittleEndian.Uint32(data[4*i:]) }
	if op == b.failOp {
		b.reply(op, 0, nil, 0x06)
		return
	}
	switch op {
	case opSync:
		for i := 0; i < 8; i++ {
			b.reply(op, 0x20120707, nil, 0)
		}
	case opGetSecurityInfo:
		if b.noSecInfo {
			b.reply(op, 0, nil, invalidMessage)
			return
		}
		var flags uint32
		if b.secureDL {
			flags |= secureDLFlag
		}
		info := append(le32(flags), make([]byte, 8)...)
		info = append(info, le32(b.chipID, 0)...)
		b.reply(op, 0, info, 0)
	case opReadReg:
		addr := word(0)
		if addr == chipMagicReg {
			b.reply(op, b.magic, nil, 0)
			return
		}
		b.reply(op, b.regs[addr], nil, 0)
	case opWriteReg:
		for i := 0; i+16 <= len(data); i += 16 {
			addr, val, mask := binary.LittleEndian.Uint32(data[i:]), binary.LittleEndian.Uint32(data[i+4:]), binary.LittleEndian.Uint32(data[i+8:])
			b.regs[addr] = b.regs[addr]&^mask | val&mask
			if addr == spiCmdReg && val&spiCmdUsr != 0 {
				if b.regs[spiUsr2Reg]&0xff == flashRDID {
					b.regs[spiW0Reg] = b.flashID
				}
				b.regs[spiCmdReg] &^= spiCmdUsr
			}
		}
		b.reply(op, 0, nil, 0)
	case opSPIAttach:
		if len(data) != 8 {
			b.reply(op, 0, nil, 0x01)
			return
		}
		b.reply(op, 0, nil, 0)
	case opSPISetParams:
		b.setParams = word(1)
		b.reply(op, 0, nil, 0)
	case opChangeBaud:
		b.reply(op, 0, nil, 0)
		b.chipBaud = int(word(0))
	case opFlashBegin, opFlashDeflBegin:
		if op == opFlashDeflBegin && b.refuseDefl {
			b.reply(op, 0, nil, invalidMessage)
			return
		}
		size, blocks, blockSize, offset := int(word(0)), int(word(1)), int(word(2)), int(word(3))
		if len(data) != 20 || blockSize != romBlock || offset+size > len(b.flash) {
			b.reply(op, 0, nil, 0x01)
			return
		}
		// Flash erases whole 4 KiB sectors.
		for i := offset / sectorSize * sectorSize; i < (offset+size+sectorSize-1)/sectorSize*sectorSize; i++ {
			b.flash[i] = 0xFF
		}
		b.writeOffset, b.writeSize, b.blocks, b.nextSeq = offset, size, blocks, 0
		b.deflating = op == opFlashDeflBegin
		b.defl.Reset()
		b.reply(op, 0, nil, 0)
	case opFlashData, opFlashDeflData:
		if len(data) < 16 {
			b.reply(op, 0, nil, invalidMessage)
			return
		}
		ln, seq := int(word(0)), int(word(1))
		payload := data[16:]
		switch {
		case ln != len(payload) || seq != b.nextSeq || (op == opFlashDeflData) != b.deflating:
			b.reply(op, 0, nil, invalidMessage)
			return
		case checksum(payload) != chk:
			b.reply(op, 0, nil, 0x07)
			return
		}
		b.nextSeq++
		if op == opFlashData {
			copy(b.flash[b.writeOffset+seq*romBlock:], payload)
			b.reply(op, 0, nil, 0)
			return
		}
		b.defl.Write(payload)
		if b.nextSeq == b.blocks {
			r, err := zlib.NewReader(bytes.NewReader(b.defl.Bytes()))
			if err != nil {
				b.reply(op, 0, nil, 0x0b)
				return
			}
			plain, err := io.ReadAll(r)
			if errors.Is(err, zlib.ErrChecksum) {
				b.reply(op, 0, nil, 0x0c)
				return
			}
			if err != nil || len(plain) > b.writeSize {
				b.reply(op, 0, nil, 0x0b)
				return
			}
			copy(b.flash[b.writeOffset:], plain)
		}
		b.reply(op, 0, nil, 0)
	case opSPIFlashMD5:
		addr, size := int(word(0)), int(word(1))
		if addr+size > len(b.flash) {
			b.reply(op, 0, nil, 0x01)
			return
		}
		if b.corrupt {
			b.flash[addr] ^= 0x01
			b.corrupt = false
		}
		sum := md5.Sum(b.flash[addr : addr+size])
		b.reply(op, 0, []byte(hex.EncodeToString(sum[:])), 0)
	case opFlashEnd, opFlashDeflEnd:
		b.reply(op, 0, nil, 0)
		b.download = false
	default:
		b.reply(op, 0, nil, invalidMessage)
	}
}
