package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// AuditFile is the monitor's audit trail (#619); its rotated archives are
// audit.<UTC time>.jsonl beside it (frontend/lib/auth/audit.js archiveName).
const AuditFile = "audit.jsonl"

var (
	auditArchive = regexp.MustCompile(`^audit\.\d{8}T\d{9}Z\.jsonl$`)
	auditSeal    = regexp.MustCompile(`,"line_hash":"([0-9a-f]{64})"\}$`)
)

// ChainResult is the hash-chain check of a copied audit trail.
type ChainResult struct {
	Checked int // chained lines whose link and seal hold
	Legacy  int // lines before the first chained one (written before #619): counted, not checked
	Skipped int // a torn last line of audit.jsonl (the copy raced an append)
}

// CheckAuditChain checks the audit trail copied into dir — the archives
// oldest first, then audit.jsonl — the way the Audit page's "chain intact"
// check does (frontend/lib/auth/auditchain.js verifyChain, #619): from the
// first line with a "prev" field, every line must be a JSON object whose prev
// is the SHA-256 of the previous line's exact bytes ("" for the first line of
// the trail) and whose line_hash is the SHA-256 of the line up to it. Blank
// lines are ignored; a torn last line of audit.jsonl is skipped, as in
// CheckLines. No trail at all checks nothing.
func CheckAuditChain(dir string) (ChainResult, error) {
	var res ChainResult
	files, err := AuditTrailFiles(dir)
	if err != nil {
		return res, err
	}
	var prev []byte // the previous line's bytes; nil before the first line
	started := false
	for i, path := range files {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return res, fmt.Errorf("read %s: %w", path, err)
		}
		torn := i == len(files)-1 && len(b) > 0 && b[len(b)-1] != '\n'
		lines := bytes.Split(b, []byte{'\n'})
		for n, line := range lines {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var entry map[string]json.RawMessage
			isObject := json.Unmarshal(line, &entry) == nil && entry != nil
			_, chained := entry["prev"]
			if !started && !chained {
				if !isObject && torn && n == len(lines)-1 {
					res.Skipped++
					continue
				}
				res.Legacy++
				prev = line
				continue
			}
			started = true
			reason := chainBreak(line, isObject, chained, entry["prev"], prev)
			if reason == "damaged" && torn && n == len(lines)-1 {
				res.Skipped++
				continue
			}
			if reason != "" {
				return res, fmt.Errorf("audit chain broken in %s line %d: %s", filepath.Base(path), n+1, reason)
			}
			res.Checked++
			prev = line
		}
	}
	return res, nil
}

// chainBreak is why one line breaks the chain, "" when it holds — the reasons
// of verifyChain: damaged (not a JSON object), unchained (no prev after the
// chain started), link (prev is not the previous line's hash), edited (the
// line's own line_hash does not match it).
func chainBreak(line []byte, isObject, chained bool, rawPrev json.RawMessage, prevLine []byte) string {
	if !isObject {
		return "damaged"
	}
	if !chained {
		return "unchained"
	}
	expected := ""
	if prevLine != nil {
		expected = sha256Hex(prevLine)
	}
	var got string
	if json.Unmarshal(rawPrev, &got) != nil || got != expected {
		return "link"
	}
	m := auditSeal.FindSubmatchIndex(line)
	if m == nil {
		return "edited"
	}
	body := append(append([]byte{}, line[:m[0]]...), '}')
	if sha256Hex(body) != string(line[m[2]:m[3]]) {
		return "edited"
	}
	return ""
}

// AuditTrailFiles lists dir's audit archives oldest first (the UTC time in the
// name sorts as text), then audit.jsonl (which may not exist); a missing dir is none.
func AuditTrailFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && auditArchive.MatchString(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return append(files, filepath.Join(dir, AuditFile)), nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
