package main

import "testing"

func TestParseRexSurfaces(t *testing.T) {
	ttys := func(pids []int) map[int]string {
		return map[int]string{100: "/dev/ttys001"}
	}
	out := `[{"id":"session:a block:1","pid":100,"cwd":"/src/app","title":"fix login"},
	         {"id":"session:a block:2","pid":0,"cwd":"","title":""}]`
	got := parseRexSurfaces(out, ttys)
	if len(got) != 2 {
		t.Fatalf("want 2 surfaces, got %d", len(got))
	}
	want := Terminal{ID: "session:a block:1", Tty: "/dev/ttys001", CWD: "/src/app", Title: "fix login"}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
	if got[1].Tty != "" {
		t.Fatalf("pane without a pid should have no tty, got %q", got[1].Tty)
	}

	for _, empty := range []string{"", "{}", "\n{}\n", "not json"} {
		if got := parseRexSurfaces(empty, ttys); got != nil {
			t.Fatalf("%q: want nil, got %+v", empty, got)
		}
	}
}
