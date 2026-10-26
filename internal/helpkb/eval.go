package helpkb

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Question is one entry of a question set: the question, its category, the
// sources that answer it (a hit's Source matches one by prefix, so
// "OptionStrategy.md#4-how-a-trade-can-end" accepts that section's every
// passage) and whether the right answer is "I don't know".
type Question struct {
	Question string   `yaml:"question"`
	Category string   `yaml:"category"`
	Expect   []string `yaml:"expect"`
	DontKnow bool     `yaml:"dont_know"`
}

// ParseQuestions reads a question set (YAML: a list under "questions").
func ParseQuestions(data []byte) ([]Question, error) {
	var doc struct {
		Questions []Question `yaml:"questions"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("read question set: %w", err)
	}
	for i, q := range doc.Questions {
		if strings.TrimSpace(q.Question) == "" || (len(q.Expect) == 0) == !q.DontKnow {
			return nil, fmt.Errorf("question %d (%q): needs a question and either expect or dont_know", i+1, q.Question)
		}
	}
	return doc.Questions, nil
}

// Score is retrieval quality over a group of questions. HitAt1/HitAt3 are
// over the answerable questions (an expected source first / in the top 3);
// DontKnowPrecision is the share of NoAnswer verdicts that were right
// (questions marked dont_know), NaN-free: 1 when NoAnswer never fired.
type Score struct {
	Questions         int     `json:"questions"`
	Answerable        int     `json:"answerable"`
	HitAt1            float64 `json:"hit_at_1"`
	HitAt3            float64 `json:"hit_at_3"`
	NoAnswers         int     `json:"no_answers"`
	DontKnowPrecision float64 `json:"dont_know_precision"`
	// Misses are the answerable questions with no expected source in the top 3.
	Misses []string `json:"misses,omitempty"`
}

// Eval is the score of every category and of the whole set ("all").
type Eval struct {
	All        Score            `json:"all"`
	Categories map[string]Score `json:"categories"`
}

// Evaluate runs every question through the index (top 3, NoAnswer at
// threshold) and scores it.
func Evaluate(ix *Index, qs []Question, threshold float64) Eval {
	type tally struct {
		n, answerable, hit1, hit3, none, noneRight int
		misses                                     []string
	}
	all := &tally{}
	cats := map[string]*tally{}
	for _, q := range qs {
		hits := ix.Search(q.Question, 3)
		c := cats[q.Category]
		if c == nil {
			c = &tally{}
			cats[q.Category] = c
		}
		for _, t := range []*tally{all, c} {
			t.n++
			if NoAnswer(hits, threshold) {
				t.none++
				if q.DontKnow {
					t.noneRight++
				}
			}
			if q.DontKnow {
				continue
			}
			t.answerable++
			if rank := expectedRank(hits, q.Expect); rank == 1 {
				t.hit1++
				t.hit3++
			} else if rank > 0 {
				t.hit3++
			} else {
				t.misses = append(t.misses, q.Question)
			}
		}
	}
	score := func(t *tally) Score {
		s := Score{Questions: t.n, Answerable: t.answerable, NoAnswers: t.none, DontKnowPrecision: 1, Misses: t.misses}
		if t.answerable > 0 {
			s.HitAt1 = float64(t.hit1) / float64(t.answerable)
			s.HitAt3 = float64(t.hit3) / float64(t.answerable)
		}
		if t.none > 0 {
			s.DontKnowPrecision = float64(t.noneRight) / float64(t.none)
		}
		return s
	}
	out := Eval{All: score(all), Categories: map[string]Score{}}
	for name, t := range cats {
		out.Categories[name] = score(t)
	}
	return out
}

// expectedRank is the 1-based rank of the first hit citing an expected
// source, 0 when none does.
func expectedRank(hits []Hit, expect []string) int {
	for i, h := range hits {
		for _, e := range expect {
			if strings.HasPrefix(h.Source, e) {
				return i + 1
			}
		}
	}
	return 0
}

// Text is the evaluation as printed by cmd/helpkb -eval: one line per
// category (sorted), then the whole set, then the questions missed.
func (e Eval) Text() string {
	var b strings.Builder
	line := func(name string, s Score) {
		hits := "hit@1     –    hit@3     –  " // no answerable question in the group
		if s.Answerable > 0 {
			hits = fmt.Sprintf("hit@1 %5.1f %%  hit@3 %5.1f %%", 100*s.HitAt1, 100*s.HitAt3)
		}
		fmt.Fprintf(&b, "%-14s questions %3d  %s  dont_know precision %5.1f %% (%d no-answer)\n",
			name, s.Questions, hits, 100*s.DontKnowPrecision, s.NoAnswers)
	}
	names := make([]string, 0, len(e.Categories))
	for n := range e.Categories {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		line(n, e.Categories[n])
	}
	line("all", e.All)
	for _, m := range e.All.Misses {
		fmt.Fprintf(&b, "missed: %s\n", m)
	}
	return b.String()
}
