package shadow

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// maxLine bounds one line of the file when it is read back.
const maxLine = 1 << 20

// Book holds the open shadow legs and appends every change to the file.
// Loop-owned: the decision loop is its only user. The API reads the file.
type Book struct {
	file *os.File
	open map[string]*Leg
	// spent: held legs an exit rule already ended: not opened again until the
	// hold ends (one shadow leg per held leg). noLeg: the rule last written as
	// a no_leg line, by leg. savedAt: when each open leg's line was written.
	spent   map[string]bool
	noLeg   map[string]string
	savedAt map[string]time.Time
}

// Open restores the book from path, so a restart goes on with the same legs,
// and opens it for appending.
func Open(path string) (*Book, error) {
	recs, skipped, err := Read(path)
	if err != nil {
		return nil, err
	}
	if skipped > 0 {
		slog.Warn("shadow legs: unreadable lines skipped", "path", path, "lines", skipped)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("shadow legs dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open shadow legs %s: %w", path, err)
	}
	b := &Book{file: f, open: map[string]*Leg{}, spent: map[string]bool{}, noLeg: map[string]string{}, savedAt: map[string]time.Time{}}
	for _, r := range recs {
		b.restore(r)
	}
	slog.Info("shadow legs restored", "path", path, "open", len(b.open), "lines", len(recs))
	return b, nil
}

// restore applies one line of the file.
func (b *Book) restore(r Record) {
	switch r.Event {
	case EventOpen, EventUpdate:
		leg := r.Leg
		b.open[r.Key] = &leg
		delete(b.noLeg, r.Key)
		delete(b.spent, r.Key)
	case EventClose:
		delete(b.open, r.Key)
		if endsHold(r.ExitReason) {
			b.spent[r.Key] = true
		}
	case EventNoLeg:
		b.noLeg[r.Key] = r.HeldBy
	}
}

// endsHold reports whether an exit ended the held leg itself, not the hold.
func endsHold(reason string) bool {
	return reason == ReasonStopLoss || reason == ReasonDTERoll || reason == ReasonDelta
}

// Close closes the file.
func (b *Book) Close() error { return b.file.Close() }

// Legs lists the open legs, ordered by key.
func (b *Book) Legs() []*Leg {
	out := make([]*Leg, 0, len(b.open))
	for _, l := range b.open {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Following reports whether key has an open leg, or had one an exit ended
// and its hold still lasts: either way no new leg opens.
func (b *Book) Following(key string) bool {
	_, open := b.open[key]
	return open || b.spent[key]
}

// Start opens leg, whose market fields the caller filled in r.
func (b *Book) Start(r Record) {
	r.Event = EventOpen
	leg := r.Leg
	b.open[r.Key] = &leg
	delete(b.noLeg, r.Key)
	b.savedAt[r.Key] = r.At
	b.write(r)
}

// Save writes an open leg's state when UpdateEvery has passed or force is set.
func (b *Book) Save(l *Leg, now time.Time, force bool) {
	if !force && now.Sub(b.savedAt[l.Key]) < UpdateEvery {
		return
	}
	b.savedAt[l.Key] = now
	b.write(Record{Event: EventUpdate, At: now, Leg: *l})
}

// End removes a leg that Leg.Finish closed and writes its outcome.
func (b *Book) End(r Record) {
	r.Event = EventClose
	delete(b.open, r.Key)
	delete(b.savedAt, r.Key)
	if endsHold(r.ExitReason) {
		b.spent[r.Key] = true
	}
	b.write(r)
}

// NoteNoLeg writes, once per rule, that a held leg has nothing to follow.
func (b *Book) NoteNoLeg(l Leg, at time.Time) {
	if b.noLeg[l.Key] == l.HeldBy {
		return
	}
	b.noLeg[l.Key] = l.HeldBy
	b.write(Record{Event: EventNoLeg, At: at, Leg: l})
}

// Forget clears a held leg whose hold ended: a later hold is a new leg.
func (b *Book) Forget(key string) {
	delete(b.spent, key)
	delete(b.noLeg, key)
}

// Held lists the keys with a state left from an earlier hold.
func (b *Book) Held() []string {
	out := make([]string, 0, len(b.spent)+len(b.noLeg))
	for k := range b.spent {
		out = append(out, k)
	}
	for k := range b.noLeg {
		out = append(out, k)
	}
	return out
}

func (b *Book) write(r Record) {
	r.DTE = r.Position().DTEAt(r.At)
	data, err := json.Marshal(r)
	if err != nil {
		slog.Error("shadow legs: encode failed", "key", r.Key, "err", err)
		return
	}
	if _, err := b.file.Write(append(data, '\n')); err != nil {
		slog.Error("shadow legs: write failed", "key", r.Key, "err", err)
	}
}

// Read returns every line of the file at path, in order, and how many could
// not be read. A missing file is no lines. The replay reads the file with it.
func Read(path string) ([]Record, int, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read shadow legs %s: %w", path, err)
	}
	defer f.Close()
	var out []Record
	skipped := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil || r.Key == "" {
			skipped++
			continue
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return out, skipped, fmt.Errorf("read shadow legs %s: %w", path, err)
	}
	return out, skipped, nil
}
