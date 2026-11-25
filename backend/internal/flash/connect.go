package flash

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// timings: esptool's defaults. Tests shrink them.
type timings struct {
	poll        time.Duration // read granularity
	enterBoot   time.Duration // EN held low with IO0 low
	resetDelay  time.Duration // IO0 kept low after EN goes high
	resetExtra  time.Duration // the same, second try, for slow boards
	usbSettle   time.Duration // each step of the USB-Serial-JTAG sequence
	hardHold    time.Duration // EN low for a hard reset
	syncWait    time.Duration // wait for a SYNC reply
	syncTries   int           // SYNCs after each reset
	attempts    int           // resets before giving up
	cmd         time.Duration // default command timeout
	baudSettle  time.Duration // after CHANGE_BAUDRATE
	erasePerMB  time.Duration
	writePerMB  time.Duration
	md5PerMB    time.Duration
	afterSyncRx time.Duration // drain the extra SYNC replies
}

var tm = timings{
	poll:        100 * time.Millisecond,
	enterBoot:   100 * time.Millisecond,
	resetDelay:  50 * time.Millisecond,
	resetExtra:  550 * time.Millisecond,
	usbSettle:   100 * time.Millisecond,
	hardHold:    100 * time.Millisecond,
	syncWait:    100 * time.Millisecond,
	syncTries:   5,
	attempts:    7,
	cmd:         3 * time.Second,
	baudSettle:  50 * time.Millisecond,
	erasePerMB:  40 * time.Second,
	writePerMB:  40 * time.Second,
	md5PerMB:    8 * time.Second,
	afterSyncRx: 100 * time.Millisecond,
}

// ErrNoBootloader: the board never answered in its download mode.
var ErrNoBootloader = errors.New("the board didn't answer in download mode: hold BOOT, press and release RST, let go of BOOT, then try again")

// ErrWrongChip: the board isn't an ESP32-S3.
var ErrWrongChip = errors.New("this flasher only writes ESP32-S3 boards")

// resetKind is a way into the ROM bootloader.
type resetKind int

const (
	resetClassic     resetKind = iota // DTR/RTS on IO0/EN through two transistors (CH340, CP210x, FTDI boards)
	resetClassicSlow                  // the same, IO0 held longer
	resetUSBJTAG                      // the S3's own USB-Serial-JTAG
)

func (k resetKind) String() string {
	return [...]string{"classic", "classic, slow", "USB-Serial-JTAG"}[k]
}

// resetsFor picks the reset sequences to try from the port's USB ids (""
// when unknown).
func resetsFor(vid, pid string) []resetKind {
	switch {
	case vid == "303a" && pid == "1001":
		return []resetKind{resetUSBJTAG}
	case vid == "303a":
		return []resetKind{resetUSBJTAG, resetClassic}
	case vid == "":
		return []resetKind{resetClassic, resetUSBJTAG, resetClassicSlow}
	}
	return []resetKind{resetClassic, resetClassicSlow}
}

func (c *conn) reset(k resetKind) error {
	steps := func(fs ...func() error) error {
		for _, f := range fs {
			if err := f(); err != nil {
				return err
			}
		}
		return nil
	}
	dtr := func(v bool) func() error { return func() error { return c.setDTR(v) } }
	rts := func(v bool) func() error { return func() error { return c.setRTS(v) } }
	wait := func(d time.Duration) func() error { return func() error { return c.sleep(d) } }
	switch k {
	case resetUSBJTAG:
		return steps(rts(false), dtr(false), wait(tm.usbSettle),
			dtr(true), rts(false), wait(tm.usbSettle),
			rts(true), dtr(false), rts(true), wait(tm.usbSettle),
			dtr(false), rts(false))
	default:
		d := tm.resetDelay
		if k == resetClassicSlow {
			d = tm.resetExtra
		}
		return steps(dtr(false), rts(true), wait(tm.enterBoot),
			dtr(true), rts(false), wait(d),
			dtr(false))
	}
}

// hardReset leaves download mode and starts the firmware: EN pulsed low
// with IO0 high.
func (c *conn) hardReset(usb bool) error {
	hold, after := tm.hardHold, time.Duration(0)
	if usb {
		hold, after = 2*tm.hardHold, 2*tm.hardHold
	}
	if err := c.setDTR(false); err != nil {
		return err
	}
	if err := c.setRTS(true); err != nil {
		return err
	}
	if err := c.sleep(hold); err != nil {
		return err
	}
	if err := c.setRTS(false); err != nil {
		return err
	}
	return c.sleep(after)
}

func (c *conn) sync() error {
	payload := append([]byte{0x07, 0x07, 0x12, 0x20}, bytes.Repeat([]byte{0x55}, 32)...)
	if _, _, err := c.command(opSync, payload, 0, tm.syncWait); err != nil {
		return err
	}
	// The ROM answers one SYNC eight times: let the rest go by.
	deadline := time.Now().Add(tm.afterSyncRx)
	for {
		if _, err := c.readFrame(deadline); err != nil {
			if errors.Is(err, errNoReply) {
				c.resetInput()
				return nil
			}
			return err
		}
	}
}

var bootLine = regexp.MustCompile(`boot:(0x[0-9a-fA-F]+)( \([^)]*\))?`)

// enter resets the board into its ROM bootloader and syncs with it.
func (c *conn) enter(kinds []resetKind) error {
	for i := 0; i < tm.attempts; i++ {
		if err := c.reset(kinds[i%len(kinds)]); err != nil {
			return fmt.Errorf("reset the board: %w", err)
		}
		for j := 0; j < tm.syncTries; j++ {
			c.resetInput()
			err := c.sync()
			if err == nil {
				return nil
			}
			if !errors.Is(err, errNoReply) {
				return err
			}
		}
	}
	noise := string(c.dec.noise)
	switch m := bootLine.FindStringSubmatch(noise); {
	case bytes.Contains(c.dec.noise, []byte("waiting for download")):
		return fmt.Errorf("%w (it is in download mode but doesn't answer: try another cable or USB socket)", ErrNoBootloader)
	case m != nil:
		return fmt.Errorf("%w (it started its firmware instead, boot:%s%s)", ErrNoBootloader, m[1], m[2])
	case len(noise) > 0:
		return fmt.Errorf("%w (it only sent other output)", ErrNoBootloader)
	}
	return fmt.Errorf("%w (no reply at all)", ErrNoBootloader)
}

// ESP32-S3 registers.
const (
	chipMagicReg     = 0x40001000 // differs per chip on chips before the S3
	uartDevBufNo     = 0x3FCEF14C // ROM .bss: which port the ROM talks on
	uartNoUSBOTG     = 3
	uartNoUSBJTAG    = 4
	efuseMACReg      = 0x60007000 + 0x044
	rtcCntl          = 0x60008000
	rtcWDTConfig0    = rtcCntl + 0x98
	rtcWDTConfig1    = rtcCntl + 0x9C
	rtcWDTWProtect   = rtcCntl + 0xB0
	rtcSWDConf       = rtcCntl + 0xB4
	rtcSWDWProtect   = rtcCntl + 0xB8
	rtcWDTKey        = 0x50D83AA1
	rtcSWDKey        = 0x8F1D312A
	rtcSWDAutoFeed   = 1 << 31
	rtcOption1       = 0x6000812C
	forceDownloadBit = 0x1

	spiBase      = 0x60002000
	spiCmdReg    = spiBase + 0x00
	spiUsrReg    = spiBase + 0x18
	spiUsr2Reg   = spiBase + 0x20
	spiMisoDlen  = spiBase + 0x28
	spiW0Reg     = spiBase + 0x58
	spiCmdUsr    = 1 << 18
	spiUsrCmd    = 1 << 31
	spiUsrMiso   = 1 << 28
	flashRDID    = 0x9F
	s3ChipID     = 9
	secureDLFlag = 1 << 2
)

var chipIDs = map[uint32]string{0: "ESP32", 2: "ESP32-S2", 5: "ESP32-C3", 9: "ESP32-S3", 12: "ESP32-C2", 13: "ESP32-C6", 16: "ESP32-H2", 18: "ESP32-P4"}
var chipMagics = map[uint32]string{0x00F01D83: "ESP32", 0x000007C6: "ESP32-S2", 0xFFF0C101: "ESP8266"}

// session is a synced ROM loader on an ESP32-S3.
type session struct {
	*conn
	usbJTAG, usbOTG bool
	chip            Chip
	flashID         uint32
}

// detect makes sure it is an ESP32-S3: chip id from GET_SECURITY_INFO,
// falling back to the magic register of older chips to name them.
func (c *conn) detect() (secure bool, err error) {
	_, body, err := c.command(opGetSecurityInfo, nil, 0, tm.cmd)
	if err != nil {
		return false, fmt.Errorf("identify the chip: %w", err)
	}
	// flags(4) flash_crypt_cnt(1) key_purposes(7) chip_id(4) api_version(4), then 4 status bytes.
	if len(body) >= 24 && body[20] == 0 {
		flags := binary.LittleEndian.Uint32(body[0:])
		id := binary.LittleEndian.Uint32(body[12:])
		if id != s3ChipID {
			name := chipIDs[id]
			if name == "" {
				name = fmt.Sprintf("unknown Espressif chip (id %d)", id)
			}
			return false, fmt.Errorf("%w: this board has an %s", ErrWrongChip, name)
		}
		return flags&secureDLFlag != 0, nil
	}
	magic, err := c.readReg(chipMagicReg)
	if err != nil {
		return false, fmt.Errorf("%w: the chip doesn't say what it is (%v)", ErrWrongChip, err)
	}
	name := chipMagics[magic]
	if name == "" {
		name = fmt.Sprintf("unknown chip (magic 0x%08x)", magic)
	}
	return false, fmt.Errorf("%w: this board has an %s", ErrWrongChip, name)
}

// open resets into the bootloader, checks the chip, reads its MAC and flash
// id, and attaches the SPI flash.
func open(c *conn, kinds []resetKind) (*session, error) {
	if err := c.enter(kinds); err != nil {
		return nil, err
	}
	secure, err := c.detect()
	if err != nil {
		return nil, err
	}
	if secure {
		return nil, errors.New("the chip is in secure download mode: this flasher can't verify what it writes there, use esptool")
	}
	s := &session{conn: c, chip: Chip{Name: "ESP32-S3"}}
	if v, err := c.readReg(uartDevBufNo); err == nil {
		s.usbOTG, s.usbJTAG = v&0xff == uartNoUSBOTG, v&0xff == uartNoUSBJTAG
	}
	if s.usbJTAG {
		// Over USB-Serial-JTAG the RTC and super watchdogs keep running in
		// download mode and would reset the chip halfway through a write.
		if err := s.disableWatchdogs(); err != nil {
			return nil, err
		}
	}
	mac0, err := c.readReg(efuseMACReg)
	if err != nil {
		return nil, err
	}
	mac1, err := c.readReg(efuseMACReg + 4)
	if err != nil {
		return nil, err
	}
	s.chip.MAC = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", byte(mac1>>8), byte(mac1), byte(mac0>>24), byte(mac0>>16), byte(mac0>>8), byte(mac0))
	if _, _, err := c.check("attach the SPI flash", opSPIAttach, make([]byte, 8), 0, 0, tm.cmd); err != nil {
		return nil, err
	}
	if id, err := s.readFlashID(); err == nil {
		s.flashID = id
		s.chip.FlashSize = flashSizeFromID(id)
	}
	return s, nil
}

func (s *session) disableWatchdogs() error {
	steps := [][2]uint32{{rtcWDTWProtect, rtcWDTKey}, {rtcWDTConfig0, 0}, {rtcWDTWProtect, 0}, {rtcSWDWProtect, rtcSWDKey}}
	for _, st := range steps {
		if err := s.writeReg(st[0], st[1], 0xFFFFFFFF); err != nil {
			return fmt.Errorf("turn off the watchdogs: %w", err)
		}
	}
	v, err := s.readReg(rtcSWDConf)
	if err != nil {
		return err
	}
	if err := s.writeReg(rtcSWDConf, v|rtcSWDAutoFeed, 0xFFFFFFFF); err != nil {
		return err
	}
	return s.writeReg(rtcSWDWProtect, 0, 0xFFFFFFFF)
}

// readFlashID runs the SPI flash's RDID (0x9F) through the SPI controller's
// user command registers, as the ROM has no command for it.
func (s *session) readFlashID() (uint32, error) {
	oldUsr, err := s.readReg(spiUsrReg)
	if err != nil {
		return 0, err
	}
	oldUsr2, err := s.readReg(spiUsr2Reg)
	if err != nil {
		return 0, err
	}
	for _, w := range [][2]uint32{
		{spiMisoDlen, 24 - 1},
		{spiUsrReg, spiUsrCmd | spiUsrMiso},
		{spiUsr2Reg, 7<<28 | flashRDID},
		{spiW0Reg, 0},
		{spiCmdReg, spiCmdUsr},
	} {
		if err := s.writeReg(w[0], w[1], 0xFFFFFFFF); err != nil {
			return 0, err
		}
	}
	done := false
	for i := 0; i < 10 && !done; i++ {
		v, err := s.readReg(spiCmdReg)
		if err != nil {
			return 0, err
		}
		done = v&spiCmdUsr == 0
	}
	if !done {
		return 0, errors.New("the SPI flash didn't answer its id")
	}
	id, err := s.readReg(spiW0Reg)
	if err != nil {
		return 0, err
	}
	_ = s.writeReg(spiUsrReg, oldUsr, 0xFFFFFFFF)
	_ = s.writeReg(spiUsr2Reg, oldUsr2, 0xFFFFFFFF)
	return id & 0xFFFFFF, nil
}

// flashSizeFromID reads the capacity byte of a JEDEC id (manufacturer in the
// low byte, capacity in the high byte, as the ROM returns it); 0 = unknown.
func flashSizeFromID(id uint32) int64 {
	c := (id >> 16) & 0xff
	switch {
	case c >= 0x12 && c <= 0x1C: // 256 KiB … 256 MiB
		return 1 << c
	case c >= 0x20 && c <= 0x22: // some vendors: 64 MiB … 256 MiB
		return 1 << (c - 0x20 + 26)
	case c >= 0x32 && c <= 0x39: // others: 256 KiB … 64 MiB
		return 1 << (c - 0x32 + 18)
	}
	return 0
}

// leave starts the firmware: clears the forced download boot, then resets.
// Over native USB-OTG the RTS line does nothing, so the RTC watchdog
// restarts the chip instead.
func (s *session) leave() error {
	_ = s.writeReg(rtcOption1, 0, forceDownloadBit)
	if s.usbOTG {
		_ = s.writeReg(rtcWDTWProtect, rtcWDTKey, 0xFFFFFFFF)
		_ = s.writeReg(rtcWDTConfig1, 2000, 0xFFFFFFFF)
		_ = s.send(opWriteReg, le32(rtcWDTConfig0, 1<<31|5<<28|1<<8|2, 0xFFFFFFFF, 0), 0)
		return nil
	}
	return s.hardReset(s.usbJTAG)
}
