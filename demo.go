package main

import (
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
	return []Session{
		mk("src/api-gateway", "busy", "Add rate-limiting middleware", "claude-opus-4-8", "main",
			"add a per-IP token-bucket limiter to the gateway middleware",
			"I'll add a per-IP token bucket backed by an in-memory store, keyed by client IP, with a configurable refill rate.", 2*min, true),
		mk("src/api-gateway", "idle", "Write integration tests", "claude-sonnet-4-6", "tests",
			"cover the webhook handler with integration tests",
			"Added table-driven tests for the webhook handler covering success, retry, and signature-failure paths.", 14*min, true),
		mk("src/web-app", "busy", "Fix hydration mismatch on dashboard", "claude-opus-4-8", "main",
			"the dashboard throws a hydration mismatch on first load",
			"The mismatch is from rendering the timestamp on the server; I'll defer it to a client-only effect.", 1*min, true),
		mk("src/web-app", "waiting", "Refactor auth context", "claude-sonnet-4-6", "auth-refactor",
			"split the auth context so tokens and user profile live separately",
			"Ready to move profile state out of AuthContext into ProfileContext — shall I update all 12 consumers?", 5*min, true),
		mk("src/infra", "idle", "Terraform module for RDS", "claude-opus-4-8", "main",
			"write a terraform module for a multi-AZ postgres RDS instance",
			"Created modules/rds with multi-AZ, automated backups, and a parametrized instance class.", hr, true),
		mk("src/cli-tool", "idle", "Port flags to cobra", "claude-opus-4-8", "main",
			"port the hand-rolled flag parsing over to cobra",
			"Migrated the root command and three subcommands to cobra; flag names and defaults are unchanged.", 3*hr, false), // detached
	}
}

func demoHistory() []HistEntry {
	now := time.Now()
	mk := func(cwd, title, model, prompt, reply string, age time.Duration) HistEntry {
		return HistEntry{
			SessionID: "demo-" + title, CWD: demoDir(cwd), Title: title, Model: model,
			Branch: "main", LastPrompt: prompt, LastReply: reply, ModAt: now.Add(-age),
		}
	}
	return []HistEntry{
		mk("src/web-app", "Set up Vitest and React Testing Library", "claude-sonnet-4-6",
			"set up vitest with react testing library",
			"Configured Vitest with jsdom and RTL, plus a sample component test and an npm script.", 2*time.Hour),
		mk("src/api-gateway", "Design webhook delivery with retries", "claude-opus-4-8",
			"design reliable webhook delivery with retries",
			"Proposed an outbox table + exponential-backoff worker with a dead-letter queue after 5 attempts.", 5*time.Hour),
		mk("src/infra", "Monthly cost report script", "claude-sonnet-4-6",
			"script a monthly AWS cost report grouped by service",
			"Wrote a script using Cost Explorer that emails a per-service breakdown on the 1st.", 26*time.Hour),
		mk("src/notes", "Draft Q3 planning doc", "claude-opus-4-8",
			"draft an outline for the Q3 planning doc",
			"Drafted an outline: goals, bets, staffing, risks, and a rough timeline.", 50*time.Hour),
	}
}

// demoDriver is a no-op terminal so focus/resume don't touch real apps.
type demoDriver struct{}

func (demoDriver) Name() string                 { return "ghostty" }
func (demoDriver) Surfaces() []Terminal         { return nil }
func (demoDriver) Focus(string) error           { return nil }
func (demoDriver) OpenResume(_, _ string) error { return nil }
