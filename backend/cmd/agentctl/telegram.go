package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/telegram"
)

// startTelegram runs the Telegram bridges in the service. Their prompts go
// in like any other: POST /v1/agents/{id}/prompt with "mode": "queue", as
// the device "telegram" (scope act), so they wait for a busy agent and show
// in the audit log.
func startTelegram(srv *server.Server, st *store.Store, keys pki.Keystore) *telegram.Service {
	who := devices.Device{Name: "telegram", Scope: devices.Act}
	base, _ := standInAPI()
	s := &telegram.Service{
		Keys:  keys,
		Path:  filepath.Join(paths.Data(), "telegram.json"),
		Dir:   filepath.Join(paths.Data(), "telegram"),
		Base:  base, // tests: a stand-in for the Bot API on this computer ("": Telegram's)
		Limit: server.MaxPromptBytes,
		Logf:  log.Printf,
		Deliver: func(agent, text string) (telegram.Result, error) {
			body, _ := json.Marshal(map[string]string{"text": text, "mode": "queue"})
			code, out := srv.Dispatch(who, http.MethodPost, "/v1/agents/"+url.PathEscape(agent)+"/prompt", body)
			var r struct {
				Status   string `json:"status"`
				Position int    `json:"position"`
				Error    string `json:"error"`
			}
			_ = json.Unmarshal(out, &r)
			if code >= 300 {
				if r.Error == "" {
					r.Error = http.StatusText(code)
				}
				return telegram.Result{}, errors.New(r.Error)
			}
			return telegram.Result{Queued: r.Status == "queued", Position: r.Position}, nil
		},
		Agent: func(id string) (telegram.AgentInfo, bool) {
			e, ok := st.Get(id)
			if !ok {
				return telegram.AgentInfo{}, false
			}
			title := e.A.Title
			if title == "" {
				title = e.A.AITitle
			}
			if title == "" {
				title = e.A.Folder
			}
			n := 0
			if srv.Queue != nil {
				n = srv.Queue.Count(id)
			}
			return telegram.AgentInfo{Title: title, Status: string(e.A.Status), Queued: n, Ended: e.A.Status == model.Exited}, true
		},
	}
	if err := s.Start(context.Background()); err != nil {
		log.Printf("telegram: %v", err)
	}
	return s
}

// standInAPI reads AGENTS_DECK_TELEGRAM_API, a stand-in for the Bot API in
// tests. Only an address on this computer counts (telegram.StandIn); any
// other value is ignored, and the log says so. set: a stand-in is in use.
func standInAPI() (base string, set bool) {
	v := os.Getenv("AGENTS_DECK_TELEGRAM_API")
	base, ok := telegram.StandIn(v)
	if !ok {
		log.Printf("telegram: AGENTS_DECK_TELEGRAM_API ignored: a stand-in Bot API must be on this computer (http://127.0.0.1:<port>)")
	}
	return base, base != ""
}

// telegramInDemo: a demo runs the Telegram bridges only against a stand-in
// Bot API on this computer, for tests; without one it runs none.
func telegramInDemo() bool {
	_, set := standInAPI()
	return set
}

// telegramCmd: agentctl telegram [--bridge <id>] [setup | link <agent> | unlink | drop | pair | user <id> | off | status].
// Everything goes through the service (/local/telegram), which keeps each
// bridge's token in the keystore; the token is read here without echo and
// never printed.
func telegramCmd(args []string) error {
	fs := flag.NewFlagSet("telegram", flag.ExitOnError)
	bridge := fs.String("bridge", "", "the bridge `id` (needed when there are several)")
	_ = fs.Parse(reorder(args, map[string]bool{"--bridge": true, "-bridge": true}))
	args = fs.Args()
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	req := telegram.Request{Bridge: *bridge}
	switch sub {
	case "status", "ls":
		q := ""
		if *bridge != "" {
			q = "?bridge=" + url.QueryEscape(*bridge)
		}
		var res struct{ Bridges []telegram.BridgeStatus }
		if err := call("GET", "/local/telegram"+q, nil, &res); err != nil {
			return err
		}
		if len(res.Bridges) == 0 {
			if *bridge != "" {
				return fmt.Errorf("there's no bridge %q", *bridge)
			}
			fmt.Println("No Telegram bridge yet. Start with: agentctl telegram setup")
			return nil
		}
		for i, b := range res.Bridges {
			if i > 0 {
				fmt.Println()
			}
			printBridge(b, len(res.Bridges) > 1 || b.ID != "default")
		}
		return nil
	case "setup":
		tok, err := readToken()
		if err != nil {
			return err
		}
		req.Op, req.Token = "setup", tok
	case "pair":
		req.Op = "pair"
	case "link":
		if len(args) != 2 {
			return errors.New("usage: agentctl telegram [--bridge <id>] link <agent id>   (agentctl ls shows the ids)")
		}
		req.Op, req.Agent = "link", args[1]
	case "user":
		id, err := strconv.ParseInt(strings.TrimSpace(argAt(args, 1)), 10, 64)
		if err != nil || id <= 0 {
			return errors.New("usage: agentctl telegram [--bridge <id>] user <Telegram user id>   (a number)")
		}
		req.Op, req.UserID = "user", id
	case "unlink":
		req.Op = "unlink"
	case "drop":
		req.Op = "drop"
	case "off":
		req.Op = "off"
	default:
		return fmt.Errorf("unknown telegram command %q: setup, link, unlink, drop, pair, user, off, status", sub)
	}
	var st telegram.BridgeStatus
	if err := callTimeout("POST", "/local/telegram", req, &st, 40*time.Second); err != nil {
		return err
	}
	if req.Op == "drop" {
		fmt.Printf("Dropped %d held message(s): deleted from this computer; they stay in your Telegram chat.\n", st.Dropped)
	}
	if req.Op == "link" && st.Delivered > 0 {
		fmt.Printf("Delivered %d held batch(es) to %s.\n", st.Delivered, st.Agent)
	}
	printBridge(st, st.ID != "default")
	return nil
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// readToken reads a bot token from the terminal without showing it, or from
// a pipe.
func readToken() (string, error) {
	fi, _ := os.Stdin.Stat()
	if fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprint(os.Stderr, "Paste the bot token from @BotFather (it isn't shown): ")
		if restore, err := echoOff(os.Stdin); err == nil {
			defer func() { restore(); fmt.Fprintln(os.Stderr) }()
		}
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	tok := strings.TrimSpace(line)
	if tok == "" {
		if err != nil {
			return "", fmt.Errorf("no token read: %w", err)
		}
		return "", errors.New("no token given")
	}
	return tok, nil
}

func printBridge(b telegram.BridgeStatus, named bool) {
	title := "Telegram"
	if named {
		title = "Telegram bridge " + b.ID
	}
	if !b.On {
		fmt.Printf("%s: off (no bot token; agentctl telegram setup", title)
		if named {
			fmt.Printf(" --bridge %s", b.ID)
		}
		fmt.Println(")")
	} else {
		bot := b.Bot
		if bot == "" {
			bot = "(checking the token)"
		}
		fmt.Printf("%s: %s\n", title, bot)
	}
	switch {
	case b.PairCode != "":
		fmt.Printf("  Pair it: in Telegram, open %s and send\n\n      /start %s\n\n  (valid %d minutes", b.Bot, b.PairCode, int(telegram.PairTTL/time.Minute))
		if strings.HasPrefix(b.Bot, "@") {
			fmt.Printf(", or open https://t.me/%s?start=%s", strings.TrimPrefix(b.Bot, "@"), b.PairCode)
		}
		fmt.Println(")")
	case b.UserID == 0:
		fmt.Println("  Paired with: nobody yet (agentctl telegram pair)")
	default:
		who := b.UserName
		if who == "" {
			who = "Telegram user " + strconv.FormatInt(b.UserID, 10)
		}
		fmt.Println("  Paired with:", who)
	}
	if b.Agent == "" {
		fmt.Println("  Linked to:   no agent yet (agentctl telegram link <agent id>)")
	} else {
		fmt.Println("  Linked to:  ", b.Agent)
	}
	if b.Held > 0 {
		fmt.Printf("  Held:        %d message(s) in %d batch(es), delivered when you link a running agent (or: agentctl telegram drop)\n", b.HeldMessages, b.Held)
	}
	if b.Waiting > 0 {
		fmt.Printf("  Waiting:     %d batch(es) to deliver\n", b.Waiting)
	}
	if b.Pending > 0 {
		fmt.Printf("  Arriving:    %d message(s), sent after a few quiet seconds\n", b.Pending)
	}
	if b.Ignored > 0 {
		fmt.Printf("  Ignored:     %d message(s) from other accounts since the service started\n", b.Ignored)
	}
	fmt.Println("  Files in:   ", b.Inbox)
	if b.Error != "" {
		fmt.Println("  Problem:    ", b.Error)
	}
}
