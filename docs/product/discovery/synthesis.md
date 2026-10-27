# Synthesis template — cycle-holder survey (E5)

**Status:** template (#178). It turns the survey's responses into evidence and ends with the PRD §6 kill criterion: **< 30 % of cycle holders surveyed would let software trade their account → no-go** (the PRD says "interviewed"; the survey's respondents stand in for them).

**Where data goes:** the exported answers, per-response notes and the waiting-list e-mails stay only in `data/product/interviews/` (gitignored, never committed, never in an issue or a pull request). Only the summary tables of §2–§5, with anonymous IDs (`P01`, `P02`…), no names, no amounts and no identifying detail, may go into a pull request that updates the PRD.

## 0. Run the tally

Count with the script, not by hand, so the numbers can be repeated:

```bash
python3 scripts/product/survey_tally.py data/product/interviews/<export>.csv \
  -out data/product/interviews/tally.md          # -json for JSON; -map map.json if the form names columns differently
```

It prints qualified vs screened out (by segment), every Q1–Q8 option's count and share, the NPS on N1 (Q1 = never tried skipped), the fake-door outcomes (`survey.md` §5), by segment, and the Q6 delegation share next to the 30 % threshold, with n and a small-sample note under 15. Free text (Q4, F2) and the F1 e-mails are never printed, only how many answered. Use its figures in §3 and §4.

## 1. Per-response line

The export has one row per response. Add these columns to it in `data/product/interviews/`:

| Column | From |
|---|---|
| ID | `P01`, `P02`… |
| Segment | cycle holder · active option trader (`screener.md` §3) |
| Channel | the link it came through |
| Fake door | Joined · Declined · Undecided (`survey.md` §5) |
| Jobs | from Q4 and F2, one line each: "When …, I want …, so I can …" |
| Pains | from Q4 and F2, one line each |

## 2. Pains and jobs

One row per pain or job written in more than one response. Count only responses where the person described it happening to them.

| # | Pain or job | Responses (IDs) | Count | Cycle holders in the count | Strongest quote (ID) |
|---|---|---|---|---|---|
| 1 | | | | | |

## 3. What they use and pay today

| Product or method (Q1) | Responses | Still using (Q3) | NPS (N1) | Kinds of cost (Q8) |
|---|---|---|---|---|
| | | | | |

NPS = % of scores 9–10 − % of scores 0–6, over the responses that named that product; with fewer than 5 responses show the scores, not an NPS.

## 4. Kill criterion (PRD §6)

Count **cycle holders only**; active option traders are reported beside them but never change the verdict.

| Measure | Definition | Value |
|---|---|---|
| N | cycle holders who answered F1 | |
| Joined | cycle holders whose fake-door outcome is **Joined** — they left an e-mail for a trial in which software places orders on their own account | |
| Share | Joined ÷ N, in % | |
| Past delegation | cycle holders who answered yes to Q6 (have given software or a person permission to trade their account) — supporting evidence, not the verdict | |
| Active option traders who joined | for comparison only | |

**Verdict** (pick one):

- **Not enough evidence** — N < 10. Keep the survey open for more cycle holders (`screener.md` quotas).
- **Kill criterion met** — Share < 30 %. Recommend **no-go** on letting software trade customer accounts, with the F2 quotes that explain why.
- **Kill criterion not met** — Share ≥ 30 %. Discovery continues to the waiting-list and pricing tests of E5.

The verdict is one row of E5's go / no-go scorecard (`../go-no-go.md`, `scripts/product/go_no_go.py`), beside the fake door, the hold-the-coin test and the legal gate.

Undecided counts as not joined: a "not now" is not a decision to let software trade. If Share and Past delegation point in different directions (for example many joined but none ever delegated before), say so beside the verdict — it is the first thing to probe in the next round.

## 5. What changes in the PRD

One line per change the evidence supports (persona, jobs, pains, positioning, pricing research, kill criterion verdict), each with the IDs behind it. These go to the PM as a pull request on `docs/product/PRD.md`; a person's merge approves them.

| PRD section | Change | Evidence (IDs, table row) |
|---|---|---|
| | | |
