package claudedata

import (
	"os"
	"runtime"
	"sync"
)

// The session browser needs a title, a message count, the working directory and
// the model for every transcript on the machine — which means reading every
// transcript. With a few hundred sessions that is tens of seconds of identical
// work on every visit to the page, so two things keep it cheap:
//
//   - scanFiles parses files in parallel (the work is JSON parsing, so it
//     parallelises well);
//   - the metadata cache below remembers each file's result keyed by
//     path + mtime + size, so a revisit costs a handful of stat calls.
//
// A changed file is always re-read: mtime and size are checked on every lookup.
// Nothing here caches agent-specific identity (a Claude session id comes from
// the file name, a Codex one from inside the file), only the fields that require
// parsing it.

// sessionBrowserMeta is everything the browser needs from a transcript, minus
// the bits that come from the path rather than the contents.
type sessionBrowserMeta struct {
	ID    string // id found inside the file (Codex/Buddy); empty for Claude
	Title string
	Msgs  int
	Cwd   string
	Model string
}

type scanCacheEntry struct {
	mtime, size int64
	meta        sessionBrowserMeta
}

var scanCache = struct {
	sync.Mutex
	m map[string]scanCacheEntry
}{m: make(map[string]scanCacheEntry)}

// maxScanCacheEntries bounds the cache so a machine with an enormous session
// history cannot grow it without limit. Hitting the cap simply drops the whole
// map, which costs one slow scan and no correctness.
const maxScanCacheEntries = 20000

func lookupScan(key string, mtime, size int64) (sessionBrowserMeta, bool) {
	scanCache.Lock()
	defer scanCache.Unlock()
	e, ok := scanCache.m[key]
	if !ok || e.mtime != mtime || e.size != size {
		return sessionBrowserMeta{}, false
	}
	return e.meta, true
}

func storeScan(key string, mtime, size int64, meta sessionBrowserMeta) {
	scanCache.Lock()
	defer scanCache.Unlock()
	if len(scanCache.m) >= maxScanCacheEntries {
		scanCache.m = make(map[string]scanCacheEntry)
	}
	scanCache.m[key] = scanCacheEntry{mtime: mtime, size: size, meta: meta}
}

// statForScan returns the freshness key used by the metadata cache.
func statForScan(path string) (info os.FileInfo, mtime, size int64, ok bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, 0, false
	}
	return info, info.ModTime().UnixMilli(), info.Size(), true
}

// scanFiles maps fn over every path with a bounded worker pool, returning the
// results in the same order as the input so callers keep a deterministic list
// before they sort it.
func scanFiles(paths []string, fn func(string) SessionMeta) []SessionMeta {
	if len(paths) == 0 {
		return nil
	}
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8 // past this the work is I/O-bound and extra workers only add contention
	}
	if workers > len(paths) {
		workers = len(paths)
	}

	out := make([]SessionMeta, len(paths))
	if workers <= 1 {
		for i, p := range paths {
			out[i] = fn(p)
		}
		return out
	}

	indexes := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				out[i] = fn(paths[i])
			}
		}()
	}
	for i := range paths {
		indexes <- i
	}
	close(indexes)
	wg.Wait()
	return out
}
