package model

import (
	"strings"
	"testing"
)

func TestPromptFileNames(t *testing.T) {
	home := "/Users/sam example/Library/agents deck" // spaces in the data folder
	in := strings.Join(strings.Fields("[Telegram] 3 messages from Robin Vale, 2 images, 1 file "+
		"1. 26 Nov 2026 14:58: see https://example.com/a/b/c.png and the logs "+
		"   image: "+home+"/telegram/default/inbox/2026-11-26/145800-12/01-photo.jpg "+
		"2. forwarded from Sam Example, 26 Nov 2026 14:50: "+
		"   image: "+home+"/telegram/design/inbox/2026-11-26/145800-12/02-photo-2.png "+
		"   file: "+home+"/telegram/default/inbox/2026-11-26/145800-12/03-payments-1105.log "+
		"   video too big for the Bot API (40 MB): long_clip.mov, open it in Telegram "+
		"The messages are long: all of them are in "+home+"/telegram/default/inbox/2026-11-26/145800-12/messages.md"), " ")
	got := PromptFileNames(in)
	for _, bad := range []string{"/Users", "sam example", "inbox/", "/telegram/"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q is still in what the deck shows:\n%s", bad, got)
		}
	}
	for _, want := range []string{"image: 01-photo.jpg", "image: 02-photo-2.png", "file: 03-payments-1105.log",
		"are in messages.md", "https://example.com/a/b/c.png", "long_clip.mov, open it in Telegram"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	// Any other prompt is left alone, paths and all.
	other := "fix /Users/sam/dev/app/telegram/default/inbox/2026-11-26/x/y.go"
	if PromptFileNames(other) != other {
		t.Fatal("a prompt not from the bridge changed")
	}
}
