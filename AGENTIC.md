# Agentic development

This bot is developed by a small team of AI agents with a human in charge.

This file is the overview: the pipeline, who merges and the budget.
Each agent has its own file under `docs/agents/`, listed in the [index](#the-agents-one-file-each) below.

The most important agent is the **SME agent**, a senior options portfolio manager.
Every day it reads the running bots and finds the business problems worth solving: margin left idle, risk taken that the rules did not intend, orders that never fill, money lost to a rule that misfires.
It writes each one as a business case, with what it costs and what "fixed" means, and the next day it checks on live data that the fix paid off.
It decides *what is worth building*, so every other step depends on it.

The **PM agent**, a senior product manager, owns the order of the work.
A person approves the product plan in advance (the PRD and its epics); the PM agent then reads every new ticket first, places it in one epic, ranks it, and hands the squad only work that plan approved.

The other agents are engineering delivery.
One turns approved issues into tested pull requests.
A second reviews each of those pull requests and sends it back until its findings are fixed.
A deploy step ships what is merged to the **mvp** environment; the human promotes releases from there to **stable** (see [Environments](docs/operations.md#environments-stable-and-mvp)).
The human decides what gets built (the PRD, and every ticket outside it) and every change to a business rule or policy.
Since 2026-10-28 (#468) a PR that the Reviewer passes clean, with green CI and no policy change, merges without a person: **the agents propose and check; a human approves policy.**

Everything happens in the open on GitHub, so each step can be audited: the [project board](https://github.com/users/rchau73/projects/1), the issues labelled [`agent:sme`](https://github.com/rchau73/optionsBot/issues?q=label%3Aagent%3Asme) and the pull requests labelled [`agent:dev`](https://github.com/rchau73/optionsBot/pulls?q=label%3Aagent%3Adev).

![The agentic workflow: SME agent (business) → issue → human triage → Dev agent → PR → Reviewer agent → human review and merge → testnet deploy → SME verifies](docs/agentic_workflow.png)

## The agents, one file each

| File | What it covers |
|---|---|
| [docs/agents/sme.md](docs/agents/sme.md) | The SME agent: the daily review, what makes it different, its first run |
| [docs/agents/ticket.md](docs/agents/ticket.md) | What a ticket looks like: the SME issue template |
| [docs/agents/pm.md](docs/agents/pm.md) | The PM agent: the product order and the rules for Ready |
| [docs/agents/pm-answer.md](docs/agents/pm-answer.md) | Talking to the PM: answers on the ticket, who merges a PRD change, design and the showcase |
| [docs/agents/dev.md](docs/agents/dev.md) | The Dev agent's checks: CI runs every suite, failures go back to the agent |
| [docs/agents/pr-gate.md](docs/agents/pr-gate.md) | The PR gate: only ready PRs reach you, auto-merge and the policy paths |
| [docs/agents/release.md](docs/agents/release.md) | Automatic release: weekly, hotfix and escalation |
| [docs/agents/run-ledger.md](docs/agents/run-ledger.md) | The run ledger: which agent did what, when and how it ended |
| [docs/agents/stuck-cards.md](docs/agents/stuck-cards.md) | Stuck cards: In progress or In review with nothing working on them |
| [docs/agents/metrics.md](docs/agents/metrics.md) | Agent metrics: the weekly report on the loop |
| [docs/agents/dora.md](docs/agents/dora.md) | Delivery report (DORA) |
| [docs/agents/evidence.md](docs/agents/evidence.md) | Weekly evidence report and the direction trial |
| [docs/agents/usage.md](docs/agents/usage.md) | Weekly usage report and the assistant quality review |
| [docs/agents/backup.md](docs/agents/backup.md) | Nightly backup and its restore check |
| [docs/agents/flaky.md](docs/agents/flaky.md) | Nightly flaky-test hunt |
| [docs/agents/requests.md](docs/agents/requests.md) | Assistant requests: the private queue and the PM's triage |

The Reviewer and Deploy steps are described in the loop and the guardrails below.

## The loop

| # | Step | Who | What happens |
|---|---|---|---|
| ⓪ | **Approve the product plan** | **human — gate 0** | The PM agent's persona ([product-manager](.claude/skills/product-manager/SKILL.md)) writes [`docs/product/PRD.md`](docs/product/PRD.md): vision, segments, go-to-market MVP, metrics, roadmap and **epics** with their business requirements. It changes only through a pull request; **the merge is the approval.** The human merges every change to a strategy policy. The PM merges the rest itself (see *Talking to the PM* below). The PM agent keeps one `epic` issue per PRD epic on the board. |
| ① | **Find the business problem** | **SME agent, daily** | Reads the testnet bots' state, events, trades, P&L and margin, plus its own previous reports. Finds what costs money or adds unintended risk (freezes, idle margin, one-sided books, bad fills, stops) and tuning opportunities, and traces each one to the trading rule behind it. See [below](docs/agents/sme.md). |
| ② | **Report** | script | Opens one GitHub issue per finding on the **Triage** column, as a business case: cost, evidence, options and acceptance criteria. A finding seen again becomes a comment, not a duplicate. |
| ③ | **Triage** | **PM agent**, every 6 min · **human — gate 1** for the rest | The PM agent reads every new Triage card first: puts it in exactly one epic (a sub-issue), sets its **Priority** (P0–P3) and moves it to **Ready** only when the approved PRD covers it (see [below](docs/agents/pm.md)). Everything else stays in Triage with its recommendation, and the human refines it, moves it, or extends the PRD. It also **drives every approved epic**: an epic running dry gets its next stories, a card that depends on another ("Depends on #N", "Waits until <date>") waits in **Blocked** with `pm:waiting` and is released to Ready when that clears, every card carries its epic's coloured label (`E1`…`E13`) and `[E#]` title prefix, and Ready is ordered by priority (Triage: cards waiting for you last). |
| ④⑤ | **Build** | Dev agents, every 6 min, one at a time by default (`agents.env`) | Each takes the top free Ready issue (Priority, then oldest; one instance per card) and implements it on its own branch, in an isolated worktree, with tests and docs. Then it commits. |
| ⑥ | **Verify & propose** | script | Re-runs every check itself (gofmt, vet, race tests, frontend lint/test/build), pushes, and opens a PR that closes the issue → **In review**. If checks fail, or the agent stops with questions → **Blocked**, with the questions on the issue. |
| ⑦ | **Review** | Reviewer agents, every 6 min, one at a time by default | Reviews each new commit of a Dev agent PR as a second engineer: correctness, the trading rules, whether every acceptance scenario has a real test. It checks each finding against the code before reporting it, then posts one review with inline comments. A **blocking** finding moves the card back to **Ready**, and the Dev agent fixes it on the same PR and answers each finding in the PR body. After 2 fix rounds the human decides. No blocking finding → the PR is labelled `agent:reviewed`. The diff comes one file per changed path (`.agent-review/diff/<path>.diff`, with an index of every file and its line count), and the review must name each file `read: yes|no`: one left unread or out makes the verdict `incomplete`, never clean (#476: PR #474's 2,381-line diff was read to line 2,000 and passed). The gate treats that as not reviewed; the commit is retried once on the next tick, then the human reviews it (`agent:needs-human`). So when the human opens a PR, the review loop has already run. |
| ⑧ | **Review & merge** | **PR gate; human — gate 2 for policy** | GitHub requires a PR with green CI for `main`. A PR reviewed clean, up to date with `main` and with **no business rule or policy change** is merged by the PR gate (#468). A policy change waits for the human, who reads the agent's review and the code and merges, or requests changes: moving the card back to Ready makes the Dev agent address that review on the same PR. |
| ⑨⑩ | **Deploy** | script, every 6 min | When `main` moves, it rebuilds and restarts the **mvp** environment (its own clone and Deribit testnet subaccount) and checks that every bot's decision loop reports again. Then it moves every closed issue's card to **Done**. Every tick also does that for issues closed by hand, since the board's own "Item closed" automation is off. **Stable** (the track record) changes only when the human runs `scripts/promote.sh <tag>`: after 3 days in mvp, with no `sme:not-fixed` issue open, and with an automatic rollback if the health check fails. |
| ⑪ | **Verify the outcome** | **SME agent, next day** | Checks the closed issue's acceptance criteria against fresh data: did the business problem go away, not just did the code merge. It comments "fixed", or reopens the issue to Triage. |

## The agents

| | **SME agent** (business) | **PM agent** (product) | Dev agent | Reviewer agent | Deploy |
|---|---|---|---|---|---|
| Brain | Claude Code with the [option-bot-specialist](.claude/skills/option-bot-specialist/SKILL.md) skill: a senior options PM | Claude Code with the [product-manager](.claude/skills/product-manager/SKILL.md) skill: a senior fintech SaaS PM | Claude Code with the [developer](.claude/skills/developer/SKILL.md) skill: a senior Go + React engineer | the same developer skill in its *Review mode*, in a separate run that did not write the code | none (a script) |
| Trigger | daily, launchd | every 6 min, a new or changed **Triage** card, a card with no epic, an epic with no moving story (once a day, one PR-sized story), a `pm:waiting` card to release, or a person's answer on an `agent:pm` ticket (up to 2 answers in parallel) | every 6 min, a card in **Ready** (skipping one that waits: "Depends on #N" while #N is open, "Waits until <date>"), up to 3 in parallel | every 6 min, an `agent:dev` PR in **In review** with an unreviewed commit, up to 3 in parallel | every 6 min, `main` moved |
| Can | **read** the collected data and the docs | **read** the PRD, the board and the docs; edit and commit `docs/product/` only (answers) | read/edit files, run build/test/lint, `git add`/`commit` | **read** the code at the PR head, run build/vet/test | `git pull --ff-only`, `docker compose up`, move cards |
| Cannot | run commands, edit anything, use the network or MCP tools | edit outside `docs/product/`, push, call GitHub; approve a prototype or a strategy-policy change (its script merges only PRD pull requests with no policy change) | push, call GitHub, use the network, docker, `rm`, `git reset`/`checkout`; merge or deploy | edit, commit, push, call GitHub, use the network; approve or merge | deploy live or to stable; touch a clone that isn't a clean `main` |
| Output | a report + a findings block → issues | a report + a decisions block → epic links, Priority, Ready (gated); a PRD pull request or a ticket reply | commits + a PR body → a PR | a findings block → one PR review; card → Ready or stays for the human | containers restarted, cards → Done |

The agents' permissions are enforced by Claude Code, not by prompting: `--permission-mode dontAsk` with explicit allowed and denied tool lists ([`sme_review.sh`](scripts/agents/sme_review.sh), [`pm_agent.sh`](scripts/agents/pm_agent.sh), [`pm_answer.sh`](scripts/agents/pm_answer.sh), [`dev_agent.sh`](scripts/agents/dev_agent.sh), [`review_agent.sh`](scripts/agents/review_agent.sh)).
Anything not on the allowed list is refused.
Every GitHub action is taken by the shell script around the agent, after its own checks, never by the model itself.

## Guardrails

- **Human gates at the decisions that matter:** every strategy policy in the product plan (merging a PRD change that touches one), the prototype (`prototype:approved`), every ticket the PRD does not cover (Triage → Ready), and what ships when it changes a policy (merge).
  The PR gate merges only Dev agent PRs reviewed clean, CI green, up to date with `main`, and policy-free by both the Reviewer's declaration and `pr_gate.py` `POLICY_PATHS` (#468; `PR_AUTO_MERGE=0` in `agents.env` turns it off).
  The PM agent's script merges only its own PRD pull requests with no strategy policy change, `docs/product/` only, CI green.
  A ruleset on `main` requires a pull request with green CI and blocks force pushes and branch deletion.
- **Least privilege:** the agent that looks at trading data cannot change code, and the agent that changes code cannot see the network or GitHub.
- **Trust, but verify:** the Dev agent's "tests pass" is not taken at its word.
  The script runs the checks the change touches itself, and CI runs every suite on the PR, which stays a draft until CI is green ([Check failures](docs/agents/dev.md)).
  Then a separate Reviewer agent reads the change with no stake in it, and must back each finding with a concrete failure: an unverified first pass on PR #91 had 3 false findings out of 10.
- **Bounded review loop:** the Reviewer sends a PR back at most twice (`REVIEW_MAX_ROUNDS`), reviews each commit once, and a failed review hands the PR to the human instead of retrying.
  Its review is a comment, not an approval: the agents use the human's GitHub login, which can't approve its own PR, so the board column carries the decision and merging stays with the human.
- **Testnet only, mvp only:** automatic deploys go to mvp and refuse to run if its bots are live.
  Stable moves only by `promote.sh` — a person's, or the weekly or hotfix release on a testnet stable (["Automatic release"](docs/agents/release.md)); live deploys stay manual.
- **A broken main never leaves mvp down** (#204): when a deploy fails its compose step or health check, `deploy.sh` puts the mvp clone back on the last commit that passed, rebuilds and health-checks it, and records a `rollback` line in `deploys.tsv`.
  It also files one `bug` issue in Triage, `[E6] mvp deploy failed: <sha> (<subject>)`, with the failed step and the last 80 log lines, secrets redacted (`deploy_issue.py`).
  The PM triages that issue like any bug, so a fix is on its way.
  A rollback that is unhealthy too is notified loudly and stops there, with no retry loop.
  Details: [operations](docs/operations.md#agents-sme-review-pm-dev-agent-mvp-deploy).
- **Environments share nothing:** each has its own clone, Deribit subaccount, monitor port and data; `envctl.sh` refuses to start one whose Deribit account or port another uses (two bots on one account cancel each other's orders at startup and adopt each other's positions).
- **Data exposure:** this repo is public.
  On testnet, issues carry full evidence, because the money is fake.
  If a bot runs live, the SME agent is told to leave balances, sizes and P&L out of issues; the full report stays local (`data/sme/`, gitignored).
- **Stop instead of guessing:** the Dev agent stops when an issue is ambiguous, needs a product decision, or would change live order behaviour beyond what the issue says.
  It posts its questions and moves the card to **Blocked**.
- **Idempotent and bounded:** one Dev task at a time, with locks and stale-lock recovery.
  **No duplicate tickets:** the SME agent reads every open issue (any label, with its body and last comments) and those closed in the last 14 days, and reports a problem already tracked as an update to that ticket, posted only when the data says something new.
  Tickets parked in Triage for a week or two to collect data are not re-filed or re-posted daily.
  The script adds two checks of its own, on the fingerprint and on near-identical titles.
  A failed deploy commit is not retried in a loop.
- **Audit trail:** every agent action is an issue, a comment, a commit or a PR, labelled `agent:*`, with its run log in `data/agents/`.
  Each pickup comment numbers the attempt and says what came before (blocked, sent back by the Reviewer, or a human's comment), so an issue tells its whole story even when a card was moved back on the board.
- **Run ledger** (#206): one file answers "which agent did what, when, on which card, and how did it end?" — see [Run ledger](docs/agents/run-ledger.md).

## How AI is used in this repo beyond the loop

- **Project memory.**
  [`CLAUDE.md`](CLAUDE.md) records the architecture and every rule *with the incident that caused it* (dates, coins, numbers).
  New work, by a human or an agent, starts from what already went wrong.
- **Two expert personas as skills.**
  The [developer](.claude/skills/developer/SKILL.md) skill brings engineering judgement: plan first, tests and logs by default, simple code, guard rails.
  The [option-bot-specialist](.claude/skills/option-bot-specialist/SKILL.md) skill brings trading judgement: risk before return, hypotheses tested in backtests, P&L with market context.
  The same skills drive interactive sessions and the unattended agents.
- **Rules backed by evidence.**
  Strategy rules are compared with `cmd/stress` replays before they change, and the numbers go in the PR.

## Example

On 2026-10-15 the ETH bot sat flat for days, frozen by a DVOL band change that could not move its IM limit: under a confirmed negative gamma regime the limit is pinned to the lowest band either way.
The fix ([#79](https://github.com/rchau73/optionsBot/pull/79)) was built in an interactive session.
Later that day, the SME agent's first unattended review flagged the same problem on its own, with the same diagnosis and fix, plus three new findings: BTC GEX sheds whipsawing on an unstable flip, short-DTE slots with no expiry for 31 h, and an unmanaged long put on the account.
That is the loop this workflow automates.

## Run it

```bash
gh auth refresh -s project            # once: the board needs the project scope
scripts/agents/setup_github.sh        # once: labels, project board, ruleset on main
scripts/agents/install.sh             # launchd: SME daily at 09:00; tick every 6 min (deploy, then review/PM/Dev in parallel; Mondays also metrics and evidence; nightly the flaky-test hunt and the backup)
launchctl kickstart gui/$(id -u)/com.optionsbot.sme-review   # run a review now
scripts/agents/install.sh --uninstall
```

Runs on the Mac that runs the bots, logged in to Claude Code and `gh`, **from the mvp clone** (`~/dev/go/optionsBot-env/mvp/scripts/agents/install.sh`), so every deploy also updates the agents' scripts.
Logs: `data/sme/<date>/` (reports, collected data) and `data/agents/` (dev runs, reviews, deploys, launchd).

## Trade-offs and next steps

- **One GitHub identity.**
  The agents act with the human's `gh` login, so the separation of duties is enforced by tool permissions and the ruleset, not by GitHub identity.
  Next step: a GitHub App or a machine user for the agents, with "require one approval" on `main`, so GitHub itself enforces that someone other than the author approves.
- **One machine.**
  The bots, the monitor and the agents run on one Mac (launchd catches up after sleep).
  Moving the bots to a server would move the agents with them; moving only the Dev agent to GitHub Actions is possible, but deploy and the SME agent need the bots' local API.
- **Measure the agents.**
  Weekly report: see [Agent metrics](docs/agents/metrics.md), including what a person overruled (#292).
  Not visible there: board moves and Priority changes, which leave no trace on an issue.

---

*Educational study of an automated trading system — not investment advice. Options trading can lose more than the premium collected; crypto markets are highly volatile.*

## Parallel runs and limits

`tick.sh` runs every 6 minutes (launchd, `AbandonProcessGroup`) and starts in the background up to `DEV_PARALLEL` (1) Dev agents, `REVIEW_PARALLEL` (1) Reviewers and `PM_ANSWER_PARALLEL` (1) PM answers, plus one PM agent (defaults 1 each since 2026-10-23; raise them in `agents.env`, below).
Each instance claims its card (`data/agents/claims/<kind>-<n>`, holding its pid — free the moment the process ends), picks under a short mutex so two never take the same card, and serialises git fetch/worktree/push in the shared clone (`git_locked`).
Independent cards are built at once; dependent ones wait in Blocked (`pm:waiting`).

**Limits adapt by themselves** (product owner, 2026-10-19: "find the optimum value"): when a run hits the Claude usage limit or GitHub's rate limit (`note_limit`), no new Claude run starts for 30 minutes, then one at a time for an hour, then the configured parallelism again (`parallel_max`).
A Dev card that hit a limit goes back to Ready, not Blocked; a PM answer stays due.
Set the `*_PARALLEL` variables in the launchd plist to change the ceilings.

**The budget: the agents' share of your Claude plan** (product owner, 2026-10-23: the agents used the plan so fast that Claude Desktop was unusable).
The agents share the person's Claude plan, so a file the person edits bounds them: `<envs>/agents.env` (`~/dev/go/optionsBot-env/agents.env`, outside both clones so a deploy never resets it; template [`agents.env.example`](scripts/agents/agents.env.example)), read at every tick — an edit applies within 6 minutes, no reinstall:

| Setting | Effect |
|---|---|
| `AGENTS_PAUSED=1` | no new Claude run; deploys, the PR gate and runs already going continue |
| `AGENT_HOURS=20-8` | local hours a run may start in (wraps midnight; empty or `0-24` = any) |
| `AGENT_MAX_RUNS_PER_DAY=20` | Claude runs started per local day, every agent together (default 30) |
| `DEV_PARALLEL`, `REVIEW_PARALLEL`, `PM_ANSWER_PARALLEL` | instances at once (default 1 each) |
| `DEV_MODEL`, `REVIEW_MODEL`, `PM_MODEL`, `SME_MODEL` | e.g. `sonnet`: far less of the plan per run |

`budget_reason` (`limit_lib.sh`, tested in `test_budget.py`) is checked by `parallel_max`, so every Claude run waits on it like on a limit pause — the PM's triage included (until 2026-10-23 it asked Claude inside a pause, 22 failed runs that day); `budget_take` records each run in `data/agents/claude_runs` just before `claude -p`.
The daily SME review honours the pause and the daily count, not the hours.
A variable set in the environment wins over the file.

**A limit never looks like a question** (#237: Claude's "You've hit your weekly limit · resets …" was not in the pattern, and 28 approved cards were labelled `agent:needs-human` and moved to Blocked in 2–5 s each).
The wordings live in one place, `LIMIT_PATTERN` in [`limit_lib.sh`](scripts/agents/limit_lib.sh) (weekly, session and usage limits, the generic `hit your … limit` and `limit · resets` forms, GitHub's rate limits).
As a second guard, `note_stop` treats any run that exits non-zero in under 60 s with no answer block (`RESULT:`, `REVIEW-JSON`, `PM-…`) like a limit: the pause starts, the Dev card goes back to Ready without `agent:needs-human`, a review or PM answer is retried.
The Dev agent posts one limit comment per episode: none when the card's last comment (its own pickups aside) already is one.
Each tick, before the Dev agents start and never inside a pause, [`requeue.sh`](scripts/agents/requeue.sh) re-queues the cards such a limit blocked before this guard existed (rules in `requeue.py`, tested in `test_requeue.py`):
an open Blocked card with `agent:needs-human`, without `pm:waiting`, whose newest Dev stop says "the agent did not finish", with no person's comment after it,
and whose newest run log (`data/agents/dev/issue-<n>-*/agent.md`) matches `LIMIT_PATTERN`,
loses the label, moves to Ready and gets one comment ("🤖 Re-queued: the earlier stop was a usage limit, not a question.").
A stop that carries the agent's questions, or that a person answered, stays with the person.

**Token savers** (product owner, 2026-10-27):
- **One card per epic at a time.**
  The Dev pick order skips any card whose epic another instance is building (`pm_board.py ready`).
  Cards of one epic touch the same files, and building two at once made the second PR conflict and cost a fix-up run.
  **Exception, delivery blockers** (product owner 2026-10-29, #459): cards labelled `blocker` may be built up to 3 at once in one epic (`BLOCKER_PARALLEL`), since they were planned to touch different files.
  **While any `blocker` card is open (any status), only blocker cards are picked:** every other card waits, whatever its priority or epic (product owner, 2026-10-29).
  With no open `blocker` card, normal picking and one card per epic apply again.
  The total number of Dev agents is still `DEV_PARALLEL` in `agents.env`.
- **Lighter model for documents.**
  When `DEV_MODEL` is unset, a card labelled `documentation`, or one in an epic of `DEV_LIGHT_EPICS` (default `E5,E11`), runs on `DEV_LIGHT_MODEL` (default `sonnet`).
  Code cards use the account default (`pm_board.py model`; both settings live in `agents.env`).
- **The full test suite runs once, in CI** (#472).
  The agent runs only the tests its change touches, and so does the script (`checkfail.py`): 2.5–30 min of Mac time saved per run.
  CI runs every suite on the PR; a failure goes back to the agent ([Check failures](docs/agents/dev.md)).
