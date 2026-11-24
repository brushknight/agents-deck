package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/limits"
)

// RateLimitReport is the newest rate_limits Codex wrote for one limit id,
// and when.
type RateLimitReport struct {
	At     time.Time
	Limits limits.CodexRateLimits
}

// Newest is the most recently written rollout under sessions in the last
// days (rollouts sit in the folder of the day their session started), with
// its modification time; "" when there is none.
func Newest(sessions string, days int, now time.Time) (string, time.Time) {
	best, when := "", time.Time{}
	for d := 0; d <= days; d++ {
		day := now.AddDate(0, 0, -d)
		m, _ := filepath.Glob(filepath.Join(sessions, day.Format("2006"), day.Format("01"), day.Format("02"), "rollout-*.jsonl"))
		for _, f := range m {
			if st, err := os.Stat(f); err == nil && st.ModTime().After(when) {
				best, when = f, st.ModTime()
			}
		}
	}
	return best, when
}

const rateTail = 1 << 20

// RateLimits reads the last rate_limits per limit id from the end of a
// rollout (token_count events carry them on every turn).
func RateLimits(path string) []RateLimitReport {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := max(st.Size()-rateTail, 0)
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	buf, _ := io.ReadAll(io.LimitReader(f, rateTail))
	if off > 0 { // skip the cut line
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	last := map[string]RateLimitReport{}
	var order []string
	sc := bufio.NewScanner(bytes.NewReader(buf))
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		b := sc.Bytes()
		if !bytes.Contains(b, []byte(`"rate_limits"`)) {
			continue
		}
		var l struct {
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Type       string                  `json:"type"`
				RateLimits *limits.CodexRateLimits `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(b, &l) != nil || l.Payload.Type != "token_count" || l.Payload.RateLimits == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil {
			continue
		}
		id := l.Payload.RateLimits.LimitID
		if _, ok := last[id]; !ok {
			order = append(order, id)
		}
		last[id] = RateLimitReport{At: at, Limits: *l.Payload.RateLimits}
	}
	out := make([]RateLimitReport, 0, len(order))
	for _, id := range order {
		out = append(out, last[id])
	}
	return out
}
