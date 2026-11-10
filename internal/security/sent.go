package security

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Raise keeps the alerts whose ID is not yet in the file at sentPath (one ID
// per line; a missing file is none) and appends their IDs to it (0600), so the
// same pattern never alerts twice. The IDs are written before the alerts are
// returned: an alert is raised at most once, even when sending it fails.
func Raise(alerts []Alert, sentPath string) ([]Alert, error) {
	sent, err := readSent(sentPath)
	if err != nil {
		return nil, err
	}
	fresh := New(alerts, sent)
	if len(fresh) == 0 {
		return nil, nil
	}
	f, err := os.OpenFile(sentPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", sentPath, err)
	}
	var b strings.Builder
	for _, a := range fresh {
		b.WriteString(a.ID + "\n")
	}
	_, werr := f.WriteString(b.String())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, fmt.Errorf("write %s: %w", sentPath, werr)
	}
	return fresh, nil
}

// readSent reads the alert IDs already raised; a missing file is none.
func readSent(path string) (map[string]bool, error) {
	sent := map[string]bool{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return sent, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if id := strings.TrimSpace(sc.Text()); id != "" {
			sent[id] = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return sent, nil
}
