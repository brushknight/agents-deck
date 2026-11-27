package opencode

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

func TestNamesAndSince(t *testing.T) {
	s := Session{Title: "fix", Directory: "/work/app", ModelID: "m", ProviderID: "p",
		TimeUpdated: 1_800_000_100_000, TimeBusy: 1_800_000_000_000, TimeIdle: 1_799_999_000_000}
	if s.DisplayTitle() != "fix" || s.ModelName() != "p/m" || s.ModelLabel() != "m" {
		t.Fatalf("names: %+v", s)
	}
	if s.Since(model.Running).UnixMilli() != s.TimeBusy || s.Since(model.Idle).UnixMilli() != s.TimeIdle {
		t.Fatalf("since: %+v", s)
	}
}

func TestRecentSessionsFromStore(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("no sqlite3")
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "opencode.db")
	cmd := exec.Command("sqlite3", db, `
create table session_v2(id text primary key, project_id text, directory text, title text, model text, agent text,
 tokens_input integer default 0, tokens_output integer default 0, tokens_reasoning integer default 0,
 tokens_cache_read integer default 0, tokens_cache_write integer default 0, cost real default 0,
 time_created integer, time_updated integer, time_busy integer, time_idle integer,
 time_suspended integer, time_archived integer, idle_outcome text);
create table session_message(id text, session_id text, type text, seq integer, time_created integer, time_updated integer, data text);
insert into session_v2 values('ses_1','p','/work/app','try it','{"id":"m","providerID":"p"}','build',10,20,0,30,0,0.5,1,`+strconv.FormatInt(time.Now().UnixMilli(), 10)+`,`+strconv.FormatInt(time.Now().UnixMilli(), 10)+`,0,null,null,'succeeded');
insert into session_message values('m1','ses_1','user',1,1,1,'{}');
insert into session_message values('m2','ses_1','assistant',2,2,2,'{}');
insert into session_v2 values('ses_old','p','/work/old','old','{"id":"m"}','build',0,0,0,0,0,0,1,1,0,0,null,null,'succeeded');
insert into session_message values('m3','ses_old','user',1,1,1,'{}');
`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite: %v %s", err, out)
	}
	t.Setenv("OPENCODE_DB", db)
	list, err := RecentSessions(time.Hour, nil)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	s := list[0]
	if s.Title != "try it" || s.Turns != 1 || s.ModelID != "m" || s.TokensOutput != 20 || s.CacheRead != 30 {
		t.Fatalf("session: %+v", s)
	}
	all, err := RecentSessions(time.Hour, []string{"ses_old"})
	if err != nil || len(all) != 2 {
		t.Fatalf("extra session: %+v %v", all, err)
	}
}
