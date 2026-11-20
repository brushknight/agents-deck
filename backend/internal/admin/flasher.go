package admin

import (
	"context"
	"fmt"
	"io"

	"github.com/brushknight/agents-deck/backend/internal/flash"
	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// SerialFlasher is the real Flasher: it opens the port itself (only ever a
// registered one, after the Deck Link manager let go of it) and runs the Go
// flasher on it.
type SerialFlasher struct{}

func (SerialFlasher) Probe(ctx context.Context, port serial.Info) (flash.Chip, error) {
	p, err := serial.Open(port.Path, 115200)
	if err != nil {
		return flash.Chip{}, fmt.Errorf("couldn't open %s: %w", port.Path, err)
	}
	defer p.Close()
	return flash.ProbeWith(ctx, p, flash.Options{VID: port.VID, PID: port.PID})
}

func (SerialFlasher) Write(ctx context.Context, port serial.Info, image io.Reader, size int64, offset uint32, prog flash.Progress) error {
	p, err := serial.Open(port.Path, 115200)
	if err != nil {
		return fmt.Errorf("couldn't open %s: %w", port.Path, err)
	}
	defer p.Close()
	return flash.WriteWith(ctx, p, image, size, offset, prog, flash.Options{VID: port.VID, PID: port.PID})
}
