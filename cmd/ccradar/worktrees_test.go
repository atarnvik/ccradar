package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestStaleWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t",
		"GIT_COMMITTER_EMAIL": "t@t", "GIT_CONFIG_GLOBAL": "/dev/null", "CCRADAR_NO_PR_STATUS": ""} {
		t.Setenv(k, v)
	}
	base, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(base, "app")
	os.MkdirAll(repo, 0o755)
	gitT(t, repo, "init", "-q")
	gitT(t, repo, "remote", "add", "origin", "git@github.com:acme/app.git")
	gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	wt := func(name string) string {
		p := filepath.Join(repo, ".worktrees", name)
		gitT(t, repo, "worktree", "add", "-q", "-b", name, p)
		return p
	}
	merged, dirty, _, live, _ := wt("merged"), wt("dirty"), wt("open"), wt("live"), wt("no-pr")
	os.WriteFile(filepath.Join(dirty, "wip.txt"), []byte("x"), 0o644)
	gitT(t, dirty, "add", "wip.txt")

	var asked []string
	lookupBranchPRs = func(repo string, wts []worktree) map[string][]wtPR {
		if repo != "acme/app" {
			t.Errorf("repo = %q", repo)
		}
		for _, w := range wts {
			asked = append(asked, w.Branch)
		}
		return map[string][]wtPR{
			"merged": {{12, "MERGED"}, {9, "CLOSED"}},
			"dirty":  {{7, "CLOSED"}},
			"open":   {{13, "OPEN"}, {8, "MERGED"}},
			"live":   {{14, "MERGED"}},
		}
	}
	defer func() { lookupBranchPRs = branchPRs }()

	now := time.Now()
	m := model{view: viewHistory, width: 120, height: 20, sessionsReady: true, histReady: true}
	// Sessions started in the main checkout; one live session sits in a worktree.
	m.history = []HistEntry{{SessionID: "a", CWD: repo, Title: "Main work", ModAt: now}, {SessionID: "b", CWD: merged, Title: "In the worktree", ModAt: now}}
	m.sessions = []Session{{PID: 999999, SessionID: "live", CWD: live, Status: "idle", UpdatedAt: now.UnixMilli()}}
	m.rebuild()
	cmd := m.startWTScan()
	tm, _ := m.Update(cmd())
	m = tm.(model)

	if strings.Contains(strings.Join(asked, ","), "live") {
		t.Errorf("a worktree with a live session shouldn't be looked up: %v", asked)
	}
	var got []string
	for _, s := range m.wtStale {
		got = append(got, filepath.Base(s.Path))
	}
	if strings.Join(got, ",") != "merged,dirty" && strings.Join(got, ",") != "dirty,merged" {
		t.Fatalf("stale = %v, want merged and dirty (open has an open PR, no-pr has none, live is in use)", got)
	}
	if v := m.View(); !strings.Contains(v, "⎇ 2 stale (W)") || !strings.Contains(v, "app ⎇ merged  ⑂ PRs done") {
		t.Fatalf("view:\n%s", v)
	}

	// W lists them; enter arms, a second enter removes the selected one.
	tm, _ = m.Update(key("W"))
	m = tm.(model)
	for i, s := range m.wtStale {
		if s.Path == merged {
			m.wtCursor = i
		}
	}
	tm, _ = m.Update(key("enter"))
	m = tm.(model)
	if !strings.Contains(m.flash, "press enter again") || !strings.Contains(m.flash, "branch merged is kept") {
		t.Fatalf("flash = %q", m.flash)
	}
	tm, cmd = m.Update(key("enter"))
	m = tm.(model)
	tm, _ = m.Update(cmd())
	m = tm.(model)
	if _, err := os.Stat(merged); !os.IsNotExist(err) || !strings.Contains(m.flash, "removed") {
		t.Fatalf("merged worktree should be gone (flash %q, stat err %v)", m.flash, err)
	}
	gitT(t, repo, "rev-parse", "--verify", "refs/heads/merged") // the branch is kept

	// git refuses the dirty one, and it stays listed.
	for _, k := range []string{"a", "a"} {
		tm, cmd = m.Update(key(k))
		m = tm.(model)
	}
	tm, _ = m.Update(cmd())
	m = tm.(model)
	if _, err := os.Stat(dirty); err != nil || !strings.Contains(m.flash, "couldn't remove") {
		t.Fatalf("dirty worktree must survive (flash %q)", m.flash)
	}
	if len(m.wtStale) != 1 || !m.wtPicking {
		t.Fatalf("dirty worktree should stay listed: %+v", m.wtStale)
	}
}

func TestWTName(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		w    staleWT
		want string
	}{
		{staleWT{Path: home + "/src/app/.worktrees/fix-x", Main: home + "/src/app"}, "~/src/app ⎇ fix-x"},
		{staleWT{Path: "/private/tmp/claude-501/-Users-me-src-app/scratch/wt-readme", Main: home + "/src/app"}, "~/src/app ⎇ wt-readme"},
		{staleWT{Path: home + "/src/app/.worktrees/fix-x"}, "~/src/app ⎇ fix-x"},
	}
	for _, c := range cases {
		if got := wtName(c.w); got != c.want {
			t.Errorf("wtName(%s) = %q, want %q", c.w.Path, got, c.want)
		}
	}
	if got := fitWTName("~/development/personal/claude_dashboard ⎇ wt-readme", 30); got != "claude_dashboard ⎇ wt-readme" {
		t.Errorf("fitWTName = %q", got)
	}
	if got := fitWTName("~/a ⎇ "+strings.Repeat("x", 40), 20); len([]rune(got)) != 20 {
		t.Errorf("fitWTName long tail = %q", got)
	}
}

func TestFitWTNameCutsRepo(t *testing.T) {
	if got := fitWTName("~/development/personal/claude_dashboard ⎇ wt-readme", 25); got != "…de_dashboard ⎇ wt-readme" {
		t.Errorf("fitWTName = %q", got)
	}
}
