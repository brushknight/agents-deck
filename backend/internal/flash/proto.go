package flash

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// The Espressif serial bootloader protocol, as the ROM of the ESP32-S3
// speaks it (docs.espressif.com, esptool, "Serial Protocol").
const (
	opFlashBegin      = 0x02
	opFlashData       = 0x03
	opFlashEnd        = 0x04
	opSync            = 0x08
	opWriteReg        = 0x09
	opReadReg         = 0x0a
	opSPISetParams    = 0x0b
	opSPIAttach       = 0x0d
	opChangeBaud      = 0x0f
	opFlashDeflBegin  = 0x10
	opFlashDeflData   = 0x11
	opFlashDeflEnd    = 0x12
	opSPIFlashMD5     = 0x13
	opGetSecurityInfo = 0x14

	checksumSeed = 0xEF
	// romBlock is the data block size for the ROM loader (esptool's
	// FLASH_WRITE_SIZE without the stub), for plain and compressed writes.
	romBlock   = 0x400
	sectorSize = 0x1000
	romBaud    = 115200

	invalidMessage = 0x05 // ROM error: invalid message (unknown command)
)

// SLIP framing.
const (
	slipEnd    = 0xC0
	slipEsc    = 0xDB
	slipEscEnd = 0xDC
	slipEscEsc = 0xDD
)

func slipEncode(p []byte) []byte {
	out := make([]byte, 0, len(p)+8)
	out = append(out, slipEnd)
	for _, b := range p {
		switch b {
		case slipEnd:
			out = append(out, slipEsc, slipEscEnd)
		case slipEsc:
			out = append(out, slipEsc, slipEscEsc)
		default:
			out = append(out, b)
		}
	}
	return append(out, slipEnd)
}

// slipDecoder turns a byte stream into frames. Bytes outside frames (the boot
// log, an app's output) are kept, last 4 KiB, to explain a failed connect.
type slipDecoder struct {
	in, esc bool
	buf     []byte
	noise   []byte
}

func (d *slipDecoder) feed(b byte) ([]byte, bool) {
	if !d.in {
		if b == slipEnd {
			d.in, d.esc, d.buf = true, false, d.buf[:0]
			return nil, false
		}
		d.noise = append(d.noise, b)
		if len(d.noise) > 4096 {
			d.noise = d.noise[len(d.noise)-4096:]
		}
		return nil, false
	}
	if d.esc {
		d.esc = false
		switch b {
		case slipEscEnd:
			d.buf = append(d.buf, slipEnd)
		case slipEscEsc:
			d.buf = append(d.buf, slipEsc)
		default: // not SLIP after all
			d.in = false
		}
		return nil, false
	}
	switch b {
	case slipEnd:
		if len(d.buf) == 0 {
			return nil, false // back-to-back delimiters: a new frame starts
		}
		d.in = false
		return append([]byte(nil), d.buf...), true
	case slipEsc:
		d.esc = true
	default:
		d.buf = append(d.buf, b)
	}
	return nil, false
}

func checksum(data []byte) uint32 {
	s := byte(checksumSeed)
	for _, b := range data {
		s ^= b
	}
	return uint32(s)
}

func le32(vs ...uint32) []byte {
	out := make([]byte, 4*len(vs))
	for i, v := range vs {
		binary.LittleEndian.PutUint32(out[4*i:], v)
	}
	return out
}

// ROMError is a command the ROM loader answered with a failure status.
type ROMError struct {
	What string
	Code byte
}

var romErrors = map[byte]string{
	0x01: "invalid input parameter", 0x02: "out of memory", 0x03: "failed to send",
	0x04: "failed to receive", 0x05: "invalid message", 0x06: "wrong result",
	0x07: "checksum error", 0x08: "flash write error", 0x09: "flash read error",
	0x0a: "flash read length error", 0x0b: "deflate error", 0x0c: "deflate Adler-32 error",
	0x0d: "deflate parameter error", 0x0e: "invalid RAM binary size", 0x0f: "invalid RAM binary address",
	0x64: "invalid parameter", 0x65: "invalid format", 0x66: "description too long",
	0x67: "bad encoding description", 0x69: "insufficient storage",
}

func (e *ROMError) Error() string {
	why := romErrors[e.Code]
	if why == "" {
		why = "unknown error"
	}
	return fmt.Sprintf("%s: the bootloader said %s (0x%02x)", e.What, why, e.Code)
}

var errNoReply = errors.New("no reply from the bootloader")

// conn is one conversation with the ROM loader.
type conn struct {
	ctx    context.Context
	p      serial.Port
	dec    slipDecoder
	frames [][]byte
	rbuf   []byte
	dtr    bool
}

func newConn(ctx context.Context, p serial.Port) (*conn, error) {
	c := &conn{ctx: ctx, p: p, rbuf: make([]byte, 4096)}
	// Short reads, so a cancelled context and deadlines are noticed quickly.
	if err := p.SetReadTimeout(tm.poll); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *conn) sleep(d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *conn) setDTR(v bool) error {
	c.dtr = v
	return c.p.SetDTR(v)
}

// setRTS writes DTR again after RTS: some Windows USB CDC drivers only send
// the line state when DTR is written.
func (c *conn) setRTS(v bool) error {
	if err := c.p.SetRTS(v); err != nil {
		return err
	}
	return c.p.SetDTR(c.dtr)
}

func (c *conn) resetInput() {
	c.frames = nil
	c.dec.in, c.dec.esc = false, false
}

// readFrame returns the next SLIP frame, or errNoReply at the deadline.
func (c *conn) readFrame(deadline time.Time) ([]byte, error) {
	for {
		if len(c.frames) > 0 {
			f := c.frames[0]
			c.frames = c.frames[1:]
			return f, nil
		}
		if err := c.ctx.Err(); err != nil {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, errNoReply
		}
		n, err := c.p.Read(c.rbuf)
		if err != nil && !errors.Is(err, serial.ErrTimeout) {
			return nil, err
		}
		for _, b := range c.rbuf[:n] {
			if f, ok := c.dec.feed(b); ok {
				c.frames = append(c.frames, f)
			}
		}
	}
}

func (c *conn) send(op byte, data []byte, chk uint32) error {
	pkt := make([]byte, 8, 8+len(data))
	pkt[0], pkt[1] = 0x00, op
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(data)))
	binary.LittleEndian.PutUint32(pkt[4:], chk)
	pkt = append(pkt, data...)
	_, err := c.p.Write(slipEncode(pkt))
	return err
}

// command sends a request and waits for the response to it: (value, data).
func (c *conn) command(op byte, data []byte, chk uint32, timeout time.Duration) (uint32, []byte, error) {
	if err := c.send(op, data, chk); err != nil {
		return 0, nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := c.readFrame(deadline)
		if err != nil {
			return 0, nil, err
		}
		if len(f) < 8 || f[0] != 0x01 {
			continue
		}
		body := f[8:]
		if f[1] == op {
			return binary.LittleEndian.Uint32(f[4:]), body, nil
		}
	}
}

// check runs a command and checks its status bytes, which follow respLen
// bytes of data (the ROM sends 4 status bytes: status, error, 2 reserved).
func (c *conn) check(what string, op byte, data []byte, chk uint32, respLen int, timeout time.Duration) (uint32, []byte, error) {
	val, body, err := c.command(op, data, chk, timeout)
	if err != nil {
		if errors.Is(err, errNoReply) {
			return 0, nil, fmt.Errorf("%s: %w", what, err)
		}
		return 0, nil, err
	}
	if len(body) < respLen+2 {
		if len(body) >= 2 && body[0] != 0 {
			return 0, nil, &ROMError{What: what, Code: body[1]}
		}
		return 0, nil, fmt.Errorf("%s: short reply (%d bytes)", what, len(body))
	}
	if body[respLen] != 0 {
		return 0, nil, &ROMError{What: what, Code: body[respLen+1]}
	}
	return val, body[:respLen], nil
}

func (c *conn) readReg(addr uint32) (uint32, error) {
	v, _, err := c.check(fmt.Sprintf("read register 0x%08x", addr), opReadReg, le32(addr), 0, 0, tm.cmd)
	return v, err
}

func (c *conn) writeReg(addr, value, mask uint32) error {
	_, _, err := c.check(fmt.Sprintf("write register 0x%08x", addr), opWriteReg, le32(addr, value, mask, 0), 0, 0, tm.cmd)
	return err
}

// timeoutPerMB scales a timeout with the data size, never under tm.cmd.
func timeoutPerMB(perMB time.Duration, size int64) time.Duration {
	d := time.Duration(float64(perMB) * float64(size) / 1e6)
	if d < tm.cmd {
		return tm.cmd
	}
	return d
}
