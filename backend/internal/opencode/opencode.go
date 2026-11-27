// Package opencode shows OpenCode's recent sessions on the board and talks to
// its local service for live status, permissions, prompts and interrupts.
// OpenCode (https://opencode.ai) keeps sessions in a SQLite store under its
// data folder; the store is opened through the system sqlite3, never written.
package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Home is OpenCode's data folder.
func Home() string {
	if d := os.Getenv("OPENCODE_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "opencode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode")
}

// DB is OpenCode's SQLite store.
func DB() string {
	if p := os.Getenv("OPENCODE_DB"); p != "" {
		return p
	}
	return filepath.Join(Home(), "opencode.db")
}

// Session is one recent OpenCode session, without its messages.
type Session struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Directory    string  `json:"directory"`
	ModelID      string  `json:"model_id"`
	ProviderID   string  `json:"provider_id"`
	TimeUpdated  int64   `json:"time_updated"`
	TimeBusy     int64   `json:"time_busy"`
	TimeIdle     int64   `json:"time_idle"`
	TokensInput  int64   `json:"tokens_input"`
	TokensOutput int64   `json:"tokens_output"`
	CacheRead    int64   `json:"tokens_cache_read"`
	CacheWrite   int64   `json:"tokens_cache_write"`
	Cost         float64 `json:"cost"`
	Turns        int     `json:"turns"`
}

// RecentSessions lists non-archived sessions updated within window plus the
// given ids (live sessions older than the window), newest first. No message
// content is read.
var ErrNoSQLite = errors.New("sqlite3 not found; install it to show OpenCode sessions")

func RecentSessions(window time.Duration, extraIDs []string) ([]Session, error) {
	db := DB()
	if _, err := os.Stat(db); err != nil {
		return nil, nil
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, ErrNoSQLite
	}
	since := time.Now().Add(-window).UnixMilli()
	where := "s.time_archived is null and s.time_updated >= " + strconv.FormatInt(since, 10)
	var extra []string
	for _, id := range extraIDs {
		valid := id != ""
		for _, r := range id {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				valid = false
				break
			}
		}
		if valid {
			extra = append(extra, "'"+id+"'")
		}
	}
	if len(extra) > 0 {
		where = "s.time_archived is null and (s.time_updated >= " + strconv.FormatInt(since, 10) +
			" or s.id in (" + strings.Join(extra, ",") + "))"
	}
	q := `select s.id, coalesce(s.title,'') as title, s.directory,
       coalesce(json_extract(s.model,'$.id'),'') as model_id,
       coalesce(json_extract(s.model,'$.providerID'),'') as provider_id,
       coalesce(s.time_updated,0) as time_updated,
       coalesce(s.time_busy,0) as time_busy, coalesce(s.time_idle,0) as time_idle,
       coalesce(s.tokens_input,0) as tokens_input, coalesce(s.tokens_output,0) as tokens_output,
       coalesce(s.tokens_cache_read,0) as tokens_cache_read,
       coalesce(s.tokens_cache_write,0) as tokens_cache_write, coalesce(s.cost,0) as cost,
       (select count(*) from session_message m where m.session_id=s.id and m.type='user') as turns
from session_v2 s
where ` + where + `
order by s.time_updated desc limit 32`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-readonly", "-json", db, q)
	proc.Tidy(cmd)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil, err
	}
	var list []Session
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// AgentID derives a stable board id from an OpenCode session id.
func AgentID(id string) string {
	sum := sha256.Sum256([]byte("opencode:" + id))
	return "k" + hex.EncodeToString(sum[:])[:5]
}

// Since is when the current state began, as far as the store tells.
func (s Session) Since(st model.Status) time.Time {
	if st == model.Running && s.TimeBusy > 0 {
		return time.UnixMilli(s.TimeBusy)
	}
	if s.TimeIdle > 0 {
		return time.UnixMilli(s.TimeIdle)
	}
	return time.UnixMilli(s.TimeUpdated)
}

// DisplayTitle is the session title, else its folder.
func (s Session) DisplayTitle() string {
	if s.Title != "" {
		return s.Title
	}
	return filepath.Base(s.Directory)
}

// ModelLabel is the short model name OpenCode used.
func (s Session) ModelLabel() string {
	if s.ModelID != "" {
		return s.ModelID
	}
	return s.ProviderID
}

// ModelName is the provider-qualified model id.
func (s Session) ModelName() string {
	if s.ProviderID != "" && s.ModelID != "" {
		return s.ProviderID + "/" + s.ModelID
	}
	return s.ModelID
}
