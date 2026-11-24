// Package model holds the wire types of protocol v1 (docs/protocol.md).
package model

import "time"

type Status string

const (
	Starting Status = "starting"
	Running  Status = "running"
	Waiting  Status = "waiting"
	Idle     Status = "idle"
	Error    Status = "error"
	Exited   Status = "exited"
)

type State struct {
	Server Server  `json:"server"`
	Agents []Agent `json:"agents"`
	// Magic is this screen's part in the magic trick (absent: none, or the
	// trick is off). Only the giver and the receiver get it.
	Magic *Magic `json:"magic,omitempty"`
	// Limits is how much of each account's usage is gone, as Claude Code
	// and Codex last reported it (absent: nothing reported yet).
	Limits []Limits `json:"limits,omitempty"`
}

// Limits is one account's usage limits in one tool: Claude Code per account
// (config folder), Codex per login. Only what the tools report themselves,
// for direct use of Claude Code and Codex.
type Limits struct {
	Tool    string        `json:"tool"`              // claude | codex
	Account string        `json:"account,omitempty"` // the account's name; "" with one account, and for Codex
	Plan    string        `json:"plan,omitempty"`    // the plan, as the tool names it ("" when not known)
	Level   string        `json:"level"`             // the worst window shown: ok | near | low | out
	AsOf    time.Time     `json:"asOf"`              // when the tool last reported
	Source  string        `json:"source"`            // statusline | transcript | rollout
	Windows []LimitWindow `json:"windows"`
}

// LimitWindow is one limit: a rolling window (the week), one model's week,
// or a spend limit.
type LimitWindow struct {
	ID       string    `json:"id"`                // seven_day, five_hour, model_week:<model>, spend, or <limit>:<window> for Codex
	Label    string    `json:"label"`             // "week", "Fable week", "5 hours", "spend"
	Minutes  int       `json:"minutes,omitempty"` // the window's length, when known
	Model    string    `json:"model,omitempty"`   // a per-model limit: the model family ("fable")
	Used     int       `json:"used"`              // percent used, 0..100 (a spend limit can pass 100)
	ResetsAt time.Time `json:"resetsAt,omitzero"` // when it resets
	Level    string    `json:"level"`             // ok (< 80) | near (80..94) | low (95..99) | out (100, or hit)
	Hit      bool      `json:"hit,omitempty"`     // the tool said this limit stopped it
	Reset    bool      `json:"reset,omitempty"`   // reset since the last report: Used is not known now
	Hidden   bool      `json:"hidden,omitempty"`  // kept for the record, not shown (five_hour)
}

// Magic: the giver (its agents are taken) or the receiver (it takes them
// with a knock), whether they are taken now, and where this screen's tiles
// fly: in from Arrive, out to Leave (corners: top-left, top-right,
// bottom-left, bottom-right).
type Magic struct {
	Role   string `json:"role"` // giver | receiver
	Taken  bool   `json:"taken"`
	Arrive string `json:"arrive"`
	Leave  string `json:"leave"`
}

type Server struct {
	Name    string    `json:"name"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}

// LastPromptMax is how much of an agent's last prompt the deck keeps: enough
// for the sidebar on Home to show it all, mostly. Boards get the first 160
// characters (link.options).
const LastPromptMax = 1000

type Agent struct {
	ID   string `json:"id"`
	Slot int    `json:"slot"`
	// AttentionSlot is its spot on the "Needs you" grid, 1..16 (4 x 4, by
	// rows), or 0 while it isn't there. The service decides it (store/attention.go).
	AttentionSlot int        `json:"attentionSlot"`
	Title         string     `json:"title"`
	AITitle       string     `json:"aiTitle"`
	Tool          string     `json:"tool"`
	Status        Status     `json:"status"`
	StatusSince   time.Time  `json:"statusSince"`
	Cwd           string     `json:"cwd"`
	Folder        string     `json:"folder"`
	Branch        string     `json:"branch"`
	Model         string     `json:"model"`
	ModelLabel    string     `json:"modelLabel"`
	Effort        string     `json:"effort,omitempty"` // reasoning effort of its latest turn (low, medium, high, xhigh, max)
	Activity      *Activity  `json:"activity"`
	LastPrompt    string     `json:"lastPrompt"` // up to LastPromptMax characters; boards get the first 160
	Waiting       *Prompt    `json:"waiting"`
	Error         *ErrInfo   `json:"error"`
	Tokens        Tokens     `json:"tokens"`
	Context       Context    `json:"context"`
	CostUSD       float64    `json:"costUsd"`
	Turns         int        `json:"turns"`
	Focused       bool       `json:"focused"`
	Attached      bool       `json:"attached"`
	Resumable     bool       `json:"resumable"`           // exited Claude agent that POST /resume can bring back
	Lost          bool       `json:"lost,omitempty"`      // ended with the tmux server, not by itself: POST /v1/restore brings it back
	Unseen        bool       `json:"unseen"`              // finished a turn you haven't looked at yet ("hungry")
	External      bool       `json:"external"`            // owned by another app (Codex): view and focus only
	App           string     `json:"app,omitempty"`       // the app it runs in when not agentctl's: "orca", "herdr", "cmux" (answer, interrupt and focus work there)
	Subagents     []Subagent `json:"subagents,omitempty"` // subagents still working, oldest first
	Queued        int        `json:"queued"`              // prompts waiting to be typed in once its turn ends ("mode": "queue")
	Hat           *Hat       `json:"hat,omitempty"`       // the pixel hat for its working folder (nil: none)
	Account       *Account   `json:"account,omitempty"`   // the Claude account it runs in (nil: only one account is set up)
	StartedAt     time.Time  `json:"startedAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastActive    time.Time  `json:"lastActive,omitzero"` // its latest message sent or received (zero: not known yet)
	OutUntil      time.Time  `json:"outUntil,omitzero"`   // its account (or its model) is out of usage until then (zero: not out)
}

// Hat is the pixel hat an agent wears, picked by its working folder: shape
// and colour from a stable hash of the folder (Auto), or chosen by hand.
type Hat struct {
	Shape string `json:"shape"` // hard hat, cap, beanie, helmet, wizard, beret, chef, crown, headphones, propeller, top hat, cowboy, bandana
	Color string `json:"color"` // teal, blue, yellow, green, purple, pink, sky, ink
	Auto  bool   `json:"auto"`  // picked by the hash, not by hand
}

// Account is the Claude Code account (config folder) a session runs in.
type Account struct {
	Name  string `json:"name"`
	Color string `json:"color"` // teal, blue, yellow, green, purple, pink, sky, ink (the hat colours)
}

// Subagent is one of an agent's subagents that is still working.
type Subagent struct {
	ID    string `json:"id"`
	Title string `json:"title"`          // the task description it was given
	Type  string `json:"type,omitempty"` // agent type (Explore, general-purpose, fork…)
	Tool  string `json:"tool,omitempty"` // tool of its latest call, for the critter's pose
}

type Activity struct {
	Tool   string `json:"tool"`
	Detail string `json:"detail"`
}

type Prompt struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // permission | question | input
	Title   string   `json:"title"`
	Detail  string   `json:"detail"`
	Context string   `json:"context"`
	Options []Option `json:"options"`
}

type Option struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Primary  bool   `json:"primary,omitempty"`
	Selected bool   `json:"selected,omitempty"` // ticked: a pick-any question's answer so far
}

type ErrInfo struct {
	Message string `json:"message"`
}

type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

type Context struct {
	Used   int64 `json:"used"`
	Window int64 `json:"window"`
}
