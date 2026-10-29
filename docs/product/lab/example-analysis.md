# Example: AI analysis of a settings comparison (E9, #426)

What `go run ./cmd/analyze` writes as `analysis.md` beside a comparison's `report.json`. Offline only: no monitor page shows it until legal question L10 and E9's UI prototype are answered. Percentages and counts only.

The comparison is the saved fixture `tests/testdata/aianalysis/report.json` (BTC, 2026-10-12 → 2026-10-25, `gamma_flip_buffer_sd` 1.5 → 1.25 and `repair_cooldown_hours` 72 → 48, weekly windows, and the neighbours of `gamma_flip_buffer_sd` with `-neighbours`, #495) and the answer is `tests/testdata/aianalysis/answer.md`, run with `-fake`; the last sentence of "Where it is fragile" is the one the command adds for the setting the stress does not model. `tests/aianalysis_generate_test.go` (`TestAnalyze_SavedExamplePasses`) checks that the text below still passes `aianalysis.Check` on that report.

---8<--- analysis.md

> **Backtested/replayed, not a promise: modelled fills, prices and margin on past or scripted data. Past or simulated results do not predict future results.**

AI-generated analysis of backtested and simulated results; not advice; past and simulated results do not predict future results

## What changed
The GEX shed buffer `gamma_flip_buffer_sd` goes from 1.5 to 1.25 daily standard deviations.
The wait before a stopped leg is re-sold, `repair_cooldown_hours`, goes from 72 to 48 hours.

## Where the proposal is better or worse
In the backtest the proposal returns 2.11 % against 1.84 % now, on 31 trades against 26.
Its worst week is deeper in the backtest: -0.88 % against -0.62 %.
In the replay of the recorded books the proposal returns 0.57 % against 0.67 % now.
The replay has 6 repairs and 4 sheds with the proposal, against 3 and 2 now.
In the crash shock the proposal ends at -5.6 % against -4.8 %; in the rally shock both end at -1.6 %.

## Where it is fragile
The backtest and the replay disagree on the sign: the backtest has the proposal ahead, the replay behind.
Week 2026-W42 is an order flip, and the worst week of both settings is 2026-W43.
Over the weeks the proposal won 1 of 2 in the backtest and 0 of 2 in the replay.
Around the proposed buffer of 1.25, the backtest has a cliff between 1.25 and 1.4: the worst week goes from -0.88 % to -2.05 % and the max drawdown from 1.15 % to 2.27 %, each past the 1 pp threshold.
On the low side the backtest is smooth, and the replay shows no cliff across the neighbours from 1 to 1.5.
The stress does not model `repair_cooldown_hours`, so its figures cannot show the effect of that change.

## What to test next
Run the replay with daily windows over the same span to see which days carry the gap.
Run the comparison on another two weeks of recorded books.
Run the neighbours of the other changed setting as well.

---

_Model `fake:answer.md` on `report.json`, 2026-10-26T09:00:00Z. Checked by `aianalysis.Check`; offline, shown to no user._
