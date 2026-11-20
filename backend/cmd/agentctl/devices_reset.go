package main

// agentctl devices reset: start the deck's certificates over, when the
// certificate authority's key is lost (a new Mac, a restored data folder, a
// cleaned Keychain) or you want every network device to pair again.

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// resetKeystore is where the CA key lives (the Keychain on macOS). Tests
// replace it in TestMain, so no test can reach the real Keychain.
var resetKeystore = pki.DefaultKeystore

// resetFiles are the files a reset backs up and replaces.
type resetFiles struct {
	Data, CA, ServerCert, ServerKey, Devices string
}

func defaultResetFiles() resetFiles {
	return resetFiles{Data: paths.Data(), CA: paths.CACert(), ServerCert: paths.ServerCert(),
		ServerKey: paths.ServerKey(), Devices: paths.Devices()}
}

// resetReport is what a reset did and what each device needs next.
type resetReport struct {
	Backup  string   // the dated folder with the old files
	Network []string // paired over the network: removed, pair them again
	Boards  []string // had a Wi-Fi certificate: plug in by USB once
}

// resetCertificates backs up the CA certificate (and its key, when that is
// a file), the network listener's certificate and key and devices.json into a dated folder in the data
// folder, then removes the CA (certificate and stored key) and the
// listener's certificate. Network devices leave devices.json; boards keep
// their names and approval but lose their Wi-Fi certificate, and ask for a
// new one the next time they are on USB. The service must not be running.
func resetCertificates(f resetFiles, ks pki.Keystore, now time.Time) (resetReport, error) {
	var rep resetReport
	base := filepath.Join(f.Data, "backups", "certificates-"+now.Format("2006-01-02-150405"))
	dir := base
	for i := 2; ; i++ {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			break
		}
		dir = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return rep, err
	}
	rep.Backup = dir
	for _, p := range []string{f.CA, f.ServerCert, f.ServerKey, f.Devices} {
		if err := copyPrivate(p, filepath.Join(dir, filepath.Base(p))); err != nil {
			return rep, fmt.Errorf("backing up %s: %w", filepath.Base(p), err)
		}
	}
	// Where the CA key is a file (Linux, Windows), it is kept too, 0600 in
	// the 0700 backup folder: the same protection it had. A Keychain key is
	// never copied out of the Keychain.
	if fk, ok := ks.(pki.FileKeystore); ok {
		if err := copyPrivate(filepath.Join(fk.Dir, pki.CAKeyName), filepath.Join(dir, pki.CAKeyName)); err != nil {
			return rep, fmt.Errorf("backing up the CA key: %w", err)
		}
	}

	reg := &devices.Registry{Path: f.Devices}
	if _, err := os.Stat(f.Devices); err == nil {
		for _, d := range reg.List() {
			switch {
			case d.Kind != devices.Serial:
				if err := reg.Remove(d.Name); err != nil {
					return rep, err
				}
				rep.Network = append(rep.Network, d.Name)
			case d.CertSerial != "":
				if err := reg.ClearBoardCert(d.Name); err != nil {
					return rep, err
				}
				rep.Boards = append(rep.Boards, d.Name)
			}
		}
	}

	if err := pki.RemoveCA(f.CA, ks); err != nil {
		return rep, err
	}
	for _, p := range []string{f.ServerCert, f.ServerKey} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rep, err
		}
	}
	return rep, nil
}

// copyPrivate copies src to dst readable by you only; a missing src is no error.
func copyPrivate(src, dst string) error {
	b, err := os.ReadFile(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

func devicesReset(args []string) error {
	fs := flag.NewFlagSet("devices reset", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "don't ask: reset at once")
	if err := fs.Parse(args); err != nil {
		return err
	}
	f := defaultResetFiles()
	ks := resetKeystore(f.Data)
	fmt.Printf(`This starts the deck's certificates over:
  - the deck makes a new certificate authority (its key goes in the %s)
  - phones and panels paired over the network must pair again
  - boards on Wi-Fi get a new certificate the next time they're plugged in by USB
The old files are kept in %s.
`, ks.Where(), filepath.Join(f.Data, "backups"))
	if !*yes {
		if !isTerminal() {
			return errors.New("type it in a terminal, or add --yes to reset without asking")
		}
		fmt.Print("Type reset to go on: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "reset" {
			fmt.Println("nothing changed")
			return nil
		}
	}

	// The running service keeps devices.json in memory and would write the
	// old list back: stop it first, and start it again after.
	restart := false
	if serviceAnswers() {
		m, _, err := serviceManager()
		if err != nil {
			return err
		}
		if reg := m.Registration(); !reg.Registered || reg.Note != "" {
			return errors.New("the service is running, but not as the service that starts at login (agentctl serve in a terminal?): stop it, then run agentctl devices reset again")
		}
		fmt.Println("stopping the service…")
		if err := m.Uninstall(io.Discard); err != nil {
			return fmt.Errorf("stopping the service: %w", err)
		}
		restart = true
	}

	rep, err := resetCertificates(f, ks, time.Now())
	if err == nil {
		printReset(os.Stdout, rep)
	}
	if restart {
		fmt.Println("starting the service again…")
		m, _, merr := serviceManager()
		if merr == nil {
			merr = m.Install(io.Discard)
		}
		if merr != nil {
			return fmt.Errorf("the service didn't start again: %w (start it with agentctl install)", merr)
		}
	} else if err == nil {
		fmt.Println("Start the service (agentctl install, or agentctl serve) to make the new certificate authority.")
	}
	return err
}

func serviceAnswers() bool {
	resp, err := unixClient(2 * time.Second).Get("http://agentd/v1/state")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func printReset(w io.Writer, rep resetReport) {
	fmt.Fprintf(w, "Done. The old certificates and devices.json are in %s.\n", rep.Backup)
	if len(rep.Network) > 0 {
		fmt.Fprintf(w, "Pair these again (Settings > Network, or agentctl pair <name>): %s\n", strings.Join(rep.Network, ", "))
	}
	if len(rep.Boards) > 0 {
		fmt.Fprintf(w, "Plug these into this computer by USB once, to get their new Wi-Fi certificate: %s\n", strings.Join(rep.Boards, ", "))
	}
	if len(rep.Network) == 0 && len(rep.Boards) == 0 {
		fmt.Fprintln(w, "No device had a certificate, so none needs anything.")
	}
	fmt.Fprintln(w, "Boards on USB carry on as before.")
}
