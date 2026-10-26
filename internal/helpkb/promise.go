package helpkb

import (
	"regexp"
	"strings"
)

// promiseRe is the glossary's no-return-promises check
// (frontend/__tests__/glossary.test.js): words that would promise a return.
var promiseRe = regexp.MustCompile(`(?i)guarantee|risk[- ]free|no risk|can(not|'t) lose|sure (profit|win|income)|always (profit|win|earn)|will earn|safe income`)

// negatedPromises are the published sentences where a promise word is used to
// deny a promise, never to make one. The sources' wording is not ours to
// change here, so each is allowed by the exact phrase around the match.
var negatedPromises = []string{
	"triggers, not guaranteed prices", // OptionStrategy.md §6 Loss B: a stop is not a guaranteed price
	"adds no risk until",              // catalogue skipped:margin_unknown: the bot sells nothing
}

// Promises lists every promise word in the passages, as "<id>: <match>",
// except those inside a negatedPromises phrase.
func Promises(passages []Passage) []string {
	var out []string
	for _, p := range passages {
		for _, text := range []string{p.Title, p.Text} {
			for _, loc := range promiseRe.FindAllStringIndex(text, -1) {
				if !negated(text, loc[0], loc[1]) {
					out = append(out, p.ID+": "+text[loc[0]:loc[1]])
				}
			}
		}
	}
	return out
}

func negated(text string, start, end int) bool {
	for _, phrase := range negatedPromises {
		for i := 0; ; {
			j := strings.Index(text[i:], phrase)
			if j < 0 {
				break
			}
			if i+j <= start && end <= i+j+len(phrase) {
				return true
			}
			i += j + 1
		}
	}
	return false
}
