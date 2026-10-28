package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"time"
)

// Event is one accepted line of events.jsonl: the envelope and its
// properties. UserID and SessionID are used to count people and sessions and
// never reach the report.
type Event struct {
	Name        string
	TS          time.Time
	UserID      string
	SessionID   string
	Role        string
	Environment string // "" when the line has none
	Props       map[string]any
}

// Why a line was rejected: a category, never the line's value (it may be
// what the plan keeps out).
const (
	RejectParse    = "not_json"          // not a JSON object
	RejectEvent    = "unknown_event"     // event not in the plan
	RejectEnvelope = "bad_envelope"      // ts, user_id, session_id, role, environment or app_version missing or malformed
	RejectProperty = "unknown_property"  // a field the plan does not allow for that event
	RejectValue    = "value_not_allowed" // a property outside its rule
)

// Log is an events file read whole: the accepted events and what was
// rejected, with the span the file covers.
type Log struct {
	Path     string
	Missing  bool // the file does not exist
	Lines    int  // non-empty lines read
	Events   []Event
	Rejected map[string]int // reason → lines
}

// Load reads an events file. A missing file is an empty Log with Missing
// set, not an error: nothing is written until someone has said yes.
func Load(path string) (Log, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Log{Path: path, Missing: true, Rejected: map[string]int{}}, nil
	}
	if err != nil {
		return Log{}, fmt.Errorf("open events %s: %w", path, err)
	}
	defer f.Close()
	lg, err := Read(f)
	if err != nil {
		return Log{}, fmt.Errorf("read events %s: %w", path, err)
	}
	lg.Path = path
	return lg, nil
}

// Read parses JSON lines: every line is either an Event or a counted
// rejection, never both and never in part.
func Read(r io.Reader) (Log, error) {
	lg := Log{Rejected: map[string]int{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lg.Lines++
		ev, reason := ParseLine(line)
		if reason != "" {
			lg.Rejected[reason]++
			continue
		}
		lg.Events = append(lg.Events, ev)
	}
	return lg, sc.Err()
}

// ParseLine checks one stored line against the plan — the envelope (§3) and
// that event's allow-listed properties (§2, §4) — and returns the Event or
// the reject reason.
func ParseLine(line []byte) (Event, string) {
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil || m == nil {
		return Event{}, RejectParse
	}
	name, _ := m["event"].(string)
	allowed, ok := Events[name]
	if !ok {
		return Event{}, RejectEvent
	}
	ev := Event{Name: name, Props: map[string]any{}}
	tsRaw, _ := m["ts"].(string)
	ts, err := time.Parse(time.RFC3339, tsRaw)
	if err != nil {
		return Event{}, RejectEnvelope
	}
	ev.TS = ts.UTC()
	ev.UserID, _ = m["user_id"].(string)
	ev.SessionID, _ = m["session_id"].(string)
	ev.Role, _ = m["role"].(string)
	if !userIDRe.MatchString(ev.UserID) || !sessionIDRe.MatchString(ev.SessionID) || !slices.Contains(roles, ev.Role) {
		return Event{}, RejectEnvelope
	}
	if v, has := m["environment"]; has {
		ev.Environment, _ = v.(string)
		if !slices.Contains(environments, ev.Environment) {
			return Event{}, RejectEnvelope
		}
	}
	if v, has := m["app_version"]; has {
		s, _ := v.(string)
		if !versionRe.MatchString(s) {
			return Event{}, RejectEnvelope
		}
	}
	for k, v := range m {
		switch k {
		case "event", "ts", "user_id", "session_id", "role", "environment", "app_version":
			continue
		}
		if !slices.Contains(allowed, k) {
			return Event{}, RejectProperty
		}
		if !valueOK(Properties[k], v) {
			return Event{}, RejectValue
		}
		ev.Props[k] = v
	}
	return ev, ""
}
