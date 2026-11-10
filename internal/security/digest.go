package security

import (
	"fmt"
	"strings"
	"time"

	"optionsbot/internal/backup"
	"optionsbot/internal/evidence"
)

// Counted is the digest's list of audit actions, in the order shown, with the
// words a person reads (frontend/docs/sign-in.md "Audit" lists them all).
var Counted = []struct{ Action, Label string }{
	{"sign_in_failed", "Failed sign-ins"},
	{"locked", "Lockouts"},
	{"password_change_failed", "Failed password changes"},
	{"password_reset", "Passwords set by an admin"},
	{"reset_link_issued", "Reset links issued"},
	{"password_reset_by_link", "Passwords reset with a link"},
	{"role_changed", "Role changes"},
	{"user_created", "Users created"},
	{"user_deleted", "Users deleted by an admin"},
	{"account_deleted", "Accounts deleted by their owner"},
	{"session_ended", "Sessions ended"},
	{"signed_out_everywhere", "Signed out everywhere"},
}

// Chain is the hash-chain check of the trail (backup.CheckAuditChain, the
// Audit page's verifyChain): intact, or the problem in words.
type Chain struct {
	Intact  bool   `json:"intact"`
	Checked int    `json:"checked"`
	Legacy  int    `json:"legacy"`
	Problem string `json:"problem,omitempty"` // where and why it broke, or why it could not be checked
}

// ChainOf turns CheckAuditChain's answer into a Chain.
func ChainOf(res backup.ChainResult, err error) Chain {
	c := Chain{Intact: err == nil, Checked: res.Checked, Legacy: res.Legacy}
	if err != nil {
		c.Problem = err.Error()
	}
	return c
}

// Count is one action's lines in the week and in the week before.
type Count struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	Week   int    `json:"week"`
	Before int    `json:"week_before"`
}

// Digest is the weekly security digest: counts only, no name, IP or secret.
type Digest struct {
	Week            string     `json:"week"`
	Empty           bool       `json:"empty"` // the trail has no line at all: "No data yet"
	Chain           Chain      `json:"chain"`
	Counts          []Count    `json:"counts"`
	LockedUsernames int        `json:"locked_usernames"`  // distinct usernames locked out in the week
	SignInSources   int        `json:"sign_in_sources"`   // distinct sources (IP addresses) of the week's sign-ins
	LastAdminChange *time.Time `json:"last_admin_change"` // the last line that gave or took the admin role, any week
	Skipped         int        `json:"skipped_lines"`     // lines that are not readable audit lines
}

// Build is the digest of week w from the trail and its chain check: per
// counted action the lines in [w.From, w.To) and in the 7 days before.
func Build(t Trail, chain Chain, w evidence.Window) Digest {
	d := Digest{Week: w.Label, Empty: len(t.Entries) == 0, Chain: chain, Skipped: t.Skipped}
	before := w.From.AddDate(0, 0, -7)
	week, prior := map[string]int{}, map[string]int{}
	locked, sources := map[string]bool{}, map[string]bool{}
	for _, e := range t.Entries {
		if changesAdminRole(e) && (d.LastAdminChange == nil || e.Time.After(*d.LastAdminChange)) {
			at := e.Time
			d.LastAdminChange = &at
		}
		switch {
		case !e.Time.Before(w.From) && e.Time.Before(w.To):
			week[e.Action]++
			if e.Action == "locked" {
				locked[e.Username] = true
			}
			if e.Action == "signed_in" && e.IP != "" {
				sources[e.IP] = true
			}
		case !e.Time.Before(before) && e.Time.Before(w.From):
			prior[e.Action]++
		}
	}
	for _, c := range Counted {
		d.Counts = append(d.Counts, Count{Action: c.Action, Label: c.Label, Week: week[c.Action], Before: prior[c.Action]})
	}
	d.LockedUsernames = len(locked)
	d.SignInSources = len(sources)
	return d
}

// count is the digest's count of one action ({} when it is not counted).
func (d Digest) count(action string) Count {
	for _, c := range d.Counts {
		if c.Action == action {
			return c
		}
	}
	return Count{}
}

// chainLine is the chain's state in one sentence.
func (d Digest) chainLine() string {
	if !d.Chain.Intact {
		return fmt.Sprintf("Audit chain BROKEN: %s. The counts below come from a trail that may have been changed; check it on the Audit page.", d.Chain.Problem)
	}
	s := fmt.Sprintf("Audit chain intact — %d lines checked.", d.Chain.Checked)
	if d.Chain.Legacy > 0 {
		s += fmt.Sprintf(" %d legacy lines before the chain are not covered.", d.Chain.Legacy)
	}
	return s
}

// Headline is the digest in one line, for the weekly notification: the chain
// first, then the failed sign-ins, lockouts and role changes against the week before.
func (d Digest) Headline() string {
	if d.Empty && d.Chain.Intact {
		return fmt.Sprintf("Security %s: no data yet, the audit trail has no line.", d.Week)
	}
	chain := "chain intact"
	if !d.Chain.Intact {
		chain = "AUDIT CHAIN BROKEN"
	}
	f, l, r := d.count("sign_in_failed"), d.count("locked"), d.count("role_changed")
	return fmt.Sprintf("Security %s: %s; %d failed sign-ins (%d the week before), %d lockouts (%d), %d role changes (%d).",
		d.Week, chain, f.Week, f.Before, l.Week, l.Before, r.Week, r.Before)
}

// Markdown is the digest as the weekly usage report's "Security" section.
// A broken chain opens it, before any count.
func (d Digest) Markdown() string {
	var b strings.Builder
	b.WriteString("## Security\n\n")
	if !d.Chain.Intact {
		b.WriteString("**" + d.chainLine() + "**\n\n")
	}
	if d.Empty {
		b.WriteString("No data yet: the audit trail has no line.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "From the monitor's audit trail, %s (UTC), against the week before. Counts only: no name, address or secret.\n\n", d.Week)
	if d.Chain.Intact {
		b.WriteString(d.chainLine() + "\n\n")
	}
	b.WriteString("| What | This week | Week before |\n| --- | ---: | ---: |\n")
	for _, c := range d.Counts {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", c.Label, c.Week, c.Before)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Usernames locked out this week: %d\n", d.LockedUsernames)
	fmt.Fprintf(&b, "- Distinct sources signing in this week: %d\n", d.SignInSources)
	last := "none recorded"
	if d.LastAdminChange != nil {
		last = d.LastAdminChange.Format("2006-01-02")
	}
	fmt.Fprintf(&b, "- Last change of the admin role: %s\n", last)
	if d.Skipped > 0 {
		fmt.Fprintf(&b, "- Unreadable lines left out: %d\n", d.Skipped)
	}
	return b.String()
}
