# optionsBot — a project summary for recruiters

**Roberto Chau** · Senior Software Engineering Manager · AI-driven engineering leadership · São Paulo, Brazil (remote or hybrid)
LinkedIn: [linkedin.com/in/robertochau](https://www.linkedin.com/in/robertochau)

optionsBot is a personal project I built and run: an automated options-trading system for the Deribit crypto exchange, developed by a team of AI agents that I designed, govern and review.
It shows two things at once: production-grade backend engineering against a real exchange, and an AI-first software development lifecycle (SDLC) with people in charge of every risky decision.

> It is a software-engineering study that trades on Deribit **testnet**, not a commercial product and not investment advice.

---

## At a glance (2026-10-30)

| | |
|---|---|
| Time frame | June – October 2026, alone, alongside a full-time job |
| Code | ~75,000 lines of Go, ~38,000 lines of Next.js/React, 160 test files, 87 % coverage |
| Delivery | 718 commits, 302 merged pull requests, 255 issues on a Kanban board |
| AI agents | 167 of the 302 merged PRs were implemented by the Dev agent; 158 PR reviews by the Reviewer agent, 19 of them blocking |
| Releases | two environments (mvp on every merge, stable on versioned releases after a 3-day soak), v0.3.2 today |
| Delivery metrics (week 2026-W44) | 84 mvp deploys, merge → live 4 min (median), change failure rate 1 % |

---

## What the system does

- **Trades a short-volatility strategy.**
  It sells BTC and ETH option strangles and manages each through its life: entry pricing, fills, take-profit, rolls, stop-loss, delta exits, repairs and a kill switch.
- **Treats the exchange as unreliable.**
  One gateway owns every connection, with a priority queue, two rate-limit pools, retries with backoff, a circuit breaker and automatic reconnects.
- **Never assumes an order's fate.**
  An order whose submit timed out may still be on the book, so every order has a unique label; the bot cancels it by label and reconciles its own book with the exchange every cycle before it trusts either.
- **Manages risk with the exchange's own numbers.**
  Margin limits follow volatility bands and the market's gamma regime (dealer gamma exposure from open interest), changes need confirmation on daily closes, and new risk freezes whenever data is missing or stale.
- **Explains every decision.**
  Every order, skip, fill and close goes to a decision journal with a snapshot of the market at that moment, which feeds the monitor, the P&L attribution and the weekly evidence report.
- **Tests the same rules offline.**
  A backtester, parameter sweeps, a stress test and a replay of recorded order books run the live strategy code on past data.
- **Has a product around it.**
  A Next.js monitor with sign-in and roles, an AI assistant that answers questions about the bot's decisions from its own docs and journal (retrieval-augmented, with an answer guard and a golden-question evaluation set), and a PRD with epics.

Architecture details: [docs/architecture.md](docs/architecture.md) · diagrams: [docs/README.md](docs/README.md).

---

## How it is built: an AI-first SDLC

The code is written mostly by Claude Code agents running on a schedule.
My work is the system they run in: the roles, the rules, the guardrails and the decisions only a person may take.

| Agent | Job |
|---|---|
| Subject-matter expert | reviews the running bots daily and files issues with evidence |
| Product manager | triages every issue into an epic of the approved PRD and ranks the board |
| Developer | takes one Ready card, writes the change and its tests, opens a pull request |
| Reviewer | reviews every pull request against the repository's rules and posts findings |
| PR gate and deploy | merge policy-free PRs that reviewed clean, deploy main to mvp, record every deploy |

**Guardrails I designed:**
- Changes to trading rules, risk limits, configuration, CI, agent permissions or the release scripts are *policy*: agents may propose them, only a person merges them.
- Agents never trade live, never deploy to the stable environment and never hold credentials beyond testnet.
- The repository is public, so a pre-commit hook and a secret scan over every commit block any credential.
- CI runs formatting, vet, shellcheck, race-detector tests and a coverage floor; a release must run 3 days on mvp first, and every exception is audited.
- Each agent has a written skill (developer, product manager, UX designer, options specialist, legal counsel) and each folder has its own short instructions, so an agent loads only what its task needs.
- Delivery is measured with the four DORA metrics per week and environment, and every merged PR is traced to the minute it went live on each environment.

Workflow and guardrails in full: [AGENTIC.md](AGENTIC.md).

---

## Three incidents and what now prevents them

1. **A night of churn on a wrong book (2026-10-06, testnet).**
   The bot's book and the exchange disagreed, and for hours the bot bought back at the ask and re-sold at the bid, shrinking every position and leaving the account long.
   Now every buy-back is capped at the short the exchange holds this cycle, an unexpected long is sold, a slot that churns is paused, and a difference is adopted into the book only after two consecutive cycles, logged as an error and journaled.
2. **Puts sold seconds after a restart into a falling market (2026-10-09).**
   The bot traded before it knew the market regime.
   Now no new risk is taken until a fresh gamma-exposure snapshot exists, and whenever it is older than 5 minutes.
3. **Two agent mistakes caught by the system, not by luck (2026-10-30).**
   A Dev agent's test fixture used realistic-looking fake keys, and the secret scan stopped the PR.
   Rebuilding that PR on the wrong base would have reverted two merged changes, and the Reviewer agent blocked it.
   Neither mistake reached the main branch.

---

## What I decided, and what the agents did

- **Mine:** the product direction and PRD, the trading and risk rules, the architecture boundaries, every policy merge, every release to stable, and the agent workflow itself.
- **The agents':** most of the implementation, the tests, the first review of every change, triage and routine merges and deploys inside the rules above.
- I review the agents' work through their tickets and pull requests, the same way I would lead an engineering team.

---

## Skills and keywords

**AI engineering:** AI-first SDLC, agentic workflows, multi-agent systems, Claude Code, Anthropic Claude API, LLM agents, Model Context Protocol (MCP), prompt engineering, agent guardrails, human-in-the-loop, AI code review, spec-to-code, retrieval-augmented generation (RAG), LLM evaluation (golden sets), AI governance.

**Engineering leadership:** engineering management, technical strategy, AI adoption, delivery metrics (DORA), Kanban, product discovery, PRD, release management, change management.

**Backend and systems:** Go (Golang), concurrency, distributed systems, event-driven design, rate limiting, circuit breakers, resilience, idempotency, reconciliation, WebSocket and JSON-RPC APIs, observability (structured logging, audit journals).

**Frontend and product:** Next.js, React, TypeScript, Tailwind CSS, dashboards, authentication and roles, analytics.

**DevOps and quality:** CI/CD, GitHub Actions, Docker, test automation, race detection, coverage gates, secret scanning (gitleaks), shellcheck, DevSecOps.

**Domain:** fintech, algorithmic trading, crypto derivatives, options (Greeks, volatility, margin), risk management, backtesting.

---

## Where to look first

| To see | Open |
|---|---|
| The exchange gateway (rate limits, circuit breaker, retries) | [internal/gateway](internal/gateway) |
| Orders with an unknown outcome and reconciliation | [internal/strategy/orphans.go](internal/strategy/orphans.go), [internal/strategy/safety.go](internal/strategy/safety.go) |
| The margin policy, pure and tested | [internal/risk](internal/risk) |
| The agents and their guardrails | [AGENTIC.md](AGENTIC.md), [scripts/agents](scripts/agents) |
| The delivery (DORA) report | [cmd/dora](cmd/dora) |
| The strategy in plain language | [OptionStrategy.md](OptionStrategy.md) |
