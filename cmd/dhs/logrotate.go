package main

// Daily log rotation for 24/7/365 watchers.
//
// The uniform logging contract (epic #987) writes every connector's log to
// .cache/logs/<proto>/<host>/<verb>.log. That was opened with os.Create: one
// file, truncated at start, growing without bound. A watcher that runs for a
// year produces one unmanageable file and loses its own history on every
// restart — both unacceptable for the 24/7 operating mode.
//
// dailyWriter replaces that with one file per calendar day:
//
//	.cache/logs/cerebrum-nb/10.6.250.5/watch-2026-09-04.log
//	.cache/logs/cerebrum-nb/10.6.250.5/watch-2026-09-05.log
//
// Two deliberate behaviour choices:
//
//   - Files are opened O_APPEND, not truncated. Restarting a watcher must not
//     erase the day's evidence — the usual reason for a restart is that
//     something went wrong and the operator wants to compare before/after.
//   - The roll is driven by the injected clock and evaluated on write, so no
//     background timer goroutine exists to leak, and tests advance time
//     instead of sleeping.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dhs/internal/clock"
)

// dayLayout is the date stamp in a rotated file name. Sorts lexicographically
// in calendar order, which is what makes retention pruning a simple sort.
const dayLayout = "2006-01-02"

// dailyWriter is an io.WriteCloser that rolls to a new file when the local
// calendar day changes. Safe for concurrent use — slog handlers write from
// whichever goroutine logs.
type dailyWriter struct {
	dir    string // directory holding the rotated files
	base   string // file name stem, e.g. "watch"
	ext    string // extension including the dot, e.g. ".log"
	retain int    // days of history to keep; 0 = keep everything
	clk    clock.Clock

	mu  sync.Mutex
	f   *os.File
	day string // day stamp of the currently open file
}

// newDailyWriter prepares rotation for the logical path (e.g.
// ".../watch.log"). The directory is created if needed. The first file is
// opened eagerly so a permission problem surfaces at startup rather than at
// the first log record.
func newDailyWriter(path string, retain int, clk clock.Clock) (*dailyWriter, error) {
	if clk == nil {
		clk = clock.System()
	}
	if retain < 0 {
		retain = 0
	}
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		return nil, fmt.Errorf("log path %q has no file name", path)
	}
	if ext == "" {
		ext = ".log"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("log dir %s: %w", dir, err)
	}
	w := &dailyWriter{dir: dir, base: base, ext: ext, retain: retain, clk: clk}
	if err := w.rollTo(w.clk.Now()); err != nil {
		return nil, err
	}
	return w, nil
}

// pathFor renders the rotated file name for a given day.
func (w *dailyWriter) pathFor(day string) string {
	return filepath.Join(w.dir, w.base+"-"+day+w.ext)
}

// dailyPathFor renders the rotated file name a logical log path resolves to on
// the given day, without opening anything. Used by the CLI banner so the
// operator is told the file that actually exists.
func dailyPathFor(path string, t time.Time) string {
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if ext == "" {
		ext = ".log"
	}
	return filepath.Join(dir, base+"-"+t.Format(dayLayout)+ext)
}

// rollTo closes the current file and opens the one for t's calendar day.
// Caller must not hold w.mu (rollTo is called under it from Write, and
// directly from the constructor before any concurrency exists).
func (w *dailyWriter) rollTo(t time.Time) error {
	day := t.Format(dayLayout)
	if w.f != nil && w.day == day {
		return nil
	}
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	f, err := os.OpenFile(w.pathFor(day), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", w.pathFor(day), err)
	}
	w.f, w.day = f, day
	w.prune(t)
	return nil
}

// Write appends p, rolling first if the calendar day has changed.
func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.clk.Now()
	if now.Format(dayLayout) != w.day {
		if err := w.rollTo(now); err != nil {
			return 0, err
		}
	}
	if w.f == nil {
		return 0, os.ErrClosed
	}
	return w.f.Write(p)
}

// Close releases the current file. Idempotent.
func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// CurrentPath reports the file being written right now (test + diagnostics).
func (w *dailyWriter) CurrentPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pathFor(w.day)
}

// prune deletes rotated files older than the retention window. retain == 0
// keeps everything (the default — losing an operator's history silently would
// be worse than a large directory). Failures are ignored: a locked or
// permission-denied old file must never take down a running watcher.
func (w *dailyWriter) prune(now time.Time) {
	if w.retain <= 0 {
		return
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	prefix := w.base + "-"
	cutoff := now.AddDate(0, 0, -w.retain).Format(dayLayout)

	var stale []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, w.ext) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, prefix), w.ext)
		if _, perr := time.Parse(dayLayout, day); perr != nil {
			continue // not one of ours
		}
		// dayLayout sorts lexicographically in calendar order.
		if day < cutoff {
			stale = append(stale, filepath.Join(w.dir, name))
		}
	}
	sort.Strings(stale)
	for _, p := range stale {
		_ = os.Remove(p)
	}
}
