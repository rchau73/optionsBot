// Package delivery measures how the team delivers (#470): the four DORA
// metrics per ISO week and per environment (mvp, stable), and every merged
// pull request with the time it went live on each. It is pure: Build works on
// what the readers decoded from files that scripts/agents/dora.sh dumps (main's
// git history, tags, gh's pull requests and issues) and from deploys.tsv and
// promotions.tsv. It never calls git, GitHub or Deribit.
//
// What counts, in plain words:
//   - a PR is a "Merge pull request #N" merge on main's first-parent line;
//     its first commit is the earliest commit the merge brought in;
//   - it is live on mvp at the first healthy mvp deploy whose commit contains
//     it, and on stable at the first release (a new tag, promoted) that does;
//     main's first-parent line is linear, so "contains" is "is at or after
//     on that line";
//   - a failure is a rollback line, an "[E6] mvp deploy failed" issue, a
//     "Revert" PR or a `regression` bug naming the PR that caused it.
//
// Nothing is estimated: a week before deploys were recorded says so.
package delivery

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Environments and deploys.tsv kinds.
const (
	EnvMVP    = "mvp"
	EnvStable = "stable"

	KindDeploy   = "deploy"   // a healthy mvp deploy (no kind field)
	KindRollback = "rollback" // the environment went back to the commit named
	KindRelease  = "release"  // promote.sh: stable is on the tag, healthy
)

// Failure kinds.
const (
	FailRollback   = "rollback"
	FailDeploy     = "deploy_failed"
	FailRevert     = "revert"
	FailRegression = "regression"
)

// DeployIssueWindow is how long after a rollback line its "[E6] mvp deploy
// failed" issue may be filed and still be the same failure (deploy.sh files
// it right after the rollback and retries each tick until it is filed).
const DeployIssueWindow = 6 * time.Hour

// Commit is one commit of main's first-parent line.
type Commit struct {
	SHA     string
	Parents []string
	At      time.Time // committer time: for a merge, when it merged
	FirstAt time.Time // merges: the earliest commit it brought in (zero if unknown)
	Subject string
}

// Tag is a git tag and the commit it names.
type Tag struct{ Name, Commit string }

// DeployLine is one line of deploys.tsv.
type DeployLine struct {
	At          time.Time
	Env, Commit string
	Kind, Tag   string
}

// Promotion is one line of promotions.tsv.
type Promotion struct {
	At          time.Time
	Tag, Commit string
}

// PR is a merged pull request as gh lists it.
type PR struct {
	Number              int
	Title, Branch, Body string
	CreatedAt, MergedAt time.Time
	Labels              []string
}

// Issue is an issue as gh lists it.
type Issue struct {
	Number      int
	Title, Body string
	Labels      []string
	CreatedAt   time.Time
	ClosedAt    *time.Time
}

// Inputs is everything Build reads. From is the backfill start: weeks begin
// at its ISO week's Monday.
type Inputs struct {
	Main       []Commit // oldest first
	Tags       []Tag
	Deploys    []DeployLine
	Promotions []Promotion
	PRs        []PR
	Issues     []Issue
	From       time.Time
	Problems   []string // the readers' skipped lines
}

// PRRecord is one merged PR and when it went live.
type PRRecord struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Epic        string     `json:"epic,omitempty"`
	Branch      string     `json:"branch"`
	Commit      string     `json:"commit"`
	FirstCommit *time.Time `json:"first_commit"`
	Merged      time.Time  `json:"merged"`
	MVPLive     *time.Time `json:"mvp_live"`
	MVPCommit   string     `json:"mvp_commit,omitempty"`
	MVPNote     string     `json:"mvp_note,omitempty"` // not recorded | not live yet
	StableLive  *time.Time `json:"stable_live"`
	Release     string     `json:"release,omitempty"`
	StableNote  string     `json:"stable_note,omitempty"` // not released yet
	Reverts     int        `json:"reverts,omitempty"`     // the PR this one reverts
	Failures    []string   `json:"failures,omitempty"`    // ids of the failures it caused
}

// Release is a new tag promoted to stable.
type Release struct {
	Tag    string    `json:"tag"`
	Commit string    `json:"commit"`
	At     time.Time `json:"at"`
	Failed bool      `json:"failed"`
}

// Failure is one failed deploy or release and its restore.
type Failure struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Env          string     `json:"env"`
	At           time.Time  `json:"at"`
	RestoredAt   *time.Time `json:"restored_at"`
	RestoreHours *float64   `json:"restore_hours"`
	CausedBy     int        `json:"caused_by,omitempty"` // the PR
	FixPR        int        `json:"fix_pr,omitempty"`
	Issue        int        `json:"issue,omitempty"`
	Commit       string     `json:"commit,omitempty"`
	Detail       string     `json:"detail"`
}

// EnvWeek is one environment's four metrics in one week.
type EnvWeek struct {
	Recorded       bool     `json:"recorded"`
	Note           string   `json:"note,omitempty"`
	Deploys        int      `json:"deploys"`            // healthy deploys (mvp) or new releases (stable)
	PerDay         *float64 `json:"per_day,omitempty"`  // mvp: deploys a day
	PerWeek        *float64 `json:"per_week,omitempty"` // stable: releases a week
	FrequencyBand  string   `json:"frequency_band,omitempty"`
	MergeToLive    Stat     `json:"merge_to_live"`
	CommitToLive   Stat     `json:"commit_to_live"`
	Attempts       int      `json:"attempts"` // deploys + failed attempts
	Failed         int      `json:"failed"`
	FailureRatePct *float64 `json:"failure_rate_pct"`
	FailureBand    string   `json:"failure_band,omitempty"`
	FailureNote    string   `json:"failure_note,omitempty"`
	Restore        Stat     `json:"restore"`
	Unrestored     int      `json:"unrestored"`
}

// Week is one ISO week.
type Week struct {
	Week          string    `json:"week"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	Merged        int       `json:"merged"`
	CommitToMerge Stat      `json:"commit_to_merge"`
	MVP           EnvWeek   `json:"mvp"`
	Stable        EnvWeek   `json:"stable"`
	Summary       string    `json:"summary"` // SummaryMarkdown: what dora.sh posts on the E6 epic (#473 shows it)
}

// Deploy is one healthy mvp deploy (#473: the release train's ticks).
type Deploy struct {
	At     time.Time `json:"at"`
	Commit string    `json:"commit"`
}

// Report is dora.json.
type Report struct {
	Generated          time.Time  `json:"generated"`
	From               time.Time  `json:"from"`
	MVPRecordedFrom    *time.Time `json:"mvp_recorded_from"`
	StableRecordedFrom *time.Time `json:"stable_recorded_from"`
	Weeks              []Week     `json:"weeks"`
	PRs                []PRRecord `json:"prs"`
	Releases           []Release  `json:"releases"`
	MVPDeploys         []Deploy   `json:"mvp_deploys"`
	Failures           []Failure  `json:"failures"`
	Problems           []string   `json:"problems"`
}

var (
	mergeRe      = regexp.MustCompile(`^Merge pull request #(\d+) from (\S+)`)
	epicTitleRe  = regexp.MustCompile(`^\[(E\d+)\]`)
	epicLabelRe  = regexp.MustCompile(`^E\d+$`)
	revertRe     = regexp.MustCompile(`^Revert "(.*)"$`)
	revertBranch = regexp.MustCompile(`^revert-(\d+)-`)
	deployIssue  = regexp.MustCompile(`^\[E6\] mvp deploy failed: ([0-9a-f]{7,40})`)
	causedByRe   = regexp.MustCompile(`(?im)^\W*caused by:?\s*#(\d+)`)
)

// closesRe matches a PR body that closes issue n.
func closesRe(n int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`(?i)\b(close[sd]?|fix(e[sd])?|resolve[sd]?)\s+#%d\b`, n))
}

// liveDeploy is a healthy deploy (mvp) or a new release (stable).
type liveDeploy struct {
	At     time.Time
	Idx    int // position of its commit on main's first-parent line
	Commit string
	Tag    string
	failed bool
}

type envTrack struct {
	start   *time.Time
	deploys []*liveDeploy // by time
}

// first is the first deploy that contains the commit at idx.
func (e *envTrack) first(idx int) *liveDeploy {
	for _, d := range e.deploys {
		if d.Idx >= idx {
			return d
		}
	}
	return nil
}

// after is the first deploy strictly after t.
func (e *envTrack) after(t time.Time) *liveDeploy {
	for _, d := range e.deploys {
		if d.At.After(t) {
			return d
		}
	}
	return nil
}

type builder struct {
	in       Inputs
	now      time.Time
	idx      map[string]int
	problems []string
	seenProb map[string]bool
	envs     map[string]*envTrack
	merges   []*PRRecord
	mergeIdx map[int]int // PR number → position on main
	prs      map[int]PR
	byNumber map[int]*PRRecord
	failures []Failure
}

func (b *builder) problem(format string, a ...any) {
	p := fmt.Sprintf(format, a...)
	if !b.seenProb[p] {
		b.seenProb[p] = true
		b.problems = append(b.problems, p)
	}
}

// resolve finds a commit on main's first-parent line by full sha or by a
// unique prefix of at least 7 characters.
func (b *builder) resolve(sha string) (int, bool) {
	if i, ok := b.idx[sha]; ok {
		return i, true
	}
	if len(sha) < 7 {
		return 0, false
	}
	found := -1
	for s, i := range b.idx {
		if strings.HasPrefix(s, sha) {
			if found >= 0 {
				return 0, false
			}
			found = i
		}
	}
	return found, found >= 0
}

// Build computes the report at now.
func Build(in Inputs, now time.Time) Report {
	now = now.UTC()
	b := &builder{in: in, now: now, idx: map[string]int{}, seenProb: map[string]bool{},
		envs: map[string]*envTrack{EnvMVP: {}, EnvStable: {}}, mergeIdx: map[int]int{},
		prs: map[int]PR{}, byNumber: map[int]*PRRecord{}}
	for _, p := range in.Problems {
		b.problem("%s", p)
	}
	for i, c := range in.Main {
		b.idx[c.SHA] = i
	}
	for _, p := range in.PRs {
		b.prs[p.Number] = p
	}
	from := mondayOf(in.From)

	b.readMerges(from)
	releases := b.readEnvs()
	b.placePRs()
	b.deployFailures()
	b.revertFailures()
	b.regressionFailures()
	sort.SliceStable(b.failures, func(i, j int) bool { return b.failures[i].At.Before(b.failures[j].At) })

	r := Report{Generated: now, From: from, MVPRecordedFrom: b.envs[EnvMVP].start,
		StableRecordedFrom: b.envs[EnvStable].start, Failures: b.failures}
	for _, d := range releases {
		r.Releases = append(r.Releases, Release{Tag: d.Tag, Commit: d.Commit, At: d.At, Failed: d.failed})
	}
	r.MVPDeploys = []Deploy{}
	for _, d := range b.envs[EnvMVP].deploys {
		r.MVPDeploys = append(r.MVPDeploys, Deploy{At: d.At, Commit: d.Commit})
	}
	for _, m := range b.merges {
		r.PRs = append(r.PRs, *m)
	}
	for ws := from; !ws.After(now); ws = ws.AddDate(0, 0, 7) {
		w := b.week(ws)
		w.Summary = SummaryMarkdown(w)
		r.Weeks = append(r.Weeks, w)
	}
	r.Problems = b.problems
	if r.Problems == nil {
		r.Problems = []string{}
	}
	return r
}

// readMerges lists the PR merges on main from the backfill start.
func (b *builder) readMerges(from time.Time) {
	for i, c := range b.in.Main {
		if len(c.Parents) < 2 || c.At.Before(from) {
			continue
		}
		m := mergeRe.FindStringSubmatch(c.Subject)
		if m == nil {
			b.problem("merge %s (%q) names no pull request, skipped", short(c.SHA), c.Subject)
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if _, dup := b.byNumber[n]; dup {
			b.problem("PR #%d merged twice (%s), the later merge skipped", n, short(c.SHA))
			continue
		}
		rec := &PRRecord{Number: n, Branch: branchOf(m[2]), Commit: c.SHA, Merged: c.At}
		if !c.FirstAt.IsZero() {
			t := c.FirstAt
			rec.FirstCommit = &t
		}
		if p, ok := b.prs[n]; ok {
			rec.Title, rec.Epic = p.Title, epicOf(p)
			if p.Branch != "" {
				rec.Branch = p.Branch
			}
		}
		b.merges = append(b.merges, rec)
		b.byNumber[n] = rec
		b.mergeIdx[n] = i
	}
}

// readEnvs builds each environment's deploys and the rollback failures;
// it returns stable's releases.
func (b *builder) readEnvs() []*liveDeploy {
	mvp, stable := b.envs[EnvMVP], b.envs[EnvStable]
	type rollback struct {
		env    string
		at     time.Time
		commit string
	}
	var rollbacks []rollback
	for _, d := range b.in.Deploys {
		if d.Env == EnvMVP && mvp.start == nil {
			t := d.At
			mvp.start = &t
		}
		switch {
		case d.Kind == KindRollback:
			rollbacks = append(rollbacks, rollback{d.Env, d.At, d.Commit})
		case d.Env == EnvMVP && d.Kind == KindDeploy:
			i, ok := b.resolve(d.Commit)
			if !ok {
				b.problem("mvp deploy %s: commit %s is not on main, skipped", d.At.Format(time.RFC3339), short(d.Commit))
				continue
			}
			mvp.deploys = append(mvp.deploys, &liveDeploy{At: d.At, Idx: i, Commit: d.Commit})
		case d.Env == EnvStable && d.Kind == KindRelease:
			// Releases are gathered below with promotions.tsv.
		default:
			b.problem("deploys.tsv %s %s line of kind %q is not used, skipped", d.At.Format(time.RFC3339), d.Env, d.Kind)
		}
	}

	// Stable: a release is a tag's first promotion, from promotions.tsv (the
	// backfill) or a release line (promote.sh from #470 on). A tag promoted
	// again later is a rollback re-run, not a new release.
	tagCommit := map[string]string{}
	for _, t := range b.in.Tags {
		tagCommit[t.Name] = t.Commit
	}
	type cand struct {
		at          time.Time
		tag, commit string
	}
	var cands []cand
	for _, p := range b.in.Promotions {
		cands = append(cands, cand{p.At, p.Tag, p.Commit})
	}
	for _, d := range b.in.Deploys {
		if d.Env == EnvStable && d.Kind == KindRelease {
			cands = append(cands, cand{d.At, d.Tag, d.Commit})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].at.Before(cands[j].at) })
	released := map[string]bool{}
	for _, c := range cands {
		if released[c.tag] {
			continue
		}
		released[c.tag] = true
		commit := c.commit
		if tc, ok := tagCommit[c.tag]; ok {
			commit = tc
		}
		i, ok := b.resolve(commit)
		if !ok {
			b.problem("release %s: commit %s is not on main, skipped", c.tag, short(commit))
			continue
		}
		stable.deploys = append(stable.deploys, &liveDeploy{At: c.at, Idx: i, Commit: commit, Tag: c.tag})
	}
	for _, t := range b.in.Tags {
		if _, ok := b.resolve(t.Commit); !ok {
			b.problem("tag %s: commit %s is not on main, skipped", t.Name, short(t.Commit))
		} else if !released[t.Name] {
			b.problem("tag %s was never promoted to stable: not a release", t.Name)
		}
	}
	if len(stable.deploys) > 0 {
		t := stable.deploys[0].At
		stable.start = &t
	}

	for _, rb := range rollbacks {
		e := b.envs[rb.env]
		f := Failure{ID: fmt.Sprintf("rollback/%s/%s", rb.env, rb.at.Format(time.RFC3339)), Kind: FailRollback,
			Env: rb.env, At: rb.at, Commit: rb.commit,
			Detail: fmt.Sprintf("%s rolled back to %s", rb.env, short(rb.commit))}
		b.restoreAt(&f, e.after(rb.at))
		b.failures = append(b.failures, f)
	}
	return stable.deploys
}

// placePRs finds when each PR went live.
func (b *builder) placePRs() {
	mvp, stable := b.envs[EnvMVP], b.envs[EnvStable]
	for _, m := range b.merges {
		i := b.mergeIdx[m.Number]
		switch d := mvp.first(i); {
		case mvp.start == nil || m.Merged.Before(*mvp.start):
			m.MVPNote = "not recorded"
		case d == nil:
			m.MVPNote = "not live yet"
		default:
			t := d.At
			m.MVPLive, m.MVPCommit = &t, d.Commit
		}
		if d := stable.first(i); d != nil {
			t := d.At
			m.StableLive, m.Release = &t, d.Tag
		} else {
			m.StableNote = "not released yet"
		}
	}
}

// deployFailures adds the "[E6] mvp deploy failed: <sha>" issues: the same
// failure as a rollback line just before it, else one of its own.
func (b *builder) deployFailures() {
	issues := append([]Issue(nil), b.in.Issues...)
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].CreatedAt.Before(issues[j].CreatedAt) })
	for _, is := range issues {
		m := deployIssue.FindStringSubmatch(is.Title)
		if m == nil {
			continue
		}
		matched := false
		for k := range b.failures {
			f := &b.failures[k]
			gap := is.CreatedAt.Sub(f.At)
			if f.Kind == FailRollback && f.Env == EnvMVP && f.Issue == 0 && gap >= 0 && gap <= DeployIssueWindow {
				f.Issue, f.Commit = is.Number, m[1]
				f.Detail = fmt.Sprintf("deploy of %s failed (#%d), %s", m[1], is.Number, f.Detail)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		f := Failure{ID: fmt.Sprintf("deploy_failed/#%d", is.Number), Kind: FailDeploy, Env: EnvMVP,
			At: is.CreatedAt, Issue: is.Number, Commit: m[1],
			Detail: fmt.Sprintf("deploy of %s failed (#%d), no rollback line", m[1], is.Number)}
		b.restoreAt(&f, b.envs[EnvMVP].after(is.CreatedAt))
		b.failures = append(b.failures, f)
	}
}

// revertFailures: a merged `Revert "…"` PR is a failure of the PR it
// reverts, on each environment where that PR went live before the revert.
func (b *builder) revertFailures() {
	for _, r := range b.merges {
		t := revertRe.FindStringSubmatch(r.Title)
		if t == nil {
			continue
		}
		caused := 0
		if m := revertBranch.FindStringSubmatch(r.Branch); m != nil {
			caused, _ = strconv.Atoi(m[1])
		} else {
			for _, o := range b.merges {
				if o.Number != r.Number && o.Title == t[1] {
					caused = o.Number
				}
			}
		}
		if _, ok := b.byNumber[caused]; !ok || caused == 0 {
			b.problem("revert PR #%d: the PR it reverts is not a merged PR in the window, skipped", r.Number)
			continue
		}
		r.Reverts = caused
		detected := r.Merged
		if p, ok := b.prs[r.Number]; ok && !p.CreatedAt.IsZero() {
			detected = p.CreatedAt
		}
		b.linkedFailure(FailRevert, fmt.Sprintf("revert/#%d", r.Number), caused, r.Number, 0, detected, nil,
			fmt.Sprintf("#%d reverted by #%d", caused, r.Number))
	}
}

// regressionFailures: a `regression` bug with a "Caused by #N" line (in its
// body, or in the body of the PR that fixes it) is a failure of PR N.
func (b *builder) regressionFailures() {
	for _, is := range b.in.Issues {
		if !slices.Contains(is.Labels, "regression") {
			continue
		}
		var fix *PRRecord
		re := closesRe(is.Number)
		for _, m := range b.merges {
			if p, ok := b.prs[m.Number]; ok && re.MatchString(p.Body) && (fix == nil || m.Merged.Before(fix.Merged)) {
				fix = m
			}
		}
		caused := 0
		if m := causedByRe.FindStringSubmatch(is.Body); m != nil {
			caused, _ = strconv.Atoi(m[1])
		} else if fix != nil {
			if m := causedByRe.FindStringSubmatch(b.prs[fix.Number].Body); m != nil {
				caused, _ = strconv.Atoi(m[1])
			}
		}
		if caused == 0 {
			b.problem("regression #%d names no PR (no \"Caused by #N\" line), skipped", is.Number)
			continue
		}
		if _, ok := b.byNumber[caused]; !ok {
			b.problem("regression #%d: caused by #%d, which is not a merged PR in the window, skipped", is.Number, caused)
			continue
		}
		fixNum := 0
		if fix != nil {
			fixNum = fix.Number
		}
		b.linkedFailure(FailRegression, fmt.Sprintf("regression/#%d", is.Number), caused, fixNum, is.Number,
			is.CreatedAt, is.ClosedAt, fmt.Sprintf("#%d caused regression #%d", caused, is.Number))
	}
}

// linkedFailure records a failure caused by PR caused on each environment
// where it went live: from max(detected, its live time) to the fix PR live
// there (or, with no fix PR, the issue's close). The deploy that carried the
// PR counts as a failed deploy. A fix live with or before the cause is no
// failure on that environment (they shipped together).
func (b *builder) linkedFailure(kind, id string, caused, fixPR, issue int, detected time.Time, closed *time.Time, detail string) {
	for _, env := range []string{EnvMVP, EnvStable} {
		e := b.envs[env]
		cd := e.first(b.mergeIdx[caused])
		if cd == nil || (env == EnvMVP && b.byNumber[caused].MVPLive == nil) {
			continue
		}
		var fd *liveDeploy
		if fixPR != 0 {
			fd = e.first(b.mergeIdx[fixPR])
			if fd != nil && !fd.At.After(cd.At) {
				continue
			}
		}
		at := detected
		if cd.At.After(at) {
			at = cd.At
		}
		f := Failure{ID: id + "/" + env, Kind: kind, Env: env, At: at, CausedBy: caused, FixPR: fixPR,
			Issue: issue, Commit: cd.Commit, Detail: detail}
		switch {
		case fd != nil:
			b.restoreAt(&f, fd)
		case fixPR == 0 && closed != nil:
			t := *closed
			if t.Before(at) {
				t = at
			}
			f.RestoredAt, f.RestoreHours = &t, ptr(round2(t.Sub(at).Hours()))
			f.Detail += ", closed with no fix PR found"
		}
		cd.failed = true
		rec := b.byNumber[caused]
		rec.Failures = append(rec.Failures, f.ID)
		b.failures = append(b.failures, f)
	}
}

// restoreAt ends f at deploy d. A fix live before the failure was detected
// (a regression filed after its fix PR went live) moves the failure's time
// back to the fix's: a failure starts at the earlier of detected and fix
// live, so its restore is never negative.
func (b *builder) restoreAt(f *Failure, d *liveDeploy) {
	if d == nil {
		return
	}
	t := d.At
	if t.Before(f.At) {
		f.At = t
	}
	f.RestoredAt, f.RestoreHours = &t, ptr(round2(t.Sub(f.At).Hours()))
}

// week computes one ISO week's figures.
func (b *builder) week(ws time.Time) Week {
	we := ws.AddDate(0, 0, 7)
	y, n := ws.ISOWeek()
	w := Week{Week: fmt.Sprintf("%d-W%02d", y, n), From: ws, To: we}
	in := func(t time.Time) bool { return !t.Before(ws) && t.Before(we) }
	var toMerge []float64
	for _, m := range b.merges {
		if in(m.Merged) {
			w.Merged++
			if m.FirstCommit != nil {
				toMerge = append(toMerge, m.Merged.Sub(*m.FirstCommit).Hours())
			}
		}
	}
	w.CommitToMerge = StatOf(toMerge, LeadTimeBand)
	w.MVP = b.envWeek(EnvMVP, ws, we, in)
	w.Stable = b.envWeek(EnvStable, ws, we, in)
	return w
}

func (b *builder) envWeek(env string, ws, we time.Time, in func(time.Time) bool) EnvWeek {
	e := b.envs[env]
	what := "deploy"
	if env == EnvStable {
		what = "release"
	}
	if e.start == nil || !we.After(*e.start) {
		return EnvWeek{Note: what + " not recorded",
			MergeToLive: Stat{Note: what + " not recorded"}, CommitToLive: Stat{Note: what + " not recorded"},
			Restore: Stat{Note: what + " not recorded"}}
	}
	ew := EnvWeek{Recorded: true}
	span0, span1 := ws, we
	if e.start.After(ws) {
		span0 = *e.start
		ew.Note = "recorded from " + e.start.Format("2006-01-02 15:04") + " UTC"
	}
	if b.now.Before(span1) {
		span1 = b.now
	}
	for _, d := range e.deploys {
		if in(d.At) {
			ew.Deploys++
			if d.failed {
				ew.Failed++
			}
		}
	}
	if days := span1.Sub(span0).Hours() / 24; days > 0 {
		if env == EnvStable {
			perWeek := float64(ew.Deploys) * 7 / days
			ew.PerWeek = ptr(round2(perWeek))
			ew.FrequencyBand = WeeklyFrequencyBand(perWeek)
		} else {
			perDay := float64(ew.Deploys) / days
			ew.PerDay = ptr(round2(perDay))
			ew.FrequencyBand = FrequencyBand(perDay)
		}
	}
	var toLive, fromCommit []float64
	for _, m := range b.merges {
		live := m.MVPLive
		if env == EnvStable {
			live = m.StableLive
		}
		if live == nil || !in(*live) {
			continue
		}
		toLive = append(toLive, live.Sub(m.Merged).Hours())
		if m.FirstCommit != nil {
			fromCommit = append(fromCommit, live.Sub(*m.FirstCommit).Hours())
		}
	}
	ew.MergeToLive = StatOf(toLive, LeadTimeBand)
	ew.CommitToLive = StatOf(fromCommit, LeadTimeBand)

	ew.Attempts = ew.Deploys
	var restore []float64
	for _, f := range b.failures {
		if f.Env != env || !in(f.At) {
			continue
		}
		if f.Kind == FailRollback || f.Kind == FailDeploy {
			ew.Attempts++
			ew.Failed++
		}
		if f.RestoreHours != nil {
			restore = append(restore, *f.RestoreHours)
		} else {
			ew.Unrestored++
		}
	}
	if ew.Attempts == 0 {
		ew.FailureNote = "no " + what
	} else {
		pct := round2(100 * float64(ew.Failed) / float64(ew.Attempts))
		ew.FailureRatePct = &pct
		if ew.Attempts < MinSamples {
			ew.FailureNote = tooFew(ew.Attempts)
		} else {
			ew.FailureBand = FailureRateBand(pct)
		}
	}
	ew.Restore = StatOf(restore, RestoreBand)
	if ew.Restore.N == 0 && ew.Unrestored == 0 {
		ew.Restore.Note = "no failure"
	}
	return ew
}

func mondayOf(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// branchOf strips the "owner/" of a merge subject's "from owner/branch".
func branchOf(s string) string {
	if _, branch, ok := strings.Cut(s, "/"); ok {
		return branch
	}
	return s
}

func epicOf(p PR) string {
	if m := epicTitleRe.FindStringSubmatch(p.Title); m != nil {
		return m[1]
	}
	for _, l := range p.Labels {
		if epicLabelRe.MatchString(l) {
			return l
		}
	}
	return ""
}
