package claude

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// An AskUserQuestion with several questions, or with a question that takes
// more than one answer, is a form in Claude Code: a tab per question and a
// review tab ("Submit answers" / "Cancel"). On a pick-one tab a number picks
// and moves on; on a pick-any tab a number ticks an answer and Tab moves on.
// The deck shows one step of the form at a time as a question prompt; each
// answer builds the next step with a new prompt id.

type askInput struct {
	Questions []struct {
		Question    string `json:"question"`
		Header      string `json:"header"`
		MultiSelect bool   `json:"multiSelect"`
		Options     []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
}

// newForm builds the form for an AskUserQuestion input, or nil when the input
// is a single pick-one question (one key answers it) or can't be walked from
// the deck (a question without options).
func newForm(input json.RawMessage) *store.Form {
	var in askInput
	if json.Unmarshal(input, &in) != nil || len(in.Questions) == 0 {
		return nil
	}
	if len(in.Questions) == 1 && !in.Questions[0].MultiSelect {
		return nil
	}
	f := &store.Form{}
	for _, q := range in.Questions {
		if len(q.Options) == 0 {
			return nil
		}
		fq := store.FormQuestion{Question: strings.TrimSpace(q.Question), Header: strings.TrimSpace(q.Header), Multi: q.MultiSelect}
		for _, o := range q.Options {
			fq.Options = append(fq.Options, store.FormOption{Label: o.Label, Description: o.Description})
		}
		f.Questions = append(f.Questions, fq)
	}
	f.Picked = make([][]int, len(f.Questions))
	f.Answered = make([]bool, len(f.Questions))
	return f
}

// ActiveForm is the entry's form when it built the prompt that is open now.
func ActiveForm(e *store.Entry) *store.Form {
	f := e.Form
	if f == nil || e.A.Waiting == nil || e.A.Waiting.ID != f.PromptID || e.A.Status != model.Waiting {
		return nil
	}
	return f
}

// formPrompt builds the prompt for the form's current step, with a new id.
func formPrompt(e *store.Entry) *model.Prompt {
	f := e.Form
	n := len(f.Questions)
	p := &model.Prompt{ID: e.NextPromptID(), Kind: "question"}
	f.PromptID = p.ID
	if f.Step >= n {
		p.Title = "submit these answers?"
		p.Context = fmt.Sprintf("review · %d %s", n, plural(n, "question", "questions"))
		var lines []string
		for i, q := range f.Questions {
			lines = append(lines, lower(q.Question)+" → "+formAnswerText(f, i))
		}
		p.Detail = clip(strings.Join(lines, "\n"), 2000)
		p.Options = []model.Option{{Key: "1", Label: "submit answers", Primary: true}, {Key: "2", Label: "cancel"}}
		return p
	}
	q := f.Questions[f.Step]
	p.Title = lower(q.Question)
	p.Context = fmt.Sprintf("question %d of %d", f.Step+1, n)
	if q.Header != "" {
		p.Context = lower(q.Header) + " · " + p.Context
	}
	if q.Multi {
		p.Context += " · pick any"
	}
	for i, o := range q.Options {
		label := o.Label
		if o.Description != "" {
			label += " — " + o.Description
		}
		p.Options = append(p.Options, model.Option{Key: strconv.Itoa(i + 1), Label: label,
			Primary: !q.Multi && i == 0, Selected: q.Multi && picked(f, f.Step, i)})
	}
	if q.Multi {
		next := "next — on to the next question"
		if f.Step == n-1 {
			next = "next — review your answers"
		}
		p.Options = append(p.Options, model.Option{Key: strconv.Itoa(len(q.Options) + 1), Label: next, Primary: true})
	}
	return p
}

func formAnswerText(f *store.Form, i int) string {
	if !f.Answered[i] && len(f.Picked[i]) == 0 {
		return "no answer"
	}
	var labels []string
	for _, k := range f.Picked[i] {
		labels = append(labels, f.Questions[i].Options[k].Label)
	}
	if len(labels) == 0 {
		if f.Questions[i].Multi && f.Answered[i] {
			return "none"
		}
		return "answered in the terminal"
	}
	return strings.Join(labels, ", ")
}

func picked(f *store.Form, q, opt int) bool {
	for _, k := range f.Picked[q] {
		if k == opt {
			return true
		}
	}
	return false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// FormKeys is what to type into Claude Code for a deck key on the form's
// current step: text typed as-is, or named tmux keys. ok is false for a key
// the step doesn't have.
//
// How the form takes keys (Claude Code 2.1.306, tried with tmux send-keys):
//   - One pick-one question is no form: no tab row, a number answers at once.
//   - Anything else (two or more questions, or any pick-any question, even
//     alone) has a tab row "← ☐ Color ☐ Size ☐ Toppings ✔ Submit →"; a
//     question's box turns ☒ once answered.
//   - Pick-one tab: a number picks and moves to the next tab; after the last
//     question comes the review.
//   - Pick-any tab: rows "1. [ ] Cheese"; a number ticks that row (the cursor
//     stays), again unticks; answers are kept in tick order. Tab moves on to
//     the next tab or the review (also for a lone pick-any question).
//   - Review: "Review your answers", then "1. Submit answers" / "2. Cancel".
//     1 sends the answers ("User answered Claude's questions"), 2 cancels
//     ("User declined to answer questions") and the turn ends. Esc cancels
//     anywhere.
//   - Every question also ends with "Type something" and "Chat about this"
//     rows (free text): the deck doesn't offer them; focus terminal does.
func FormKeys(f *store.Form, key string) (text string, keys []string, ok bool) {
	k, err := strconv.Atoi(key)
	if err != nil || k < 1 {
		return "", nil, false
	}
	if f.Step >= len(f.Questions) {
		if k > 2 {
			return "", nil, false
		}
		return key, nil, true // 1. Submit answers, 2. Cancel
	}
	q := f.Questions[f.Step]
	switch {
	case k <= len(q.Options):
		return key, nil, true // picks (pick-one) or ticks (pick-any)
	case q.Multi && k == len(q.Options)+1:
		return "", []string{"Tab"}, true // done: on to the next tab
	}
	return "", nil, false
}

// FormDone tells what an answer typed into the form did.
type FormDone int

const (
	FormNext      FormDone = iota // the form is still open: a new step is waiting
	FormSubmitted                 // the answers went to Claude
	FormCancelled                 // the form was cancelled
)

// AdvanceForm records an answer typed into the open form and opens its next
// step (a new Waiting prompt), or closes it.
func AdvanceForm(e *store.Entry, key string) FormDone {
	f := e.Form
	k, _ := strconv.Atoi(key)
	f.Typed = time.Now()
	if f.Step >= len(f.Questions) {
		e.Form = nil
		if k == 1 {
			e.SetStatus(model.Running)
			return FormSubmitted
		}
		e.SetStatus(model.Idle) // Claude stops and waits for you
		return FormCancelled
	}
	q := f.Questions[f.Step]
	switch {
	case k <= len(q.Options) && q.Multi:
		if picked(f, f.Step, k-1) {
			out := f.Picked[f.Step][:0:0]
			for _, x := range f.Picked[f.Step] {
				if x != k-1 {
					out = append(out, x)
				}
			}
			f.Picked[f.Step] = out
		} else {
			f.Picked[f.Step] = append(f.Picked[f.Step], k-1)
		}
	case k <= len(q.Options):
		f.Picked[f.Step], f.Answered[f.Step] = []int{k - 1}, true
		f.Step++
	default: // done with a pick-any question
		f.Answered[f.Step] = true
		f.Step++
	}
	e.SetStatus(model.Waiting)
	e.A.Waiting = formPrompt(e)
	return FormNext
}

var (
	// tabBarRe is the form's tab row: "←  ☒ Color  ☐ Size  ✔ Submit  →".
	tabBarRe = regexp.MustCompile(`[☐☒].*✔\s*Submit`)
	// tickRe is one pick-any row: "❯ 1. [✔] Cheese".
	tickRe = regexp.MustCompile(`^[\s│|]*(?:[❯›>]\s*)?(\d{1,2})[.)]\s+\[(.)\]`)
)

// SyncForm reads the form off the agent's screen and follows it when it
// shows another tab than the deck thinks (answered in the terminal), or other
// ticks on a pick-any tab. It reports whether the prompt changed. A screen
// without the form leaves it alone.
func SyncForm(e *store.Entry, screen string) bool {
	f := ActiveForm(e)
	if f == nil || f.Screen {
		return false
	}
	lines := strings.Split(screen, "\n")
	bar := -1
	for i, l := range lines {
		if tabBarRe.MatchString(l) {
			bar = i
		}
	}
	if bar < 0 {
		return false
	}
	step := -1
	body := bar + 1
	for ; body < len(lines); body++ {
		t := strings.TrimSpace(lines[body])
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Review your answers") {
			step = len(f.Questions)
		} else {
			for i, q := range f.Questions {
				if t == q.Question || (q.Question != "" && strings.HasPrefix(q.Question, t)) {
					step = i
					break
				}
			}
		}
		break
	}
	if step < 0 {
		return false
	}
	changed := step != f.Step
	// The tab row marks answered questions ☒, in order.
	marks := []rune{}
	for _, r := range lines[bar] {
		if r == '☐' || r == '☒' {
			marks = append(marks, r)
		}
	}
	if len(marks) == len(f.Questions) {
		for i, m := range marks {
			if a := m == '☒'; a != f.Answered[i] && !f.Questions[i].Multi {
				f.Answered[i] = a
				if !a {
					f.Picked[i] = nil
				}
				changed = true
			}
		}
	}
	if step < len(f.Questions) && f.Questions[step].Multi {
		var ticks []int
		seen := false
		for _, l := range lines[body:] {
			m := tickRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			seen = true
			n, _ := strconv.Atoi(m[1])
			if n >= 1 && n <= len(f.Questions[step].Options) && m[2] != " " {
				ticks = append(ticks, n-1)
			}
		}
		if seen && !sameInts(ticks, f.Picked[step]) {
			f.Picked[step] = ticks
			changed = true
		}
	}
	if !changed {
		return false
	}
	f.Step = step
	e.A.Waiting = formPrompt(e)
	return true
}

// sameInts compares two sets of indexes.
func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	in := map[int]bool{}
	for _, x := range a {
		in[x] = true
	}
	for _, x := range b {
		if !in[x] {
			return false
		}
	}
	return true
}

// boxRe is a pick-any row's tick box: "[✔] Cheese", "[ ] Ham".
var boxRe = regexp.MustCompile(`^\[(.)\]\s*`)

// ScreenFormPrompt reads the tab of a question form that is on screen, for an
// agent whose hooks the deck doesn't get (it runs in Orca, herdr or cmux): the
// question with its answers (ticked ones selected, plus "next" on a pick-any
// tab), or the review. Free-text rows are left out. nil when no form is on
// screen. It sets e.Form so a "next" answer is typed as Tab.
func ScreenFormPrompt(e *store.Entry, screen string) *model.Prompt {
	lines := strings.Split(screen, "\n")
	bar := -1
	for i, l := range lines {
		if tabBarRe.MatchString(l) {
			bar = i
		}
	}
	if bar < 0 {
		return nil
	}
	n := strings.Count(lines[bar], "☐") + strings.Count(lines[bar], "☒")
	body := bar + 1
	for body < len(lines) && strings.TrimSpace(lines[body]) == "" {
		body++
	}
	if body >= len(lines) {
		return nil
	}
	head := strings.TrimSpace(lines[body])
	p := &model.Prompt{Kind: "question"}
	f := &store.Form{Screen: true}
	if strings.HasPrefix(head, "Review your answers") {
		p.Title = "submit these answers?"
		p.Context = fmt.Sprintf("review · %d %s", n, plural(n, "question", "questions"))
		var out []string
	review:
		for _, l := range lines[body+1:] {
			t := strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(t, "Ready to submit"):
				break review
			case strings.HasPrefix(t, "● "):
				out = append(out, lower(strings.TrimPrefix(t, "● ")))
			case strings.HasPrefix(t, "→ ") && len(out) > 0:
				out[len(out)-1] += " " + t
			}
		}
		p.Detail = clip(strings.Join(out, "\n"), 2000)
		p.Options = []model.Option{{Key: "1", Label: "submit answers", Primary: true}, {Key: "2", Label: "cancel"}}
	} else {
		p.Title = lower(head)
		p.Context = fmt.Sprintf("form · %d %s", n, plural(n, "question", "questions"))
		multi := false
		for _, l := range lines[body+1:] {
			m := optionRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			label := strings.TrimSpace(m[2])
			sel := false
			if b := boxRe.FindStringSubmatch(label); b != nil {
				multi, sel = true, b[1] != " "
				label = strings.TrimSpace(label[len(b[0]):])
			}
			if k := strings.ToLower(label); strings.HasPrefix(k, "type something") || strings.HasPrefix(k, "chat about this") {
				continue
			}
			p.Options = append(p.Options, model.Option{Key: m[1], Label: label, Selected: sel})
		}
		if len(p.Options) == 0 {
			return nil
		}
		if multi {
			p.Context += " · pick any"
			f.Next = strconv.Itoa(len(p.Options) + 1)
			p.Options = append(p.Options, model.Option{Key: f.Next, Label: "next — on to the next tab", Primary: true})
		} else {
			p.Options[0].Primary = true
		}
	}
	p.ID = e.NextPromptID()
	f.PromptID = p.ID
	e.Form = f
	return p
}

// ScreenFormKey is what to type through an app for a deck key: Tab for the
// "next" option of a pick-any tab read off the screen, else the key itself.
func ScreenFormKey(e *store.Entry, key string) string {
	if f := e.Form; f != nil && f.Screen && f.Next != "" && key == f.Next && e.A.Waiting != nil && e.A.Waiting.ID == f.PromptID {
		return "\t"
	}
	return key
}

// AskClosed reports whether Claude Code's question dialog (one question or a
// form) has closed on the screen, and whether with answers: the last line
// about it, "User answered Claude's questions" or "User declined to answer
// questions", is below the dialog's last footer ("… · Esc to cancel").
// Cancelling fires no hook (Claude Code 2.1.306: no PostToolUse, no Stop), so
// the screen is the only way to tell.
func AskClosed(screen string) (closed, answered bool) {
	footer, mark := -1, -1
	for i, l := range strings.Split(screen, "\n") {
		switch {
		case strings.Contains(l, "Esc to cancel"):
			footer = i
		case strings.Contains(l, "User answered Claude's questions"):
			mark, answered = i, true
		case strings.Contains(l, "User declined to answer questions"):
			mark, answered = i, false
		}
	}
	if mark < 0 || mark < footer {
		return false, false
	}
	return true, answered
}
