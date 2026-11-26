// Package security builds the admins' weekly security digest and the same-day
// alerts (#717) from the monitor's audit trail (data/monitor/audit.jsonl and
// its archives, frontend/docs/audit.md): counts only — never a password,
// token, reset link, IP address or a person's name in what it writes.
// The trail is read, never written; the rules are pure (lines and `now` in).
package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/backup"
)

// Entry is the part of one audit line the digest and the alerts read.
// Username, Target and IP are used to count distinct people and sources and
// never reach the digest or an alert's text.
type Entry struct {
	Time     time.Time
	Action   string
	Username string // who acted ("bootstrap" for the first-admin setup)
	Target   string // the person acted on, when the action has one
	To       string // the new role of user_created and role_changed
	From     string // the old role of role_changed
	IP       string
}

// line is the JSON shape of an audit line (lib/auth/service.js audit()).
type line struct {
	Time     string `json:"time"`
	Action   string `json:"action"`
	Username string `json:"username"`
	Target   string `json:"target"`
	To       string `json:"to"`
	From     string `json:"from"`
	IP       string `json:"ip"`
}

// Trail is the audit trail read whole: its entries oldest first and the lines
// that could not be read (not a JSON object, or no valid time), counted only.
type Trail struct {
	Entries []Entry
	Skipped int
}

// ReadTrail reads the audit trail in dir — the rotated archives oldest first,
// then audit.jsonl (backup.AuditTrailFiles) — read-only. A missing folder or
// file is an empty trail, not an error.
func ReadTrail(dir string) (Trail, error) {
	var t Trail
	files, err := backup.AuditTrailFiles(dir)
	if err != nil {
		return t, err
	}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return t, fmt.Errorf("read %s: %w", path, err)
		}
		for _, raw := range bytes.Split(b, []byte{'\n'}) {
			if len(bytes.TrimSpace(raw)) == 0 {
				continue
			}
			e, ok := parseLine(raw)
			if !ok {
				t.Skipped++
				continue
			}
			t.Entries = append(t.Entries, e)
		}
	}
	return t, nil
}

func parseLine(raw []byte) (Entry, bool) {
	var l line
	if json.Unmarshal(raw, &l) != nil || l.Action == "" {
		return Entry{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, l.Time)
	if err != nil {
		return Entry{}, false
	}
	return Entry{Time: at.UTC(), Action: l.Action, Username: l.Username, Target: l.Target, To: l.To, From: l.From, IP: l.IP}, true
}

// promotesToAdmin is true for a line that gives someone the admin role
// outside the first-admin setup: a role change to admin, or an admin
// creating another admin. The first start's own admin (username "bootstrap",
// lib/auth/service.js bootstrapAdmin) is the setup and never counts.
func promotesToAdmin(e Entry) bool {
	switch e.Action {
	case "role_changed":
		return e.To == "admin"
	case "user_created":
		return e.To == "admin" && e.Username != bootstrapActor
	}
	return false
}

// changesAdminRole is true for any line that moves the admin role: a
// promotion (including the first-admin setup) or an admin demoted.
func changesAdminRole(e Entry) bool {
	switch e.Action {
	case "role_changed":
		return e.To == "admin" || e.From == "admin"
	case "user_created":
		return e.To == "admin"
	}
	return false
}

const bootstrapActor = "bootstrap"
