package proc

import (
	"bufio"
	"context"
	"errors"
	"log"
	"os/exec"
	"time"
)

// readMachine keeps one `iostat -n0 -w 5` running and reads its rows. When
// it stops, it starts again after 5 s, then 10 s, up to 5 minutes between
// tries; a run that lasted 10 minutes resets the wait. When ctx ends the
// iostat is killed (Tidy). If the service dies without that, iostat dies on
// its next write to the closed pipe.
func readMachine(ctx context.Context, m *MachineMeter) {
	wait := machineEvery
	for {
		start := time.Now()
		err := runIostat(ctx, m)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= 10*time.Minute {
			wait = machineEvery
		}
		m.setWhy("iostat stopped: " + err.Error())
		log.Printf("cpu: iostat stopped (%v), starting it again in %s", err, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(2*wait, 5*time.Minute)
	}
}

func runIostat(ctx context.Context, m *MachineMeter) error {
	cmd := exec.CommandContext(ctx, "/usr/sbin/iostat", "-n0", "-w", "5")
	Tidy(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p := newIostatParser()
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if busy, ok := p.line(sc.Text()); ok {
			m.add(time.Now(), busy)
		}
	}
	if err := cmd.Wait(); err != nil {
		return err
	}
	return errors.New("it ended")
}
