package recorder

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// A day file is a series of gzip members, one per snapshot (#661). A write cut
// off by a restart once left a bare member header in 2026-11-04.csv.gz: gzip
// then treats everything after it as that member's data, so the hours after
// it could not be read although they were whole. Hence both sides here: a
// member reaches the file whole or not at all (appendMember), and a reader
// steps over a damaged stretch to the next complete member (readMembers).

// gzipMagic starts every gzip member (the last byte: deflate).
var gzipMagic = []byte{0x1f, 0x8b, 0x08}

// appendMember adds plain to path as one more gzip member. The file is
// replaced by a complete copy (a rename), never written in place, so a stop at
// any moment leaves the file as it was, at most with a stale path.tmp the next
// write overwrites. A day file is small, about 1 MB at most.
func appendMember(path string, plain []byte) error {
	var member bytes.Buffer
	zw := gzip.NewWriter(&member)
	if _, err := zw.Write(plain); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp := path + ".tmp"
	if err := writeSynced(tmp, old, member.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Rename(tmp, path)
}

// writeSynced writes the parts to path and syncs them to disk.
func writeSynced(path string, parts ...[]byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, part := range parts {
		if _, err := file.Write(part); err != nil {
			return err
		}
	}
	return file.Sync()
}

// hole is a damaged stretch of a day file. A zero time means the stretch
// starts the file (lastGood) or runs to its end (resumes).
type hole struct {
	lastGood, resumes time.Time
}

// readMembers gives read each complete member of the file in turn, decompressed;
// read returns the times of the member's first and last record (zero: none).
// A damaged stretch is skipped and warned about with the day and the last good
// time; an error from read stops the file (a bad row in a complete member).
func readMembers(path string, read func(plain []byte) (first, last time.Time, err error)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var (
		holes    []hole
		lastGood time.Time
		zr       gzip.Reader
	)
	for pos := 0; pos < len(data); {
		plain, size, err := readMember(&zr, data[pos:])
		if err != nil {
			holes = append(holes, hole{lastGood: lastGood})
			pos = nextMember(&zr, data, pos+1)
			continue
		}
		pos += size
		first, last, err := read(plain)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if first.IsZero() {
			continue
		}
		if n := len(holes); n > 0 && holes[n-1].resumes.IsZero() {
			holes[n-1].resumes = first
		}
		lastGood = last
	}
	for _, h := range holes {
		name := filepath.Base(path)
		slog.Warn("recorded day damaged: its complete members were read", "day", name[:min(10, len(name))], "file", path,
			"last_good", timeOr(h.lastGood, "start of the day"), "resumes", timeOr(h.resumes, "not in this file"))
	}
	return nil
}

// readMember decompresses the member at the start of data and returns its
// bytes on disk. Any error means it is not a complete member.
func readMember(zr *gzip.Reader, data []byte) (plain []byte, size int, err error) {
	br := bytes.NewReader(data) // a ByteReader: gzip then stops exactly at the member's end
	if err := zr.Reset(br); err != nil {
		return nil, 0, err
	}
	zr.Multistream(false)
	if plain, err = io.ReadAll(zr); err != nil {
		return nil, 0, err
	}
	return plain, len(data) - br.Len(), nil
}

// nextMember returns the offset of the first complete member at or after from,
// or len(data). The magic bytes can occur inside compressed data; the member's
// checksum tells those apart.
func nextMember(zr *gzip.Reader, data []byte, from int) int {
	for from < len(data) {
		i := bytes.Index(data[from:], gzipMagic)
		if i < 0 {
			break
		}
		from += i
		if _, _, err := readMember(zr, data[from:]); err == nil {
			return from
		}
		from++
	}
	return len(data)
}

func timeOr(t time.Time, none string) string {
	if t.IsZero() {
		return none
	}
	return t.UTC().Format(time.RFC3339)
}
