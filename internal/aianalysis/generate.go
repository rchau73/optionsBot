package aianalysis

// The generator (#426): one model call on a comparison's report.json, the
// answer checked with Check, and analysis.md|json written beside the report
// only when it passes. Offline tool; nothing serves the result.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"optionsbot/internal/compare"
)

// AnalysisModel is the one call the generator makes: the prompt and the
// report in, the analysis text out. Tests pass a fake.
type AnalysisModel interface {
	Analyze(ctx context.Context, system, user string) (string, error)
}

// ErrNoAPIKey stops the command before anything is sent.
var ErrNoAPIKey = errors.New("ANTHROPIC_API_KEY is not set: export it (the person running the tool writes it in their environment) or pass -fake <answer.md>; nothing was sent")

// KindSection is a generator rule, not a checker rule: one of the four
// sections is missing or out of order.
const KindSection = "missing_section"

// Sections are the analysis's headings, in this order (prompt.txt).
var Sections = []string{"What changed", "Where the proposal is better or worse", "Where it is fragile", "What to test next"}

// fragileSection is the section that must name the settings the stress does
// not model.
const fragileSection = "Where it is fragile"

// Section is one heading of the analysis and its text.
type Section struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Analysis is analysis.json. Text is the checked text, Disclaimer first.
type Analysis struct {
	Disclaimer  string    `json:"disclaimer"`
	Label       string    `json:"label"`
	Model       string    `json:"model"`
	Report      string    `json:"report"`
	GeneratedAt time.Time `json:"generated_at"`
	NotModelled []string  `json:"not_modelled,omitempty"`
	Sections    []Section `json:"sections"`
	Text        string    `json:"text"`
}

// FileModel is the -fake model: its answer is a file's text.
type FileModel struct{ Path string }

// Analyze returns the file's text.
func (f FileModel) Analyze(context.Context, string, string) (string, error) {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return "", fmt.Errorf("read fake answer: %w", err)
	}
	return string(b), nil
}

// ModelFor picks the model: the fake answer file when given, else the
// Messages API with apiKey — none → ErrNoAPIKey, before any request.
func ModelFor(fake, apiKey string) (AnalysisModel, string, error) {
	if fake != "" {
		return FileModel{Path: fake}, "fake:" + filepath.Base(fake), nil
	}
	if apiKey == "" {
		return nil, "", ErrNoAPIKey
	}
	return &Anthropic{APIKey: apiKey, Model: Model}, Model, nil
}

// UserMessage is the user turn: the report as data.
func UserMessage(report []byte) string {
	return "<comparison>\n" + strings.TrimSpace(string(report)) + "\n</comparison>"
}

// Generate asks the model for the analysis of report and checks it. The
// settings the stress does not model are added to the fragile section when
// the answer leaves one out. Violations non-empty = the analysis must not be
// written; err is a failed call or an unreadable report.
func Generate(ctx context.Context, m AnalysisModel, modelName string, report []byte, now time.Time) (Analysis, []Violation, error) {
	var r compare.Report
	if err := json.Unmarshal(report, &r); err != nil {
		return Analysis{}, nil, fmt.Errorf("report is not a comparison report.json: %w", err)
	}
	answer, err := m.Analyze(ctx, Prompt, UserMessage(report))
	if err != nil {
		return Analysis{}, nil, fmt.Errorf("model call: %w", err)
	}
	text := NameNotModelled(strings.TrimSpace(answer), r.Stress.NotModelled)
	full := compare.Disclaimer + "\n\n" + text
	sections := SplitSections(text)
	vs := append(Check(report, full), missingSections(sections)...)
	return Analysis{
		Disclaimer:  compare.Disclaimer,
		Label:       Label,
		Model:       modelName,
		GeneratedAt: now.UTC(),
		NotModelled: r.Stress.NotModelled,
		Sections:    sections,
		Text:        full,
	}, vs, nil
}

// NameNotModelled adds one sentence to the fragile section naming every key
// of notModelled the section does not name yet. A text without that section
// is returned as is (missingSections reports it).
func NameNotModelled(text string, notModelled []string) string {
	lines := strings.Split(text, "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		title, ok := heading(l)
		if !ok {
			continue
		}
		if start >= 0 {
			end = i
			break
		}
		if strings.EqualFold(title, fragileSection) {
			start = i
		}
	}
	if start < 0 {
		return text
	}
	body := strings.Join(lines[start+1:end], "\n")
	var absent []string
	for _, k := range notModelled {
		if !strings.Contains(body, k) {
			absent = append(absent, "`"+k+"`")
		}
	}
	if len(absent) == 0 {
		return text
	}
	insert := end
	for insert > start+1 && strings.TrimSpace(lines[insert-1]) == "" {
		insert--
	}
	add := "The stress does not model " + strings.Join(absent, ", ") + ", so its figures cannot show the effect of that change."
	out := append(append(append([]string{}, lines[:insert]...), add), lines[insert:]...)
	return strings.Join(out, "\n")
}

// SplitSections cuts text at its "## " headings; text before the first one
// (the label) is left out.
func SplitSections(text string) []Section {
	var out []Section
	for _, l := range strings.Split(text, "\n") {
		if title, ok := heading(l); ok {
			out = append(out, Section{Title: title})
			continue
		}
		if len(out) > 0 {
			s := &out[len(out)-1]
			s.Text += l + "\n"
		}
	}
	for i := range out {
		out[i].Text = strings.TrimSpace(out[i].Text)
	}
	return out
}

func heading(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "## ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "## ")), true
}

// missingSections asks for the four sections, in their order.
func missingSections(got []Section) []Violation {
	var out []Violation
	next := 0
	for _, want := range Sections {
		found := false
		for i := next; i < len(got); i++ {
			if strings.EqualFold(got[i].Title, want) {
				found, next = true, i+1
				break
			}
		}
		if !found {
			out = append(out, Violation{Kind: KindSection, Text: want})
		}
	}
	return out
}

// Options are one run of cmd/analyze.
type Options struct {
	ReportPath string // data/compare/<time>/report.json
	Model      AnalysisModel
	ModelName  string
	Now        time.Time
}

// ErrRejected is returned when the analysis fails the checks: nothing written.
var ErrRejected = errors.New("the analysis does not meet the contract (docs/ai-analysis.md); nothing was written")

// Run reads the report, generates and checks the analysis and writes
// analysis.md and analysis.json beside the report. A rejected analysis
// returns its violations and ErrRejected, and writes nothing.
func Run(ctx context.Context, o Options) (md, js string, vs []Violation, err error) {
	report, err := os.ReadFile(o.ReportPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("read report: %w", err)
	}
	a, vs, err := Generate(ctx, o.Model, o.ModelName, report, o.Now)
	if err != nil {
		return "", "", nil, err
	}
	if len(vs) > 0 {
		return "", "", vs, ErrRejected
	}
	a.Report = filepath.Base(o.ReportPath)
	dir := filepath.Dir(o.ReportPath)
	md, js = filepath.Join(dir, "analysis.md"), filepath.Join(dir, "analysis.json")
	if err := os.WriteFile(md, []byte(Markdown(a)), 0o644); err != nil {
		return "", "", nil, fmt.Errorf("write %s: %w", md, err)
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return "", "", nil, err
	}
	if err := os.WriteFile(js, append(data, '\n'), 0o644); err != nil {
		return "", "", nil, fmt.Errorf("write %s: %w", js, err)
	}
	return md, js, nil, nil
}

// Markdown is analysis.md: the checked text, the Disclaimer first, then a
// footer saying where it came from (not part of the checked text).
func Markdown(a Analysis) string {
	body := strings.TrimSpace(strings.TrimPrefix(a.Text, a.Disclaimer))
	return fmt.Sprintf("> **%s**\n\n%s\n\n---\n\n_Model `%s` on `%s`, %s. Checked by `aianalysis.Check`; offline, shown to no user._\n",
		a.Disclaimer, body, a.Model, a.Report, a.GeneratedAt.Format(time.RFC3339))
}
