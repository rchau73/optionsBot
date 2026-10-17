# Templates

Every document starts with a 3–5 line summary and the decision you need. Keep it short, mark assumptions and end with the next step, its owner and its date.

## Product one-pager / PRD

```markdown
# <Feature or product name>
**Summary:** <what, for whom, why now, in 3 lines>   **Decision needed:** <…>   **Status:** draft | review | approved

## Problem
<one sentence> · who has it (segment) · how they solve it today · what it costs them
## Evidence
<interviews (n, dates), data, tickets, churn reasons, competitor moves — with sources>
## Goals and non-goals
Goals: <outcome + metric + target + date>   Non-goals: <what we deliberately don't do>
## Users and scenarios
<persona → scenario → expected outcome>
## Solution options considered
| Option | Value | Effort | Risk | Why chosen / rejected |   (include the bold option)
## Requirements
Must · Should · Could, each testable. UX: flows, empty states, tips/videos, accessibility, languages.
## Metrics and instrumentation
Primary · secondary · guardrails · events to add · dashboard
## Risks, compliance and open questions
<regulatory (needs legal review?), security, data, dependencies>
## Rollout
Slices · feature flag · beta group · staged % · kill criteria · review dates (1 / 2 / 6 weeks)
```

## Experiment brief

```markdown
# Experiment: <name>
Hypothesis: We believe <change> for <segment> will <metric> by <MDE> because <evidence>.
Primary metric · secondary · guardrails
Design: A/B (unit: user) · arms · traffic split · sample size per arm · duration (≥ 1 weekly cycle)
Decision rule: ship if <…>; kill if <…>; iterate if <…>
Risks / compliance:
Result (filled after): effect + CI · SRM check · segments · decision · learning
```

## Pricing proposal

```markdown
# Pricing proposal: <product>
Summary · decision needed
Personas and willingness to pay (sources)
Value metric and why
Tiers: | Tier | For whom | Price monthly / annual | Includes | Fences |
Trial / free tier design
Competitor benchmark (dated, sourced)
Unit economics per tier: ARPA, gross margin, CAC payback, LTV:CAC
Scenarios (pessimistic / base / optimistic): conversion, mix, churn → MRR 6 / 12 months
Migration of existing customers · communication plan
Legal / tax checks · how we will test it · review date
```

## Launch plan (GTM)

```markdown
# Launch: <name> — <date>
ICP and segment · positioning statement (for / who / unlike / we / proof)
Messaging (tested with n users) · pricing and packaging
Channels and budget, with expected CAC per channel
Phases: private beta → public beta → GA, with entry criteria for each
Readiness: product, support (FAQ, macros), docs and videos, legal review, status page, monitoring, rollback
Success metrics: day 1 · week 1 · month 1, with targets
Owners and timeline · risks and mitigations
```

## Post-launch review

```markdown
# Post-launch review: <name> — <1 | 2 | 6> weeks after
Metric vs target (table) · guardrails · adoption funnel
What users say (quotes, tickets) · what surprised us
Decision: scale | iterate (what) | roll back · next review date
```

## Competitor brief

```markdown
# Competitor: <name> — reviewed <date>
Segment · positioning · pricing and packaging (source link, date)
Strengths · weaknesses · what customers praise / hate (sources)
Recent moves · what it means for us · our response (or "watch")
```

## Roadmap (Now / Next / Later)

```markdown
North star: <metric>   Strategy in one sentence: <…>
| Horizon | Outcome (metric) | Bets | Confidence | Owner |
Now (committed, this quarter) · Next (likely, next 1–2 quarters) · Later (direction, may change)
Not doing (and why)
```

## Issue for this repo (AGENTIC.md board)

```markdown
## User story
As a <persona>, I want <…> so that <…>.
## Business problem
## Evidence
## Proposed solution (and options rejected)
## Metrics
## Acceptance criteria
## Decision needed
```
