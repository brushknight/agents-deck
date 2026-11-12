// Package flash writes a firmware image to an ESP32-S3 through its ROM
// serial bootloader: no esptool, no Python, no stub loader.
package flash

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// Chip is what Probe found.
type Chip struct {
	Name      string // "ESP32-S3"
	MAC       string // aa:bb:cc:dd:ee:ff, from eFuse
	FlashSize int64  // bytes, from the flash's JEDEC id; 0 when it couldn't be read
}

// Progress reports a stage ("connect", "erase", "write", "verify", "reset")
// and how far it is, in bytes of the image (0, 0 when it has no size).
type Progress func(stage string, done, total int64)

// Options tune Probe and Write. The zero value is fine.
type Options struct {
	// VID and PID of the port (serial.Info), lower-case hex. They pick the
	// reset sequence: the S3's own USB (303a:1001) has its own. Unknown ids
	// try both.
	VID, PID string
	// Baud to write at once connected, on a USB serial bridge. 0 means
	// 460800; 115200 keeps the ROM's rate. Native USB ignores it.
	Baud int
}

// ErrVerify: what is in flash after writing is not the image.
var ErrVerify = errors.New("the flash doesn't hold the image after writing (MD5 differs)")

// Probe enters the ROM bootloader, reads the chip, and resets the board
// into its firmware again.
func Probe(ctx context.Context, p serial.Port) (Chip, error) {
	return ProbeWith(ctx, p, Options{})
}

// ProbeWith is Probe with options.
func ProbeWith(ctx context.Context, p serial.Port, o Options) (Chip, error) {
	c, err := newConn(ctx, p)
	if err != nil {
		return Chip{}, err
	}
	s, err := open(c, resetsFor(o.VID, o.PID))
	if err != nil {
		return Chip{}, err
	}
	chip := s.chip
	if err := s.leave(); err != nil {
		return chip, fmt.Errorf("reset the board: %w", err)
	}
	return chip, nil
}

// Write puts image (size bytes) into flash at offset, verifies it, and
// resets the board into it.
func Write(ctx context.Context, p serial.Port, image io.Reader, size int64, offset uint32, prog Progress) error {
	return WriteWith(ctx, p, image, size, offset, prog, Options{})
}

// WriteWith is Write with options.
func WriteWith(ctx context.Context, p serial.Port, image io.Reader, size int64, offset uint32, prog Progress, o Options) error {
	if prog == nil {
		prog = func(string, int64, int64) {}
	}
	if size <= 0 {
		return errors.New("the image is empty")
	}
	if offset%sectorSize != 0 {
		return fmt.Errorf("offset 0x%x: the ROM bootloader writes whole 4 KiB sectors, use a multiple of 0x1000", offset)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(image, data); err != nil {
		return fmt.Errorf("read the image: %w", err)
	}
	prog("connect", 0, 0)
	c, err := newConn(ctx, p)
	if err != nil {
		return err
	}
	s, err := open(c, resetsFor(o.VID, o.PID))
	if err != nil {
		return err
	}
	flashSize := s.chip.FlashSize
	if flashSize > 0 && int64(offset)+size > flashSize {
		return fmt.Errorf("the image (%d bytes at 0x%x) doesn't fit in this board's %d MB flash", size, offset, flashSize>>20)
	}
	params := flashSize
	if params == 0 {
		params = 16 << 20 // unknown: the largest S3 module; the ROM only uses it as a bound
	}
	if _, _, err := s.check("set the flash parameters", opSPISetParams, le32(0, uint32(params), 64<<10, sectorSize, 256, 0xFFFF), 0, 0, tm.cmd); err != nil {
		return err
	}
	if !s.usbJTAG && !s.usbOTG {
		baud := o.Baud
		if baud == 0 {
			baud = 460800
		}
		if baud != romBaud {
			if err := s.changeBaud(baud); err != nil {
				return err
			}
		}
	}
	// A whole-flash image is written in pieces that leave the board's saved
	// settings (name, certificate, Wi-Fi, Bluetooth bond) where they are.
	parts := keepSettings(data, offset)
	var done int64
	for _, part := range parts {
		base, total := done, size
		pp := func(stage string, d, _ int64) { prog(stage, base+d, total) }
		if err := s.writeDeflate(part.data, part.offset, pp); err != nil {
			var re *ROMError
			if !errors.As(err, &re) || re.What != "begin the compressed write" {
				return err
			}
			// A ROM that won't take compressed data: write it plain.
			if err := s.writePlain(part.data, part.offset, pp); err != nil {
				return err
			}
		}
		done += int64(len(part.data))
	}
	prog("verify", 0, size)
	for _, part := range parts {
		if err := s.verify(part.data, part.offset); err != nil {
			return err
		}
	}
	prog("verify", size, size)
	prog("reset", 0, 0)
	if err := s.leave(); err != nil {
		return fmt.Errorf("written and verified, but the reset failed (unplug the board and plug it in again): %w", err)
	}
	return nil
}

// changeBaud: the ROM answers at the old rate, then listens at the new one.
func (s *session) changeBaud(baud int) error {
	if _, _, err := s.check("change the baud rate", opChangeBaud, le32(uint32(baud), 0), 0, 0, tm.cmd); err != nil {
		return err
	}
	if err := s.p.SetBaud(baud); err != nil {
		return fmt.Errorf("set the port to %d baud: %w", baud, err)
	}
	if err := s.sleep(tm.baudSettle); err != nil {
		return err
	}
	s.resetInput()
	if _, err := s.readReg(chipMagicReg); err != nil {
		return fmt.Errorf("no answer at %d baud (try --baud 115200): %w", baud, err)
	}
	return nil
}

// checkpoint maps a position in the compressed stream to the image bytes
// fully contained before it.
type checkpoint struct{ comp, plain int64 }

// compress deflates data in zlib format (the ROM checks the Adler-32),
// flushing every 4 KiB so each sent block's share of the image is known.
func compress(data []byte) ([]byte, []checkpoint, error) {
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, nil, err
	}
	var cps []checkpoint
	for i := 0; i < len(data); i += sectorSize {
		end := min(i+sectorSize, len(data))
		if _, err := w.Write(data[i:end]); err != nil {
			return nil, nil, err
		}
		if err := w.Flush(); err != nil {
			return nil, nil, err
		}
		cps = append(cps, checkpoint{int64(buf.Len()), int64(end)})
	}
	if err := w.Close(); err != nil {
		return nil, nil, err
	}
	cps = append(cps, checkpoint{int64(buf.Len()), int64(len(data))})
	return buf.Bytes(), cps, nil
}

// plainBefore: image bytes certainly written once comp compressed bytes are in.
func plainBefore(cps []checkpoint, comp int64) int64 {
	var p int64
	for _, c := range cps {
		if c.comp > comp {
			break
		}
		p = c.plain
	}
	return p
}

// plainAfter: image bytes that comp compressed bytes can at most produce.
func plainAfter(cps []checkpoint, comp int64) int64 {
	for _, c := range cps {
		if c.comp >= comp {
			return c.plain
		}
	}
	return cps[len(cps)-1].plain
}

func (s *session) writeDeflate(data []byte, offset uint32, prog Progress) error {
	size := int64(len(data))
	comp, cps, err := compress(data)
	if err != nil {
		return err
	}
	blocks := (len(comp) + romBlock - 1) / romBlock
	// The ROM erases up front, the image size rounded up to whole blocks.
	erase := (len(data) + romBlock - 1) / romBlock * romBlock
	prog("erase", 0, size)
	if _, _, err := s.check("begin the compressed write", opFlashDeflBegin,
		le32(uint32(erase), uint32(blocks), romBlock, offset, 0), 0, 0, timeoutPerMB(tm.erasePerMB, int64(erase))); err != nil {
		return err
	}
	prog("erase", size, size)
	prog("write", 0, size)
	for seq := 0; seq < blocks; seq++ {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		lo, hi := seq*romBlock, min((seq+1)*romBlock, len(comp))
		block := comp[lo:hi]
		plain := plainAfter(cps, int64(hi)) - plainBefore(cps, int64(lo))
		pkt := append(le32(uint32(len(block)), uint32(seq), 0, 0), block...)
		if _, _, err := s.check(fmt.Sprintf("write block %d of %d", seq+1, blocks), opFlashDeflData, pkt, checksum(block), 0, timeoutPerMB(tm.writePerMB, plain)); err != nil {
			return err
		}
		prog("write", plainBefore(cps, int64(hi)), size)
	}
	prog("write", size, size)
	// No FLASH_DEFL_END: on the ROM it leaves the bootloader, and the MD5
	// check comes first.
	return nil
}

func (s *session) writePlain(data []byte, offset uint32, prog Progress) error {
	size := int64(len(data))
	blocks := (len(data) + romBlock - 1) / romBlock
	prog("erase", 0, size)
	if _, _, err := s.check("begin the write", opFlashBegin,
		le32(uint32(len(data)), uint32(blocks), romBlock, offset, 0), 0, 0, timeoutPerMB(tm.erasePerMB, size)); err != nil {
		return err
	}
	prog("erase", size, size)
	for seq := 0; seq < blocks; seq++ {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		lo, hi := seq*romBlock, min((seq+1)*romBlock, len(data))
		block := make([]byte, romBlock)
		copy(block, data[lo:hi])
		for i := hi - lo; i < romBlock; i++ {
			block[i] = 0xFF // the last block, padded as erased flash
		}
		pkt := append(le32(romBlock, uint32(seq), 0, 0), block...)
		if _, _, err := s.check(fmt.Sprintf("write block %d of %d", seq+1, blocks), opFlashData, pkt, checksum(block), 0, timeoutPerMB(tm.writePerMB, romBlock)); err != nil {
			return err
		}
		prog("write", int64(hi), size)
	}
	return nil
}

// verify asks the ROM for the MD5 of the written region (32 hex digits).
func (s *session) verify(data []byte, offset uint32) error {
	size := int64(len(data))
	_, res, err := s.check("read back the MD5", opSPIFlashMD5, le32(offset, uint32(size), 0, 0), 0, 32, timeoutPerMB(tm.md5PerMB, size))
	if err != nil {
		return err
	}
	sum := md5.Sum(data)
	got := strings.ToLower(string(res))
	if got == hex.EncodeToString(sum[:]) {
		return nil
	}
	empty := md5.Sum(bytes.Repeat([]byte{0xFF}, len(data)))
	if got == hex.EncodeToString(empty[:]) {
		return fmt.Errorf("%w: the region is still empty", ErrVerify)
	}
	return ErrVerify
}

// part is one stretch of an image and where it goes in flash.
type part struct {
	data   []byte
	offset uint32
}

// keepSettings cuts a whole-flash image (one written at 0x0, holding the
// bootloader, the partition table and the app) into the pieces to write,
// leaving out the board's settings partitions where the image only has
// padding there. A merged image fills the gap between the partition table
// and the app with 0xFF, and that gap is where the board keeps its name,
// certificate and Wi-Fi network (the "nvs" partition): writing the padding
// would erase them. Anything else is written as it is.
func keepSettings(data []byte, offset uint32) []part {
	whole := []part{{data, offset}}
	const tableAt, entry = 0x8000, 32
	if offset != 0 || len(data) < tableAt+entry {
		return whole
	}
	type span struct{ from, to int }
	var skip []span
	for at := tableAt; at+entry <= len(data) && at < tableAt+0xC00; at += entry {
		e := data[at : at+entry]
		if e[0] != 0xAA || e[1] != 0x50 { // end of the table (or its MD5 line)
			break
		}
		typ, sub := e[2], e[3]
		from := int(uint32(e[4]) | uint32(e[5])<<8 | uint32(e[6])<<16 | uint32(e[7])<<24)
		size := int(uint32(e[8]) | uint32(e[9])<<8 | uint32(e[10])<<16 | uint32(e[11])<<24)
		// data partitions that hold settings: nvs (0x02) and nvs keys (0x04)
		if typ != 0x01 || (sub != 0x02 && sub != 0x04) {
			continue
		}
		to := from + size
		if from%sectorSize != 0 || to%sectorSize != 0 || from <= tableAt || to > len(data) {
			continue
		}
		blank := true
		for _, b := range data[from:to] {
			if b != 0xFF {
				blank = false
				break
			}
		}
		if blank {
			skip = append(skip, span{from, to})
		}
	}
	if len(skip) == 0 {
		return whole
	}
	sort.Slice(skip, func(i, j int) bool { return skip[i].from < skip[j].from })
	var parts []part
	at := 0
	for _, sp := range skip {
		if sp.from > at {
			parts = append(parts, part{data[at:sp.from], uint32(at)})
		}
		at = sp.to
	}
	if at < len(data) {
		parts = append(parts, part{data[at:], uint32(at)})
	}
	return parts
}
