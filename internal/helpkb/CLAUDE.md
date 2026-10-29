# internal/helpkb

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/helpkb`** + `cmd/helpindex`, `cmd/helpkb` — the E10 assistant's only corpus (#312, #259, #190):
  - OptionStrategy.md (split on headings, anchors as GitHub makes them), `docs/parameters.md` (the settings reference, source `parameters.md#<anchor>`)
    and `frontend/content/monitor-guide.md` (the user-facing Monitor guide, one section per menu page, source `monitor-guide.md#<anchor>`, #418),
  - every `frontend/content/glossary.json` term and every sentence of `api.Catalogue()` (`internal/api/catalogue.go`: the plain-words catalogue rendered through `ExplainEvent`/`ExplainTrade`, keys `<event>:<value>`)
  - as passages ≤ `MaxChunkChars` (1,200; a long table split between rows with its header, a list between items; a line that is only an HTML comment, such as the guide's `<!-- guide: … -->` markers, is dropped)
    with `id` (source/ordinal), `source` (`OptionStrategy.md#<anchor>` | `glossary:<term>` | `explain:<key>`), `title`, `text` and the source file's git blob hash → `frontend/content/help-index.json`.
  `tests/helpindex_test.go` fails until `go run ./cmd/helpindex` is re-run after any of the three sources changes,
  and runs the glossary's no-return-promises regex over every passage (a promise word that denies one is allowed by its exact phrase, `negatedPromises`).
  Pure BM25 `Search` and `NoAnswer` (the best passage holds under `DefaultNoAnswer` of the question's IDF weight → "I don't know");
  - `tests/helpkb_test.go` scores `tests/testdata/helpkb_questions.yaml` and fails when hit@3 falls under `helpkb_baseline.json` (`-args -update-help` rewrites the index and the baseline).
  A paragraph or list item written one sentence per line is rejoined with spaces (`joinSoftBreaks`, #463), so reflowing a source does not change its passages.
  No model, no network; nothing in the bot uses it.
