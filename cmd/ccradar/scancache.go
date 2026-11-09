package main

import (
	"os"
	"sync"
	"time"
)

// scanCache remembers each transcript's scan by size and mtime, so the
// refresh loop only re-reads transcripts that changed. Background agents'
// transcripts can run past 100 MB while rarely changing.
var scanCache = struct {
	sync.Mutex
	m map[string]cachedScan
}{m: map[string]cachedScan{}}

type cachedScan struct {
	size int64
	mod  time.Time
	tm   transcriptMeta
}

// scanTranscriptCached is scanTranscript with the cache in front. The result
// shares slices with the cache, so callers must not modify it.
func scanTranscriptCached(path string) transcriptMeta {
	fi, err := os.Stat(path)
	if err != nil {
		return scanTranscript(path)
	}
	scanCache.Lock()
	c, ok := scanCache.m[path]
	scanCache.Unlock()
	if ok && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.tm
	}
	tm := scanTranscript(path)
	scanCache.Lock()
	scanCache.m[path] = cachedScan{size: fi.Size(), mod: fi.ModTime(), tm: tm}
	scanCache.Unlock()
	return tm
}
