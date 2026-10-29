package delivery

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The files below are written by scripts/agents/dora.sh (git and gh dumps)
// and by deploy.sh/promote.sh (deploys.tsv, promotions.tsv). Every reader
// returns the lines it could not use as problems: reported, never counted.

// ReadMain reads main's first-parent history, as dora.sh writes it with
// `git log --first-parent --format='%H%x09%P%x09%ct'` plus, for each merge,
// the author time of the earliest commit in merge^1..merge^2:
//
//	<sha>\t<parents, space separated>\t<commit unix time>\t<first commit unix time or empty>\t<subject>
//
// git prints newest first; the result is oldest first (index = position).
func ReadMain(r io.Reader) (commits []Commit, problems []string, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		text := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(text) == "" {
			continue
		}
		f := strings.SplitN(text, "\t", 5)
		if len(f) < 5 || f[0] == "" {
			problems = append(problems, fmt.Sprintf("main.tsv line %d: want 5 tab-separated fields, skipped", n))
			continue
		}
		at, ok := unixTime(f[2])
		if !ok {
			problems = append(problems, fmt.Sprintf("main.tsv line %d (%s): bad commit time %q, skipped", n, short(f[0]), f[2]))
			continue
		}
		c := Commit{SHA: f[0], Parents: strings.Fields(f[1]), At: at, Subject: f[4]}
		if f[3] != "" {
			if first, ok := unixTime(f[3]); ok {
				c.FirstAt = first
			}
		}
		commits = append(commits, c)
	}
	if err := sc.Err(); err != nil {
		return nil, problems, fmt.Errorf("read main history: %w", err)
	}
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, problems, nil
}

// ReadTags reads `git for-each-ref refs/tags` as "<tag>\t<commit>" lines
// (the commit an annotated tag points to).
func ReadTags(r io.Reader) (tags []Tag, problems []string, err error) {
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) < 2 || f[0] == "" || f[1] == "" {
			problems = append(problems, fmt.Sprintf("tags.tsv line %d: want <tag>\\t<commit>, skipped", n))
			continue
		}
		tags = append(tags, Tag{Name: f[0], Commit: f[1]})
	}
	if err := sc.Err(); err != nil {
		return nil, problems, fmt.Errorf("read tags: %w", err)
	}
	return tags, problems, nil
}

// ReadDeploys reads <envs>/history/deploys.tsv (env_record lines):
//
//	<RFC 3339 time>\t<env>\t<commit>[\t<kind>[\t<tag>]]
//
// env is mvp or stable; kind is empty (a healthy mvp deploy), rollback
// (deploy.sh or promote.sh went back to the commit named) or release
// (promote.sh: stable is on <tag> and healthy). Ordered by time.
func ReadDeploys(r io.Reader) (lines []DeployLine, problems []string, err error) {
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) < 3 {
			problems = append(problems, fmt.Sprintf("deploys.tsv line %d: want <time>\\t<env>\\t<commit>, skipped", n))
			continue
		}
		at, perr := time.Parse(time.RFC3339, f[0])
		if perr != nil {
			problems = append(problems, fmt.Sprintf("deploys.tsv line %d: bad time %q, skipped", n, f[0]))
			continue
		}
		d := DeployLine{At: at.UTC(), Env: f[1], Commit: f[2], Kind: KindDeploy}
		if len(f) > 3 && f[3] != "" {
			d.Kind = f[3]
		}
		if len(f) > 4 {
			d.Tag = f[4]
		}
		switch {
		case d.Env != EnvMVP && d.Env != EnvStable:
			problems = append(problems, fmt.Sprintf("deploys.tsv line %d: unknown environment %q, skipped", n, d.Env))
			continue
		case d.Kind != KindDeploy && d.Kind != KindRollback && d.Kind != KindRelease:
			problems = append(problems, fmt.Sprintf("deploys.tsv line %d: unknown kind %q, skipped", n, d.Kind))
			continue
		case d.Commit == "":
			problems = append(problems, fmt.Sprintf("deploys.tsv line %d: no commit, skipped", n))
			continue
		}
		lines = append(lines, d)
	}
	if err := sc.Err(); err != nil {
		return nil, problems, fmt.Errorf("read deploys: %w", err)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].At.Before(lines[j].At) })
	return lines, problems, nil
}

// ReadPromotions reads <envs>/history/promotions.tsv (promote.sh, every
// healthy promotion): "<time>\t<tag>\t<commit>". Ordered by time.
func ReadPromotions(r io.Reader) (proms []Promotion, problems []string, err error) {
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) < 3 {
			problems = append(problems, fmt.Sprintf("promotions.tsv line %d: want <time>\\t<tag>\\t<commit>, skipped", n))
			continue
		}
		at, perr := time.Parse(time.RFC3339, f[0])
		if perr != nil {
			problems = append(problems, fmt.Sprintf("promotions.tsv line %d: bad time %q, skipped", n, f[0]))
			continue
		}
		proms = append(proms, Promotion{At: at.UTC(), Tag: f[1], Commit: f[2]})
	}
	if err := sc.Err(); err != nil {
		return nil, problems, fmt.Errorf("read promotions: %w", err)
	}
	sort.SliceStable(proms, func(i, j int) bool { return proms[i].At.Before(proms[j].At) })
	return proms, problems, nil
}

type ghLabel struct {
	Name string `json:"name"`
}

func labelNames(ls []ghLabel) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

// ReadPRs reads `gh pr list --state merged --json
// number,title,headRefName,body,createdAt,mergedAt,labels`.
func ReadPRs(r io.Reader) ([]PR, error) {
	var raw []struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		Branch    string    `json:"headRefName"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"createdAt"`
		MergedAt  time.Time `json:"mergedAt"`
		Labels    []ghLabel `json:"labels"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("read pull requests: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, p := range raw {
		prs = append(prs, PR{Number: p.Number, Title: p.Title, Branch: p.Branch, Body: p.Body,
			CreatedAt: p.CreatedAt.UTC(), MergedAt: p.MergedAt.UTC(), Labels: labelNames(p.Labels)})
	}
	return prs, nil
}

// ReadIssues reads `gh issue list --state all --json
// number,title,body,labels,createdAt,closedAt`.
func ReadIssues(r io.Reader) ([]Issue, error) {
	var raw []struct {
		Number    int        `json:"number"`
		Title     string     `json:"title"`
		Body      string     `json:"body"`
		CreatedAt time.Time  `json:"createdAt"`
		ClosedAt  *time.Time `json:"closedAt"`
		Labels    []ghLabel  `json:"labels"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("read issues: %w", err)
	}
	issues := make([]Issue, 0, len(raw))
	for _, i := range raw {
		is := Issue{Number: i.Number, Title: i.Title, Body: i.Body, CreatedAt: i.CreatedAt.UTC(), Labels: labelNames(i.Labels)}
		if i.ClosedAt != nil && !i.ClosedAt.IsZero() {
			t := i.ClosedAt.UTC()
			is.ClosedAt = &t
		}
		issues = append(issues, is)
	}
	return issues, nil
}

// Files names the inputs cmd/dora reads. Only Main is required; a missing
// optional file is a problem line (its figures then say "not recorded").
type Files struct {
	Main, Tags, PRs, Issues, Deploys, Promotions string
}

// Load reads every file into Inputs (From is the caller's).
func Load(f Files) (Inputs, error) {
	var in Inputs
	main, err := os.Open(f.Main)
	if err != nil {
		return in, fmt.Errorf("open main history %s: %w", f.Main, err)
	}
	defer main.Close()
	var p []string
	if in.Main, p, err = ReadMain(main); err != nil {
		return in, err
	}
	in.Problems = append(in.Problems, p...)

	optional := func(path, what string, read func(io.Reader) error) error {
		if path == "" {
			in.Problems = append(in.Problems, what+": no file given, not read")
			return nil
		}
		fh, err := os.Open(path)
		if os.IsNotExist(err) {
			in.Problems = append(in.Problems, fmt.Sprintf("%s: %s does not exist, not read", what, path))
			return nil
		}
		if err != nil {
			return fmt.Errorf("open %s %s: %w", what, path, err)
		}
		defer fh.Close()
		return read(fh)
	}
	steps := []struct {
		path, what string
		read       func(io.Reader) error
	}{
		{f.Tags, "tags", func(r io.Reader) (err error) {
			in.Tags, p, err = ReadTags(r)
			in.Problems = append(in.Problems, p...)
			return err
		}},
		{f.PRs, "pull requests", func(r io.Reader) (err error) { in.PRs, err = ReadPRs(r); return err }},
		{f.Issues, "issues", func(r io.Reader) (err error) { in.Issues, err = ReadIssues(r); return err }},
		{f.Deploys, "deploys", func(r io.Reader) (err error) {
			in.Deploys, p, err = ReadDeploys(r)
			in.Problems = append(in.Problems, p...)
			return err
		}},
		{f.Promotions, "promotions", func(r io.Reader) (err error) {
			in.Promotions, p, err = ReadPromotions(r)
			in.Problems = append(in.Problems, p...)
			return err
		}},
	}
	for _, s := range steps {
		if err := optional(s.path, s.what, s.read); err != nil {
			return in, err
		}
	}
	return in, nil
}

func unixTime(s string) (time.Time, bool) {
	sec, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(sec, 0).UTC(), true
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
