package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

// The update package tells a release build from a build from source by this.
func init() { update.BuildVersion = version }

// releaseKeyCmd: the release signature (hidden from help; used by the release
// workflow and by whoever holds the key).
//
//	agentctl release-key new
//	agentctl release-key sign <file>                  key from $AGENTS_DECK_RELEASE_KEY, writes <file>.sig
//	agentctl release-key verify <file> [--pub <base64>]
func releaseKeyCmd(args []string) error {
	const usage = "usage: agentctl release-key new | sign <file> | verify <file> [--pub <base64>]"
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "new":
		priv, pub, err := update.NewKey()
		if err != nil {
			return err
		}
		fmt.Printf("private: %s\npublic:  %s\n", priv, pub)
		fmt.Fprintln(os.Stderr, "\nThe private key signs releases: store it as the GitHub Actions secret AGENTS_DECK_RELEASE_KEY")
		fmt.Fprintln(os.Stderr, "and keep a backup outside GitHub. The public key goes in backend/internal/update/release.pub.")
		return nil
	case "sign":
		if len(args) != 2 {
			return errors.New(usage)
		}
		env := os.Getenv("AGENTS_DECK_RELEASE_KEY")
		if strings.TrimSpace(env) == "" {
			return errors.New("set AGENTS_DECK_RELEASE_KEY to the private release key")
		}
		priv, err := update.ParsePrivateKey(env)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		sig := update.Sign(priv, data)
		if err := os.WriteFile(args[1]+".sig", sig, 0o644); err != nil {
			return err
		}
		pub := update.PublicOf(priv)
		fmt.Printf("%s.sig: signed with the key whose public half is %s\n", args[1], pub)
		if built, err := update.ReleaseKey(); err == nil && built != nil {
			if p, _ := update.ParsePublicKey(pub); !built.Equal(p) {
				fmt.Fprintln(os.Stderr, "note: this isn't the key in release.pub of this build (expected only while changing keys)")
			}
		}
		return nil
	case "verify":
		var file, pubFlag string
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			switch a := rest[i]; {
			case a == "--pub" || a == "-pub":
				if i+1 >= len(rest) {
					return errors.New(usage)
				}
				pubFlag = rest[i+1]
				i++
			case strings.HasPrefix(a, "--pub="):
				pubFlag = strings.TrimPrefix(a, "--pub=")
			case file == "":
				file = a
			default:
				return errors.New(usage)
			}
		}
		if file == "" {
			return errors.New(usage)
		}
		pub, err := update.ReleaseKey()
		if pubFlag != "" {
			pub, err = update.ParsePublicKey(pubFlag)
		}
		if err != nil {
			return err
		}
		if pub == nil {
			return errors.New("this build has no release key: pass --pub <base64>")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(file + ".sig")
		if err != nil {
			return err
		}
		if err := update.Verify(pub, data, sig); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		fmt.Printf("%s: signature good\n", file)
		return nil
	}
	return errors.New(usage)
}

// updateCmd: agentctl update [--check] [--yes]
func updateCmd(args []string) error {
	check, yes := false, false
	for _, a := range args {
		switch a {
		case "--check", "-check":
			check = true
		case "--yes", "-yes", "-y":
			yes = true
		default:
			return errors.New("usage: agentctl update [--check] [--yes]")
		}
	}
	if done, err := updateViaService(check, yes); done { // admin_cli.go
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	u := update.New(version)
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	st, err := u.Check(cctx)
	cancel()
	if err != nil {
		return err
	}
	printUpdate(st)
	if check || (st.State != update.StateAvailable && st.State != update.StateReady) || u.Kind == update.KindDeb {
		return nil
	}
	if st.State == update.StateAvailable {
		fmt.Printf("downloading %s…\n", st.Version)
		if st, err = u.Download(ctx); err != nil {
			return err
		}
		fmt.Println("downloaded and checked")
	}
	if !yes {
		fmt.Printf("Install agents deck %s now? The service restarts; agents keep running. [y/N] ", st.Version)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Println("not installed; it stays downloaded for next time")
			return nil
		}
	}
	if err := u.Apply(ctx); err != nil {
		return err
	}
	fmt.Printf("installing %s: the service restarts in a moment\n", st.Version)
	return nil
}

func printUpdate(st update.State) {
	fmt.Printf("installed: %s (%s)\n", st.Current, st.Install)
	if st.Version != "" {
		fmt.Printf("latest:    %s\n", st.Version)
	}
	switch st.State {
	case update.StateNone:
		fmt.Println("up to date")
	case update.StateAvailable:
		fmt.Println("a new version is available")
	case update.StateReady:
		fmt.Println("a new version is downloaded and ready to install")
	}
	if st.Message != "" {
		fmt.Println(st.Message)
	}
	if st.Notes != "" {
		fmt.Printf("\nWhat's new:\n%s\n", strings.TrimSpace(st.Notes))
	}
}

// firmwareCmd: agentctl firmware ls
func firmwareCmd(args []string) error {
	if len(args) != 1 || (args[0] != "ls" && args[0] != "list") {
		return errors.New("usage: agentctl firmware ls")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := firmware.New()
	cat, err := c.Latest(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("firmware in release %s\n", cat.Version)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "BOARD\tNAME\tCHIP\tWI-FI\tOVER THE AIR\tSIZE")
	for _, b := range cat.Boards {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d KB\n", b.Board, b.Name, b.Chip, yesNo(b.Wifi), yesNo(b.OTA != nil), (b.Size+1023)/1024)
	}
	tw.Flush()
	if c.Releases.Key == nil {
		fmt.Println("\nThis build has no release key: it lists the firmware but never downloads or flashes it.")
	}
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
