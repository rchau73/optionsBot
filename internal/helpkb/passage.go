// Package helpkb is the assistant's help knowledge index (#312, #259, #190):
// it splits the product's published help — OptionStrategy.md, the monitor's
// glossary and the bot's plain-words catalogue — into small passages, each
// with a stable citation, and finds the best passages for a question with a
// keyword (BM25) search. No model, no embeddings, no network: the E10
// assistant may answer only from these passages, and cite them.
//
// Parse, Build, NewIndex, Search and Eval are pure; load.go reads the files.
// cmd/helpindex writes the index to frontend/content/help-index.json and
// cmd/helpkb searches it from the command line. Nothing in the bot uses it.
package helpkb

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxChunkChars is the longest passage text, in characters (runes), unless a
// single sentence or table row is longer on its own.
const MaxChunkChars = 1200

// Passage is one citable chunk of the help.
type Passage struct {
	// ID is stable: the source and the passage's ordinal within it
	// ("OptionStrategy.md#4-how-a-trade-can-end/2").
	ID string `json:"id"`
	// Source is the citation: "OptionStrategy.md#<heading anchor>",
	// "glossary:<term>" or "explain:<journal value>".
	Source string `json:"source"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	// Blob is the git blob hash of the file the passage comes from, so a
	// reader can tell which version of the source it quotes.
	Blob string `json:"blob"`
}

// Anchor is the part of the source after "#" or ":" — the heading anchor,
// glossary term or catalogue key the passage cites.
func (p Passage) Anchor() string {
	if i := strings.IndexAny(p.Source, "#:"); i >= 0 {
		return p.Source[i+1:]
	}
	return ""
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6}) +(.+?) *#*$`)
	linkRe    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	imageRe   = regexp.MustCompile(`^!\[[^\]]*\]\([^)]*\)$`)
	listRe    = regexp.MustCompile(`^(\s*)([-*+]|\d+\.) `)
	// A line that is only an HTML comment: the guide's `<!-- guide: … -->`
	// markers (#483) and a generated file's notice are for maintainers.
	commentRe = regexp.MustCompile(`^<!--.*-->$`)
)

// HeadingAnchor is GitHub's anchor of a heading: lower case, every character
// but letters, digits, spaces, "_" and "-" dropped, spaces turned to "-"
// ("9. Market regimes and what the bot does" → "9-market-regimes-and-what-the-bot-does").
// It matches frontend/test/glossarySources.js markdownAnchors.
func HeadingAnchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Anchors lists the heading anchors of a markdown file, as GitHub makes them
// (a repeated heading gets "-1", "-2", …), in order.
func Anchors(markdown []byte) []string {
	var out []string
	for _, s := range sections(string(markdown)) {
		out = append(out, s.anchor)
	}
	return out
}

type section struct {
	anchor, title string
	body          []string
}

// sections splits markdown on its headings (outside code fences). A ##
// section's title is its heading; a deeper one's is "parent › heading", so a
// passage of "Loss A" still says it is a worked example of a loss.
func sections(md string) []section {
	var (
		out     []section
		seen    = map[string]int{}
		parents = map[int]string{}
		inFence bool
	)
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}
		m := headingRe.FindStringSubmatch(line)
		if inFence || m == nil {
			if len(out) > 0 {
				out[len(out)-1].body = append(out[len(out)-1].body, line)
			}
			continue
		}
		level, heading := len(m[1]), m[2]
		anchor := HeadingAnchor(heading)
		if n := seen[anchor]; n > 0 {
			seen[anchor] = n + 1
			anchor = fmt.Sprintf("%s-%d", anchor, n)
		} else {
			seen[anchor] = 1
		}
		parents[level] = heading
		title := heading
		if level > 2 {
			if p, ok := parents[level-1]; ok {
				title = p + " › " + heading
			}
		}
		for l := level + 1; l <= 6; l++ {
			delete(parents, l)
		}
		out = append(out, section{anchor: anchor, title: title})
	}
	return out
}

// Parse splits a markdown file into passages: one or more per heading (a
// section's text up to the next heading), each at most MaxChunkChars. Tables
// and lists stay whole when they fit; a longer one is split between rows
// (each piece repeats the table's header) or items. Links keep their text,
// images and "---" rules are dropped. The same input always gives the same
// passages; Blob is left for Build to fill.
func Parse(name string, markdown []byte) []Passage {
	var out []Passage
	for _, s := range sections(string(markdown)) {
		source := name + "#" + s.anchor
		chunks := chunk(blocks(s.body))
		if len(chunks) == 0 {
			// A heading with no text of its own (e.g. a section that is only
			// subsections) is still citable.
			chunks = []string{s.title}
		}
		for i, c := range chunks {
			out = append(out, Passage{ID: fmt.Sprintf("%s/%d", source, i+1), Source: source, Title: s.title, Text: c})
		}
	}
	return out
}

// TextPassages makes the passages of one plain-text entry (a glossary term, a
// catalogue sentence): paragraphs separated by blank lines, split like a
// section when over MaxChunkChars.
func TextPassages(source, title, text string) []Passage {
	var out []Passage
	for i, c := range chunk(blocks(strings.Split(text, "\n"))) {
		out = append(out, Passage{ID: fmt.Sprintf("%s/%d", source, i+1), Source: source, Title: title, Text: c})
	}
	return out
}

type blockKind int

const (
	paragraph blockKind = iota
	table
	list
)

type block struct {
	kind  blockKind
	lines []string
}

func (b block) text() string { return strings.Join(b.lines, "\n") }

// blocks groups a section's lines into paragraphs, tables and lists,
// separated by blank lines.
func blocks(lines []string) []block {
	var (
		out []block
		cur []string
	)
	flush := func() {
		if len(cur) == 0 {
			return
		}
		kind := paragraph
		switch first := strings.TrimSpace(cur[0]); {
		case strings.HasPrefix(first, "|"):
			kind = table
		case listRe.MatchString(cur[0]):
			kind = list
		}
		if kind != table {
			cur = joinSoftBreaks(cur)
		}
		out = append(out, block{kind: kind, lines: cur})
		cur = nil
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			flush()
		case t == "---", imageRe.MatchString(t), commentRe.MatchString(t):
			// a rule, an image or a comment: no words to cite
		default:
			cur = append(cur, strings.TrimRight(linkRe.ReplaceAllString(line, "$1"), " "))
		}
	}
	flush()
	return out
}

// softContinuation matches the quote marker a continuation line repeats.
var softContinuation = regexp.MustCompile(`^\s*>+\s?`)

// joinSoftBreaks rejoins a paragraph or list item written one sentence per
// line (#463): Markdown renders the line break as a space, so the passage is
// the same text as when the source kept the whole paragraph on one line.
// List markers (nested items too) still start a line; a quote's repeated ">"
// is dropped.
func joinSoftBreaks(lines []string) []string {
	var out []string
	for _, l := range lines {
		if len(out) == 0 || listRe.MatchString(l) {
			out = append(out, l)
			continue
		}
		prev := len(out) - 1
		if strings.HasPrefix(strings.TrimSpace(out[prev]), ">") && strings.HasPrefix(strings.TrimSpace(l), ">") {
			body := softContinuation.ReplaceAllString(l, "")
			// a bare ">" is a paragraph break inside the quote, and so is the line after it
			if strings.TrimSpace(body) == "" || strings.TrimSpace(out[prev]) == ">" || listRe.MatchString(body) {
				out = append(out, l)
				continue
			}
			l = body
		}
		out[prev] += " " + strings.TrimSpace(l)
	}
	return out
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// chunk packs blocks into passages of at most MaxChunkChars, splitting a
// block that is longer on its own.
func chunk(bs []block) []string {
	var pieces []string
	for _, b := range bs {
		if runes(b.text()) <= MaxChunkChars {
			pieces = append(pieces, b.text())
			continue
		}
		pieces = append(pieces, split(b)...)
	}
	return pack(pieces, "\n\n")
}

// split cuts an over-long block: a table between rows (each piece with the
// header), a list between items, a paragraph (or a long item) between
// sentences.
func split(b block) []string {
	switch b.kind {
	case table:
		if len(b.lines) > 2 {
			header := strings.Join(b.lines[:2], "\n")
			var out []string
			for _, rows := range pack(b.lines[2:], "\n") {
				out = append(out, header+"\n"+rows)
			}
			return out
		}
	case list:
		// Items at the first item's indent; sub-items stay with their parent.
		base := listRe.FindStringSubmatch(b.lines[0])[1]
		var items []string
		for _, l := range b.lines {
			if m := listRe.FindStringSubmatch(l); len(items) == 0 || (m != nil && m[1] == base) {
				items = append(items, l)
			} else {
				items[len(items)-1] += "\n" + l
			}
		}
		var out []string
		for _, it := range items {
			if runes(it) > MaxChunkChars {
				out = append(out, pack(sentences(it), " ")...)
			} else {
				out = append(out, it)
			}
		}
		return pack(out, "\n")
	}
	return pack(sentences(b.text()), " ")
}

// sentences splits text after ". ", "! " or "? " (a full stop followed by a
// space); the space is dropped and restored when packed.
func sentences(text string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(text); i++ {
		if (text[i] == '.' || text[i] == '!' || text[i] == '?') && text[i+1] == ' ' {
			out = append(out, text[start:i+1])
			start = i + 2
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// pack joins consecutive pieces with sep while the result stays within
// MaxChunkChars; a piece longer on its own stands alone.
func pack(pieces []string, sep string) []string {
	var out []string
	cur := ""
	for _, p := range pieces {
		switch {
		case cur == "":
			cur = p
		case runes(cur)+runes(sep)+runes(p) <= MaxChunkChars:
			cur += sep + p
		default:
			out = append(out, cur)
			cur = p
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
