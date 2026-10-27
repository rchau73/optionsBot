# AI analysis of a settings comparison — contract and checker

Status: **offline only** (#257, epic E9). No analysis is shown to any user, no endpoint serves one, no model is called and no trading code reads this. Showing one to a manager waits for legal question **L10** (is an AI analysis shown to a manager investment advice?) and the E9 open questions (#185, linked to L24). Until then the fallback of the legal brief applies: only the comparison's own numbers are shown. L12 (EU AI Act labelling) is met by design: every output carries the label below and is to be logged.

## Input

One JSON document: the `report.json` of `cmd/compare` (#186, `internal/compare.Report`) — the settings that differ (`diffs`), the backtest, the replay and the stress for the current (`base`) and the proposed settings (net return, worst week, max drawdown, fees, trade counts, sheds and stops), plus:

- the catalogue entry of each changed key (`config.Tunables()`, #255) when available, else the key names;
- the run's data coverage: an optional top-level `"coverage": {"missing_days": ["2026-10-14", …]}` (days with no recorded data, listed, never filled).

## Output

Four sections, in this order, in plain words:

1. **What changed** — the differing settings.
2. **Where the proposal is better or worse** — per tool, numbers from the input only.
3. **Where it is fragile** — a tool or window where the sign flips, few trades, missing days.
4. **What to test next** — another tool or window to run; never "apply it".

## Rules

- The text carries, word for word: *AI-generated analysis of backtested and simulated results; not advice; past and simulated results do not predict future results* (`aianalysis.Label`; line breaks and case are ignored).
- Every number (percentage, coin amount, count, year) is a value of the input, matched at the number of decimals the text writes (`412.5` may be written `413`). A model copies figures; it does not add, subtract or average (a difference is not "in the input"). A date is accepted only when it appears in the input. Digit runs touching a letter (`SMA9`, `27NOV`) are names, not figures; `10` (the thin-data threshold) is always allowed. Avoid numbered lists: a bare `1.` is a figure.
- No advice or return promise: the phrases in `aianalysis.BannedPhrases` ("guarantee", "will earn", "you should", "risk-free", "expected profit", "apply this change", …), matched case-insensitively even when negated.
- Thin data is admitted: when a tool (`backtest`, `replay`) ran with fewer than `aianalysis.MinTrades` (10) trades in either setting, one sentence names the tool and says "I can't tell from this run" or that the result is uncertain (also "too few", "thin", "not enough", "fragile"). The same for missing days: a sentence with "missing" and such a word.
- No forward-looking return statement; no "you should".

## Checker

`internal/aianalysis` — pure Go, no network:

```go
violations := aianalysis.Check(reportJSON, analysisText) // empty = meets the contract
```

A `Violation` is `{kind, text, sentence}`; kinds: `number` (the figure and its sentence), `label`, `banned_phrase`, `missing_caveat`, `bad_input`. The prompt is `internal/aianalysis/prompt.txt` (embedded as `aianalysis.Prompt`); the model is the constant `aianalysis.Model` (`claude-opus-5-5`). Calling it is a later story, after L10; whatever it returns must pass `Check` before anyone could see it, and a failing text is replaced by the comparison's numbers alone.

Tests: `tests/aianalysis_test.go` (fixed sample analyses: grounded passes, invented number, advice wording, thin data, missing days, label).
