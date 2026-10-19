# Synthesis template — cycle-holder interviews (E5)

**Status:** template (#178). It turns each interview into evidence and ends with the PRD §6 kill criterion: **< 30 % of interviewed cycle holders would let software trade their account → no-go**.

**Where data goes:** the filled per-interview sheets, raw notes, names and contacts stay only in `data/product/interviews/` (gitignored, never committed, never in an issue or a pull request). Only the summary tables of §2–§4, with anonymous IDs (`P01`, `P02`…), no names, no amounts and no identifying detail, may go into a pull request that updates the PRD.

## 1. Per-interview sheet

Copy this block into `data/product/interviews/P<nn>.md` for each interview and fill it within 24 hours of the call.

```markdown
# P<nn> — <date>
- Segment (screener): cycle holder | active option trader
- Found through: community | referral | waiting list | other
- Holding story (Q1–Q2), one line:
- Last attempt to earn (Q3–Q5): when, what used, how it went
- Worst moment (Q6–Q7):
- How they judged it worth it (Q8):
- Past delegation (Q9–Q11): yes | no — what they allowed, what they kept, any surprise
- Pays today (Q12–Q13): kinds of cost only (fee, subscription, share of yield, course) — no amounts
- Fake door (part G): Joined | Declined | Undecided
- Why (word for word):
- Jobs (one line each, "When …, I want …, so I can …"):
- Pains (one line each):
- Quotes (word for word, with the question ID):
```

## 2. Pains and jobs

One row per pain or job heard in more than one interview. Count only interviews where the person described it happening to them, not ones where they agreed it sounded bad.

| # | Pain or job | Interviews (IDs) | Count | Cycle holders in the count | Strongest quote (ID) |
|---|---|---|---|---|---|
| 1 | | | | | |

## 3. What they use and pay today

| Product or method used | Interviews (IDs) | Why they chose it | Why they stopped | Kind of cost |
|---|---|---|---|---|
| | | | | |

## 4. Kill criterion (PRD §6)

Count **cycle holders only**; active option traders are reported beside them but never change the verdict.

| Measure | Definition | Value |
|---|---|---|
| N | cycle holders interviewed | |
| Joined | cycle holders whose fake-door outcome is **Joined** — they gave a contact for a trial in which software places orders on their own account | |
| Share | Joined ÷ N, in % | |
| Past delegation | cycle holders who answered yes to Q9 (have given software or a person permission to trade their account) — supporting evidence, not the verdict | |
| Active option traders who joined | for comparison only | |

**Verdict** (pick one):

- **Not enough evidence** — N < 10. Recruit more cycle holders before deciding (`screener.md` quotas).
- **Kill criterion met** — Share < 30 %. Recommend **no-go** on letting software trade customer accounts, with the quotes that explain why.
- **Kill criterion not met** — Share ≥ 30 %. Discovery continues to the waiting-list and pricing tests of E5.

Undecided counts as not joined: a "maybe" is not a decision to let software trade. If Share and Past delegation point in different directions (for example many joined but none ever delegated before), say so beside the verdict — it is the first thing to probe in the next round.

## 5. What changes in the PRD

One line per change the evidence supports (persona, jobs, pains, positioning, pricing research, kill criterion verdict), each with the IDs behind it. These go to the PM as a pull request on `docs/product/PRD.md`; a person's merge approves them.

| PRD section | Change | Evidence (IDs, table row) |
|---|---|---|
| | | |
