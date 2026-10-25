package claude

import (
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

const formInput = `{"questions":[
 {"question":"Which color?","header":"Color","options":[{"label":"Red","description":"warm"},{"label":"Green"},{"label":"Blue"}]},
 {"question":"Which size?","header":"Size","options":[{"label":"Small"},{"label":"Large"}]},
 {"question":"Which toppings?","header":"Toppings","multiSelect":true,"options":[{"label":"Cheese"},{"label":"Ham"},{"label":"Olives"}]}]}`

func keys(p *model.Prompt) string {
	var k []string
	for _, o := range p.Options {
		tick := ""
		if o.Selected {
			tick = "*"
		}
		k = append(k, o.Key+"="+tick+o.Label)
	}
	return strings.Join(k, " | ")
}

// answer types a deck key into the open form, as the daemon does once it has
// sent the keys FormKeys gives.
func answer(t *testing.T, e *store.Entry, key string) FormDone {
	t.Helper()
	f := ActiveForm(e)
	if f == nil {
		t.Fatalf("no open form for key %s: %+v", key, e.A.Waiting)
	}
	if _, _, ok := FormKeys(f, key); !ok {
		t.Fatalf("key %s refused on %+v", key, e.A.Waiting)
	}
	return AdvanceForm(e, key)
}

func TestFormWalk(t *testing.T) {
	e := &store.Entry{}
	Apply(e, ev("PreToolUse", "tool", "AskUserQuestion", "input", formInput))
	p := e.A.Waiting
	if e.A.Status != model.Waiting || p == nil || p.Kind != "question" || p.Title != "which color?" ||
		p.Context != "color · question 1 of 3" || keys(p) != "1=Red — warm | 2=Green | 3=Blue" {
		t.Fatalf("step 1: %+v", p)
	}
	// The permission hook for the same call leaves the form alone.
	Apply(e, ev("PermissionRequest", "tool", "AskUserQuestion", "input", formInput))
	if e.A.Waiting != p {
		t.Fatalf("permission hook replaced the form: %+v", e.A.Waiting)
	}
	if text, k, _ := FormKeys(e.Form, "2"); text != "2" || k != nil {
		t.Fatalf("pick-one types its number: %q %v", text, k)
	}
	if _, _, ok := FormKeys(e.Form, "4"); ok {
		t.Fatal("step 1 has no key 4")
	}
	answer(t, e, "2")
	p2 := e.A.Waiting
	if p2.ID == p.ID || p2.Title != "which size?" || p2.Context != "size · question 2 of 3" {
		t.Fatalf("step 2: %+v", p2)
	}
	answer(t, e, "1")
	p3 := e.A.Waiting
	if p3.Context != "toppings · question 3 of 3 · pick any" ||
		keys(p3) != "1=Cheese | 2=Ham | 3=Olives | 4=next — review your answers" || !p3.Options[3].Primary {
		t.Fatalf("step 3: %+v", p3)
	}
	if text, k, _ := FormKeys(e.Form, "4"); text != "" || len(k) != 1 || k[0] != "Tab" {
		t.Fatalf("done on a pick-any tab is Tab: %q %v", text, k)
	}
	answer(t, e, "1")
	answer(t, e, "3")
	answer(t, e, "2")
	answer(t, e, "2") // ticked twice: off again
	if got := keys(e.A.Waiting); got != "1=*Cheese | 2=Ham | 3=*Olives | 4=next — review your answers" {
		t.Fatalf("ticks: %s", got)
	}
	answer(t, e, "4")
	r := e.A.Waiting
	if r.Title != "submit these answers?" || r.Context != "review · 3 questions" || keys(r) != "1=submit answers | 2=cancel" ||
		r.Detail != "which color? → Green\nwhich size? → Small\nwhich toppings? → Cheese, Olives" {
		t.Fatalf("review: %+v", r)
	}
	if answer(t, e, "1") != FormSubmitted || e.A.Status != model.Running || e.A.Waiting != nil || e.Form != nil {
		t.Fatalf("submitted: %+v %+v", e.A, e.Form)
	}
}

func TestFormCancel(t *testing.T) {
	e := &store.Entry{}
	Apply(e, ev("PreToolUse", "tool", "AskUserQuestion", "input",
		`{"questions":[{"question":"Which pets?","multiSelect":true,"options":[{"label":"Cat"},{"label":"Dog"}]}]}`))
	if p := e.A.Waiting; p == nil || p.Context != "question 1 of 1 · pick any" || keys(p) != "1=Cat | 2=Dog | 3=next — review your answers" {
		t.Fatalf("a single pick-any question is a form: %+v", p)
	}
	answer(t, e, "3")
	if d := e.A.Waiting.Detail; d != "which pets? → none" {
		t.Fatalf("review: %q", d)
	}
	if answer(t, e, "2") != FormCancelled || e.A.Status != model.Idle || e.Form != nil {
		t.Fatalf("cancelled: %+v", e.A)
	}
}

func TestSingleQuestionIsNoForm(t *testing.T) {
	e := &store.Entry{}
	Apply(e, ev("PreToolUse", "tool", "AskUserQuestion", "input", `{"questions":[{"question":"A?","options":[{"label":"x"},{"label":"y"}]}]}`))
	if e.Form != nil || e.A.Waiting == nil || e.A.Waiting.Context != "question · 1 of 1" {
		t.Fatalf("one pick-one question is answered with one key: %+v %+v", e.Form, e.A.Waiting)
	}
}

func TestFormClosesWithTheTool(t *testing.T) {
	e := &store.Entry{}
	Apply(e, ev("PreToolUse", "tool", "AskUserQuestion", "input", formInput))
	Apply(e, ev("PostToolUse", "tool", "AskUserQuestion"))
	if e.Form != nil || ActiveForm(e) != nil {
		t.Fatal("the form should close once the tool ran (answered in the terminal)")
	}
}

func TestPromptForKeepsFormsInTheTerminal(t *testing.T) {
	// Apps that run agents (Orca, herdr, cmux) type keys their own way: no form there.
	e := &store.Entry{}
	p := PromptFor(e, "AskUserQuestion", []byte(formInput))
	if p.Kind != "input" || len(p.Options) != 0 || e.Form != nil {
		t.Fatalf("%+v", p)
	}
}

const screenSize = `
❯ make me a pizza
────────────────────────────────────────────────────────────────
←  ☒ Color  ☐ Size  ☐ Toppings  ✔ Submit  →

Which size?

❯ 1. Small
     Small
  2. Large
     Large
  3. Type something.
────────────────────────────────────────────────────────────────
  4. Chat about this

Enter to select · Tab/Arrow keys to navigate · Esc to cancel`

const screenToppings = `
────────────────────────────────────────────────────────────────
←  ☒ Color  ☒ Size  ☒ Toppings  ✔ Submit  →

Which toppings?

❯ 1. [✔] Cheese
         Cheese
  2. [ ] Ham
         Ham
  3. [✔] Olives
         Olives
  4. [ ] Type something
     Submit
────────────────────────────────────────────────────────────────
  5. Chat about this
`

const screenReview = `
←  ☒ Color  ☒ Size  ☒ Toppings  ✔ Submit  →

Review your answers

 ● Which color?
   → Green
Ready to submit your answers?

❯ 1. Submit answers
  2. Cancel
`

func TestSyncFormFollowsTheTerminal(t *testing.T) {
	e := &store.Entry{}
	Apply(e, ev("PreToolUse", "tool", "AskUserQuestion", "input", formInput))
	first := e.A.Waiting.ID
	if SyncForm(e, "no form on screen yet") {
		t.Fatal("a screen without the form changes nothing")
	}
	if !SyncForm(e, screenSize) {
		t.Fatal("question 1 answered in the terminal: the deck should move on")
	}
	if p := e.A.Waiting; p.ID == first || p.Title != "which size?" || e.Form.Step != 1 || !e.Form.Answered[0] {
		t.Fatalf("after sync: %+v %+v", p, e.Form)
	}
	if SyncForm(e, screenSize) {
		t.Fatal("same screen again: no change")
	}
	if !SyncForm(e, screenToppings) {
		t.Fatal("ticks typed in the terminal")
	}
	if got := keys(e.A.Waiting); got != "1=*Cheese | 2=Ham | 3=*Olives | 4=next — review your answers" {
		t.Fatalf("ticks: %s", got)
	}
	if !SyncForm(e, screenReview) || e.Form.Step != 3 {
		t.Fatalf("review: %+v", e.Form)
	}
	if d := e.A.Waiting.Detail; d != "which color? → answered in the terminal\nwhich size? → answered in the terminal\nwhich toppings? → Cheese, Olives" {
		t.Fatalf("review detail: %q", d)
	}
}

func TestScreenFormPrompt(t *testing.T) {
	e := &store.Entry{}
	if ScreenFormPrompt(e, "❯ 1. Yes\n  2. No") != nil {
		t.Fatal("no tab row: not a form")
	}
	p := ScreenFormPrompt(e, screenSize)
	if p == nil || p.Title != "which size?" || p.Context != "form · 3 questions" || keys(p) != "1=Small | 2=Large" || !p.Options[0].Primary {
		t.Fatalf("pick-one tab (free-text rows left out): %+v", p)
	}
	if ScreenFormKey(e, "1") != "1" {
		t.Fatal("a pick-one key is typed as is")
	}
	p = ScreenFormPrompt(e, screenToppings)
	if p == nil || p.Context != "form · 3 questions · pick any" || keys(p) != "1=*Cheese | 2=Ham | 3=*Olives | 4=next — on to the next tab" {
		t.Fatalf("pick-any tab: %+v", p)
	}
	e.A.Waiting = p
	if ScreenFormKey(e, "4") != "\t" || ScreenFormKey(e, "2") != "2" {
		t.Fatal("next on a pick-any tab is Tab")
	}
	p = ScreenFormPrompt(e, screenReview)
	if p == nil || p.Title != "submit these answers?" || p.Detail != "which color? → Green" || keys(p) != "1=submit answers | 2=cancel" {
		t.Fatalf("review: %+v", p)
	}
}

func TestAskClosed(t *testing.T) {
	if c, _ := AskClosed(screenToppings + "\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel"); c {
		t.Fatal("the form is open")
	}
	declined := "⏺ User declined to answer questions\n  ⎿  · Which pet? (Cat / Dog)\n✻ Cooked for 3s"
	if c, a := AskClosed(declined); !c || a {
		t.Fatalf("declined: %v %v", c, a)
	}
	answered := "⏺ User answered Claude's questions:\n  ⎿  · Which pet? → Cat"
	if c, a := AskClosed(answered); !c || !a {
		t.Fatalf("answered: %v %v", c, a)
	}
	// An earlier question's line above a dialog that is open now.
	if c, _ := AskClosed(declined + "\n" + screenSize); c {
		t.Fatal("a new dialog below an old answer is open")
	}
}
