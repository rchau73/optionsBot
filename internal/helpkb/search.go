package helpkb

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// BM25's usual constants: k1 caps how much a repeated word counts, b how much
// a long passage is discounted.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
	// titleMatchBoost multiplies the score of a passage whose title has
	// exactly the question's words: the question names that term.
	titleMatchBoost = 2.0
)

// DefaultNoAnswer is the share of a question's weight (its words' IDF) the
// best passage must contain for the question to count as answerable: below
// it the assistant says "I don't know" rather than guess.
const DefaultNoAnswer = 0.5

// Hit is a passage found for a question.
type Hit struct {
	Passage
	// Score is the passage's BM25 score for the question.
	Score float64
	// Coverage is the share of the question's weight (the IDF of each of its
	// words, a word no passage has counting as the rarest) found in the passage.
	Coverage float64
}

// Index is a keyword index over passages.
type Index struct {
	docs   []Passage
	titles [][]string // each title's words
	tf     []map[string]int
	lens   []int
	df     map[string]int
	avgDL  float64
}

// NewIndex indexes each passage's title and text; the title counts twice, as
// it names what the passage is about ("stop-loss" before "stop_loss_multiplier").
func NewIndex(passages []Passage) *Index {
	ix := &Index{docs: passages, df: map[string]int{}}
	total := 0
	for _, p := range passages {
		toks := Tokens(p.Title + " " + p.Title + " " + p.Text)
		tf := map[string]int{}
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			ix.df[t]++
		}
		ix.titles = append(ix.titles, unique(Tokens(p.Title)))
		ix.tf = append(ix.tf, tf)
		ix.lens = append(ix.lens, len(toks))
		total += len(toks)
	}
	if len(passages) > 0 {
		ix.avgDL = float64(total) / float64(len(passages))
	}
	return ix
}

// idf is BM25's inverse document frequency; a word no passage has gets the
// highest value (df 0).
func (ix *Index) idf(term string) float64 {
	n, df := float64(len(ix.docs)), float64(ix.df[term])
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

// Search returns the k passages scoring highest for the question (fewer when
// fewer match), best first; ties keep the index's order.
func (ix *Index) Search(question string, k int) []Hit {
	terms := unique(Tokens(question))
	if len(terms) == 0 || k <= 0 {
		return nil
	}
	weight := 0.0
	for _, t := range terms {
		weight += ix.idf(t)
	}
	var hits []Hit
	for i, tf := range ix.tf {
		score, found := 0.0, 0.0
		for _, t := range terms {
			f := float64(tf[t])
			if f == 0 {
				continue
			}
			idf := ix.idf(t)
			found += idf
			norm := 1 - bm25B + bm25B*float64(ix.lens[i])/ix.avgDL
			score += idf * f * (bm25K1 + 1) / (f + bm25K1*norm)
		}
		if sameTerms(ix.titles[i], terms) {
			// "what is a stop-loss" names the glossary's "stop-loss" exactly.
			score *= titleMatchBoost
		}
		if score > 0 {
			hits = append(hits, Hit{Passage: ix.docs[i], Score: score, Coverage: found / weight})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

// NoAnswer reports that the help does not answer the question: no passage
// matched, or the best one holds less than threshold of the question's
// weight (DefaultNoAnswer) — the question's rarest words are not in it.
func NoAnswer(hits []Hit, threshold float64) bool {
	return len(hits) == 0 || hits[0].Coverage < threshold
}

var tokenRe = regexp.MustCompile(`[\p{L}\p{N}]+(?:['’.][\p{L}\p{N}]+)*`)

// stopWords are words of a question that say nothing about its subject.
var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the and or but if then so as of to in on at by for from with without into
		about over under is are was were be been being am do does did done doing has have had having
		it its this that these those there here which who whom whose what when where why how
		i me my we our you your he she they them their his her can could would should will shall may might must
		not no than too very just also any some all each more most such own same only
		happen happens happened tell explain mean means meaning does work works`) {
		stopWords[w] = true
	}
}

// Tokens lower-cases text, splits it into words (keeping "0.30" and "leg's"
// whole), drops stop words and reduces each word to a simple stem.
func Tokens(text string) []string {
	var out []string
	for _, w := range tokenRe.FindAllString(strings.ToLower(text), -1) {
		w = strings.TrimSuffix(strings.TrimSuffix(w, "'s"), "’s")
		if stopWords[w] {
			continue
		}
		out = append(out, stem(w))
	}
	return out
}

// stem is a deliberately small English stemmer: plurals and -ing/-ed, with a
// doubled final consonant undone ("stopped", "stopping" → "stop").
func stem(w string) string {
	if strings.ContainsAny(w, "0123456789") {
		return w
	}
	cut := ""
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "sses"):
		return w[:len(w)-2]
	case len(w) > 5 && strings.HasSuffix(w, "ing"):
		cut = w[:len(w)-3]
	case len(w) > 4 && strings.HasSuffix(w, "ed"):
		cut = w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us"):
		return w[:len(w)-1]
	default:
		return w
	}
	if n := len(cut); n >= 2 && cut[n-1] == cut[n-2] && !strings.ContainsRune("lsz", rune(cut[n-1])) {
		cut = cut[:n-1]
	}
	return cut
}

// sameTerms reports whether two lists of unique words hold the same words.
func sameTerms(a, b []string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, t := range a {
		set[t] = true
	}
	for _, t := range b {
		if !set[t] {
			return false
		}
	}
	return true
}

func unique(ts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range ts {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
