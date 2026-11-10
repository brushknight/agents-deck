package flash

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// The protocol's waits, shrunk for a simulated board.
	tm.poll = 5 * time.Millisecond
	tm.enterBoot = 10 * time.Millisecond
	tm.resetDelay = 20 * time.Millisecond
	tm.resetExtra = 40 * time.Millisecond
	tm.usbSettle = 10 * time.Millisecond
	tm.hardHold = 10 * time.Millisecond
	tm.syncWait = 20 * time.Millisecond
	tm.afterSyncRx = 10 * time.Millisecond
	tm.baudSettle = time.Millisecond
	tm.cmd = time.Second
	m.Run()
}

func image(n int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	// Half random, half runs: something deflate has work to do on.
	for i := 0; i < n; i++ {
		if (i/4096)%2 == 0 {
			b[i] = byte(r.Intn(256))
		} else {
			b[i] = byte(i / 512)
		}
	}
	return b
}

func TestSlipRoundTrip(t *testing.T) {
	in := []byte{1, slipEnd, 2, slipEsc, 3, slipEnd, slipEsc}
	var d slipDecoder
	var got []byte
	for _, c := range append([]byte("noise"), slipEncode(in)...) {
		if f, ok := d.feed(c); ok {
			got = f
		}
	}
	if !bytes.Equal(got, in) || string(d.noise) != "noise" {
		t.Fatalf("got %x noise %q", got, d.noise)
	}
}

func TestFlashSizeFromID(t *testing.T) {
	for id, want := range map[uint32]int64{0x1640EF: 4 << 20, 0x1840EF: 16 << 20, 0x174068: 8 << 20, 0x3925C2: 32 << 20, 0: 0, 0xFFFFFF: 0} {
		if got := flashSizeFromID(id); got != want {
			t.Errorf("0x%06x: %d, want %d", id, got, want)
		}
	}
}

func TestProbe(t *testing.T) {
	for _, usb := range []bool{false, true} {
		b := newFakeBoard()
		b.usbJTAG = usb
		if usb {
			b.regs[uartDevBufNo] = uartNoUSBJTAG
		}
		opts := Options{VID: "1a86", PID: "7523"}
		if usb {
			opts = Options{VID: "303a", PID: "1001"}
		}
		chip, err := ProbeWith(context.Background(), b, opts)
		if err != nil {
			t.Fatalf("usb=%v: %v", usb, err)
		}
		want := Chip{Name: "ESP32-S3", MAC: "11:22:33:44:55:66", FlashSize: 4 << 20}
		if chip != want {
			t.Errorf("usb=%v: %+v, want %+v", usb, chip, want)
		}
		time.Sleep(20 * time.Millisecond) // the fake's EN delay
		b.mu.Lock()
		if b.download {
			t.Errorf("usb=%v: Probe left the board in download mode", usb)
		}
		if usb && b.regs[rtcSWDConf]&rtcSWDAutoFeed == 0 {
			t.Error("watchdogs left running over USB-Serial-JTAG")
		}
		b.mu.Unlock()
	}
}

// Unknown USB ids: the classic reset fails on a native USB board, the USB
// sequence comes next and works.
func TestProbeUnknownPortTriesBothResets(t *testing.T) {
	b := newFakeBoard()
	b.usbJTAG = true
	if _, err := Probe(context.Background(), b); err != nil {
		t.Fatal(err)
	}
}

func TestWrite(t *testing.T) {
	cases := []struct {
		name   string
		size   int
		offset uint32
		opts   Options
		plain  bool
	}{
		{"at 0x0", 200_000, 0x0, Options{}, false},
		{"at an offset", 70_001, 0x10000, Options{}, false},
		{"115200", 5000, 0x8000, Options{Baud: 115200}, false},
		{"ROM without deflate", 9000, 0x1000, Options{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newFakeBoard()
			b.refuseDefl = c.plain
			next := (c.offset + uint32(c.size) + sectorSize - 1) / sectorSize * sectorSize
			b.flash[next] = 0x42 // the sector after the image: must survive
			img := image(c.size, int64(c.size))
			var stages []string
			var last int64
			err := WriteWith(context.Background(), b, bytes.NewReader(img), int64(len(img)), c.offset, func(stage string, done, total int64) {
				if len(stages) == 0 || stages[len(stages)-1] != stage {
					stages = append(stages, stage)
				}
				if stage == "write" {
					if done < last || done > total {
						t.Errorf("write progress %d/%d after %d", done, total, last)
					}
					last = done
				}
			}, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			if got := b.flash[c.offset : c.offset+uint32(c.size)]; !bytes.Equal(got, img) {
				t.Error("flash differs from the image")
			}
			if b.flash[next] != 0x42 {
				t.Error("the sector after the image was erased")
			}
			if c.offset > 0 && b.flash[c.offset-1] != 0xFF {
				t.Error("the byte before the image changed")
			}
			if got := strings.Join(stages, ","); got != "connect,erase,write,verify,reset" {
				t.Errorf("stages %s", got)
			}
			if last != int64(c.size) {
				t.Errorf("write progress ended at %d of %d", last, c.size)
			}
			wantBaud := 460800
			if c.opts.Baud == 115200 {
				wantBaud = 115200
			}
			if b.hostBaud != wantBaud {
				t.Errorf("wrote at %d baud, want %d", b.hostBaud, wantBaud)
			}
			if b.setParams != 4<<20 {
				t.Errorf("SPI_SET_PARAMS size %d", b.setParams)
			}
			if bytes.Contains(b.ops, []byte{opFlashDeflEnd}) || bytes.Contains(b.ops, []byte{opFlashEnd}) {
				t.Error("sent FLASH_END to the ROM")
			}
		})
	}
}

func TestWriteOverUSBKeepsBaud(t *testing.T) {
	b := newFakeBoard()
	b.usbJTAG = true
	b.regs[uartDevBufNo] = uartNoUSBJTAG
	img := image(30_000, 1)
	if err := WriteWith(context.Background(), b, bytes.NewReader(img), int64(len(img)), 0, nil, Options{VID: "303a", PID: "1001"}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b.ops, []byte{opChangeBaud}) {
		t.Error("changed the baud rate over native USB")
	}
}

func TestWriteMD5Mismatch(t *testing.T) {
	b := newFakeBoard()
	b.corrupt = true
	img := image(10_000, 2)
	err := Write(context.Background(), b, bytes.NewReader(img), int64(len(img)), 0, nil)
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("got %v, want ErrVerify", err)
	}
}

func TestWriteCancelled(t *testing.T) {
	b := newFakeBoard()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	blocks := 0
	b.onOp = func(op byte) {
		if op == opFlashDeflData {
			if blocks++; blocks == 3 {
				cancel()
			}
		}
	}
	img := image(300_000, 3)
	err := Write(ctx, b, bytes.NewReader(img), int64(len(img)), 0, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if blocks > 4 {
		t.Errorf("kept writing after cancel: %d blocks", blocks)
	}
}

func TestNeverSyncs(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeBoard)
		want  string
	}{
		{"starts its app", func(b *fakeBoard) { b.neverDownload = true }, "boot:0x8"},
		{"silent", func(b *fakeBoard) { b.silent = true }, "no reply at all"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newFakeBoard()
			c.setup(b)
			_, err := Probe(context.Background(), b)
			if !errors.Is(err, ErrNoBootloader) {
				t.Fatalf("got %v, want ErrNoBootloader", err)
			}
			if !strings.Contains(err.Error(), "hold BOOT, press and release RST") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("message: %v", err)
			}
			if b.resets < 2 {
				t.Errorf("only %d resets tried", b.resets)
			}
		})
	}
}

func TestWrongChip(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeBoard)
		want  string
	}{
		{"ESP32-C3 by chip id", func(b *fakeBoard) { b.chipID = 5 }, "ESP32-C3"},
		{"ESP32 by magic", func(b *fakeBoard) { b.noSecInfo, b.magic = true, 0x00F01D83 }, "ESP32"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newFakeBoard()
			c.setup(b)
			img := image(100, 4)
			err := Write(context.Background(), b, bytes.NewReader(img), 100, 0, nil)
			if !errors.Is(err, ErrWrongChip) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v", err)
			}
			if bytes.Contains(b.ops, []byte{opFlashDeflBegin}) {
				t.Error("started writing to the wrong chip")
			}
		})
	}
}

func TestWriteRefusals(t *testing.T) {
	b := newFakeBoard()
	if err := Write(context.Background(), b, bytes.NewReader(make([]byte, 10)), 10, 0x800, nil); err == nil || !strings.Contains(err.Error(), "0x1000") {
		t.Errorf("unaligned offset: %v", err)
	}
	img := image(8192, 5)
	if err := Write(context.Background(), b, bytes.NewReader(img), int64(len(img)), 4<<20-4096, nil); err == nil || !strings.Contains(err.Error(), "doesn't fit") {
		t.Errorf("past the end of flash: %v", err)
	}
	b = newFakeBoard()
	b.secureDL = true
	if _, err := Probe(context.Background(), b); err == nil || !strings.Contains(err.Error(), "secure download mode") {
		t.Errorf("secure download mode: %v", err)
	}
	b = newFakeBoard()
	b.failOp = opFlashDeflData
	var re *ROMError
	if err := Write(context.Background(), b, bytes.NewReader(img), int64(len(img)), 0, nil); !errors.As(err, &re) || re.Code != 0x06 {
		t.Errorf("ROM error: %v", err)
	}
}
