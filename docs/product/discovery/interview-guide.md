# Interview guide — replaced by the survey (E5)

**Status:** replaced (#178). The product owner chose a short written survey over interviews: no calls, no recordings. Use `survey.md`. Answers and contacts stay only in `data/product/interviews/` (gitignored, never committed).

## Coding sheet

Interview notes are counted, not read aloud: after each interview, add one row to `data/product/interviews/interviews.csv` (gitignored; never committed, never pasted into an issue or a pull request).
`scripts/product/interview_tally.py` counts the sheet.
It holds participant codes and the closed codes below only: no name, e-mail, amount or private text.

| Column | Values |
|---|---|
| `participant` | `I01`, `I02`… (never a name) |
| `date` | `YYYY-MM-DD` |
| `qualified` | `yes` / `no`, using `screener.md` §1 and §3 |
| `delegation` | `yes still do` / `yes took it back` / `no` (the same three answers as survey Q6) |
| `pain_codes` | a `;` list from: `opaque_yield`, `no_risk_control`, `too_complex`, `surprise_on_account`, `fees`, `other` |
| `pain_happened` | `yes` / `no`: it happened to the person, not a hypothetical |
| `would_pay_bucket` | `nothing` / `subscription` / `per_account` / `unsure` (no amounts) |
| `trusted_testnet_trial` | `yes` / `no` / `unsure` |

A pain counts only when `pain_happened` is `yes`, as in `synthesis.md` §2: a pain named by someone it did not happen to is left out of the counts.
The tool refuses an unknown code, a missing or extra column, a duplicate participant and any cell that looks like an e-mail address or an amount; it names the row and the column, never the cell.
A sample under ~15 qualified interviews is noted in the output, never hidden.
