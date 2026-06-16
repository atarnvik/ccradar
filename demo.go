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
	mk := func(cwd, status, title, model string, age int64, open bool) Session {
		s := Session{
			PID:       4000,
			SessionID: "demo-" + title,
			CWD:       demoDir(cwd),
			Status:    status,
			UpdatedAt: now - age,
			Title:     title,
			Model:     model,
		}
		if open {
			s.SurfaceID = "demo"
		}
		return s
	}
	return []Session{
		mk("src/api-gateway", "busy", "Add rate-limiting middleware", "claude-opus-4-8", 2*min, true),
		mk("src/api-gateway", "idle", "Write integration tests", "claude-sonnet-4-6", 14*min, true),
		mk("src/web-app", "busy", "Fix hydration mismatch on dashboard", "claude-opus-4-8", 1*min, true),
		mk("src/web-app", "waiting", "Refactor auth context", "claude-sonnet-4-6", 5*min, true),
		mk("src/infra", "idle", "Terraform module for RDS", "claude-opus-4-8", hr, true),
		mk("src/cli-tool", "idle", "Port flags to cobra", "claude-opus-4-8", 3*hr, false), // detached
	}
}

func demoHistory() []HistEntry {
	now := time.Now()
	mk := func(cwd, title, model string, age time.Duration) HistEntry {
		return HistEntry{SessionID: "demo-" + title, CWD: demoDir(cwd), Title: title, Model: model, ModAt: now.Add(-age)}
	}
	return []HistEntry{
		mk("src/web-app", "Set up Vitest and React Testing Library", "claude-sonnet-4-6", 2*time.Hour),
		mk("src/api-gateway", "Design webhook delivery with retries", "claude-opus-4-8", 5*time.Hour),
		mk("src/infra", "Monthly cost report script", "claude-sonnet-4-6", 26*time.Hour),
		mk("src/notes", "Draft Q3 planning doc", "claude-opus-4-8", 50*time.Hour),
	}
}

// demoDriver is a no-op terminal so focus/resume don't touch real apps.
type demoDriver struct{}

func (demoDriver) Name() string                 { return "ghostty" }
func (demoDriver) Surfaces() []Terminal         { return nil }
func (demoDriver) Focus(string) error           { return nil }
func (demoDriver) OpenResume(_, _ string) error { return nil }
