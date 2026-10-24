package link

import (
	"bufio"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

const repo = "../../.."

// The Go limits are link.options, entry for entry.
func TestLimitsMatchOptions(t *testing.T) {
	f, err := os.Open(repo + "/proto/agentsdeck/link/v1/link.options")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := map[string]limit{}
	re := regexp.MustCompile(`^agentsdeck\.link\.v1\.(\S+)\s+(.*)$`)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := re.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m == nil {
			continue
		}
		var l limit
		for _, kv := range strings.Fields(m[2]) {
			k, v, _ := strings.Cut(kv, ":")
			n := 0
			for _, c := range v {
				n = n*10 + int(c-'0')
			}
			switch k {
			case "max_size":
				l.size = n
			case "max_count":
				l.count = n
			}
		}
		got[m[1]] = l
	}
	if !reflect.DeepEqual(got, limits) {
		for k, v := range got {
			if limits[k] != v {
				t.Errorf("%s: link.options %+v, limits.go %+v", k, v, limits[k])
			}
		}
		for k := range limits {
			if _, ok := got[k]; !ok {
				t.Errorf("%s: in limits.go, not in link.options", k)
			}
		}
	}
}

// Every web API endpoint in docs/protocol.md has a Deck Link command, every
// command in link.proto has a route, and the other way round.
func TestEndpointParity(t *testing.T) {
	doc, err := os.ReadFile(repo + "/docs/protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	endpoints := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| (GET|POST) \\| `(/v1/[^`]+)`").FindAllStringSubmatch(string(doc), -1) {
		if m[2] == "/v1/events" || m[2] == "/v1/pair" {
			continue // the stream is Subscribe + updates; pairing is for network devices
		}
		endpoints[m[1]+" "+m[2]] = true
	}
	if len(endpoints) < 12 {
		t.Fatalf("found only %d endpoints in docs/protocol.md", len(endpoints))
	}
	routed := map[string]bool{}
	for _, r := range Routes {
		for _, one := range strings.Split(r, " | ") {
			routed[one] = true
		}
	}
	for e := range endpoints {
		if !routed[e] {
			t.Errorf("web API %s has no Deck Link command (link.proto, Routes in session.go)", e)
		}
	}
	for e := range routed {
		if !endpoints[e] {
			t.Errorf("Deck Link route %s is not in docs/protocol.md", e)
		}
	}
	// Commands are Frame fields 30..49; each needs a route.
	fields := (&linkpb.Frame{}).ProtoReflect().Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if n := fd.Number(); n >= 30 && n < 50 {
			if _, ok := Routes[string(fd.Name())]; !ok {
				t.Errorf("command %s has no route", fd.Name())
			}
		}
	}
}

// Every field of the JSON Agent has a protobuf field (times as *_ms), and
// toPB fills every protobuf field when all groups are subscribed.
func TestAgentParity(t *testing.T) {
	desc := (&linkpb.Agent{}).ProtoReflect().Descriptor()
	jsonNames := map[string]bool{}
	for i := range desc.Fields().Len() {
		jsonNames[desc.Fields().Get(i).JSONName()] = true
	}
	typ := reflect.TypeOf(model.Agent{})
	for i := range typ.NumField() {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if !jsonNames[tag] && !jsonNames[tag+"Ms"] {
			t.Errorf("Agent JSON field %q has no Deck Link field (link.proto Agent)", tag)
		}
	}

	now := time.Now()
	full := model.Agent{ID: "a1", Slot: 3, Title: "t", AITitle: "ai", Tool: "claude", Status: model.Waiting, StatusSince: now,
		Cwd: "/x/y", Folder: "y", Branch: "main", Model: "m", ModelLabel: "M", Effort: "high",
		Activity: &model.Activity{Tool: "Edit", Detail: "f.go"}, LastPrompt: "do it",
		Waiting: &model.Prompt{ID: "p", Kind: "question", Title: "?", Detail: "d", Context: "c", Options: []model.Option{{Key: "1", Label: "yes"}}},
		Error:   &model.ErrInfo{Message: "boom"}, Tokens: model.Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
		Context: model.Context{Used: 5, Window: 10}, CostUSD: 1.5, Turns: 2, Focused: true, Attached: true, Resumable: true,
		Lost: true, Unseen: true, External: true, App: "orca", Hat: &model.Hat{Shape: "cap", Color: "teal", Auto: true}, Account: &model.Account{Name: "work", Color: "yellow"},
		Subagents: []model.Subagent{{ID: "s", Title: "sub", Type: "Explore", Tool: "Grep"}}, StartedAt: now, UpdatedAt: now}
	p := toPB(full, 1|2|4|8).ProtoReflect()
	var missing []string
	for i := range desc.Fields().Len() {
		if fd := desc.Fields().Get(i); !p.Has(fd) {
			missing = append(missing, string(fd.Name()))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("toPB leaves these Agent fields unset with every group subscribed: %v", missing)
	}
	// A tile-only subscription leaves the card fields out.
	tile := toPB(full, uint32(linkpb.FieldGroup_FIELDS_TILE))
	if tile.Cwd != "" || tile.Waiting != nil || tile.Tokens != nil || tile.Title != "t" || tile.SubagentCount != 1 {
		t.Errorf("tile fields: %+v", tile)
	}
}

func TestClipString(t *testing.T) {
	for _, c := range []struct {
		in   string
		size int
		max  int
	}{{"short", 12, 5}, {strings.Repeat("é", 50), 12, 11}, {strings.Repeat("a", 100), 64, 63}} {
		out := clipString(c.in, c.size)
		if len(out) > c.max || !strings.HasPrefix(c.in, strings.TrimSuffix(out, "…")) {
			t.Errorf("clip(%q, %d) = %q", c.in, c.size, out)
		}
	}
}

// Pairing and Wi-Fi (Frame fields 60 and up) aren't web API commands: each
// is documented in docs/link/protocol.md, and the Wi-Fi transport, GET
// /v1/link, is in the transports table of docs/protocol.md (not the endpoint
// table: it carries Deck Link frames, not an API call).
func TestPairingAndWifiDocumented(t *testing.T) {
	linkDoc, err := os.ReadFile(repo + "/docs/link/protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	fields := (&linkpb.Frame{}).ProtoReflect().Descriptor().Fields()
	n := 0
	for i := range fields.Len() {
		fd := fields.Get(i)
		if fd.Number() < 60 {
			continue
		}
		n++
		if msg := string(fd.Message().Name()); !strings.Contains(string(linkDoc), "`"+msg) {
			t.Errorf("%s (Frame field %d) isn't in docs/link/protocol.md", msg, fd.Number())
		}
		if _, ok := Routes[string(fd.Name())]; ok {
			t.Errorf("%s has a web API route: pairing and Wi-Fi have none", fd.Name())
		}
	}
	if n < 4 {
		t.Fatalf("found %d pairing and Wi-Fi messages", n)
	}
	doc, err := os.ReadFile(repo + "/docs/protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(doc), "## Endpoints")
	if !strings.Contains(head, "`GET /v1/link`") {
		t.Error("docs/protocol.md: the transports table doesn't list GET /v1/link")
	}
	if regexp.MustCompile("(?m)^\\| (GET|POST) \\| `/v1/link`").MatchString(string(doc)) {
		t.Error("docs/protocol.md lists /v1/link as an API endpoint: it is a transport")
	}
}
