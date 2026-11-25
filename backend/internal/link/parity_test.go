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

	"google.golang.org/protobuf/reflect/protoreflect"

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
	// Endpoints that aren't board commands, and their Deck Link side (a
	// Frame field; "" for none): the stream is Subscribe + updates; pairing
	// is for network devices; a board downloads its firmware update after
	// the deck's FirmwareOffer, which names the path.
	notCommands := map[string]string{
		"GET /v1/events":                     "subscribe",
		"POST /v1/pair":                      "",
		"GET /v1/firmware/{board}/{version}": "firmware_offer",
	}
	frame := (&linkpb.Frame{}).ProtoReflect().Descriptor().Fields()
	endpoints := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| (GET|POST) \\| `(/v1/[^`]+)`").FindAllStringSubmatch(string(doc), -1) {
		e := m[1] + " " + m[2]
		if side, ok := notCommands[e]; ok {
			if side != "" && frame.ByName(protoreflect.Name(side)) == nil {
				t.Errorf("%s: its Deck Link side %s isn't in link.proto", e, side)
			}
			delete(notCommands, e)
			continue
		}
		endpoints[e] = true
	}
	for e, side := range notCommands {
		if side != "" {
			t.Errorf("%s isn't in docs/protocol.md", e)
		}
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
	// Commands are Frame fields 30..49 and 70..89; each needs a route.
	fields := (&linkpb.Frame{}).ProtoReflect().Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if n := fd.Number(); n >= 30 && n < 50 || n >= 70 && n < 90 {
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
	full := model.Agent{ID: "a1", Slot: 3, AttentionSlot: 2, Title: "t", AITitle: "ai", Tool: "claude", Status: model.Waiting, StatusSince: now,
		Cwd: "/x/y", Folder: "y", Branch: "main", Model: "m", ModelLabel: "M", Effort: "high",
		Activity: &model.Activity{Tool: "Edit", Detail: "f.go"}, LastPrompt: "do it", LastAnswer: "done", LastAnswerAt: now,
		Waiting: &model.Prompt{ID: "p", Kind: "question", Title: "?", Detail: "d", Context: "c", Options: []model.Option{{Key: "1", Label: "yes"}}},
		Error:   &model.ErrInfo{Message: "boom"}, Tokens: model.Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
		Context: model.Context{Used: 5, Window: 10}, CostUSD: 1.5, Turns: 2, Focused: true, Attached: true, Resumable: true,
		Lost: true, Unseen: true, External: true, App: "orca", Hat: &model.Hat{Shape: "cap", Color: "teal", Auto: true}, Account: &model.Account{Name: "work", Color: "yellow"},
		Project: &model.Project{ID: "p0a1b2c3d", Name: "harbor"}, ProjectFolder: "/x",
		Subagents: []model.Subagent{{ID: "s", Title: "sub", Type: "Explore", Tool: "Grep"}}, Queued: 2, Slowed: true, SlowedByYou: true, CPU: 12.5,
		Pet: &model.Pet{Name: "Nib", Stage: "kid", Level: 6, Ate: 160000, LevelFrom: 152588, NextAt: 190735, Hearts: 3, Mood: "fed",
			Ready: true, Hat: &model.PetHat{Shape: "devil horns", Color: "teal"}, HatchedAt: now},
		StartedAt: now, UpdatedAt: now, LastActive: now.Add(-time.Hour), OutUntil: now.Add(time.Hour)}
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
	if got := toPB(full, 2).LastActiveMs; got != full.LastActive.UnixMilli() {
		t.Errorf("last_active_ms = %d, want %d", got, full.LastActive.UnixMilli())
	}
	if got := toPB(model.Agent{ID: "a2"}, 2).LastActiveMs; got != 0 {
		t.Errorf("unknown last activity: last_active_ms = %d, want 0", got)
	}
	// A tile-only subscription leaves the card fields out.
	tile := toPB(full, uint32(linkpb.FieldGroup_FIELDS_TILE))
	if tile.Cwd != "" || tile.Waiting != nil || tile.Tokens != nil || tile.Title != "t" || tile.SubagentCount != 1 || tile.AttentionSlot != 2 ||
		!tile.Slowed || !tile.SlowedByYou || tile.Cpu != 0 {
		t.Errorf("tile fields: %+v", tile)
	}
	// The project goes with the tile, the folder it matched with the card;
	// an agent in no project has neither.
	if tile.GetProject().GetId() != "p0a1b2c3d" || tile.GetProject().GetName() != "harbor" || tile.ProjectFolder != "" {
		t.Errorf("tile project: %v, folder %q", tile.GetProject(), tile.ProjectFolder)
	}
	if card := toPB(full, uint32(linkpb.FieldGroup_FIELDS_CARD)); card.ProjectFolder != "/x" || card.Project != nil {
		t.Errorf("card project: %v, folder %q", card.GetProject(), card.ProjectFolder)
	}
	if none := toPB(model.Agent{ID: "a3"}, 1|2); none.Project != nil || none.ProjectFolder != "" {
		t.Errorf("no project: %v, folder %q", none.GetProject(), none.ProjectFolder)
	}
	long := full
	long.Project = &model.Project{ID: "p0a1b2c3d", Name: strings.Repeat("n", 60)}
	if n := toPB(long, 1).GetProject().GetName(); len(n) > 47 || !strings.HasSuffix(n, "…") {
		t.Errorf("a long project name isn't clipped to Project.name: %q", n)
	}
	if n := toPB(full, uint32(linkpb.FieldGroup_FIELDS_NUMBERS)); n.Cpu != 12.5 || n.Slowed {
		t.Errorf("numbers: cpu %v, slowed %v", n.Cpu, n.Slowed)
	}
}

// Prompt and Option: every JSON field has a protobuf field, and toPB fills it.
func TestPromptParity(t *testing.T) {
	for _, c := range []struct {
		typ  reflect.Type
		desc protoreflect.MessageDescriptor
	}{
		{reflect.TypeOf(model.Prompt{}), (&linkpb.Prompt{}).ProtoReflect().Descriptor()},
		{reflect.TypeOf(model.Option{}), (&linkpb.Option{}).ProtoReflect().Descriptor()},
	} {
		for i := range c.typ.NumField() {
			tag := strings.Split(c.typ.Field(i).Tag.Get("json"), ",")[0]
			if tag != "" && tag != "-" && c.desc.Fields().ByJSONName(tag) == nil {
				t.Errorf("%s JSON field %q has no Deck Link field (link.proto %s)", c.typ.Name(), tag, c.desc.Name())
			}
		}
	}
	a := model.Agent{ID: "a1", Status: model.Waiting, Waiting: &model.Prompt{ID: "p", Kind: "question", Title: "?", Detail: "d", Context: "c",
		Options: []model.Option{{Key: "1", Label: "cheese", Primary: true, Selected: true}}}}
	o := toPB(a, uint32(linkpb.FieldGroup_FIELDS_PROMPT)).GetWaiting().GetOptions()
	if len(o) != 1 {
		t.Fatalf("options: %v", o)
	}
	desc := o[0].ProtoReflect().Descriptor()
	for i := range desc.Fields().Len() {
		if fd := desc.Fields().Get(i); !o[0].ProtoReflect().Has(fd) {
			t.Errorf("toPB leaves Option.%s unset", fd.Name())
		}
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
		if fd.Number() < 60 || fd.Number() >= 70 {
			continue // 70..89 are commands again
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

// Every JSON field of the usage limits has a Deck Link field, except the
// ones only the web and apps use (named below).
func TestLimitsParity(t *testing.T) {
	webOnly := map[string]bool{"Limits.plan": true, "Limits.source": true, "Limits.windows": true, "LimitWindow.id": true, "LimitWindow.minutes": true,
		"LimitWindow.model": true, "LimitWindow.hidden": true}
	renamed := map[string]string{"LimitWindow.reset": "stale", "Limits.windows": "windows"}
	for _, c := range []struct {
		typ  reflect.Type
		desc protoreflect.MessageDescriptor
	}{
		{reflect.TypeOf(model.Limits{}), (&linkpb.LimitSet{}).ProtoReflect().Descriptor()},
		{reflect.TypeOf(model.LimitWindow{}), (&linkpb.LimitWindow{}).ProtoReflect().Descriptor()},
	} {
		names := map[string]bool{}
		for i := range c.desc.Fields().Len() {
			names[c.desc.Fields().Get(i).JSONName()] = true
		}
		for i := range c.typ.NumField() {
			tag := strings.Split(c.typ.Field(i).Tag.Get("json"), ",")[0]
			key := c.typ.Name() + "." + tag
			if r, ok := renamed[key]; ok {
				tag = r
			} else if webOnly[key] {
				continue
			}
			if !names[tag] && !names[tag+"Ms"] {
				t.Errorf("%s has no Deck Link field (link.proto %s)", key, c.desc.Name())
			}
		}
	}
	now := time.Now()
	ls := []model.Limits{{Tool: "claude", Account: "work", Level: "near", AsOf: now, Windows: []model.LimitWindow{
		{ID: "seven_day", Label: "week", Used: 86, ResetsAt: now.Add(time.Hour), Level: "near"},
		{ID: "five_hour", Label: "5 hours", Used: 12, Level: "ok", Hidden: true},
		{ID: "model_week:fable", Label: "Fable week", Model: "fable", Used: 100, Hit: true, Level: "out"},
	}}, {Tool: "codex", Level: "ok", Windows: []model.LimitWindow{{ID: "five_hour", Hidden: true}}}}
	pb := limitsPB(ls)
	if len(pb.Sets) != 1 || len(pb.Sets[0].Windows) != 2 {
		t.Fatalf("hidden windows (and sets left empty) stay off the link: %+v", pb)
	}
	s := pb.Sets[0]
	if s.Tool != linkpb.Tool_TOOL_CLAUDE || s.Account != "work" || s.Level != linkpb.LimitLevel_LIMIT_NEAR || s.AsOfMs != now.UnixMilli() {
		t.Errorf("set: %+v", s)
	}
	if w := s.Windows[1]; w.Label != "Fable week" || !w.Hit || w.Level != linkpb.LimitLevel_LIMIT_OUT || w.Used != 100 {
		t.Errorf("fable: %+v", w)
	}
}
