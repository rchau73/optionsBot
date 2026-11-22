package compare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"optionsbot/internal/config"
)

// A ready lab candidate as a draft pull request (#862, epic E9, cmd/labpr):
// the candidate cmd/labapply checked (COMPARE_DIR/candidates/<run id>/)
// becomes branch lab/<run id> from origin/main with only the recorded values
// changed — in the shipped config and its twin — and one draft pull request
// carrying the backtested numbers and the risks. The tool never merges,
// never pushes to main, restarts no bot and sends no order: a person's merge
// is the approval and deploy.sh the deploy
// (docs/product/decisions/2026-11-20-lab-change-reaches-bot-by-pr.md).

// LabPRBranchPrefix is the branch namespace of lab pull requests.
const LabPRBranchPrefix = "lab/"

// ConfigTwins are the shipped configs kept in step (#168): they differ only
// in TwinOnlyKey, so a change to any other key goes to both.
var ConfigTwins = map[string]string{"config_btc.yaml": "config_btc.mvp.yaml", "config_btc.mvp.yaml": "config_btc.yaml"}

// TwinOnlyKey is the one key the twins hold differently.
const TwinOnlyKey = "gex_direction"

// LabPRMergeLine says who approves (every PR body carries it).
const LabPRMergeLine = "Merging this pull request is the approval: nothing reaches a running bot before a person merges it, " +
	"and then only through the usual deploy (deploy.sh → mvp; stable by scripts/promote.sh). " +
	"cmd/labpr merged nothing, pushed nothing to main, restarted no bot and sent no order."

// LabPRRefusal is a candidate labpr does not open a pull request for; the
// reasons say why (exit 2 in cmd/labpr).
type LabPRRefusal struct{ Reasons []string }

func (e *LabPRRefusal) Error() string { return "refused: " + strings.Join(e.Reasons, "; ") }

func refuseLabPR(format string, a ...any) error {
	return &LabPRRefusal{Reasons: []string{fmt.Sprintf(format, a...)}}
}

// LabPRFile is one config file the branch changes.
type LabPRFile struct {
	Path     string // relative to the repository
	Old, New []byte
}

// LabPRPlan is what the branch and the pull request hold.
type LabPRPlan struct {
	RunID, Branch, Title, Body string
	Files                      []LabPRFile
}

// PlanLabPR reads the candidate of run id under compareDir and the shipped
// configs under configDir and returns the branch's files and the PR text.
// Anything but a ready candidate that still matches the shipped config is a
// *LabPRRefusal; it reads files only.
func PlanLabPR(compareDir, configDir, id string) (LabPRPlan, error) {
	if !runIDRe.MatchString(id) {
		return LabPRPlan{}, refuseLabPR("run id %q: want a folder time like 20261012T093000Z", id)
	}
	dir := filepath.Join(compareDir, "candidates", id)
	read := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			return nil, refuseLabPR("no %s for run %s: run cmd/labapply -id %s first", name, id, id)
		}
		return b, err
	}
	raw, err := read(CandidateStatusFile)
	if err != nil {
		return LabPRPlan{}, err
	}
	var st CandidateStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return LabPRPlan{}, refuseLabPR("%s cannot be read: %v", CandidateStatusFile, err)
	}
	if st.RunID != id {
		return LabPRPlan{}, refuseLabPR("%s is for run %q, not %q", CandidateStatusFile, st.RunID, id)
	}
	if st.Status != CandidateReady {
		reasons := append([]string{fmt.Sprintf("the candidate is %q, not %q", st.Status, CandidateReady)}, st.Reasons...)
		return LabPRPlan{}, &LabPRRefusal{Reasons: reasons}
	}
	diffMD, err := read(CandidateDiffFile)
	if err != nil {
		return LabPRPlan{}, err
	}
	candidate, err := read(CandidateConfigFile)
	if err != nil {
		return LabPRPlan{}, err
	}
	if !slices.Contains(CandidateConfigs, st.Config) {
		return LabPRPlan{}, refuseLabPR("config %q is not a shipped config (%s)", st.Config, strings.Join(CandidateConfigs, ", "))
	}
	if len(st.Changes) == 0 {
		return LabPRPlan{}, refuseLabPR("the candidate changes no setting")
	}
	var sets []Setting
	var reasons []string
	for _, c := range st.Changes {
		// The guardrail (#862): no lab PR touches a core policy without the
		// product owner's approval reference — checked again here, because
		// candidate.json is a file anyone with the folder can edit.
		t, ok := TunableFor(c.Key)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("%s: not a setting of the catalogue (config.Tunables)", c.Key))
			continue
		}
		if (t.Policy != config.PolicyNone || c.CorePolicy) && strings.TrimSpace(st.ProductOwnerApproval) == "" {
			reasons = append(reasons, fmt.Sprintf("%s: %s: a core policy (%s) without the product owner's approval reference", c.Key, NeedsProductOwner, t.Policy))
			continue
		}
		v, err := recordValue(c.New)
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("%s: %v", c.Key, err))
			continue
		}
		sets = append(sets, Setting{Key: c.Key, Value: v})
	}
	if len(reasons) > 0 {
		return LabPRPlan{}, &LabPRRefusal{Reasons: reasons}
	}

	named, err := labPRFile(configDir, st.Config, sets, nil)
	if err != nil {
		return LabPRPlan{}, err
	}
	if !bytes.Equal(named.New, candidate) {
		return LabPRPlan{}, refuseLabPR("%s changed since the candidate was built: run cmd/labapply -id %s again", st.Config, id)
	}
	files := []LabPRFile{named}
	if twin, ok := ConfigTwins[st.Config]; ok {
		var twinSets []Setting
		old := map[string]string{}
		for i, s := range sets {
			if s.Key != TwinOnlyKey {
				twinSets = append(twinSets, s)
				old[s.Key] = st.Changes[i].Old
			}
		}
		if len(twinSets) > 0 {
			f, err := labPRFile(configDir, twin, twinSets, old)
			if err != nil {
				return LabPRPlan{}, err
			}
			files = append(files, f)
		}
	}

	report, err := readRunReport(compareDir, id)
	if err != nil {
		return LabPRPlan{}, refuseLabPR("the run's report cannot be read, so its numbers cannot be shown: %v", err)
	}
	keys := make([]string, len(st.Changes))
	for i, c := range st.Changes {
		keys[i] = c.Key
	}
	plan := LabPRPlan{
		RunID:  id,
		Branch: LabPRBranchPrefix + id,
		Title:  fmt.Sprintf("Lab change %s: %s (%s)", id, strings.Join(keys, ", "), st.Config),
		Body:   LabPRBody(st, settingsSection(string(diffMD)), report, files),
		Files:  files,
	}
	if r := LabPRPrivacyReasons(plan.Title+"\n"+plan.Body, st.Person); len(r) > 0 {
		return LabPRPlan{}, &LabPRRefusal{Reasons: r}
	}
	return plan, nil
}

// labPRFile is one shipped config with sets applied in place (EditConfigText,
// labapply's own edit), checked through config.Load to differ only there.
// old, when given, is each key's value the run started from, which the file
// must still hold (the twin: the candidate checked only its own file).
func labPRFile(configDir, name string, sets []Setting, old map[string]string) (LabPRFile, error) {
	path := filepath.Join(configDir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		return LabPRFile{}, refuseLabPR("%s cannot be read: %v", name, err)
	}
	shipped, err := config.Load(path)
	if err != nil {
		return LabPRFile{}, refuseLabPR("%s does not load: %v", name, err)
	}
	current := settings(shipped)
	for key, want := range old {
		if have, ok := current[key]; !ok || !sameValue(have, want) {
			return LabPRFile{}, refuseLabPR("%s: %s holds %s, the run started from %s: the twins must be put in step by hand first", key, name, have, want)
		}
	}
	edited, err := EditConfigText(raw, sets)
	if err != nil {
		return LabPRFile{}, refuseLabPR("%s: %v", name, err)
	}
	if err := loadCandidate(edited, shipped, sets); err != nil {
		return LabPRFile{}, refuseLabPR("%s: %v", name, err)
	}
	return LabPRFile{Path: name, Old: raw, New: edited}, nil
}

// settingsSection is diff.md's "## Settings" section (the table of old and
// new values) without its heading; the rest of diff.md names the person.
func settingsSection(md string) string {
	_, rest, ok := strings.Cut(md, "## Settings\n")
	if !ok {
		return ""
	}
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// LabPRBody is the pull request's text: the run, the settings with both
// values, the headline numbers in percentages under LabLabel, the fragility
// flags, the risks, the requester as ManagerHash and LabPRMergeLine. No
// amount, account detail or name.
func LabPRBody(st CandidateStatus, settingsTable string, r *Report, files []LabPRFile) string {
	var b strings.Builder
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = "`" + f.Path + "`"
	}
	fmt.Fprintf(&b, "Lab change from run `%s` (%s): only the recorded values change, in %s.\n\n", st.RunID, st.Underlying, strings.Join(paths, " and "))
	fmt.Fprintf(&b, "Confirmed in the lab by manager `%s` (a hash, as in cmd/labstats) at %s; checked by cmd/labapply (ready).\n\n", ManagerHash(st.Person), st.RecordedAt)
	b.WriteString("## Settings\n\n")
	b.WriteString(settingsTable)
	fmt.Fprintf(&b, "\n\n## Headline numbers: %s\n\n", LabLabel)
	for _, line := range labPRHeadline(r) {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString("\n## Fragility\n\n")
	switch n := r.Neighbours; {
	case n == nil:
		b.WriteString("- no fragility check was run for this setting\n")
	case len(n.Cliffs) == 0:
		b.WriteString("- no fragile spot near the proposed value (±10/20 %)\n")
	default:
		for _, c := range n.Cliffs {
			fmt.Fprintf(&b, "- fragile: %s\n", c.Detail)
		}
	}
	b.WriteString("\n## Risks\n\n")
	for _, risk := range st.Risks {
		fmt.Fprintf(&b, "- %s\n", risk)
	}
	fmt.Fprintf(&b, "\n## Approval\n\n%s\n\n", LabPRMergeLine)
	b.WriteString("_Opened by cmd/labpr (#862) from the lab candidate; the generated files (effective-config goldens, parameter docs) were rewritten by scripts/regen.sh._\n")
	return b.String()
}

// labPRHeadline is each run's return, worst week and largest drawdown,
// base → proposed, in percentages only.
func labPRHeadline(r *Report) []string {
	var out []string
	if bt := r.Backtest; bt.Ran && bt.Base.Error == "" && bt.Proposed.Error == "" {
		out = append(out, fmt.Sprintf("backtest: return %.2f %% → %.2f %%, worst week %.2f %% → %.2f %%, largest drawdown %.2f %% → %.2f %%",
			bt.Base.ReturnPct, bt.Proposed.ReturnPct, bt.Base.WorstWeekPct, bt.Proposed.WorstWeekPct, bt.Base.MaxDrawdownPct, bt.Proposed.MaxDrawdownPct))
	} else {
		out = append(out, "backtest: did not run")
	}
	if p := r.Replay; p.Ran && p.Base.Error == "" && p.Proposed.Error == "" && p.Equity > 0 {
		out = append(out, fmt.Sprintf("replay of recorded books: return %.2f %% → %.2f %%, worst week %.2f %% → %.2f %%, largest drawdown %.2f %% → %.2f %%",
			p.Base.ReturnPct, p.Proposed.ReturnPct, p.Base.WorstWeekPct, p.Proposed.WorstWeekPct,
			p.Base.MaxDrawdown/p.Equity*100, p.Proposed.MaxDrawdown/p.Equity*100))
	} else {
		out = append(out, "replay of recorded books: did not run")
	}
	return out
}

// amountRe is a currency amount: what a public PR body never holds.
var amountRe = regexp.MustCompile(`(?i)\$\s?\d|\b\d[\d,.]*\s?(usd|usdc|btc|eth)\b|\b(usd|usdc)\s?\d`)

// LabPRPrivacyReasons are the reasons text may not go to the public
// repository: the person's name as a word, or an amount.
func LabPRPrivacyReasons(text, person string) []string {
	var out []string
	if p := strings.TrimSpace(person); p != "" {
		if regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(p) + `($|[^A-Za-z0-9_])`).MatchString(text) {
			out = append(out, "the text names the person who confirmed the change: only their hash may appear")
		}
	}
	if amountRe.MatchString(text) {
		out = append(out, "the text holds an amount: percentages only")
	}
	return out
}

// LineDiff is each changed line of the files, old and new (EditConfigText
// changes lines in place and appends missing top-level keys).
func LineDiff(files []LabPRFile) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", f.Path, f.Path)
		ol, nl := strings.Split(string(f.Old), "\n"), strings.Split(string(f.New), "\n")
		for i := range max(len(ol), len(nl)) {
			var o, n string
			if i < len(ol) {
				o = ol[i]
			}
			if i < len(nl) {
				n = nl[i]
			}
			if o == n {
				continue
			}
			fmt.Fprintf(&b, "@@ line %d @@\n", i+1)
			if i < len(ol) && o != "" {
				fmt.Fprintf(&b, "-%s\n", o)
			}
			if i < len(nl) && n != "" {
				fmt.Fprintf(&b, "+%s\n", n)
			}
		}
	}
	return b.String()
}

// Runner runs one command in dir and returns its standard output (cmd/labpr:
// os/exec; the tests: a fake, no network).
type Runner interface {
	Run(dir, name string, args ...string) (string, error)
}

// LabPROptions are where RunLabPR reads and works.
type LabPROptions struct {
	ID         string
	CompareDir string // COMPARE_DIR: candidates/<id>/ and <id>/report.json
	RepoDir    string // the repository clone (its origin is GitHub)
	WorkDir    string // an empty temporary folder: the branch's worktree and the body file
	DryRun     bool   // print the branch, the diff and the body; touch nothing
}

// LabPROutcome is what RunLabPR did.
type LabPROutcome struct {
	Branch string
	URL    string   // the pull request
	State  string   // "opened", "open" (already open), "merged" (nothing to do), "dry-run"
	Files  []string // the files committed
}

// pullRequest is one line of gh pr list --json number,url,state.
type pullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

// RunLabPR opens the draft pull request of a ready candidate, or reports the
// one already there (one per run id). Its git and gh commands go through r:
// a worktree of origin/main on branch lab/<id>, the configs written, the
// generated files rewritten (scripts/regen.sh), one commit, a push of that
// branch only and gh pr create --draft. It never merges.
func RunLabPR(o LabPROptions, r Runner, out io.Writer) (LabPROutcome, error) {
	// Checked first on the clone, so a refused candidate starts no git command.
	plan, err := PlanLabPR(o.CompareDir, o.RepoDir, o.ID)
	if err != nil {
		return LabPROutcome{}, err
	}
	res := LabPROutcome{Branch: plan.Branch}
	if o.DryRun {
		fmt.Fprintf(out, "branch: %s (dry run: no branch, commit or pull request)\n\n%s\n# %s\n\n%s", plan.Branch, LineDiff(plan.Files), plan.Title, plan.Body)
		res.State = "dry-run"
		return res, nil
	}

	listed, err := r.Run(o.RepoDir, "gh", "pr", "list", "--head", plan.Branch, "--state", "all", "--json", "number,url,state")
	if err != nil {
		return res, fmt.Errorf("list the pull requests of %s: %w", plan.Branch, err)
	}
	var prs []pullRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(listed)), &prs); err != nil {
		return res, fmt.Errorf("read gh pr list: %w", err)
	}
	closed := ""
	for _, pr := range prs {
		switch strings.ToUpper(pr.State) {
		case "OPEN":
			res.URL, res.State = pr.URL, "open"
			fmt.Fprintf(out, "already open: %s (%s); nothing changed\n", pr.URL, plan.Branch)
			return res, nil
		case "MERGED":
			res.URL, res.State = pr.URL, "merged"
			fmt.Fprintf(out, "already merged: %s; nothing to do\n", pr.URL)
			return res, nil
		default:
			closed = pr.URL
		}
	}
	if closed != "" {
		return res, refuseLabPR("%s was closed without merging: a person declined it, so no new pull request is opened for run %s", closed, o.ID)
	}

	wt := filepath.Join(o.WorkDir, "worktree")
	if _, err := r.Run(o.RepoDir, "git", "fetch", "origin", "main"); err != nil {
		return res, fmt.Errorf("fetch origin main: %w", err)
	}
	if _, err := r.Run(o.RepoDir, "git", "worktree", "add", "--force", "-B", plan.Branch, wt, "origin/main"); err != nil {
		return res, fmt.Errorf("worktree for %s: %w", plan.Branch, err)
	}
	defer r.Run(o.RepoDir, "git", "worktree", "remove", "--force", wt) // best effort: the branch is pushed by then

	// Planned again on origin/main: the clone may be behind it.
	plan, err = PlanLabPR(o.CompareDir, wt, o.ID)
	if err != nil {
		return res, err
	}
	allowed := map[string]bool{}
	for _, f := range plan.Files {
		if err := os.WriteFile(filepath.Join(wt, f.Path), f.New, 0o644); err != nil {
			return res, err
		}
		allowed[f.Path] = true
	}
	if _, err := r.Run(wt, "bash", "scripts/regen.sh"); err != nil {
		return res, fmt.Errorf("regenerate the generated files: %w", err)
	}
	status, err := r.Run(wt, "git", "status", "--porcelain")
	if err != nil {
		return res, err
	}
	for _, line := range strings.Split(strings.TrimRight(status, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if !allowed[path] {
			attr, err := r.Run(wt, "git", "check-attr", "linguist-generated", "--", path)
			if err != nil || !(strings.HasSuffix(strings.TrimSpace(attr), ": set") || strings.HasSuffix(strings.TrimSpace(attr), ": true")) {
				return res, fmt.Errorf("%s changed and is neither a config of the candidate nor a generated file: nothing committed", path)
			}
		}
		res.Files = append(res.Files, path)
	}
	if _, err := r.Run(wt, "git", "add", "-A"); err != nil {
		return res, err
	}
	msg := plan.Title + "\n\nFrom the lab candidate of run " + o.ID + " (cmd/labpr, #862). A person's merge is the approval."
	if _, err := r.Run(wt, "git", "commit", "-q", "-m", msg); err != nil {
		return res, fmt.Errorf("commit: %w", err)
	}
	// The lab branch only, never main: a branch a failed earlier run pushed is
	// replaced (no pull request was open for it, checked above).
	if _, err := r.Run(wt, "git", "push", "--force", "origin", "HEAD:refs/heads/"+plan.Branch); err != nil {
		return res, fmt.Errorf("push %s: %w", plan.Branch, err)
	}
	bodyFile := filepath.Join(o.WorkDir, "body.md")
	if err := os.WriteFile(bodyFile, []byte(plan.Body), 0o600); err != nil {
		return res, err
	}
	url, err := r.Run(o.RepoDir, "gh", "pr", "create", "--draft", "--base", "main", "--head", plan.Branch, "--title", plan.Title, "--body-file", bodyFile)
	if err != nil {
		return res, fmt.Errorf("open the draft pull request: %w", err)
	}
	res.URL, res.State = strings.TrimSpace(url), "opened"
	fmt.Fprintf(out, "opened draft: %s (%s, %s)\n", res.URL, plan.Branch, strings.Join(res.Files, ", "))
	return res, nil
}
