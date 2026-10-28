package orders

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ErrNoJournalFile: the logger writes to no file (a writer-only logger), so
// there is no journal to read back.
var ErrNoJournalFile = errors.New("journal has no file")

// tailChunk is how much of orders.log one backward read takes.
const tailChunk = 64 << 10

// JournalBack reads orders.log from its end back to from and calls fn with
// each event, newest first, until fn returns false (#417: the assistant's
// week of facts; the in-memory ring holds only the last RecentEvents). Each
// event carries the seq the ring and a restart's replay give it and its
// journaled time; unreadable lines are skipped and not numbered, as the
// replay does. Lines with a time before from are skipped and the read stops
// at the first one an hour older (journal times are the market snapshot's,
// a little out of order). e.Data is valid only during the call.
//
// The file is read without holding the logger: lines written after the call
// began are not read.
func (l *Logger) JournalBack(from time.Time, fn func(e RecentEvent) bool) error {
	if l.path == "" || l.file == nil {
		return ErrNoJournalFile
	}
	// Under the write lock, the file holds exactly the events numbered so far.
	l.mu.Lock()
	seq := l.recent.lastSeq()
	info, err := l.file.Stat()
	l.mu.Unlock()
	if err != nil {
		return fmt.Errorf("stat journal %s: %w", l.path, err)
	}
	f, err := os.Open(l.path)
	if err != nil {
		return fmt.Errorf("open journal %s: %w", l.path, err)
	}
	defer f.Close()
	stopBefore := from.Add(-time.Hour)
	return linesBack(f, info.Size(), func(line []byte) bool {
		event, _, ok := decodeLine(line)
		if !ok {
			return true
		}
		at, _ := time.Parse(time.RFC3339Nano, field(line, timestampKey))
		s := seq
		seq--
		switch {
		case s == 0:
			return false // more lines than events numbered: nothing older is citable
		case at.IsZero():
			return true
		case at.Before(stopBefore):
			return false
		case at.Before(from):
			return true
		}
		return fn(RecentEvent{Seq: s, Event: event, At: at, Data: line})
	})
}

// linesBack calls fn with each non-empty line of r[0:size), last first, until
// fn returns false. A line is valid only during its call.
func linesBack(r io.ReaderAt, size int64, fn func(line []byte) bool) error {
	var carry []byte // the start of the line the previous chunk began inside
	for end := size; end > 0; {
		start := max(0, end-tailChunk)
		buf := make([]byte, int(end-start)+len(carry))
		if _, err := r.ReadAt(buf[:end-start], start); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read journal: %w", err)
		}
		copy(buf[end-start:], carry)
		for {
			i := bytes.LastIndexByte(buf, '\n')
			if i < 0 {
				break
			}
			if line := bytes.TrimSpace(buf[i+1:]); len(line) > 0 && !fn(line) {
				return nil
			}
			buf = buf[:i]
		}
		carry = buf
		end = start
	}
	if line := bytes.TrimSpace(carry); len(line) > 0 {
		fn(line)
	}
	return nil
}
