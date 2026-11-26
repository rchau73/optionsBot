// Command security writes the admins' weekly security digest and runs the
// same-day alert check (#717, internal/security) from one environment's audit
// trail (data/monitor/audit.jsonl and its archives). Counts only: never a
// password, token, reset link, IP address or a person's name. Read-only on the
// trail; it writes only the digest files and the alert IDs already raised.
//
//	go run ./cmd/security -audit-dir data/monitor -week 2026-W45 -out data/agents/security/2026-W45/mvp
//	  → <out>/digest.md (the usage report's "Security" section) and digest.json; a `HEADLINE <text>` line
//	go run ./cmd/security -audit-dir data/monitor -alerts -sent data/agents/security/mvp.alerts
//	  → one `ALERT <text>` line per alert not raised before; their IDs are appended to -sent
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"optionsbot/internal/backup"
	"optionsbot/internal/evidence"
	"optionsbot/internal/security"
)

func main() {
	dir := flag.String("audit-dir", "data/monitor", "the folder holding audit.jsonl and its archives")
	week := flag.String("week", "", "ISO week of the digest, e.g. 2026-W45 (default: the last complete week)")
	out := flag.String("out", "", "digest: folder for digest.md and digest.json")
	alerts := flag.Bool("alerts", false, "instead: run the alert check and print the alerts not raised before")
	sent := flag.String("sent", "", "alerts: the file of alert IDs already raised (one per line, appended to)")
	flag.Parse()

	var err error
	if *alerts {
		err = runAlerts(*dir, *sent, time.Now().UTC())
	} else {
		err = runDigest(*dir, *week, *out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "security:", err)
		os.Exit(1)
	}
}

func runDigest(dir, week, out string) error {
	if out == "" {
		return errors.New("-out is required for the digest")
	}
	w := evidence.LastWeek(time.Now())
	if week != "" {
		var err error
		if w, err = evidence.ParseWeek(week); err != nil {
			return err
		}
	}
	t, err := security.ReadTrail(dir)
	if err != nil {
		return err
	}
	d := security.Build(t, security.ChainOf(backup.CheckAuditChain(dir)), w)
	if err := os.MkdirAll(out, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", out, err)
	}
	js, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("encode digest: %w", err)
	}
	for name, body := range map[string][]byte{"digest.md": []byte(d.Markdown()), "digest.json": append(js, '\n')} {
		if err := os.WriteFile(filepath.Join(out, name), body, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	fmt.Println("HEADLINE", d.Headline())
	return nil
}

func runAlerts(dir, sentPath string, now time.Time) error {
	if sentPath == "" {
		return errors.New("-sent is required for the alert check")
	}
	t, err := security.ReadTrail(dir)
	if err != nil {
		return err
	}
	fresh, err := security.Raise(security.Alerts(t.Entries, now), sentPath)
	if err != nil {
		return err
	}
	for _, a := range fresh {
		fmt.Println("ALERT", a.Text)
	}
	return nil
}
