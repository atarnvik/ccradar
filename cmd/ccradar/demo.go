package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Demo mode (CCRADAR_DEMO=1) feeds ccradar a fixed, made-up dataset and a no-op
// terminal driver so the README GIF is clean, reproducible, and leaks no real
// project names. It's wired in at the three data seams: loadSessions,
// loadHistory, and activeDriver (and it suppresses the update check).
func demoMode() bool { return os.Getenv("CCRADAR_DEMO") != "" }

func demoDir(p string) string { return filepath.Join(homeDir(), p) }

func demoSessions() []Session {
	now := nowMs()
	const min = int64(60_000)
	const hr = 60 * min
	mk := func(cwd, status, title, model, branch, prompt, reply string, age int64, open bool) Session {
		s := Session{
			PID:        4000,
			SessionID:  "demo-" + title,
			CWD:        demoDir(cwd),
			Status:     status,
			UpdatedAt:  now - age,
			Title:      title,
			Model:      model,
			Branch:     branch,
			LastPrompt: prompt,
			LastReply:  reply,
		}
		if open {
			s.SurfaceID = "demo"
		}
		return s
	}
	waitingRecap := sessionDetails{
		Recap:   "AuthContext is split; ProfileContext holds the user profile. Next: update the 12 consumers, then drop the old context.",
		RecapAt: time.Now().Add(-4 * time.Minute),
	}
	local := []Session{
		mk("src/api-gateway", "busy", "Add rate-limiting middleware", "claude-opus-5-5", "main",
			"add a per-IP token-bucket limiter to the gateway middleware",
			"I'll add a per-IP token bucket backed by an in-memory store, keyed by client IP, with a configurable refill rate.", 2*min, true),
		mk("src/api-gateway", "idle", "Write integration tests", "claude-sonnet-5-5", "tests",
			"cover the webhook handler with integration tests",
			"Added table-driven tests for the webhook handler covering success, retry, and signature-failure paths.", 14*min, true),
		mk("src/web-app", "busy", "Fix hydration mismatch on dashboard", "claude-opus-5-5", "main",
			"the dashboard throws a hydration mismatch on first load",
			"The mismatch is from rendering the timestamp on the server; I'll defer it to a client-only effect.", 1*min, true),
		mk("src/web-app", "waiting", "Refactor auth context", "claude-sonnet-5-5", "auth-refactor",
			"split the auth context so tokens and user profile live separately",
			"Ready to move profile state out of AuthContext into ProfileContext — shall I update all 12 consumers?", 5*min, true),
		mk("src/infra", "idle", "Terraform module for RDS", "claude-opus-5-5", "main",
			"write a terraform module for a multi-AZ postgres RDS instance",
			"Created modules/rds with multi-AZ, automated backups, and a parametrized instance class.", hr, true),
		mk("src/cli-tool", "idle", "Port flags to cobra", "claude-opus-5-5", "main",
			"port the hand-rolled flag parsing over to cobra",
			"Migrated the root command and three subcommands to cobra; flag names and defaults are unchanged.", 3*hr, false), // detached
	}
	for i := range local {
		if local[i].Status == "waiting" {
			local[i].sessionDetails = waitingRecap
		}
	}
	local[0].PRs = []prLink{demoPR(412)}
	local[1].Shells = []bgShell{{ID: "b1", Desc: "Run the gateway on :8080", Started: time.Now().Add(-40 * time.Minute)}}
	local[2].Shells = []bgShell{
		{ID: "b2", Desc: "Start the dev server on :3000", Started: time.Now().Add(-25 * time.Minute)},
		{ID: "b3", Desc: "Watch the test suite", Started: time.Now().Add(-8 * time.Minute)},
	}
	local[3].PRs = []prLink{demoPR(398), demoPR(405)}
	local[4].PRs = []prLink{demoPR(377)}
	for i, n := range []int{184_000, 62_000, 431_000, 97_000, 23_000, 12_000} {
		local[i].Context = n
	}
	// A devcontainer source (shows as its own section).
	mkr := func(cwd, status, title, model, branch, prompt string, age int64) Session {
		return Session{
			PID: 5000, SessionID: "demo-c-" + title, CWD: cwd, Status: status,
			UpdatedAt: now - age, Title: title, Model: model, Branch: branch,
			LastPrompt: prompt, Source: "api-devcontainer", Container: "demo", SurfaceID: "demo",
		}
	}
	local = append(local,
		mkr("/workspaces/orders-svc", "busy", "Add idempotency keys to checkout", "claude-opus-5-5", "main",
			"make the checkout endpoint idempotent with request keys", 3*min),
		mkr("/workspaces/orders-svc", "waiting", "Upgrade the pg driver", "claude-sonnet-5-5", "deps",
			"bump the postgres driver and fix the breaking changes", 9*min),
	)
	return local
}

func demoHistory() []HistEntry {
	now := time.Now()
	mk := func(cwd, title, model, prompt, reply string, age time.Duration) HistEntry {
		return HistEntry{
			SessionID: "demo-" + title, CWD: demoDir(cwd), Title: title, Model: model,
			Branch: "main", LastPrompt: prompt, LastReply: reply, ModAt: now.Add(-age),
		}
	}
	costs := []costState{{4.12, 214, 18}, {11.6, 37, 4}, {0.84, 61, 0}, {2.3, 0, 0}}
	list := []HistEntry{
		mk("src/web-app", "Set up Vitest and React Testing Library", "claude-sonnet-5-5",
			"set up vitest with react testing library",
			"Configured Vitest with jsdom and RTL, plus a sample component test and an npm script.", 2*time.Hour),
		mk("src/api-gateway", "Design webhook delivery with retries", "claude-opus-5-5",
			"design reliable webhook delivery with retries",
			"Proposed an outbox table + exponential-backoff worker with a dead-letter queue after 5 attempts.", 5*time.Hour),
		mk("src/infra", "Monthly cost report script", "claude-sonnet-5-5",
			"script a monthly AWS cost report grouped by service",
			"Wrote a script using Cost Explorer that emails a per-service breakdown on the 1st.", 26*time.Hour),
		mk("src/notes", "Draft Q3 planning doc", "claude-opus-5-5",
			"draft an outline for the Q3 planning doc",
			"Drafted an outline: goals, bets, staffing, risks, and a rough timeline.", 50*time.Hour),
	}
	for i := range list {
		list[i].Cost = costs[i%len(costs)]
	}
	list[0].PRs = []prLink{demoPR(391)}
	list[1].PRs = []prLink{demoPR(366)}
	list[1].Recap = "Settled on an outbox table plus a backoff worker; the dead-letter queue and its alerting are still to do."
	list[1].RecapAt = now.Add(-5 * time.Hour)
	list = append(list,
		HistEntry{
			SessionID: "demo-c-hist", CWD: "/workspaces/orders-svc", Title: "Add OpenTelemetry spans",
			Model: "claude-opus-5-5", Branch: "main", LastPrompt: "instrument the order pipeline with otel spans",
			ModAt: now.Add(-7 * time.Hour), Source: "api-devcontainer", Container: "demo",
		},
	)
	return list
}

func demoPR(n int) prLink {
	return prLink{URL: fmt.Sprintf("https://github.com/acme/app/pull/%d", n), Repo: "acme/app", Number: n}
}

// demoPRStatus is the fixed PR status demo mode shows (it never calls gh).
func demoPRStatus() map[string]prInfo {
	st := map[int]prInfo{
		412: {State: "OPEN", Checks: "PENDING", Title: "Per-IP rate limiting in the gateway"},
		405: {State: "OPEN", Checks: "FAILURE", Title: "Split ProfileContext out of AuthContext"},
		398: {State: "MERGED", Title: "Move token refresh into a hook"},
		377: {State: "OPEN", Checks: "SUCCESS", Review: "APPROVED", Title: "RDS module with multi-AZ"},
		391: {State: "MERGED", Title: "Vitest + React Testing Library"},
		366: {State: "OPEN", Draft: true, Title: "Webhook outbox and retry worker"},
	}
	out := map[string]prInfo{}
	for n, info := range st {
		out[demoPR(n).URL] = info
	}
	return out
}

// demoStaleWorktrees is the fixed list W shows in demo mode (never removed).
func demoStaleWorktrees() []staleWT {
	now := time.Now()
	return []staleWT{
		{Path: demoDir("src/web-app/.worktrees/vitest-setup"), Branch: "vitest-setup", PRs: []wtPR{{391, "MERGED"}}, Mod: now.Add(-2 * time.Hour)},
		{Path: demoDir("src/api-gateway/.worktrees/webhook-retries"), Branch: "webhook-retries", PRs: []wtPR{{371, "MERGED"}, {366, "CLOSED"}}, Mod: now.Add(-30 * time.Hour)},
		{Path: demoDir("src/infra/.worktrees/cost-report"), Branch: "cost-report", PRs: []wtPR{{352, "MERGED"}}, Mod: now.Add(-74 * time.Hour)},
	}
}

// demoDriver is a no-op terminal so focus/resume don't touch real apps.
type demoDriver struct{}

func (demoDriver) Name() string                 { return "ghostty" }
func (demoDriver) Surfaces() []Terminal         { return nil }
func (demoDriver) Focus(string) error           { return nil }
func (demoDriver) OpenResume(_, _ string) error { return nil }
