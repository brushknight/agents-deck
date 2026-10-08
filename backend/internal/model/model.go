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
}

type Server struct {
	Name    string    `json:"name"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}

type Agent struct {
	ID          string     `json:"id"`
	Slot        int        `json:"slot"`
	Title       string     `json:"title"`
	AITitle     string     `json:"aiTitle"`
	Tool        string     `json:"tool"`
	Status      Status     `json:"status"`
	StatusSince time.Time  `json:"statusSince"`
	Cwd         string     `json:"cwd"`
	Folder      string     `json:"folder"`
	Branch      string     `json:"branch"`
	Model       string     `json:"model"`
	ModelLabel  string     `json:"modelLabel"`
	Activity    *Activity  `json:"activity"`
	LastPrompt  string     `json:"lastPrompt"`
	Waiting     *Prompt    `json:"waiting"`
	Error       *ErrInfo   `json:"error"`
	Tokens      Tokens     `json:"tokens"`
	Context     Context    `json:"context"`
	CostUSD     float64    `json:"costUsd"`
	Turns       int        `json:"turns"`
	Focused     bool       `json:"focused"`
	Attached    bool       `json:"attached"`
	Resumable   bool       `json:"resumable"`           // exited Claude agent that POST /resume can bring back
	Unseen      bool       `json:"unseen"`              // finished a turn you haven't looked at yet ("hungry")
	External    bool       `json:"external"`            // owned by another app (Codex): view and focus only
	Subagents   []Subagent `json:"subagents,omitempty"` // subagents still working, oldest first
	StartedAt   time.Time  `json:"startedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
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
	Key     string `json:"key"`
	Label   string `json:"label"`
	Primary bool   `json:"primary,omitempty"`
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
