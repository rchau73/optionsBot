package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/helpkb"
)

// The settings' plain-words tips in the help index (#621): one passage per
// tip of docs/product/education/tips.json, cited "tip:<key>", titled with the
// setting's name in words (the monitor's chip reads "Tip: <name>"), its text
// the tip's own words, so the assistant and the tip never disagree.

// tipText is a tip's words as its passage holds them.
func tipText(e helpkb.TipEntry) string {
	var paras []string
	for _, p := range []string{e.Meaning, e.RaiseRisk, e.RaiseIncome, e.PolicyNote} {
		if p != "" {
			paras = append(paras, p)
		}
	}
	return strings.Join(paras, "\n\n")
}

func helpTips(t *testing.T, s helpkb.Sources) map[string]helpkb.TipEntry {
	t.Helper()
	var tips map[string]helpkb.TipEntry
	if err := json.Unmarshal(s.Tips, &tips); err != nil {
		t.Fatal(err)
	}
	return tips
}

// missingHelp lists every tip key and glossary term with no passage of its own.
func missingHelp(f helpkb.File, tips map[string]helpkb.TipEntry, glossary map[string]json.RawMessage) []string {
	cited := map[string]bool{}
	for _, c := range f.Chunks {
		cited[c.Source] = true
	}
	var out []string
	for k := range tips {
		if !cited[helpkb.TipPrefix+k] {
			out = append(out, helpkb.TipPrefix+k)
		}
	}
	for term := range glossary {
		if !cited[helpkb.GlossaryPrefix+term] {
			out = append(out, helpkb.GlossaryPrefix+term)
		}
	}
	return out
}

func TestHelpIndexTips_TipName(t *testing.T) {
	for key, want := range map[string]string{
		"repair_cooldown_hours":        "repair cooldown",
		"max_mm_pct":                   "max mm",
		"gamma_flip_buffer_sd":         "gamma flip buffer",
		"iv_margin_bands[].max_im_pct": "iv margin bands max im",
		"dte_delta_matrix[].deltas[]":  "dte delta matrix deltas",
		"backtest.slippage_pct":        "backtest slippage",
		"stop_loss_multiplier":         "stop loss multiplier",
		"hours":                        "hours", // a unit word alone is the name
	} {
		if got := helpkb.TipName(key); got != want {
			t.Errorf("TipName(%q) = %q, want %q", key, got, want)
		}
	}
}

// Scenario: a setting question is answered from its tip — "what does the
// repair cooldown do?" finds the tip first, with the tip's words, under the
// name the chip shows as "Tip: repair cooldown".
func TestHelpIndexTips_SettingQuestionIsAnsweredFromItsTip(t *testing.T) {
	s := helpSources(t)
	tips := helpTips(t, s)
	tip, ok := tips["repair_cooldown_hours"]
	if !ok {
		t.Fatal("tips.json has no repair_cooldown_hours tip: pick another setting for this test")
	}
	hits := helpkb.NewIndex(buildHelpIndex(t, s).Chunks).Search("what does the repair cooldown do?", 3)
	if helpkb.NoAnswer(hits, helpkb.DefaultNoAnswer) {
		t.Fatalf("no answer: %+v", hits)
	}
	top := hits[0]
	if top.Source != helpkb.TipPrefix+"repair_cooldown_hours" || top.Title != "repair cooldown" {
		t.Fatalf("top hit %s (%q), want the repair cooldown tip", top.ID, top.Title)
	}
	if top.Text != tipText(tip) {
		t.Errorf("tip passage text %q, want the tip's words %q", top.Text, tipText(tip))
	}
}

// Every golden question that expects a tip (docs/product/assistant/golden.jsonl
// "tip-*") finds that tip first.
func TestHelpIndexTips_GoldenTipQuestionsRankTheirTipFirst(t *testing.T) {
	ix := helpkb.NewIndex(buildHelpIndex(t, helpSources(t)).Chunks)
	asked := 0
	for _, q := range loadGolden(t) {
		for _, src := range q.ExpectedSources {
			if !strings.HasPrefix(src, helpkb.TipPrefix) {
				continue
			}
			asked++
			hits := ix.Search(q.Question, 3)
			if helpkb.NoAnswer(hits, helpkb.DefaultNoAnswer) || hits[0].Source != src {
				t.Errorf("%s: %q does not find %s first: %+v", q.ID, q.Question, src, hits)
			}
		}
	}
	if asked < 3 {
		t.Errorf("%d golden questions expect a tip, want at least 3 (#621)", asked)
	}
}

// Scenario: every tip is findable — the committed index has a passage for
// every tip and glossary term, and the check names one that is missing.
func TestHelpIndexTips_EveryTipIsFindable(t *testing.T) {
	s := helpSources(t)
	var committed helpkb.File
	data, err := os.ReadFile(filepath.Join("..", helpkb.IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &committed); err != nil {
		t.Fatal(err)
	}
	var gloss map[string]json.RawMessage
	if err := json.Unmarshal(s.Glossary, &gloss); err != nil {
		t.Fatal(err)
	}
	tips := helpTips(t, s)
	if missing := missingHelp(committed, tips, gloss); len(missing) > 0 {
		t.Errorf("no passage in %s for: %v (run go run ./cmd/helpindex)", helpkb.IndexFile, missing)
	}
	// Negative: drop the repair cooldown tip's passage and the stop-loss term's.
	var cut helpkb.File
	for _, c := range committed.Chunks {
		if c.Source != helpkb.TipPrefix+"repair_cooldown_hours" && c.Source != helpkb.GlossaryPrefix+"stop-loss" {
			cut.Chunks = append(cut.Chunks, c)
		}
	}
	got := strings.Join(missingHelp(cut, tips, gloss), "|")
	for _, want := range []string{"tip:repair_cooldown_hours", "glossary:stop-loss"} {
		if !strings.Contains(got, want) {
			t.Errorf("a missing %s is not named: %q", want, got)
		}
	}
}

// Scenario: tip and answer agree — an edited tip, once the index is
// rebuilt, is the passage's text word for word; the committed index is
// stale until then.
func TestHelpIndexTips_TipAndAnswerAgree(t *testing.T) {
	s := helpSources(t)
	tips := helpTips(t, s)
	tip := tips["repair_cooldown_hours"]
	tip.Meaning = "Edited: hours a closed leg waits before it is sold again."
	tips["repair_cooldown_hours"] = tip
	edited, err := json.Marshal(tips)
	if err != nil {
		t.Fatal(err)
	}
	s.Tips = edited
	f := buildHelpIndex(t, s)
	var text string
	for _, c := range f.Chunks {
		if c.Source == helpkb.TipPrefix+"repair_cooldown_hours" {
			text += c.Text
			if c.Blob != helpkb.BlobHash(edited) {
				t.Errorf("%s: blob %s, want the edited file's", c.ID, c.Blob)
			}
		}
	}
	if text != tipText(tip) {
		t.Errorf("passage text %q, want the edited tip %q", text, tipText(tip))
	}
	got, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join("..", helpkb.IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, committed) {
		t.Error("an edited tip left the index unchanged: the stale check would not catch it")
	}
}

// Scenario: unknown setting — a setting with no tip and no help is not
// answered: the assistant says it does not know, as before. A tips file that
// is not JSON fails the build rather than dropping the tips.
func TestHelpIndexTips_UnknownSettingIsNotAnswered(t *testing.T) {
	s := helpSources(t)
	ix := helpkb.NewIndex(buildHelpIndex(t, s).Chunks)
	if hits := ix.Search("what does the zorblax_quiver_window setting do?", 3); !helpkb.NoAnswer(hits, helpkb.DefaultNoAnswer) {
		t.Errorf("an unknown setting is answered from %s", hits[0].ID)
	}
	s.Tips = []byte("{not json")
	if _, err := helpkb.Build(s); err == nil || !strings.Contains(err.Error(), helpkb.TipsFile) {
		t.Errorf("Build with a broken tips file: err %v, want one naming %s", err, helpkb.TipsFile)
	}
}
