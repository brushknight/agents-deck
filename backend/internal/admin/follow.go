package admin

import (
	"log"
	"os"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// followPorts keeps the registered ports on their boards. macOS names a USB
// port after its place on the hub, so a board plugged in one level further
// down comes back under another path (/dev/cu.usbmodem11401 becomes
// /dev/cu.usbmodem111401), and Linux hands out ttyACM numbers in plug order.
// A registered port remembers its board's USB identity (usbKey); when its
// path is gone and exactly one plugged-in port has that identity, the
// registration moves there, in config.json too, so `agentctl ports`,
// Settings and the Deck Link manager all see the new path.
//
// Ports registered before identities were kept get theirs filled in the first
// time they are seen plugged in. A device with no serial number (some
// CH340 and CP210x adapters) can't be followed: its registration stays on
// its path.
//
// Listing the ports runs ioreg on a Mac, so it happens only when a
// registered path is missing or a registration has no identity yet, and at
// most every followEvery.
func (a *Admin) followPorts(ps []link.PortConfig) []link.PortConfig {
	if a.ListPorts == nil || len(ps) == 0 {
		return ps
	}
	a.ports.mu.Lock()
	need := false
	for _, p := range ps {
		if (p.USB == "" && !a.ports.noSerial[p.Path]) || !exists(p.Path) {
			need = true
		}
	}
	if !need || time.Since(a.ports.looked) < followEvery {
		a.ports.mu.Unlock()
		return ps
	}
	a.ports.looked = time.Now()
	a.ports.mu.Unlock()

	listed, err := a.ListPorts()
	if err != nil {
		return ps
	}
	byPath := map[string]serial.Info{}
	byKey := map[string][]string{}
	for _, i := range listed {
		byPath[i.Path] = i
		if k := usbKey(i); k != "" {
			byKey[k] = append(byKey[k], i.Path)
		}
	}
	taken := map[string]bool{}
	for _, p := range ps {
		taken[p.Path] = true
	}
	moved := map[string]string{}   // old path → new path
	learned := map[string]string{} // path → USB identity
	out := make([]link.PortConfig, len(ps))
	copy(out, ps)
	a.ports.mu.Lock()
	for i, p := range out {
		if in, here := byPath[p.Path]; here {
			k := usbKey(in)
			switch {
			case p.USB == "" && k != "":
				out[i].USB, learned[p.Path] = k, k
			case p.USB == "" && k == "":
				if a.ports.noSerial == nil {
					a.ports.noSerial = map[string]bool{}
				}
				a.ports.noSerial[p.Path] = true
			}
			continue
		}
		if p.USB == "" {
			continue
		}
		// Gone from its path: follow it when exactly one free port is it.
		if c := byKey[p.USB]; len(c) == 1 && !taken[c[0]] {
			log.Printf("ports: the board on %s moved to %s (same USB serial number)", p.Path, c[0])
			moved[p.Path], out[i].Path, taken[c[0]] = c[0], c[0], true
		}
	}
	a.ports.mu.Unlock()
	if len(moved) == 0 && len(learned) == 0 {
		return ps
	}
	err = a.setRegisteredPorts(func(cur []link.PortConfig) []link.PortConfig {
		for i, p := range cur {
			if k, ok := learned[p.Path]; ok && p.USB == "" {
				cur[i].USB = k
			}
			if to, ok := moved[p.Path]; ok {
				cur[i].Path = to
			}
		}
		return cur
	})
	if err != nil {
		log.Printf("ports: saving the registered ports: %v", err)
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
