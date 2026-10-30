package store

import "time"

// Form is an open AskUserQuestion that takes more than one key in Claude
// Code: several questions, or a question where you can pick more than one
// answer. Claude shows it as tabs (one per question, then a review); the deck
// walks it the same way, one prompt per step. The daemon keeps it so it can
// type each answer the way that tab expects and build the next step's prompt.
type Form struct {
	PromptID  string         `json:"promptId"` // the Waiting prompt it built last; stale otherwise
	Questions []FormQuestion `json:"questions"`
	Step      int            `json:"step"`     // the tab on screen; len(Questions) is the review
	Picked    [][]int        `json:"picked"`   // option indexes picked per question
	Answered  []bool         `json:"answered"` // the question has an answer (from the deck or the terminal)
	Typed     time.Time      `json:"-"`        // when the deck last typed into it

	// Read off the screen (an agent in Orca, herdr or cmux, whose hooks the
	// deck doesn't get): only the tab on screen is known, and Next is the key
	// of its "next" option (a pick-any tab), typed as Tab.
	Screen bool   `json:"screen,omitempty"`
	Next   string `json:"next,omitempty"`
}

type FormQuestion struct {
	Question string       `json:"question"`
	Header   string       `json:"header,omitempty"`
	Multi    bool         `json:"multi,omitempty"` // pick any number, then move on
	Options  []FormOption `json:"options"`
}

type FormOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
