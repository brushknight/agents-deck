package main

// Bluetooth: the service side (the helper, the boards' links) and the CLI.
//
//	agentctl bluetooth                 whether this computer can use it, and your boards on it
//	agentctl bluetooth scan [--for 8s] look for boards nearby
//	agentctl bluetooth ls              the boards found by the last scan
//	agentctl bluetooth add <name> [--scope view|act]
//	agentctl devices uplink <name> auto|wifi|ble

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/admin"
	"github.com/brushknight/agents-deck/backend/internal/ble"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

// startBluetooth runs the Bluetooth helper (deck-ble next to agentctl, or
// $AGENTS_DECK_BLE_HELPER) and the links to the boards added over Bluetooth.
// Without a helper (or in demo mode) it does nothing and says so when asked.
func startBluetooth(srv *server.Server, hub *link.Hub, demo bool) *ble.Uplinks {
	c := &ble.Central{Path: ble.Find()}
	if demo {
		c.Path = ""
	}
	u := &ble.Uplinks{Central: c, Deck: link.ServerDeck{S: srv}, Hub: hub, Devices: srv.Devices, Audit: srv.Audit.Log}
	go c.Run(context.Background())
	go u.Run(context.Background())
	return u
}

const bluetoothUsage = "usage: agentctl bluetooth [scan [--for 8s] | ls | add <name> [--scope view|act]]"

func bluetoothCmd(args []string) error {
	if len(args) == 0 {
		return bluetoothStatus()
	}
	switch args[0] {
	case "scan":
		fs := flag.NewFlagSet("bluetooth scan", flag.ExitOnError)
		dur := fs.Duration("for", 8*time.Second, "how long to look")
		_ = fs.Parse(args[1:])
		if err := adminCall("POST", "/admin/v1/bluetooth/scan", map[string]bool{"on": true}, nil); err != nil {
			return err
		}
		fmt.Printf("looking for boards for %s… (a board in setup shows its name, deck-xxxxxx, on its screen)\n", *dur)
		time.Sleep(*dur)
		rows, err := bluetoothBoards()
		_ = adminCall("POST", "/admin/v1/bluetooth/scan", map[string]bool{"on": false}, nil)
		if err != nil {
			return err
		}
		return printBluetoothBoards(rows)
	case "ls", "list":
		rows, err := bluetoothBoards()
		if err != nil {
			return err
		}
		return printBluetoothBoards(rows)
	case "add":
		fs := flag.NewFlagSet("bluetooth add", flag.ExitOnError)
		scope := fs.String("scope", "", "what the board may do: view or act (the default for new boards when empty)")
		if len(args) < 2 {
			return errors.New(bluetoothUsage)
		}
		_ = fs.Parse(args[2:])
		rows, err := bluetoothBoards()
		if err != nil {
			return err
		}
		b, err := pickBluetoothBoard(rows, args[1])
		if err != nil {
			return err
		}
		var job admin.Job
		body := map[string]string{}
		if *scope != "" {
			body["scope"] = *scope
		}
		if err := adminCall("POST", "/admin/v1/bluetooth/boards/"+url.PathEscape(b.ID)+"/add", body, &job); err != nil {
			return err
		}
		if err := followJob(job); err != nil {
			return err
		}
		fmt.Println("it connects over Bluetooth whenever its preferred uplink isn't there: agentctl devices uplink <name> auto|wifi|ble")
		return nil
	}
	return errors.New(bluetoothUsage)
}

func bluetoothStatus() error {
	var st ble.Status
	if err := adminCall("GET", "/admin/v1/bluetooth", nil, &st); err != nil {
		return err
	}
	if !st.Available {
		fmt.Println("bluetooth: " + st.Why)
	} else {
		state := "on"
		if st.Scanning {
			state += ", scanning"
		}
		fmt.Println("bluetooth: " + state)
	}
	if st.LastError != "" {
		fmt.Println("last problem: " + st.LastError)
	}
	var res struct{ Devices []admin.DeviceRow }
	if err := adminCall("GET", "/admin/v1/devices", nil, &res); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	n := 0
	for _, d := range res.Devices {
		if !d.Bluetooth {
			continue
		}
		if n == 0 {
			fmt.Println()
			fmt.Fprintln(tw, "BOARD\tPREFERS\tIN USE\tUPLINKS")
		}
		n++
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.ID, d.UplinkPref, orDash(d.Connection), uplinkWords(d.Uplinks))
	}
	_ = tw.Flush()
	if n == 0 && st.Available {
		fmt.Println("no boards added over Bluetooth yet: agentctl bluetooth scan, then agentctl bluetooth add <name>")
	}
	return nil
}

func uplinkWords(us []admin.UplinkRow) string {
	var out []string
	for _, u := range us {
		s := u.Via + " " + u.State
		if u.Detail != "" && u.State != "in use" {
			s += " (" + u.Detail + ")"
		}
		out = append(out, s)
	}
	return orDash(strings.Join(out, ", "))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func bluetoothBoards() ([]admin.BluetoothBoard, error) {
	var res struct {
		Boards    []admin.BluetoothBoard
		Available bool
		Why       string
	}
	if err := adminCall("GET", "/admin/v1/bluetooth/boards", nil, &res); err != nil {
		return nil, err
	}
	if !res.Available && len(res.Boards) == 0 {
		return nil, errors.New(res.Why)
	}
	return res.Boards, nil
}

func printBluetoothBoards(rows []admin.BluetoothBoard) error {
	if len(rows) == 0 {
		fmt.Println("no boards found: put the board in setup from its screen, bring it closer, and scan again")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tBOARD\tFIRMWARE\tSIGNAL\tSTATE")
	for _, b := range rows {
		state := "paired with another computer"
		switch {
		case b.Added:
			state = "yours: " + b.Device
		case b.Setup && b.Device != "":
			state = "in setup, known as " + b.Device + ": agentctl bluetooth add " + b.Name
		case b.Setup:
			state = "in setup: agentctl bluetooth add " + b.Name
		case b.BoardID == "":
			state = "couldn't read it: bring it closer"
		}
		board := orDash(b.Model)
		if board == "-" {
			board = orDash(b.Board)
		}
		signal := "-"
		if b.RSSI != 0 {
			signal = fmt.Sprintf("%d dBm", b.RSSI)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", orDash(b.Name), board, orDash(b.Firmware), signal, state)
	}
	return tw.Flush()
}

// pickBluetoothBoard finds a board by its advertised name, its identifier
// (or the start of it), or its board id.
func pickBluetoothBoard(rows []admin.BluetoothBoard, ref string) (admin.BluetoothBoard, error) {
	var hits []admin.BluetoothBoard
	for _, b := range rows {
		if strings.EqualFold(b.Name, ref) || strings.EqualFold(b.BoardID, ref) || strings.HasPrefix(strings.ToLower(b.ID), strings.ToLower(ref)) {
			hits = append(hits, b)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return admin.BluetoothBoard{}, fmt.Errorf("no board %q nearby: agentctl bluetooth scan", ref)
	}
	return admin.BluetoothBoard{}, fmt.Errorf("%q matches %d boards: use the identifier from agentctl bluetooth ls", ref, len(hits))
}

// devicesUplink: agentctl devices uplink <name> auto|wifi|ble.
func devicesUplink(args []string) error {
	if len(args) != 3 || !link.ValidUplink(args[2]) || args[2] == "" {
		return errors.New("usage: agentctl devices uplink <name> auto|wifi|ble\n" +
			"  auto: USB when plugged in, else Wi-Fi, else Bluetooth\n" +
			"  wifi: USB, else Wi-Fi; Bluetooth only to set it up\n" +
			"  ble:  USB, else Bluetooth; Wi-Fi isn't used")
	}
	if err := adminCall("POST", "/admin/v1/devices/"+url.PathEscape(args[1])+"/uplink", map[string]string{"uplink": args[2]}, nil); err != nil {
		return err
	}
	fmt.Printf("%s: uplink %s\n", args[1], args[2])
	return nil
}
