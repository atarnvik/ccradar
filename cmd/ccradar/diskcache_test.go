package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resetScanCache drops the in-memory scan cache, as a new process would.
func resetScanCache() {
	scanCache.Lock()
	scanCache.m = map[string]cachedScan{}
	scanDisk.loaded, scanDisk.dirty, scanDisk.lastSave = false, false, time.Time{}
	scanCache.Unlock()
}

func TestScanCacheSurvivesRestart(t *testing.T) {
	t.Setenv("CCRADAR_CACHE_DIR", t.TempDir())
	if binaryID() == "" {
		t.Skip("can't fingerprint the test binary")
	}
	resetScanCache()
	t.Cleanup(resetScanCache)

	dir := t.TempDir()
	keep, gone := filepath.Join(dir, "keep.jsonl"), filepath.Join(dir, "gone.jsonl")
	mod := time.Now().Add(-time.Hour).Truncate(time.Second)
	writeTranscript(t, keep, mod, shellTranscript...)
	writeTranscript(t, keep, mod, append(shellTranscript, `{"type":"ai-title","aiTitle":"AAAA"}`)...)
	writeTranscript(t, gone, mod, `{"type":"ai-title","aiTitle":"Gone"}`)
	first := scanTranscriptCached(keep)
	scanTranscriptCached(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	saveScanCache(true)

	// Same size and mtime, different content: a cache hit returns the old scan.
	writeTranscript(t, keep, mod, append(shellTranscript, `{"type":"ai-title","aiTitle":"BBBB"}`)...)
	resetScanCache()
	got := scanTranscriptCached(keep)
	if got.title != "AAAA" {
		t.Fatalf("title = %q: the disk cache wasn't used after a restart", got.title)
	}
	since := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if len(got.shells.running(since)) != len(first.shells.running(since)) || got.cwd != first.cwd || !got.last.Equal(first.last) {
		t.Errorf("scan didn't round-trip: %+v vs %+v", got, first)
	}
	scanCache.Lock()
	_, hasGone := scanCache.m[gone]
	scanCache.Unlock()
	if hasGone {
		t.Error("deleted transcript should be pruned from the cache")
	}

	// A different build ignores the cache.
	var doc scanCacheDoc
	readJSON(scanCacheFile, &doc)
	doc.Binary = "another-build"
	writeJSON(scanCacheFile, doc)
	resetScanCache()
	if got := scanTranscriptCached(keep); got.title != "BBBB" {
		t.Errorf("title = %q: a cache from another build must not be used", got.title)
	}
}

func TestPRCacheSurvivesRestart(t *testing.T) {
	t.Setenv("CCRADAR_CACHE_DIR", t.TempDir())
	ownerAccounts.Lock()
	ownerAccounts.m = map[string]string{"work": "work-account"}
	ownerAccounts.Unlock()

	fetched := time.Now().Add(-time.Minute).Truncate(time.Second)
	savePRCache(map[string]prInfo{
		"u-open":   {State: "OPEN", Title: "Open one", Checks: "SUCCESS", fetched: fetched},
		"u-merged": {State: "MERGED", Title: "Merged one", fetched: fetched},
		"u-failed": {fetched: fetched}, // a failed lookup: retried by the next process
	})

	ownerAccounts.Lock()
	ownerAccounts.m = map[string]string{}
	ownerAccounts.Unlock()
	st := loadPRCache()
	if st["u-open"].State != "OPEN" || st["u-open"].Checks != "SUCCESS" || !st["u-open"].fetched.Equal(fetched) || st["u-merged"].State != "MERGED" {
		t.Fatalf("statuses = %+v", st)
	}
	if _, ok := st["u-failed"]; ok {
		t.Error("failed lookups shouldn't be cached")
	}
	ownerAccounts.Lock()
	login := ownerAccounts.m["work"]
	ownerAccounts.Unlock()
	if login != "work-account" {
		t.Errorf("owner routing not restored: %q", login)
	}
	links := []prLink{{URL: "u-open"}, {URL: "u-merged"}, {URL: "u-failed"}}
	if due := duePRs(links, st, time.Now()); len(due) != 1 || due[0].URL != "u-failed" {
		t.Errorf("due after restart = %+v, want only the failed one (others are fresh or final)", due)
	}
}
