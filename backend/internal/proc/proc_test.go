package proc

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestAlive(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Error("this process is not alive")
	}
	for _, pid := range []int{0, -1} {
		if Alive(pid) {
			t.Errorf("pid %d alive", pid)
		}
	}
}

func TestSplitCommandLine(t *testing.T) {
	for in, want := range map[string][]string{
		``:       nil,
		`claude`: {"claude"},
		`"C:\Program Files\x\claude.exe" --resume  abc`: {`C:\Program Files\x\claude.exe`, "--resume", "abc"},
		`C:\Users\a\claude.exe -p "two words"`:          {`C:\Users\a\claude.exe`, "-p", "two words"},
		`a\\\"b "c\\" d`:                                {`a\"b`, `c\`, "d"},
		`"say ""hi"""`:                                  {`say "hi"`},
		`""`:                                            {""},
	} {
		if got := SplitCommandLine(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestParseList(t *testing.T) {
	ps, err := parseList([]byte(`[{"pid":4242,"name":"claude.exe","cmd":"\"C:\\x\\claude.exe\" --resume 1","started":"2026-11-10T10:00:00.0000000Z"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].PID != 4242 || ps[0].Name != "claude.exe" || !ps[0].Started.Equal(time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %+v", ps)
	}
	if a := ps[0].Argv(); len(a) != 3 || a[0] != `C:\x\claude.exe` {
		t.Errorf("argv %q", a)
	}
	if ps, err := parseList([]byte("\r\n")); err != nil || ps != nil {
		t.Errorf("empty: %v %v", ps, err)
	}
}
