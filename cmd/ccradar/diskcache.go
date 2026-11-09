package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ccradar keeps two caches on disk so a fresh process (e.g. a popup opened
// with a hotkey) starts warm: transcript scans and PR statuses. Both are
// plain JSON under the user cache dir and safe to delete.

// diskCacheSchema versions the cache files' layout.
const diskCacheSchema = 1

// scanSaveEvery throttles writing the scan cache while ccradar runs; it is
// also written on exit.
const scanSaveEvery = 10 * time.Second

func cacheDir() string {
	if d := os.Getenv("CCRADAR_CACHE_DIR"); d != "" {
		return d
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = filepath.Join(homeDir(), ".cache")
	}
	return filepath.Join(base, "ccradar")
}

// binaryID fingerprints the running executable. Scans are only reused by the
// same build, so a change to what a scan extracts can never be masked by a
// stale cache.
var binaryID = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
})

// writeJSON writes v to name in the cache dir atomically.
func writeJSON(name string, v any) {
	dir := cacheDir()
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), filepath.Join(dir, name)) != nil {
		_ = os.Remove(tmp.Name())
	}
}

func readJSON(name string, v any) bool {
	b, err := os.ReadFile(filepath.Join(cacheDir(), name))
	return err == nil && json.Unmarshal(b, v) == nil
}

// ---- transcript scans ----

const scanCacheFile = "scans.json"

type scanCacheDoc struct {
	Schema  int
	Binary  string
	Entries map[string]scanRecord
}

// scanRecord is a cachedScan in exportable form.
type scanRecord struct {
	Size      int64
	Mod       time.Time
	CWD       string
	Title     string
	Model     string
	Branch    string
	Prompt    string
	Reply     string
	Prompts   []string
	Last      time.Time
	Continued bool
	Details   sessionDetails
	Shells    []shellRecord
}

type shellRecord struct {
	ID, Desc string
	Started  time.Time
	Ended    bool
}

func toScanRecord(c cachedScan) scanRecord {
	tm := c.tm
	r := scanRecord{
		Size: c.size, Mod: c.mod, CWD: tm.cwd, Title: tm.title, Model: tm.model, Branch: tm.branch,
		Prompt: tm.prompt, Reply: tm.reply, Prompts: tm.prompts, Last: tm.last, Continued: tm.continued,
		Details: tm.details,
	}
	for _, sh := range tm.shells.shells {
		r.Shells = append(r.Shells, shellRecord{sh.ID, sh.Desc, sh.Started, sh.ended})
	}
	return r
}

func fromScanRecord(r scanRecord) cachedScan {
	tm := transcriptMeta{
		cwd: r.CWD, title: r.Title, model: r.Model, branch: r.Branch, prompt: r.Prompt, reply: r.Reply,
		prompts: r.Prompts, last: r.Last, continued: r.Continued, details: r.Details,
	}
	for _, sh := range r.Shells {
		tm.shells.shells = append(tm.shells.shells, bgShell{ID: sh.ID, Desc: sh.Desc, Started: sh.Started, ended: sh.Ended})
	}
	return cachedScan{size: r.Size, mod: r.Mod, tm: tm}
}

// scanDisk tracks the on-disk copy of scanCache. Guarded by scanCache's lock.
var scanDisk struct {
	loaded   bool
	dirty    bool
	lastSave time.Time
}

// loadScanCacheLocked fills scanCache from disk once. Callers hold scanCache.
func loadScanCacheLocked() {
	if scanDisk.loaded {
		return
	}
	scanDisk.loaded = true
	var doc scanCacheDoc
	if !readJSON(scanCacheFile, &doc) || doc.Schema != diskCacheSchema || doc.Binary == "" || doc.Binary != binaryID() {
		return
	}
	for path, r := range doc.Entries {
		if _, ok := scanCache.m[path]; !ok {
			scanCache.m[path] = fromScanRecord(r)
		}
	}
}

// saveScanCache writes the scan cache when it changed, at most every
// scanSaveEvery unless forced (on exit). Entries for deleted transcripts are
// dropped.
func saveScanCache(force bool) {
	scanCache.Lock()
	if !scanDisk.dirty || (!force && time.Since(scanDisk.lastSave) < scanSaveEvery) {
		scanCache.Unlock()
		return
	}
	doc := scanCacheDoc{Schema: diskCacheSchema, Binary: binaryID(), Entries: make(map[string]scanRecord, len(scanCache.m))}
	for path, c := range scanCache.m {
		doc.Entries[path] = toScanRecord(c)
	}
	scanDisk.dirty = false
	scanDisk.lastSave = time.Now()
	scanCache.Unlock()

	if doc.Binary == "" {
		return
	}
	for path := range doc.Entries {
		if _, err := os.Stat(path); err != nil {
			delete(doc.Entries, path)
		}
	}
	writeJSON(scanCacheFile, doc)
}

// ---- PR statuses ----

const prCacheFile = "prs.json"

type prCacheDoc struct {
	Schema int
	PRs    map[string]prRecord
	Owners map[string]string // repo owner → gh account that can see its PRs
}

type prRecord struct {
	State, Title, Checks, Review string
	Draft                        bool
	Fetched                      time.Time
}

// loadPRCache returns the last known PR statuses and restores which account
// sees which owner, so a new process shows badges at once and only refreshes
// stale open PRs.
func loadPRCache() map[string]prInfo {
	status := map[string]prInfo{}
	var doc prCacheDoc
	if !readJSON(prCacheFile, &doc) || doc.Schema != diskCacheSchema {
		return status
	}
	for url, r := range doc.PRs {
		status[url] = prInfo{State: r.State, Draft: r.Draft, Title: r.Title, Checks: r.Checks, Review: r.Review, fetched: r.Fetched}
	}
	ownerAccounts.Lock()
	for owner, login := range doc.Owners {
		if _, ok := ownerAccounts.m[owner]; !ok {
			ownerAccounts.m[owner] = login
		}
	}
	ownerAccounts.Unlock()
	return status
}

// savePRCache writes PR statuses and the owner→account routing. Failed
// lookups aren't kept, so a new process retries them.
func savePRCache(status map[string]prInfo) {
	doc := prCacheDoc{Schema: diskCacheSchema, PRs: map[string]prRecord{}, Owners: map[string]string{}}
	for url, info := range status {
		if info.State != "" {
			doc.PRs[url] = prRecord{info.State, info.Title, info.Checks, info.Review, info.Draft, info.fetched}
		}
	}
	ownerAccounts.Lock()
	for owner, login := range ownerAccounts.m {
		doc.Owners[owner] = login
	}
	ownerAccounts.Unlock()
	writeJSON(prCacheFile, doc)
}
