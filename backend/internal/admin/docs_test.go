package admin

import (
	"os"
	"regexp"
	"testing"
)

// docs/protocol.md's Management API table and the routes in Handler are the
// same list (and kept apart from the /v1 table the Deck Link parity test reads).
func TestDocsListEveryCall(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| (GET|POST|PATCH|DELETE) \\| `(/admin/v1/[^`]+)`").FindAllStringSubmatch(string(doc), -1) {
		documented[m[1]+" "+m[2]] = true
	}
	routed := map[string]bool{}
	for _, m := range regexp.MustCompile(`mux\.HandleFunc\("((?:GET|POST|PATCH|DELETE) /admin/v1/[^"]+)"`).FindAllStringSubmatch(string(src), -1) {
		routed[m[1]] = true
	}
	if len(routed) < 15 {
		t.Fatalf("found only %d routes", len(routed))
	}
	for r := range routed {
		if !documented[r] {
			t.Errorf("%s is not in docs/protocol.md", r)
		}
	}
	for d := range documented {
		if !routed[d] {
			t.Errorf("docs/protocol.md lists %s, which isn't served", d)
		}
	}
	// None of them is in the /v1 table (the Deck Link parity test's).
	if regexp.MustCompile("(?m)^\\| (GET|POST|PATCH) \\| `/v1/admin").Match(doc) {
		t.Error("an admin call in the /v1 table")
	}
}
