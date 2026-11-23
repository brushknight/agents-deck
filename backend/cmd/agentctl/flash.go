package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/flash"
	"github.com/brushknight/agents-deck/backend/internal/serial"
)

const flashUsage = "usage: agentctl flash <port> <image.bin> [--offset 0x0] [--baud N]\n       agentctl flash <port> --probe"

// flashCmd writes a firmware image to an ESP32-S3 over USB, or with --probe
// only says what chip is on the port.
func flashCmd(args []string) error {
	for _, a := range args {
		if a == "--board" || a == "-board" || strings.HasPrefix(a, "--board=") || strings.HasPrefix(a, "-board=") {
			return flashBoardCmd(args) // admin_cli.go: through the service
		}
	}
	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	offsetFlag := fs.String("offset", "0x0", "where in flash the image goes (a multiple of 0x1000)")
	probe := fs.Bool("probe", false, "only read the chip, its MAC and flash size, then restart it")
	baud := fs.Int("baud", 0, "baud rate for writing through a USB serial chip (default 460800; native USB ignores it)")
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = append(pos, args[0]), args[1:]
	}
	_ = fs.Parse(args)
	pos = append(pos, fs.Args()...)
	if len(pos) < 1 || (!*probe && len(pos) != 2) {
		return errors.New(flashUsage)
	}
	port := pos[0]
	offset, err := strconv.ParseUint(*offsetFlag, 0, 32)
	if err != nil {
		return fmt.Errorf("--offset %s: %v", *offsetFlag, err)
	}

	var img *os.File
	var size int64
	if !*probe {
		if img, err = os.Open(pos[1]); err != nil {
			return err
		}
		defer img.Close()
		st, err := img.Stat()
		if err != nil {
			return err
		}
		size = st.Size()
	}

	info := serial.Lookup(port)
	if info.Likely != "" {
		fmt.Printf("%s: %s (%s:%s)\n", port, serial.LikelyName(info.Likely), info.VID, info.PID)
	}
	// The service lets go of the port while this runs (it no longer watches
	// its ports for other programs every few seconds).
	if adminCall("POST", "/admin/v1/ports/"+portRef(port)+"/borrow", map[string]int{"seconds": 1800}, nil) == nil {
		defer func() { _ = adminCall("POST", "/admin/v1/ports/"+portRef(port)+"/return", nil, nil) }()
	}
	p, err := serial.Open(port, 115200)
	if err != nil {
		return fmt.Errorf("open %s: %w", port, err)
	}
	defer p.Close()
	waitPortFree(port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	opts := flash.Options{VID: info.VID, PID: info.PID, Baud: *baud}

	if *probe {
		fmt.Println("connecting… (if nothing happens: hold BOOT, press and release RST, let go of BOOT)")
		chip, err := flash.ProbeWith(ctx, p, opts)
		if err != nil {
			return err
		}
		fl := "unknown (couldn't read the flash id)"
		if chip.FlashSize > 0 {
			fl = fmt.Sprintf("%d MB", chip.FlashSize>>20)
		}
		fmt.Printf("chip:  %s\nMAC:   %s\nflash: %s\n", chip.Name, chip.MAC, fl)
		return nil
	}

	start := time.Now()
	last := ""
	err = flash.WriteWith(ctx, p, img, size, uint32(offset), func(stage string, done, total int64) {
		line := stage
		switch stage {
		case "connect":
			line = "connecting…"
		case "erase":
			line = "erasing…"
		case "write", "verify":
			if total > 0 {
				line = fmt.Sprintf("%-7s %3d%%  %d / %d bytes", stage, done*100/total, done, total)
			}
		case "reset":
			line = "restarting the board"
		}
		if line != last {
			fmt.Printf("\r\033[K%s", line)
			last = line
		}
	}, opts)
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes) at 0x%x, verified, in %s\n", pos[1], size, offset, time.Since(start).Round(100*time.Millisecond))
	return nil
}

// waitPortFree gives another program a moment to let go of the port (the
// deck already did: flash borrowed the port from it).
func waitPortFree(port string) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return
	}
	me := strconv.Itoa(os.Getpid())
	said := false
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		out, _ := exec.Command(lsof, "-F", "p", port).Output()
		other := false
		for _, l := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(l, "p") && l[1:] != me {
				other = true
			}
		}
		if !other {
			return
		}
		if !said {
			fmt.Println("waiting for the other program on the port to let go (the deck does within a few seconds)…")
			said = true
		}
	}
	fmt.Println("the port is still open in another program; trying anyway")
}

// portNote is what `agentctl link ports` adds after a port: its USB id and
// what it likely is.
func portNote(i serial.Info) string {
	if i.VID == "" {
		return ""
	}
	s := fmt.Sprintf("  %s:%s", i.VID, i.PID)
	if n := serial.LikelyName(i.Likely); n != "" {
		s += "  " + n
	} else if i.Product != "" {
		s += "  " + i.Product
	}
	return s
}
