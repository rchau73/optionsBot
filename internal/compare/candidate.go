package compare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"optionsbot/internal/config"
)

// A recorded lab change as a config candidate (#806, epic E9, cmd/labapply):
// the record a manager's confirm wrote (frontend/lib/labapply.js,
// COMPARE_DIR/applied/<run id>.json) is checked again on the bot side — the
// key tunable, the value in the catalogue's range, the shipped value still
// the one the run started from, a core policy approved by the product owner,
// the whole file through config.Load — and becomes
// COMPARE_DIR/candidates/<run id>/: config.candidate.yaml (the shipped file
// with only those values changed, its comments kept), diff.md and
// candidate.json (ready | refused, with the reasons). Nothing here writes a
// shipped config, edits the record, calls a bot or the exchange: a person
// takes a ready candidate into a deploy through the usual PR
// (docs/operations.md "Lab candidates").

// Candidate statuses.
const (
	CandidateReady   = "ready"
	CandidateRefused = "refused"
)

// CandidateNotApplied is what every candidate says.
const CandidateNotApplied = "nothing was applied to a running bot: a candidate file only, " +
	"no shipped config was written and no bot was restarted; a person takes it into a deploy through a pull request"

// StaleReason is the refusal of a record whose old value is no longer the shipped one.
const StaleReason = "the shipped setting changed since the run"

// NeedsProductOwner names the refusal of a core-policy change without the
// product owner's approval reference.
const NeedsProductOwner = "needs_product_owner"

// The files of a candidate folder.
const (
	CandidateConfigFile = "config.candidate.yaml"
	CandidateDiffFile   = "diff.md"
	CandidateStatusFile = "candidate.json"
)

// CandidateConfigs are the shipped files a record may name: the lab's
// (config.LabShippedConfigs) and mvp's BTC.
var CandidateConfigs = []string{"config_btc.yaml", "config_btc.mvp.yaml", "config_eth.yaml"}

var runIDRe = regexp.MustCompile(`^\d{8}T\d{6}Z$`)

// AppliedRecord is a confirmed lab change (frontend/lib/labapply.js
// buildRecord). ProductOwnerApproval is not written by the monitor: a person
// adds the reference of the product owner's approval (an issue, a decision
// file) to let a core-policy change through.
type AppliedRecord struct {
	RunID      string `json:"run_id"`
	RecordedAt string `json:"recorded_at"`
	Person     string `json:"person"`
	Role       string `json:"role"`
	Account    struct {
		Underlying string `json:"underlying"`
		Config     string `json:"config"`
	} `json:"account"`
	Changes []struct {
		Key   string `json:"key"`
		Title string `json:"title"`
		Old   any    `json:"old"`
		New   any    `json:"new"`
	} `json:"changes"`
	Applied              bool   `json:"applied"`
	Recorded             bool   `json:"recorded"`
	ProductOwnerApproval string `json:"product_owner_approval,omitempty"`
}

// CandidateChange is one setting of a candidate.
type CandidateChange struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Old        string `json:"old"`
	New        string `json:"new"`
	CorePolicy bool   `json:"core_policy"`
	Policy     string `json:"policy"`
}

// CandidateStatus is candidate.json.
type CandidateStatus struct {
	RunID                string            `json:"run_id"`
	Status               string            `json:"status"`
	Reasons              []string          `json:"reasons"`
	NeedsProductOwner    bool              `json:"needs_product_owner"`
	ProductOwnerApproval string            `json:"product_owner_approval,omitempty"`
	Underlying           string            `json:"underlying"`
	Config               string            `json:"config"`
	Candidate            string            `json:"candidate,omitempty"` // the candidate file, when ready
	Person               string            `json:"person"`
	Role                 string            `json:"role"`
	RecordedAt           string            `json:"recorded_at"`
	Changes              []CandidateChange `json:"changes"`
	Label                string            `json:"label"`
	Risks                []string          `json:"risks"`
	Applied              bool              `json:"applied"`
	NothingApplied       string            `json:"nothing_applied"`
}

// LabLabel is the label every lab result carries (frontend/lib/lab.js LAB_LABEL).
const LabLabel = "backtested, not a forecast"

// CandidateOptions are where BuildCandidate reads and writes.
type CandidateOptions struct {
	CompareDir string // COMPARE_DIR: applied/<id>.json and <id>/report.json in, candidates/<id>/ out
	ConfigDir  string // the folder of the shipped config files (the repository)
}

// BuildCandidate checks the record of run id and writes its candidate
// folder. The status says ready or refused; an error is a record that cannot
// be read or a file that cannot be written. A second run on the same record
// and configs rewrites nothing.
func BuildCandidate(o CandidateOptions, id string) (CandidateStatus, error) {
	if !runIDRe.MatchString(id) {
		return CandidateStatus{}, fmt.Errorf("run id %q: want a folder time like 20261012T093000Z", id)
	}
	raw, err := os.ReadFile(filepath.Join(o.CompareDir, "applied", id+".json"))
	if err != nil {
		return CandidateStatus{}, fmt.Errorf("read the record: %w", err)
	}
	st, candidate := checkRecord(o, id, raw)
	dir := filepath.Join(o.CompareDir, "candidates", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return st, err
	}
	cfgPath := filepath.Join(dir, CandidateConfigFile)
	if st.Status == CandidateReady {
		st.Candidate = CandidateConfigFile
		if err := writeIfChanged(cfgPath, candidate); err != nil {
			return st, err
		}
	} else if err := os.Remove(cfgPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return st, err // a refused record never sits beside an older candidate
	}
	body, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	if err := writeIfChanged(filepath.Join(dir, CandidateDiffFile), []byte(CandidateMarkdown(st))); err != nil {
		return st, err
	}
	return st, writeIfChanged(filepath.Join(dir, CandidateStatusFile), append(body, '\n'))
}

// checkRecord is every check, in order; the candidate bytes are nil unless ready.
func checkRecord(o CandidateOptions, id string, raw []byte) (CandidateStatus, []byte) {
	st := CandidateStatus{RunID: id, Status: CandidateRefused, Reasons: []string{}, Changes: []CandidateChange{},
		Label: LabLabel, Risks: []string{}, NothingApplied: CandidateNotApplied}
	refuse := func(format string, a ...any) (CandidateStatus, []byte) {
		st.Reasons = append(st.Reasons, fmt.Sprintf(format, a...))
		return st, nil
	}
	var rec AppliedRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return refuse("the record cannot be read: %v", err)
	}
	st.Underlying, st.Config, st.Person, st.Role, st.RecordedAt = rec.Account.Underlying, rec.Account.Config, rec.Person, rec.Role, rec.RecordedAt
	st.ProductOwnerApproval = strings.TrimSpace(rec.ProductOwnerApproval)
	switch {
	case rec.RunID != id:
		return refuse("the record is for run %q, not %q", rec.RunID, id)
	case !rec.Recorded || rec.Applied:
		return refuse("the record must say recorded: true and applied: false")
	case len(rec.Changes) == 0:
		return refuse("the record changes no setting")
	case !slices.Contains(CandidateConfigs, rec.Account.Config):
		return refuse("config %q is not a shipped config (%s)", rec.Account.Config, strings.Join(CandidateConfigs, ", "))
	}
	shippedPath := filepath.Join(o.ConfigDir, rec.Account.Config)
	shippedRaw, err := os.ReadFile(shippedPath)
	if err != nil {
		return refuse("the shipped config cannot be read: %v", err)
	}
	shipped, err := config.Load(shippedPath)
	if err != nil {
		return refuse("the shipped config does not load: %v", err)
	}
	if !strings.EqualFold(shipped.Underlying, rec.Account.Underlying) {
		return refuse("%s trades %s, the record is for %s", rec.Account.Config, shipped.Underlying, rec.Account.Underlying)
	}
	if report, err := readRunReport(o.CompareDir, id); err != nil {
		st.Reasons = append(st.Reasons, fmt.Sprintf("the run's report cannot be read, so its numbers cannot be shown: %v", err))
	} else {
		st.Risks = CandidateRisks(report)
	}

	current := settings(shipped)
	fixed := config.FixedKeys()
	var sets []Setting
	for _, c := range rec.Changes {
		value, err := recordValue(c.New)
		ch := CandidateChange{Key: c.Key, Title: c.Title, Old: valueText(c.Old), New: valueText(value)}
		if err != nil {
			st.Changes = append(st.Changes, ch)
			st.Reasons = append(st.Reasons, fmt.Sprintf("%s: %v", c.Key, err))
			continue
		}
		top, _, _ := strings.Cut(listIndexRe.ReplaceAllString(c.Key, ""), ".")
		if !SettingKeyRe.MatchString(c.Key) || strings.Contains(c.Key, "[]") {
			st.Changes = append(st.Changes, ch)
			st.Reasons = append(st.Reasons, fmt.Sprintf("%s: not a key of one setting (a list element needs its index)", c.Key))
			continue
		}
		switch {
		case slices.Contains(fixed, top):
			// config.Load names it below, with its value in code.
		default:
			st.Reasons = append(st.Reasons, settingReasons(&ch, &st, c.Key, value, c.Old, current)...)
		}
		st.Changes = append(st.Changes, ch)
		sets = append(sets, Setting{Key: c.Key, Value: value})
	}
	if len(sets) < len(rec.Changes) {
		return st, nil
	}
	candidate, err := EditConfigText(shippedRaw, sets)
	if err != nil {
		return refuse("%v", err)
	}
	if err := loadCandidate(candidate, shipped, sets); err != nil {
		return refuse("%v", err)
	}
	if len(st.Reasons) > 0 {
		return st, nil
	}
	st.Status = CandidateReady
	return st, candidate
}

// settingReasons are the catalogue's and the record's refusals of one tuned
// setting; it fills ch's title and policy.
func settingReasons(ch *CandidateChange, st *CandidateStatus, key string, value, old any, current map[string]string) []string {
	t, ok := TunableFor(key)
	if !ok {
		return []string{fmt.Sprintf("%s: not a setting of the catalogue (config.Tunables)", key)}
	}
	ch.Title, ch.Policy, ch.CorePolicy = t.Title, t.Policy, t.Policy != config.PolicyNone
	var out []string
	for _, r := range RangeReasons(t, value) {
		out = append(out, fmt.Sprintf("%s: %v %s", key, valueText(value), r))
	}
	if shippedText, ok := current[key]; !ok || !sameValue(shippedText, old) {
		now := "nothing"
		if ok {
			now = shippedText
		}
		out = append(out, fmt.Sprintf("%s: %s (the run started from %s, the shipped config now has %s)", key, StaleReason, valueText(old), now))
	}
	if ch.CorePolicy && st.ProductOwnerApproval == "" {
		st.NeedsProductOwner = true
		out = append(out, fmt.Sprintf("%s: %s: a core policy (%s), so a new default needs the product owner's approval reference in the record (product_owner_approval), not a manager's confirm",
			key, NeedsProductOwner, t.Policy))
	}
	return out
}

// loadCandidate runs the candidate through config.Load (fixed keys,
// Validate) and checks it differs from the shipped config only in the
// recorded keys, at the recorded values.
func loadCandidate(candidate []byte, shipped *config.Config, sets []Setting) error {
	f, err := os.CreateTemp("", "candidate-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(candidate); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cfg, err := config.Load(f.Name())
	if err != nil {
		return fmt.Errorf("the candidate does not load: %w", err)
	}
	want := map[string]string{}
	for _, s := range sets {
		want[s.Key] = fmt.Sprint(s.Value)
	}
	var other []string
	for _, d := range DiffConfigs(shipped, cfg) {
		if v, ok := want[d.Key]; !ok || !sameValue(d.Proposed, v) {
			other = append(other, d.String())
		}
		delete(want, d.Key)
	}
	for k := range want {
		other = append(other, k+" unchanged")
	}
	if len(other) > 0 {
		slices.Sort(other)
		return fmt.Errorf("the candidate does not differ from the shipped config in exactly the recorded settings: %s", strings.Join(other, "; "))
	}
	return nil
}

// recordValue is a record's new value as the setting's type: the monitor
// writes the report's text ("0.12"), read as YAML like -set.
func recordValue(v any) (any, error) {
	if s, ok := v.(string); ok {
		var out any
		if err := yaml.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("value %q cannot be read", s)
		}
		v = out
	}
	switch v.(type) {
	case nil, map[string]any, []any:
		return nil, errors.New("the new value must be one number, true/false or a word")
	}
	return v, nil
}

// valueText is a value as the report writes it.
func valueText(v any) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprint(v)
}

// sameValue reports whether a value of the shipped config (as DiffConfigs
// writes it) says the same as v: "0.1" and 0.10 are the same.
func sameValue(text string, v any) bool {
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		if s == text {
			return true
		}
		if parsed, err := recordValue(s); err == nil {
			v = parsed
		}
	}
	if f, ok := toFloat(v); ok {
		g, err := strconv.ParseFloat(text, 64)
		return err == nil && f == g
	}
	return fmt.Sprint(v) == text
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

// stepTolerance: a value off the step by less than this is on it (frontend/lib/labvalidate.js).
const stepTolerance = 1e-9

// RangeReasons are the reasons v breaks a setting's own kind, range or step
// (frontend/lib/labvalidate.js rangeReasons); the cross rules are Validate's.
func RangeReasons(t config.Tunable, v any) []string {
	switch t.Kind {
	case config.KindEnum:
		if s, ok := v.(string); ok && slices.Contains(t.Values, s) {
			return nil
		}
		return []string{"must be one of " + strings.Join(t.Values, ", ")}
	case config.KindBool:
		if _, ok := v.(bool); ok {
			return nil
		}
		return []string{"must be true or false"}
	case config.KindText:
		if _, ok := v.(string); ok {
			return nil
		}
		return []string{"must be text"}
	}
	f, ok := toFloat(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return []string{"must be a number"}
	}
	unit := ""
	if t.Unit != "" {
		unit = " " + t.Unit
	}
	g := func(x float64) string { return strconv.FormatFloat(x, 'g', 10, 64) }
	var out []string
	if t.Kind == config.KindInteger && f != math.Trunc(f) {
		out = append(out, "must be a whole number")
	}
	if t.Min != nil && f < *t.Min {
		out = append(out, "must be at least "+g(*t.Min)+unit)
	}
	if t.Max != nil && f > *t.Max {
		out = append(out, "must be at most "+g(*t.Max)+unit)
	}
	if t.Step > 0 && t.Kind != config.KindInteger {
		base := 0.0
		if t.Min != nil {
			base = *t.Min
		}
		steps := (f - base) / t.Step
		if math.Abs(steps-math.Round(steps)) > stepTolerance*math.Max(1, math.Abs(steps)) {
			out = append(out, "must be on a step of "+g(t.Step))
		}
	}
	return out
}

// EditConfigText sets each value in a config file's text in place, so the
// candidate differs from the shipped file only on those lines and keeps its
// comments and alignment. A key the file does not hold is added at the end
// when it is a top-level key; a missing nested key or list element is an
// error.
func EditConfigText(src []byte, sets []Setting) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil || len(doc.Content) == 0 {
		return nil, fmt.Errorf("the shipped config cannot be parsed: %v", err)
	}
	lines := strings.SplitAfter(string(src), "\n")
	var added []string
	for _, s := range sets {
		text, err := scalarText(s.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Key, err)
		}
		node, err := findNode(doc.Content[0], s.Key)
		if err != nil {
			return nil, err
		}
		if node == nil {
			added = append(added, s.Key+": "+text+"\n")
			continue
		}
		if str, ok := s.Value.(string); ok && node.Style != 0 {
			text = strconv.Quote(str) // a quoted word stays quoted
		}
		if err := replaceScalar(lines, node, text, s.Key); err != nil {
			return nil, err
		}
	}
	out := strings.Join(lines, "")
	if len(added) > 0 && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out + strings.Join(added, "")), nil
}

// findNode is the scalar of key in a mapping; nil when key is a top-level
// key the file does not hold.
func findNode(root *yaml.Node, key string) (*yaml.Node, error) {
	node := root
	segs := strings.Split(key, ".")
	for i, seg := range segs {
		m := segRe.FindStringSubmatch(seg)
		if m == nil || node.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s: cannot be found in the shipped config", key)
		}
		var next *yaml.Node
		for j := 0; j+1 < len(node.Content); j += 2 {
			if node.Content[j].Value == m[1] {
				next = node.Content[j+1]
			}
		}
		if next == nil {
			if len(segs) == 1 && m[2] == "" {
				return nil, nil
			}
			return nil, fmt.Errorf("%s: not in the shipped config: add it by hand in the pull request", key)
		}
		for _, ix := range listIndexRe.FindAllString(m[2], -1) {
			n, err := strconv.Atoi(strings.Trim(ix, "[]"))
			if err != nil || next.Kind != yaml.SequenceNode || n >= len(next.Content) {
				return nil, fmt.Errorf("%s: no such list element in the shipped config", key)
			}
			next = next.Content[n]
		}
		node = next
		if i == len(segs)-1 && node.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s: not one value in the shipped config", key)
		}
	}
	return node, nil
}

// replaceScalar swaps node's text on its line for text, after checking the
// file holds exactly what was parsed there.
func replaceScalar(lines []string, node *yaml.Node, text, key string) error {
	var old string
	switch node.Style {
	case 0:
		old = node.Value
	case yaml.DoubleQuotedStyle:
		old = `"` + node.Value + `"`
	case yaml.SingleQuotedStyle:
		old = "'" + node.Value + "'"
	}
	if node.Line < 1 || node.Line > len(lines) || old == "" || strings.ContainsAny(node.Value, `"'\`) {
		return fmt.Errorf("%s: cannot be edited in place in the shipped config", key)
	}
	line := []rune(lines[node.Line-1])
	at := node.Column - 1
	if at < 0 || at+len([]rune(old)) > len(line) || string(line[at:at+len([]rune(old))]) != old {
		return fmt.Errorf("%s: cannot be edited in place in the shipped config", key)
	}
	lines[node.Line-1] = string(line[:at]) + text + string(line[at+len([]rune(old)):])
	return nil
}

var plainWordRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

// scalarText writes one value as YAML: numbers and booleans plain, a word
// plain unless YAML would read it as something else, other text quoted.
func scalarText(v any) (string, error) {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", errors.New("the value must be a finite number")
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case string:
		var back any
		if plainWordRe.MatchString(x) && yaml.Unmarshal([]byte(x), &back) == nil && back == x {
			return x, nil
		}
		return strconv.Quote(x), nil
	}
	return "", fmt.Errorf("value %v is not one number, true/false or a word", v)
}

// readRunReport reads the run's report.json.
func readRunReport(compareDir, id string) (*Report, error) {
	raw, err := os.ReadFile(filepath.Join(compareDir, id, "report.json"))
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CandidateRisks are the run's risks for the proposed config, in
// percentages only (frontend/lib/labapply.js riskLines): worst week,
// largest drawdown and fees of the starting equity, the fragility check and
// "past data only".
func CandidateRisks(r *Report) []string {
	pct := func(part, whole float64) string {
		if whole <= 0 {
			return "unknown"
		}
		return fmt.Sprintf("%.2f %%", part/whole*100)
	}
	var out []string
	if b := r.Backtest; b.Ran && b.Proposed.Error == "" {
		out = append(out, fmt.Sprintf("backtest: worst week %.2f %%, largest drawdown %.2f %%, fees %s of the starting equity",
			b.Proposed.WorstWeekPct, b.Proposed.MaxDrawdownPct, pct(b.Proposed.FeesUSD, b.EquityUSD)))
	}
	if p := r.Replay; p.Ran && p.Proposed.Error == "" {
		out = append(out, fmt.Sprintf("replay of recorded books: worst week %.2f %%, largest drawdown %s, fees %s of the starting equity",
			p.Proposed.WorstWeekPct, pct(p.Proposed.MaxDrawdown, p.Equity), pct(p.Proposed.Fees, p.Equity)))
	}
	switch n := r.Neighbours; {
	case n == nil:
		out = append(out, "no fragility check was run for this setting")
	case len(n.Cliffs) > 0:
		out = append(out, fmt.Sprintf("%d fragile spot(s) near the proposed value: a small change around it moved the result sharply", len(n.Cliffs)))
	}
	return append(out, "past data only: the market can move in ways these days did not show")
}

// CandidateMarkdown renders diff.md: the status, each setting with its
// plain title and both values, the label and the risks beside them.
func CandidateMarkdown(st CandidateStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Lab candidate %s: %s\n\n", st.RunID, st.Status)
	fmt.Fprintf(&b, "%s (%s), confirmed by %s (%s) at %s.\n\nNote: %s.\n\n", st.Config, st.Underlying, st.Person, st.Role, st.RecordedAt, CandidateNotApplied)
	if len(st.Reasons) > 0 {
		b.WriteString("## Refused\n\n")
		for _, r := range st.Reasons {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Settings\n\n| Key | Setting | Old | New | Policy |\n|---|---|---|---|---|\n")
	for _, c := range st.Changes {
		policy := c.Policy
		if c.CorePolicy {
			policy += " (core policy: the product owner approves)"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", c.Key, dashed(c.Title), c.Old, c.New, dashed(policy))
	}
	if st.ProductOwnerApproval != "" {
		fmt.Fprintf(&b, "\nProduct owner's approval: %s\n", st.ProductOwnerApproval)
	}
	fmt.Fprintf(&b, "\n## The run's numbers: %s\n\n", LabLabel)
	for _, r := range st.Risks {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	return b.String()
}

func dashed(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// writeIfChanged writes body to path unless the file already holds it, so a
// second run leaves the files as they are.
func writeIfChanged(path string, body []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, body) {
		return nil
	}
	return os.WriteFile(path, body, 0o600)
}
