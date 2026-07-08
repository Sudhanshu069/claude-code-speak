// Package transcript tails a single Claude Code transcript JSONL file and emits
// each new assistant text block. It mirrors Node src/transcript-watcher.js:
// fsnotify for near-instant reaction plus a 200ms safety-poll fallback,
// incremental byte-offset reads, boundary-safe raw-byte line buffering, and
// UUID dedup. Run is a SINGLE goroutine so fsnotify and the poll serialize
// without a mutex.
package transcript

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
)

// safetyPollInterval is the fallback poll cadence; fsnotify is only a latency
// optimization and correctness never depends on it.
const safetyPollInterval = 200 * time.Millisecond

// seenCap / seenEvictTo bound UUID dedup memory (evict oldest, FIFO).
const (
	seenCap     = 1000
	seenEvictTo = 500
)

// Event is one new assistant text block observed in the transcript.
type Event struct {
	SessionID string
	Text      string
	Time      time.Time
}

// entry is the subset of a transcript JSONL record we care about.
type entry struct {
	Type      string `json:"type"`
	UUID      string `json:"uuid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Message   struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// seenSet bounds UUID dedup memory (cap N, evict to N/2, FIFO).
type seenSet struct {
	m     map[string]struct{}
	order []string
	cap   int
}

// newSeenSet builds a seenSet with the given capacity.
func newSeenSet(cap int) *seenSet {
	return &seenSet{m: make(map[string]struct{}), cap: cap}
}

// add records id and reports whether it was new (not previously seen). When the
// set grows past cap it evicts the oldest ids down to seenEvictTo (FIFO).
func (s *seenSet) add(id string) (isNew bool) {
	if _, ok := s.m[id]; ok {
		return false
	}
	s.m[id] = struct{}{}
	s.order = append(s.order, id)
	if len(s.order) > s.cap {
		drop := s.order[:len(s.order)-seenEvictTo]
		for _, k := range drop {
			delete(s.m, k)
		}
		// Keep the most-recent seenEvictTo ids; copy handles the overlap.
		s.order = append(s.order[:0], s.order[len(s.order)-seenEvictTo:]...)
	}
	return true
}

// Watcher tails a single transcript file. Run is the sole producer of Events.
type Watcher struct {
	path   string
	out    chan Event
	offset int64
	buf    []byte
	seen   *seenSet
	fsw    *fsnotify.Watcher // created inside Run; nil => poll-only fallback
}

// New builds a watcher for one transcript path (out channel buffered ~64).
func New(path string) *Watcher {
	return &Watcher{
		path: path,
		out:  make(chan Event, 64),
		seen: newSeenSet(seenCap),
	}
}

// Events is the read side the daemon selects on. Closed when Run returns.
func (w *Watcher) Events() <-chan Event {
	return w.out
}

// Run seeks to EOF, arms fsnotify + a 200ms safety-poll ticker, and loops in a
// SINGLE goroutine (select over fsnotify events, the ticker, and ctx.Done) so
// reads serialize without a mutex. Returns ctx.Err() on cancel. fsnotify errors
// are non-fatal (drop to poll-only). Closes Events() on return.
func (w *Watcher) Run(ctx context.Context) error {
	defer close(w.out)
	defer func() {
		if w.fsw != nil {
			w.fsw.Close()
			w.fsw = nil
		}
	}()

	// Start from the end of the file: only new content is spoken. A missing
	// file just means offset 0 and the poll picks it up once it appears.
	if fi, err := os.Stat(w.path); err == nil {
		w.offset = fi.Size()
	} else {
		w.offset = 0
	}

	// Arm fsnotify for near-instant reaction. Any failure (create/add error) is
	// non-fatal: nil channels leave us in poll-only mode, which is correct.
	var fsEvents chan fsnotify.Event
	var fsErrors chan error
	if fsw, err := fsnotify.NewWatcher(); err == nil {
		if err := fsw.Add(w.path); err != nil {
			fsw.Close()
		} else {
			w.fsw = fsw
			fsEvents = fsw.Events
			fsErrors = fsw.Errors
		}
	}

	ticker := time.NewTicker(safetyPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.readNew(ctx)
		case _, ok := <-fsEvents:
			if !ok {
				fsEvents, fsErrors = nil, nil
				continue
			}
			w.readNew(ctx)
		case _, ok := <-fsErrors:
			// A watcher error (file rotated/removed, fd limits) must never kill
			// the daemon. Downgrade to poll-only; the safety poll guarantees
			// correctness on its own.
			if ok && w.fsw != nil {
				w.fsw.Close()
				w.fsw = nil
			}
			fsEvents, fsErrors = nil, nil
		}
	}
}

// readNew reads bytes past w.offset, splits complete lines, and emits assistant
// text. It is idempotent/offset-guarded, and resets to 0 on file shrink
// (truncate/rotate). Raw []byte buffering keeps line splitting boundary-safe:
// '\n' (0x0A) never occurs inside a multibyte UTF-8 sequence, so a partial rune
// at a read boundary simply stays in the buffer until the next read completes it.
func (w *Watcher) readNew(ctx context.Context) {
	fi, err := os.Stat(w.path)
	if err != nil {
		// File deleted/rotated away: no error spam, poll retries next tick.
		return
	}
	size := fi.Size()
	if size < w.offset {
		// Truncated/rotated in place — restart from the top rather than waiting
		// forever for it to grow past a now-stale offset.
		w.offset = 0
		w.buf = w.buf[:0]
	}
	if size <= w.offset {
		return
	}

	f, err := os.Open(w.path)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
		return
	}
	chunk := make([]byte, size-w.offset)
	n, err := io.ReadFull(f, chunk)
	if n <= 0 {
		return
	}
	// Partial read (file shrank mid-read) is fine: advance by what we got.
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return
	}
	w.offset += int64(n)
	w.buf = append(w.buf, chunk[:n]...)

	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i]
		w.emitLine(ctx, line)
		w.buf = w.buf[i+1:]
	}
}

// emitLine parses one JSONL line and, for a new-uuid assistant record, sends an
// Event per text block. It respects ctx so it never blocks past cancellation.
func (w *Watcher) emitLine(ctx context.Context, line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var e entry
	if err := json.Unmarshal(line, &e); err != nil {
		return // skip malformed lines
	}
	if e.Type != "assistant" {
		return
	}
	// Dedup by UUID (Claude may write a partial then a complete record). A record
	// without a uuid is always processed, matching the Node behavior.
	if e.UUID != "" && !w.seen.add(e.UUID) {
		return
	}
	sid := e.SessionID
	if sid == "" {
		sid = "unknown"
	}
	for _, block := range e.Message.Content {
		if block.Type == "text" && block.Text != "" {
			select {
			case w.out <- Event{SessionID: sid, Text: block.Text, Time: time.Now()}:
			case <-ctx.Done():
				return
			}
		}
	}
}
