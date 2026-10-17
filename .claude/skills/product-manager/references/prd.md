# The PRD (`docs/product/PRD.md`)

One living document for the product. It changes only through pull requests that a person merges, and **the merge is the approval** of the roadmap and its business requirements. `scripts/agents/pm_agent.sh` reads it from `origin/main` and keeps one `epic` issue per epic on the board.

## Structure

```markdown
# <Product> — Product Requirements
Version <n> · <date> · owner: PM · status: approved on merge

## 1. Summary            (5 lines: who, problem, solution, why now, the decision this version asks for)
## 2. Vision and strategy (where we are going in 2–3 years; what we will not do)
## 3. Customers           (segments and personas, jobs-to-be-done, alternatives they use today)
## 4. Market and competitors (summary; details and sources in data/product/, gitignored)
## 5. Value proposition and positioning
## 6. Go-to-market MVP    (segment, the smallest offer that proves value, trial, pricing hypothesis, channels, launch phases)
## 7. Metrics             (north star, input metrics, guardrails — definition, source, baseline, target)
## 8. Roadmap             (Now / Next / Later: outcomes, the epics behind each)
## 9. Epics               (one ### section per epic, format below)
## 10. Design             (principles, the flows and prototypes, links to approved prototypes)
## 11. Risks, compliance and open questions   (anything that needs legal review)
## 12. Decision log       (date · decision · options · reason · decided by)
```

## Epic format

The heading starts with the epic id. The marker right under it is what the scripts read; keep it valid JSON on one line.

```markdown
### E2 · First-session value for new investors
<!-- epic: {"id": "E2", "status": "approved", "rank": 2, "rule_areas": [], "ui": true} -->
**Outcome:** <metric moves from baseline to target by date>
**Why:** <evidence>
**Scope:** <business requirements, each testable>
**Out of scope:** <…>
**Stories (seed):** <the first stories you expect; tickets are created on the board>
**Prototype:** <link once approved, or "needed">
```

| Field | Values | Effect |
|---|---|---|
| `id` | `E1`, `E2`, … (never reused) | the epic issue is matched by it |
| `status` | `approved` · `parked` · `done` | approved: its stories may go to Ready · parked: kept, nothing goes to Ready · done: the epic issue is closed |
| `rank` | 1 = most important | guides priorities |
| `rule_areas` | subset of `entry, exits, repair, rebalance, margin, gex, sizing, orders, reconcile, killswitch, gateway, marketdata` | trading-rule areas the epic's stories may change; a story touching another area waits for a person |
| `ui` | true / false | true: UI stories need `prototype:approved` on the epic issue before Ready |

A malformed marker stops the PM agent with an error (an approved epic is never dropped silently). Keep epics outcome-shaped ("investors see value in the first session"), not feature-shaped ("build page X"), and few: 3–7 active at a time.
