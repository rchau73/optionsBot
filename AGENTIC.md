# Agentic development

This bot is developed by a small team of AI agents with a human in charge.

The most important agent is the **SME agent**, a senior options portfolio manager. Every day it reads the running bots and finds the business problems worth solving: margin left idle, risk taken that the rules did not intend, orders that never fill, money lost to a rule that misfires. It writes each one as a business case, with what it costs and what "fixed" means, and the next day it checks on live data that the fix paid off. It decides *what is worth building*, so every other step depends on it.

The **PM agent**, a senior product manager, owns the order of the work. A person approves the product plan in advance (the PRD and its epics); the PM agent then reads every new ticket first, places it in one epic, ranks it, and hands the squad only work that plan approved.

The other agents are engineering delivery. One turns approved issues into tested pull requests. A second reviews each of those pull requests and sends it back until its findings are fixed. A deploy step ships what the human merges to the **mvp** environment; the human promotes releases from there to **stable** (see [Environments](docs/operations.md#environments-stable-and-mvp)). The human decides what gets built (the PRD, and every ticket outside it) and what gets merged: **the agents propose, a human approves.**

Everything happens in the open on GitHub, so each step can be audited: the [project board](https://github.com/users/rchau73/projects/1), the issues labelled [`agent:sme`](https://github.com/rchau73/optionsBot/issues?q=label%3Aagent%3Asme) and the pull requests labelled [`agent:dev`](https://github.com/rchau73/optionsBot/pulls?q=label%3Aagent%3Adev).

![The agentic workflow: SME agent (business) → issue → human triage → Dev agent → PR → Reviewer agent → human review and merge → testnet deploy → SME verifies](docs/agentic_workflow.png)

## The loop

| # | Step | Who | What happens |
|---|---|---|---|
| ⓪ | **Approve the product plan** | **human — gate 0** | The PM agent's persona ([product-manager](.claude/skills/product-manager/SKILL.md)) writes [`docs/product/PRD.md`](docs/product/PRD.md): vision, segments, go-to-market MVP, metrics, roadmap and **epics** with their business requirements. It changes only through a pull request; **the merge is the approval.** The human merges every change to a strategy policy. The PM merges the rest itself (see *Talking to the PM* below). The PM agent keeps one `epic` issue per PRD epic on the board. |
| ① | **Find the business problem** | **SME agent, daily** | Reads the testnet bots' state, events, trades, P&L and margin, plus its own previous reports. Finds what costs money or adds unintended risk (freezes, idle margin, one-sided books, bad fills, stops) and tuning opportunities, and traces each one to the trading rule behind it. See [below](#the-sme-agent-where-business-meets-engineering). |
| ② | **Report** | script | Opens one GitHub issue per finding on the **Triage** column, as a business case: cost, evidence, options and acceptance criteria. A finding seen again becomes a comment, not a duplicate. |
| ③ | **Triage** | **PM agent**, every 6 min · **human — gate 1** for the rest | The PM agent reads every new Triage card first: puts it in exactly one epic (a sub-issue), sets its **Priority** (P0–P3) and moves it to **Ready** only when the approved PRD covers it (see [below](#the-pm-agent-the-product-order)). Everything else stays in Triage with its recommendation, and the human refines it, moves it, or extends the PRD. It also **drives every approved epic**: an epic running dry gets its next stories, a card that depends on another ("Depends on #N", "Waits until <date>") waits in **Blocked** with `pm:waiting` and is released to Ready when that clears, every card carries its epic's coloured label (`E1`…`E13`) and `[E#]` title prefix, and Ready is ordered by priority (Triage: cards waiting for you last). |
| ④⑤ | **Build** | Dev agents, every 6 min, one at a time by default (`agents.env`) | Each takes the top free Ready issue (Priority, then oldest; one instance per card) and implements it on its own branch, in an isolated worktree, with tests and docs. Then it commits. |
| ⑥ | **Verify & propose** | script | Re-runs every check itself (gofmt, vet, race tests, frontend lint/test/build), pushes, and opens a PR that closes the issue → **In review**. If checks fail, or the agent stops with questions → **Blocked**, with the questions on the issue. |
| ⑦ | **Review** | Reviewer agents, every 6 min, one at a time by default | Reviews each new commit of a Dev agent PR as a second engineer: correctness, the trading rules, whether every acceptance scenario has a real test. It checks each finding against the code before reporting it, then posts one review with inline comments. A **blocking** finding moves the card back to **Ready**, and the Dev agent fixes it on the same PR and answers each finding in the PR body. After 2 fix rounds the human decides. No blocking finding → the PR is labelled `agent:reviewed`. So when the human opens a PR, the review loop has already run. |
| ⑧ | **Review & merge** | **human — gate 2** | GitHub requires a PR with green CI for `main`. The human reads the agent's review and the code, and merges, or requests changes: moving the card back to Ready makes the Dev agent address that review on the same PR. |
| ⑨⑩ | **Deploy** | script, every 6 min | When `main` moves, it rebuilds and restarts the **mvp** environment (its own clone and Deribit testnet subaccount) and checks that every bot's decision loop reports again. Then it moves every closed issue's card to **Done**. Every tick also does that for issues closed by hand, since the board's own "Item closed" automation is off. **Stable** (the track record) changes only when the human runs `scripts/promote.sh <tag>`: after 3 days in mvp, with no `sme:not-fixed` issue open, and with an automatic rollback if the health check fails. |
| ⑪ | **Verify the outcome** | **SME agent, next day** | Checks the closed issue's acceptance criteria against fresh data: did the business problem go away, not just did the code merge. It comments "fixed", or reopens the issue to Triage. |

## The SME agent: where business meets engineering

The Dev, Reviewer and Deploy steps answer *"is it built right?"*. The SME agent answers the two questions that come before and after them: *"are we building the right thing?"* and *"did it pay off?"*. A fast delivery pipeline pointed at the wrong problem only ships waste faster. That is why the SME agent is the first step of the loop and also its last.

**What makes it different from the other agents:**

- **It speaks the business's language.** It thinks like a portfolio manager, not like a programmer. Every finding is a cost in coin, USD, idle days or risk taken, and the issue reads as a decision someone can approve: *"ETH used about 2 % of a 20 % margin limit all week"*, not *"function X returns too few lots"*.
- **It connects symptoms to rules.** It reads the bots' data *and* the trading rules in [`CLAUDE.md`](CLAUDE.md) and the code, so a finding names the rule that misfired, why it exists, and where it falls short. The Dev agent then starts from a diagnosis, not from a symptom.
- **It judges outcomes, not output.** A merged PR isn't the goal. The next day the SME agent checks the ticket's acceptance criteria against fresh market data, and reopens the issue if the problem is still there.
- **It weighs risk before return.** Its persona ([option-bot-specialist](.claude/skills/option-bot-specialist/SKILL.md)) is a desk PM who has seen books blow up selling volatility. It proposes alternatives, including *do nothing*, and states the worst case before the upside.

**What it does every day:**

| Step | What happens |
|---|---|
| Collect | A script, not the agent, pulls each bot's status, positions, orders, events, trades, P&L (and its 1-week history) and account margin from the monitor API, plus the configs and the agent's previous reports. The agent itself has no network. |
| Analyse | **Risk:** margin used against the limit, MM, one-sided books, stops. **Return:** idle margin, unfilled orders, fees, P&L by slot and exit reason. Always read in market context: DVOL, GEX regime, trend. |
| Diagnose | It traces each problem through the journal (what the bot saw, decided and got, with timestamps) to the rule that caused it. |
| Write the case | One issue per **new** problem, in the [ticket template](#what-a-ticket-looks-like): cost, evidence, options, metrics, Gherkin acceptance criteria, a recommendation. A problem an open ticket already tracks (whoever filed it) is matched by root cause, not wording, and gets a comment only when there is new data. |
| Verify | It re-checks every recently closed issue against its acceptance criteria, and comments "fixed" or reopens it. |

**Its first unattended run (2026-10-15)** filed 9 issues. By the end of the same day 8 were closed: 7 by changes built, reviewed and deployed through this loop, and 1 by the exchange listing a new expiry.

| Issue | Business problem it found | Outcome |
|---|---|---|
| [#81](https://github.com/rchau73/optionsBot/issues/81) | ETH frozen and flat for days on a volatility-band change that could not move its limit | Same diagnosis as a fix built by hand the same day ([#79](https://github.com/rchau73/optionsBot/pull/79)), found on its own |
| [#82](https://github.com/rchau73/optionsBot/issues/82) | BTC calls shed 11 min after a restart, on a guard with no warm-up | Warm-up added ([#90](https://github.com/rchau73/optionsBot/pull/90)) |
| [#83](https://github.com/rchau73/optionsBot/issues/83) | One slot per bot idle about 32 h, with no listed expiry in its window | Resolved by a new listing; the lasting fix was studied in #89 |
| [#84](https://github.com/rchau73/optionsBot/issues/84) | BTC's 50-DTE slot lost 0.04725 BTC to hedging sheds in a week | 3 of its 5 causes fixed |
| [#85](https://github.com/rchau73/optionsBot/issues/85) | A BTC top-up re-sent 6 times without a fill | Back-off on unfilled passes ([#91](https://github.com/rchau73/optionsBot/pull/91), [#94](https://github.com/rchau73/optionsBot/pull/94)) |
| [#86](https://github.com/rchau73/optionsBot/issues/86) | ETH used about 2 % of a 20 % margin limit all week | [#95](https://github.com/rchau73/optionsBot/pull/95); the root cause, orders sized up to 9× a strike's open interest, is now a product decision ([#103](https://github.com/rchau73/optionsBot/issues/103)) |
| [#87](https://github.com/rchau73/optionsBot/issues/87) | Repeated exchange connection losses on both bots | Per-connection stats in the monitor ([#100](https://github.com/rchau73/optionsBot/pull/100)) |
| [#88](https://github.com/rchau73/optionsBot/issues/88) | Is the live trial of a new hedging signal paying off? | Waiting for the human's decision |
| [#89](https://github.com/rchau73/optionsBot/issues/89) | A DTE ladder that always has a monthly expiry in the window | Ladder study tool built ([#101](https://github.com/rchau73/optionsBot/pull/101)) |

## The PM agent: the product order

The SME agent says *what is wrong*; the PM agent says *what comes next* across everything on the board, against the roadmap a person approved.

| Rule | Why |
|---|---|
| A ticket goes to Ready only if its epic is `approved` in the PRD on `main` | the person approved the business requirement in advance, when they merged the PRD |
| A ticket that changes trading rules goes only if its epic lists that rule area (`rule_areas`: entry, exits, margin, …) | live order behaviour stays a person's call unless the PRD delegated it |
| A UI story goes only once the epic carries `prototype:approved` | the human sees and approves the prototype before the squad builds it |
| It takes out of Ready only cards it put there, and never touches In progress, In review, Blocked or Done | the squad's work in flight is not re-ordered under it |
| Every decision is a comment on the issue (epic, priority, why, or what a person must decide) | audit trail |
| `agent:pm` tickets (PRD changes, decision requests) never go to Ready, and the Dev agent never builds one | they are the PM's work, not code |

Like the SME agent it is read-only: Claude Code with the [product-manager](.claude/skills/product-manager/SKILL.md) skill prints its decisions, and [`pm_agent.sh`](scripts/agents/pm_agent.sh) applies only what [`pm_board.py`](scripts/agents/pm_board.py) allows (tested in `scripts/agents/test_pm_board.py`). Without an approved PRD on `main` it does nothing.

**Talking to the PM: answer on the ticket.** The PM asks on `agent:pm` tickets. To answer, or to give it new work, comment on that ticket (or on its PRD pull request), or file an issue labelled `agent:pm`. Within 6 minutes [`pm_answer.sh`](scripts/agents/pm_answer.sh) picks up the newest person's message on it. Claude runs with the same skill in a worktree, allowed to edit only `docs/product/`. The script checks that nothing else changed and that the epic markers parse. Then it either opens or revises the PRD pull request (`Closes #n`, card → In review) or replies on the ticket (questions add `agent:needs-human`). The merge of that pull request approves the PRD. **Who merges** (product owner on #151): the PM itself merges a PRD pull request that changes no strategy policy. It gets the label `pm:self-merge`, and the script merges it once CI is green and GitHub reports it clean (`pm_board.py selfmerge`, re-checked on the exact head commit). A **strategy policy** change gets `agent:needs-human` and waits for your merge: an approved epic gaining trading-rule areas (`pm_board.py policy`, checked by the script), or a change the agent declares on its `POLICY:` line (margin, exits, entry, repair, GEX, orders, kill switch, the autonomy line; a missing line counts as a policy change). The declaration is also written into the PR body (`<!-- pm:policy=yes|no -->`) and `selfmerge` reads it again at merge time, so a PR without `pm:policy=no` is never self-merged, whatever its labels say. Taking the label off before a reply, a revision or a failure is a precondition: if GitHub refuses, the run stops before your message is marked handled, and the waiting message keeps blocking the merge. To stop a self-merge, remove the `pm:self-merge` label. Commenting also stops it: the PM acts on your message first, and if it only replies, the label comes off. A run that fails does not swallow your message (#441: on #185 a run stopped by a weekly limit hid the product owner's answer for 8 days). Its comment carries `<!-- agent:pm-failed -->`, which `pm_board.py answered` does not count as the PM's answer. A run stopped by a limit posts nothing and is retried after the pause. A run that did not finish for another reason says so and is retried on a later tick, up to 3 runs of the same message (`MAX_ANSWER_ATTEMPTS`, counted in the PM state's `attempts`); the third failure adds `agent:needs-human` and marks the message handled. Other failures (no comment, files outside `docs/product/`, a malformed epic marker) are not retried. There is nothing to trigger from chat. Every comment comes from one GitHub account, so the agents' comments carry a header (🧭 PM, 🤖 Dev/Reviewer, 🚀 deploy), and anything without one is read as yours.

**Design and the showcase.** For UI epics the PM works with the [ux-designer](.claude/skills/ux-designer/SKILL.md) skill in interactive sessions: decisions inside the approved PRD are made without waiting for a person and recorded in the PRD's decision log. The prototype (a private claude.ai Artifact) is presented on the epic issue (`prototype:review`), and the human approves it by adding `prototype:approved`. That is when the product work turns into stories the squad builds.

## What a ticket looks like

Every SME issue follows one template, written so someone with little context on the project can **approve or reject it from the ticket alone**:

1. **In one line:** the problem and its cost.
2. **User story:** *As an investor, I want …, so that …*
3. **Business problem:** what it costs, in coin, USD, idle days or risk.
4. **Business rules involved:** the rule as the bot applies it today, why it exists, and where it falls short. Jargon is defined on first use.
5. **What happened (data):** a timestamped table of what the bot saw, did and got.
6. **Proposed solution:** the steps, the alternatives considered (including *do nothing*), and effort and risk.
7. **Metrics:** baseline, target, and where each is measured.
8. **Acceptance criteria** in Gherkin (`Given … When … Then …`), including at least one scenario that protects what must not change. The Dev agent turns each scenario into a test, and the SME agent checks them against live data after the deploy.
9. **Decision needed:** *Go if … · Reject if …*, with a recommendation.

## The agents

| | **SME agent** (business) | **PM agent** (product) | Dev agent | Reviewer agent | Deploy |
|---|---|---|---|---|---|
| Brain | Claude Code with the [option-bot-specialist](.claude/skills/option-bot-specialist/SKILL.md) skill: a senior options PM | Claude Code with the [product-manager](.claude/skills/product-manager/SKILL.md) skill: a senior fintech SaaS PM | Claude Code with the [developer](.claude/skills/developer/SKILL.md) skill: a senior Go + React engineer | the same developer skill in its *Review mode*, in a separate run that did not write the code | none (a script) |
| Trigger | daily, launchd | every 6 min, a new or changed **Triage** card, a card with no epic, an epic with no moving story (once a day, one PR-sized story), a `pm:waiting` card to release, or a person's answer on an `agent:pm` ticket (up to 2 answers in parallel) | every 6 min, a card in **Ready** (skipping one that waits: "Depends on #N" while #N is open, "Waits until <date>"), up to 3 in parallel | every 6 min, an `agent:dev` PR in **In review** with an unreviewed commit, up to 3 in parallel | every 6 min, `main` moved |
| Can | **read** the collected data and the docs | **read** the PRD, the board and the docs; edit and commit `docs/product/` only (answers) | read/edit files, run build/test/lint, `git add`/`commit` | **read** the code at the PR head, run build/vet/test | `git pull --ff-only`, `docker compose up`, move cards |
| Cannot | run commands, edit anything, use the network or MCP tools | edit outside `docs/product/`, push, call GitHub; approve a prototype or a strategy-policy change (its script merges only PRD pull requests with no policy change) | push, call GitHub, use the network, docker, `rm`, `git reset`/`checkout`; merge or deploy | edit, commit, push, call GitHub, use the network; approve or merge | deploy live or to stable; touch a clone that isn't a clean `main` |
| Output | a report + a findings block → issues | a report + a decisions block → epic links, Priority, Ready (gated); a PRD pull request or a ticket reply | commits + a PR body → a PR | a findings block → one PR review; card → Ready or stays for the human | containers restarted, cards → Done |

The agents' permissions are enforced by Claude Code, not by prompting: `--permission-mode dontAsk` with explicit allowed and denied tool lists ([`sme_review.sh`](scripts/agents/sme_review.sh), [`pm_agent.sh`](scripts/agents/pm_agent.sh), [`pm_answer.sh`](scripts/agents/pm_answer.sh), [`dev_agent.sh`](scripts/agents/dev_agent.sh), [`review_agent.sh`](scripts/agents/review_agent.sh)). Anything not on the allowed list is refused. Every GitHub action is taken by the shell script around the agent, after its own checks, never by the model itself.

## Guardrails

- **Human gates at the decisions that matter:** every strategy policy in the product plan (merging a PRD change that touches one), the prototype (`prototype:approved`), every ticket the PRD does not cover (Triage → Ready), and what ships (merge). No agent merges code. The PM agent's script merges only its own PRD pull requests with no strategy policy change, `docs/product/` only, CI green. A ruleset on `main` requires a pull request with green CI and blocks force pushes and branch deletion.
- **Least privilege:** the agent that looks at trading data cannot change code, and the agent that changes code cannot see the network or GitHub.
- **Trust, but verify:** the Dev agent's "tests pass" is not taken at its word. The script re-runs every check, and a failing PR is opened as a draft and blocked. Then a separate Reviewer agent reads the change with no stake in it, and must back each finding with a concrete failure: an unverified first pass on PR #91 had 3 false findings out of 10.
- **Bounded review loop:** the Reviewer sends a PR back at most twice (`REVIEW_MAX_ROUNDS`), reviews each commit once, and a failed review hands the PR to the human instead of retrying. Its review is a comment, not an approval: the agents use the human's GitHub login, which can't approve its own PR, so the board column carries the decision and merging stays with the human.
- **Testnet only, mvp only:** automatic deploys go to mvp and refuse to run if its bots are live. Stable moves only by a person's `promote.sh`; live deploys stay manual.
- **A broken main never leaves mvp down** (#204): when a deploy fails its compose step or health check, `deploy.sh` puts the mvp clone back on the last commit that passed, rebuilds and health-checks it, and records a `rollback` line in `deploys.tsv`. It also files one `bug` issue in Triage, `[E6] mvp deploy failed: <sha> (<subject>)`, with the failed step and the last 80 log lines, secrets redacted (`deploy_issue.py`). The PM triages that issue like any bug, so a fix is on its way. A rollback that is unhealthy too is notified loudly and stops there, with no retry loop. Details: [operations](docs/operations.md#agents-sme-review-pm-dev-agent-mvp-deploy).
- **Environments share nothing:** each has its own clone, Deribit subaccount, monitor port and data; `envctl.sh` refuses to start one whose Deribit account or port another uses (two bots on one account cancel each other's orders at startup and adopt each other's positions).
- **Data exposure:** this repo is public. On testnet, issues carry full evidence, because the money is fake. If a bot runs live, the SME agent is told to leave balances, sizes and P&L out of issues; the full report stays local (`data/sme/`, gitignored).
- **Stop instead of guessing:** the Dev agent stops when an issue is ambiguous, needs a product decision, or would change live order behaviour beyond what the issue says. It posts its questions and moves the card to **Blocked**.
- **Idempotent and bounded:** one Dev task at a time, with locks and stale-lock recovery. **No duplicate tickets:** the SME agent reads every open issue (any label, with its body and last comments) and those closed in the last 14 days, and reports a problem already tracked as an update to that ticket, posted only when the data says something new. Tickets parked in Triage for a week or two to collect data are not re-filed or re-posted daily. The script adds two checks of its own, on the fingerprint and on near-identical titles. A failed deploy commit is not retried in a loop.
- **Audit trail:** every agent action is an issue, a comment, a commit or a PR, labelled `agent:*`, with its run log in `data/agents/`. Each pickup comment numbers the attempt and says what came before (blocked, sent back by the Reviewer, or a human's comment), so an issue tells its whole story even when a card was moved back on the board.
- **Run ledger** (#206): one file answers "which agent did what, when, on which card, and how did it end?" — see [Run ledger](#run-ledger) below.

## How AI is used in this repo beyond the loop

- **Project memory.** [`CLAUDE.md`](CLAUDE.md) records the architecture and every rule *with the incident that caused it* (dates, coins, numbers). New work, by a human or an agent, starts from what already went wrong.
- **Two expert personas as skills.** The [developer](.claude/skills/developer/SKILL.md) skill brings engineering judgement: plan first, tests and logs by default, simple code, guard rails. The [option-bot-specialist](.claude/skills/option-bot-specialist/SKILL.md) skill brings trading judgement: risk before return, hypotheses tested in backtests, P&L with market context. The same skills drive interactive sessions and the unattended agents.
- **Rules backed by evidence.** Strategy rules are compared with `cmd/stress` replays before they change, and the numbers go in the PR.

## Example

On 2026-10-15 the ETH bot sat flat for days, frozen by a DVOL band change that could not move its IM limit: under a confirmed negative gamma regime the limit is pinned to the lowest band either way. The fix ([#79](https://github.com/rchau73/optionsBot/pull/79)) was built in an interactive session. Later that day, the SME agent's first unattended review flagged the same problem on its own, with the same diagnosis and fix, plus three new findings: BTC GEX sheds whipsawing on an unstable flip, short-DTE slots with no expiry for 31 h, and an unmanaged long put on the account. That is the loop this workflow automates.

## Run it

```bash
gh auth refresh -s project            # once: the board needs the project scope
scripts/agents/setup_github.sh        # once: labels, project board, ruleset on main
scripts/agents/install.sh             # launchd: SME daily at 09:00; tick every 6 min (deploy, then review/PM/Dev in parallel; Mondays also metrics and evidence; nightly the flaky-test hunt and the backup)
launchctl kickstart gui/$(id -u)/com.optionsbot.sme-review   # run a review now
scripts/agents/install.sh --uninstall
```

Runs on the Mac that runs the bots, logged in to Claude Code and `gh`, **from the mvp clone** (`~/dev/go/optionsBot-env/mvp/scripts/agents/install.sh`), so every deploy also updates the agents' scripts. Logs: `data/sme/<date>/` (reports, collected data) and `data/agents/` (dev runs, reviews, deploys, launchd).

## Trade-offs and next steps

- **One GitHub identity.** The agents act with the human's `gh` login, so the separation of duties is enforced by tool permissions and the ruleset, not by GitHub identity. Next step: a GitHub App or a machine user for the agents, with "require one approval" on `main`, so GitHub itself enforces that someone other than the author approves.
- **One machine.** The bots, the monitor and the agents run on one Mac (launchd catches up after sleep). Moving the bots to a server would move the agents with them; moving only the Dev agent to GitHub Actions is possible, but deploy and the SME agent need the bots' local API.
- **Measure the agents.** Weekly report: see [Agent metrics](#agent-metrics) below, including what a person overruled (#292). Not visible there: board moves and Priority changes, which leave no trace on an issue.

---

*Educational study of an automated trading system — not investment advice. Options trading can lose more than the premium collected; crypto markets are highly volatile.*

## Parallel runs and limits

`tick.sh` runs every 6 minutes (launchd, `AbandonProcessGroup`) and starts in the background up to `DEV_PARALLEL` (1) Dev agents, `REVIEW_PARALLEL` (1) Reviewers and `PM_ANSWER_PARALLEL` (1) PM answers, plus one PM agent (defaults 1 each since 2026-10-23; raise them in `agents.env`, below). Each instance claims its card (`data/agents/claims/<kind>-<n>`, holding its pid — free the moment the process ends), picks under a short mutex so two never take the same card, and serialises git fetch/worktree/push in the shared clone (`git_locked`). Independent cards are built at once; dependent ones wait in Blocked (`pm:waiting`).

**Limits adapt by themselves** (product owner, 2026-10-19: "find the optimum value"): when a run hits the Claude usage limit or GitHub's rate limit (`note_limit`), no new Claude run starts for 30 minutes, then one at a time for an hour, then the configured parallelism again (`parallel_max`). A Dev card that hit a limit goes back to Ready, not Blocked; a PM answer stays due. Set the `*_PARALLEL` variables in the launchd plist to change the ceilings.

**The budget: the agents' share of your Claude plan** (product owner, 2026-10-23: the agents used the plan so fast that Claude Desktop was unusable). The agents share the person's Claude plan, so a file the person edits bounds them: `<envs>/agents.env` (`~/dev/go/optionsBot-env/agents.env`, outside both clones so a deploy never resets it; template [`agents.env.example`](scripts/agents/agents.env.example)), read at every tick — an edit applies within 6 minutes, no reinstall:

| Setting | Effect |
|---|---|
| `AGENTS_PAUSED=1` | no new Claude run; deploys, the PR gate and runs already going continue |
| `AGENT_HOURS=20-8` | local hours a run may start in (wraps midnight; empty or `0-24` = any) |
| `AGENT_MAX_RUNS_PER_DAY=20` | Claude runs started per local day, every agent together (default 30) |
| `DEV_PARALLEL`, `REVIEW_PARALLEL`, `PM_ANSWER_PARALLEL` | instances at once (default 1 each) |
| `DEV_MODEL`, `REVIEW_MODEL`, `PM_MODEL`, `SME_MODEL` | e.g. `sonnet`: far less of the plan per run |

`budget_reason` (`limit_lib.sh`, tested in `test_budget.py`) is checked by `parallel_max`, so every Claude run waits on it like on a limit pause — the PM's triage included (until 2026-10-23 it asked Claude inside a pause, 22 failed runs that day); `budget_take` records each run in `data/agents/claude_runs` just before `claude -p`. The daily SME review honours the pause and the daily count, not the hours. A variable set in the environment wins over the file.

**A limit never looks like a question** (#237: Claude's "You've hit your weekly limit · resets …" was not in the pattern, and 28 approved cards were labelled `agent:needs-human` and moved to Blocked in 2–5 s each). The wordings live in one place, `LIMIT_PATTERN` in [`limit_lib.sh`](scripts/agents/limit_lib.sh) (weekly, session and usage limits, the generic `hit your … limit` and `limit · resets` forms, GitHub's rate limits). As a second guard, `note_stop` treats any run that exits non-zero in under 60 s with no answer block (`RESULT:`, `REVIEW-JSON`, `PM-…`) like a limit: the pause starts, the Dev card goes back to Ready without `agent:needs-human`, a review or PM answer is retried. The Dev agent posts one limit comment per episode: none when the card's last comment (its own pickups aside) already is one. Each tick, before the Dev agents start and never inside a pause, [`requeue.sh`](scripts/agents/requeue.sh) re-queues the cards such a limit blocked before this guard existed (rules in `requeue.py`, tested in `test_requeue.py`): an open Blocked card with `agent:needs-human`, without `pm:waiting`, whose newest Dev stop says "the agent did not finish", with no person's comment after it, and whose newest run log (`data/agents/dev/issue-<n>-*/agent.md`) matches `LIMIT_PATTERN`, loses the label, moves to Ready and gets one comment ("🤖 Re-queued: the earlier stop was a usage limit, not a question."). A stop that carries the agent's questions, or that a person answered, stays with the person.

**Token savers** (product owner, 2026-10-27):
- **One card per epic at a time.** The Dev pick order skips any card whose epic another instance is building (`pm_board.py ready`). Cards of one epic touch the same files, and building two at once made the second PR conflict and cost a fix-up run.
- **Lighter model for documents.** When `DEV_MODEL` is unset, a card labelled `documentation`, or one in an epic of `DEV_LIGHT_EPICS` (default `E5,E11`), runs on `DEV_LIGHT_MODEL` (default `sonnet`). Code cards use the account default (`pm_board.py model`; both settings live in `agents.env`).
- **The full test suite runs once.** The agent runs only the tests its change touches. The script runs every suite after it finishes and sends a failure back to the agent.

## Run ledger

With up to 3 Dev and 3 Reviewer runs in parallel, the logs under `data/agents/<agent>/<run>/` have no single index. Since #206 every SME, PM, PM-answer, Dev, Reviewer, PR-gate, deploy, flaky-test-hunt, backup, usage-report and assistant-request run appends a `start` and an `end` JSON line to `<envs>/history/agent_runs.jsonl` (beside `deploys.tsv`, outside both clones):

```json
{"ts":"2026-10-22T23:10:04Z","agent":"dev","run_id":"dev-20261022T225348Z-4242-1","issue":206,"pr":271,"event":"end","outcome":"pr_opened","detail":"checks passed","log":"data/agents/dev/issue-206-20261022-225348","commit":"1a2b3c4"}
```

- **Helpers** (`lib.sh`): `run_begin <agent> [issue] [pr] [log]`, then `run_end <outcome> [detail]`; a script sets `RUN_PR`/`RUN_COMMIT` when it learns them (the PR it opened, the commit it pushed, reviewed or deployed; otherwise `commit` is the agents' own code). Each line is built by `runs.py line` and written with one `printf` under the `agent-runs` mutex, so parallel runs never interleave. The ledger never stops an agent: a line it cannot write is logged and skipped.
- **Every exit ends the run:** the `EXIT` trap writes `failed` (detail `exit <code>`) for a run that dies under `set -e`, and `done` for one that exits 0 without an outcome. Only a hard kill leaves a run open: `runs.py --open` lists those (and the runs still going), and the sweep below closes them.
- **The sweep** (#291): each tick, before any new run starts, [`sweep_runs.sh`](scripts/agents/sweep_runs.sh) runs `runs.py --sweep` under the `agent-runs` mutex. An open run whose pid (the one in its `run_id`, `<agent>-<time>-<pid>-<n>`) no longer exists gets an `end` line, outcome `abandoned`, detail `process gone`, built by the same `make_line` as `run_end`. A claim holds that same pid, so a dead run's claim is free too, and a live pid is a live run, claimed or not. A live run past its agent's time limit (`TIME_LIMIT_MIN` in `runs.py`: dev 120 min, review 60, sme 60, pm and pm_answer 30, deploy 30, backup 30, flaky 45, usage 20, requests 30, pr_gate 20; tune them from the weekly metrics' p90) is reported **once** — a marker `data/agents/runs/flagged/<run_id>` and a notification naming the agent, the card and the log. **Nothing is ever killed.** A sweep failure is logged (`data/agents/tick-sweep.log`) and the tick goes on.
- **Outcomes:** Dev `pr_opened`, `pr_updated`, `blocked`, `skipped_limit`; Reviewer `reviewed_clean`, `changes_requested`, `failed`, `skipped_limit`; PM `triaged`, `stories_filed`, `failed`, `skipped_limit`; PM answer `replied`, `prd_pr_opened`, `prd_pr_updated`, `prd_merged`, `blocked`, `skipped_limit`; SME `reported`, `failed`, `skipped_limit`; PR gate `sent_back`, `branch_updated`, `ready_to_merge`, `ready_removed`, `failed`; deploy `deployed`, `rolled_back`, `skipped`, `failed`; flaky-test hunt `filed`, `none`, `timed_out`, `failed`; usage report `posted`, `skipped`; backup `backed_up`, `failed`; assistant requests `filed`, `judged`, `none`, `skipped_limit`, `failed`; any agent `abandoned` (the sweep: its process was gone).
- **Only work is a run:** a tick where an agent finds nothing to do writes nothing. The PM agent is a run when it asks Claude (triage or planning), the PR gate once per PR it changed, the deploy once per new commit it tries.
- **Paths and ids only:** no secrets, no amounts, no prompt text; the detail is a short fixed phrase (≤ 200 characters).

```bash
python3 scripts/agents/runs.py --issue 206          # every run that touched #206, oldest first
python3 scripts/agents/runs.py --agent dev --date 2026-10-22
python3 scripts/agents/runs.py --open               # crashed (killed) or still running
python3 scripts/agents/runs.py --pr 271 --json
```

Tests: `test_runs.py` (line format, queries, the helpers sourced for real — a crash, six parallel runs; the sweep: a killed run, a live run under and over its limit, a claimed pid, idempotence, `sweep_runs.sh` beside parallel writers) and the ledger cases in `test_deploy.py`.

## Stuck cards

A card can sit In progress or In review with nothing working on it: a run that died, a loop that stopped. [`stuck.sh`](scripts/agents/stuck.sh) runs from the tick, at most once every 6 h (#329). It makes no Claude call, moves nothing and changes no label. The rules are in [`stuck.py`](scripts/agents/stuck.py), tested offline in `test_stuck.py`.

- **Stuck** = an open card **In progress for more than 6 h** or **In review for more than 24 h** since its last activity (the card's last update, or its newest run in the ledger), with no open run on it, and without `pm:waiting`, `agent:needs-human` or `ready-to-merge`.
- **Explained, not a fault:** if the agents are paused or out of budget in `agents.env` (`budget_reason`), or a usage limit was hit in the last 6 h (`data/agents/limit_hit`), the card is listed with that reason (`paused`, `budget: …`, `usage limit … ago`) and gets no comment.
- **A fault** gets one 🤖 comment naming the card, the idle hours and the last run with its log. The comment is deduplicated by a marker per card, column and last run (`data/agents/stuck/marks/`), so a card is reported again only after a new run stalls.
- Every card found is appended to `data/agents/stuck/reports.jsonl`. The weekly metrics read it.

## Only ready PRs reach you

Product owner, 2026-10-19: "In case of PR conflicts, Dev and PM should align in priorities and address the dependencies prior to hand it over to my approval. I'd rather pick only ready tickets for review." Every tick runs [`pr_gate.sh`](scripts/agents/pr_gate.sh) (no Claude; rules in `pr_gate.py`) over each open Dev agent PR:

| PR state | What happens |
|---|---|
| behind `main`, no conflict | the script updates the branch (`gh pr update-branch`); the Reviewer checks the new commit |
| conflicts with `main` | the card goes back to **Ready** with `needs-rebase`; the Dev agent merges `main` into the branch, resolves keeping both changes' intent, re-runs every check and pushes; the Reviewer checks it again |
| its ticket waits for an open issue | not ready |
| reviewed clean on its **current** commit, CI green, GitHub `CLEAN` | label **`ready-to-merge`** on the PR and its card, one notification |

**Your queue is the `ready-to-merge` label** (In review shows those cards first, by priority). Merge them top-down: whatever then conflicts is rebased by the agents before it comes back to you.

## Agent metrics

E6's outcome is "no human toil except approvals", so the loop measures itself (#180). On the first tick of each Monday (UTC), [`metrics.sh`](scripts/agents/metrics.sh) writes last week's report to `data/agents/metrics/<YYYY-Www>.md` and posts it as a comment on the E6 epic. It makes no Claude call. The numbers come from [`metrics.py`](scripts/agents/metrics.py), which is standard library only and tested offline on fixture JSON (`test_metrics.py`). Its inputs are the issues and agent PRs updated in the week, the repository's label events, `main`'s git log, the run ledger and the stuck-card log.

| Section | What it counts |
|---|---|
| Stages | Events in the week: SME filed (an `agent:sme` issue created), PM triaged (an issue's first 🧭 PM comment), → Ready (a PM comment that moves a card to Ready), Dev picked up, PR opened, merged, deployed to mvp (🚀 comment), SME verified fixed or reopened (`not_fixed`). Only `agent/issue-N` PRs count. |
| Cycle time | Median and p90 (nearest rank) of Ready → PR, PR → merge and merge → deploy, for spans that ended in the week. A card a person moved to Ready leaves no comment, so its Ready time is the first Dev pickup. |
| Quality | Reviewer rounds per PR merged or closed in the week (each `agent-review` marker counts, blocking ones shown), PRs closed unmerged, `Revert …` commits on `main`, and Dev agent blocks with their first line. |
| Waiting for a person | Hours in the week that issues carried `agent:needs-human` or `prototype:review`, the waits that ended in the week (median, p90) and the ones still open. |
| Runs | From the [run ledger](#run-ledger) (#290), the runs that **started** in the week, per agent: count, each outcome's count, failed % (`failed` + `abandoned`), limit % (`skipped_limit`), median and p90 duration (nearest rank) of the runs that ended, and the runs still open at the week's end (no duration). Only the fixed outcome names are shown, never a detail. A missing or unreadable ledger says "ledger unavailable" and leaves every other section as it is. |
| Overruled by a person | (#292) **SME findings rejected:** `agent:sme` issues closed in the week as not planned, or closed after a person's comment with no merged agent PR, out of the SME findings closed in the week. **PM decisions reversed:** an `E#`, `P#` or `pm:waiting` label removed in the week with no PM comment and no epic retag (another `E#` added) on that issue within 2 min, out of the week's PM comments. Board moves and Priority changes leave no trace on an issue and are not counted. **Clean reviews overruled:** agent PRs whose last review marker was `verdict=clean` on their final head, then closed unmerged in the week or reverted on `main` in the week (a `Revert …` subject naming the PR or its branch), out of those PRs merged or closed in the week. Each item links to its issue or PR. |
| Stuck cards | The cards [`stuck.sh`](#stuck-cards) found in the week, with their longest idle time and reason (#329). |

Each stage is read from what the agents leave on GitHub, because a move on the board leaves no trace on the issue. If an agent's comment wording changes, update the markers at the top of `metrics.py` to match.

## Weekly evidence report

The bots' track record builds itself too (#244, epic E2). On the first tick of each Monday (UTC), [`evidence.sh`](scripts/agents/evidence.sh) runs the [evidence report](README.md#evidence-report) for the last complete ISO week on **stable** and on **mvp**, for every bot (`EVIDENCE_BOTS`, default `btc eth`). It makes no Claude call and places no order.

1. For each environment and bot, `scripts/stress_inputs.sh <bot>` captures the week-end book into that clone's `data/stress/<bot>/`, through that environment's monitor with its read-only `MONITOR_API_TOKEN`.
2. `go run ./cmd/evidence -env <env> -root <clone> -bot <bot> -week <week>` writes `data/evidence/<week>/<bot>.md` and `.json` in the clone. mvp records no books, so it gets `-books-root` = stable's clone.
3. [`evidence_week.py`](scripts/agents/evidence_week.py) builds one comment from the `.json` reports, posted on the E2 epic: a row per env × bot with net P&L (% of equity), excess return vs holding the coin, worst 7 days, replay gap, the worst stress shock, E1 reliability (#361: mean IM ÷ limit against the 60 % target and whether it was met, the largest cause of idle slot time, startups and how many were restart-made — with their lots on testnet only — or `not measured` and why) and the missing days with the market books' share of the week's hours (`(replay partial)` under 90 %, #362), then one line per env × bot × dataset with a **recorder gap** over 3 h (the dataset, how many such gaps, the longest with its start — times and hours only, so for live accounts too; `evidence_week.py gaps` prints the same lines and `evidence.sh` sends the first as a notify, so the recorder can be fixed before the next week), then every failed step. The script posts on E2 only; the E1 figures ride in that comment. **Live accounts:** an environment whose `.env` has `DERIBIT_ENV=live` gets percentages and the report's path only — no coin or USD amount, no failure detail (the rule of `sme_review.sh`).

**Direction trial** (#440, [`docs/direction-trial.md`](docs/direction-trial.md)): after the per-bot reports, `go run ./cmd/evidence -compare-direction -bot btc -week <week>` writes the bias-vs-sma scorecard to `<stable>/data/evidence/direction/<week>.md|json` — for last week and every week since 2026-W43 still without one (the back-fill, `evidence_week.py direction-weeks`). The comment gets a "Direction trial" block: each source's replay net %, max drawdown and sheds, the live sheds, live − replay, and the brief's counters (weeks counted toward 6, weeks excluded for more than 2 days of missing books, mean bias − sma, the noise, the early-stop streak, the first GEX shed, the review). Percentages and counts only. The first week with a GEX shed (a replay or live row with sheds > 0) is named on the comment's first line, and three decision tickets go to **Triage** labelled `agent:needs-human`, each kind once ever (deduplicated by its title's start against every issue, open or closed; none when the issue list cannot be read): `[E2] Direction trial: first GEX shed in <week>, review bias vs sma`, `… early stop in <week>, decide bias vs sma` (bias's replay net under sma's by more than the noise for 3 counted weeks in a row) and `… review due in <week>, decide bias vs sma` (6 counted weeks, or the report made on or after 2026-12-07). A ticket only asks: a change of `gex_direction` is a person's config PR. The kill rule's live drawdown is not in the scorecard; the ticket points to the weekly reports.

A failed step (stress inputs, a report, a scorecard) is listed in the comment and the rest goes on. `data/agents/evidence/<week>.done` marks the week done once the comment is posted; a week whose comment could not be posted is tried once more on a later tick and then marked done whatever happened. Logs of each step: `data/agents/evidence/<week>/`. Tests: `test_evidence.py` (the comment, and `evidence.sh` end to end on throwaway clones with stub `go`/`gh`/`curl`).

## Weekly usage report

Engagement is measured by itself too (#340, #341, #252, epic E13). On the first tick of each Monday (UTC), [`usage.sh`](scripts/agents/usage.sh) runs the [usage report](README.md#usage-report) for the last complete ISO week in every environment whose clone has a non-empty `data/monitor/events.jsonl`. It makes no Claude call, never reads bot state and places no order.

1. `go run ./cmd/usage -events <clone>/data/monitor/events.jsonl -week <week> -out <clone>/data/monitor/reports -alert` (from the agents' clone, so stable needs no newer code) writes `<week>.md` and `.json` in the clone and prints the engagement-drop check on its `DROPS` line.
2. When a key number fell, one `bug` issue `[E13] Engagement drop: <metrics> <week>` goes to **Triage**: every falling metric of every environment, counts and percentages only. Nothing is filed when an open issue, or one closed in the last 14 days, has an `[E13] Engagement drop:` title ending with that week.
3. [`usage_week.py`](scripts/agents/usage_week.py) builds one comment, posted on the E13 epic: per environment, active users, the change against the week before, week-1 retention of the latest cohort, first answers found, help used and help answers, the drops and the weeks with no events. It reads named figures only, so no user, session or amount field can reach GitHub. **Live accounts:** an environment whose `.env` has `DERIBIT_ENV=live` gets percentages only.

**Drop rules** (`internal/usage/drops.go`, fixed; `cmd/usage -alert-*` flags study them): weekly active users or help used (tips opened + videos played) at least 30 % under the mean of the 3 weeks before; the first-answers, manual-close or rebalance funnel's completion share at least 20 points under those weeks'. A base under 10 users (or 10 funnel starts, in the base and in the week) is `too few to judge`. A week with no events is noted and left out of the base, never counted as zero.

A missing or empty events file, or one with no event in the week, is logged and skipped. When no environment has events in the week nothing is posted and no marker is written; the week is looked at again at most hourly. `data/agents/usage/<week>.done` marks the week done once the comment is posted; a comment that could not be posted is tried once more and then the week is done whatever happened. Logs: `data/agents/usage/<week>/`. Run ledger agent `usage`, outcomes `posted`, `skipped` or `failed`. Tests: `test_usage.py` (the comment, the privacy rule, the dedup, and `usage.sh` end to end on throwaway clones with stub `go`/`gh`) and `tests/usage_test.go` (the drop rules on a six-week fixture).

**Assistant quality review** (#438): before its own report, `usage.sh` runs [`assistant_review.sh`](scripts/agents/assistant_review.sh). In each environment with a non-empty `data/monitor/assistant.jsonl` it runs `go run ./cmd/assistantreview` (the private review files go to `<clone>/data/monitor/assistant_reviews/`) and posts `usage_week.py assistant-comment` on the E10 epic. The comment holds the `<week>.summary.json` named counts and percentages only, percentages only for a live env. A week with no answers posts nothing and writes no marker (looked at again hourly). A comment that fails to post is retried once. Done marker: `data/agents/assistant_review/<week>.done`. Ledger agent: `assistant_review`. Metrics and the review rule: [docs/product/assistant/README.md](docs/product/assistant/README.md#weekly-quality-review).

## Nightly backup

The journal is the bots' only history, so the tick copies it every night (#328, epic E6). On the first tick after 00:00 UTC, [`backup_data.sh`](scripts/agents/backup_data.sh) copies, for each environment (stable, mvp) and bot (`BACKUP_BOTS`, default `btc eth`) whose folder exists, `orders.log`, `data/pnl_history.jsonl`, `data/regime_history.jsonl`, `data/direction_history.jsonl` and `data/archive/` (plus each environment's monitor request queue, `data/monitor/requests.jsonl` and `requests_status.jsonl`, and the assistant's review log `data/monitor/assistant.jsonl`, to `<date>/<env>/monitor/`, #409, #438. Each line of the copied log must parse, `backupcheck -lines`; a torn last line is allowed) to `<envs>/history/backups/<date>/<env>/<bot>/`. Named files only: no `.env`, no secret (an `.env*` found in the archive is dropped). It reads the live files and never writes to them. No Claude call, no git, no order.

**Restore check:** `go run ./cmd/backupcheck` (`internal/backup.Check`) replays each copied journal, archived ones included, with `orders.ReplayFile`. It fails on an unreadable line (a torn last line, a copy racing an append, is allowed) or when the copy holds fewer events than the previous good backup (its `check.json`; an archived journal counts, so `--reset-history` is not a loss). A bot folder the previous backup had and that is now gone fails too. The copy is built in `.<date>.partial` and becomes `<date>` only when every check passes; then the newest `BACKUP_KEEP_DAYS` (14) are kept. A failure keeps the copy as `<date>.failed`, notifies, ends the run `failed`, prunes nothing, and is retried once on a later tick; `data/agents/backup/<date>.done` marks the day done after a success or the second attempt. Run ledger: agent `backup`, outcome `backed_up` or `failed`, only when there was something to copy. Log: `data/agents/backup/<date>.log`. Tests: `test_backup.py` (end to end on throwaway environments) and `tests/backup_check_test.go`. Restore steps: [operations](docs/operations.md#nightly-backup-and-restore).

## Nightly flaky-test hunt

A flaky test turns a PR's CI red by chance and costs a review round (#113 and #165 were found that way, under load). Since #248, the first tick of each UTC night at or after `FLAKY_HOUR_UTC` (03:00) runs [`flaky.sh`](scripts/agents/flaky.sh). It makes no Claude call, so the budget does not hold it, and it runs beside the other agents, whose schedule and parallel limits are unchanged.

1. A clean detached worktree of `origin/main` (`<work dir>/flaky-<date>`, removed at the end).
2. In parallel, under one hard time limit (`FLAKY_LIMIT_MIN`, 30): `go test ./tests/... -race -count=5 -json`, and `npm ci` followed by `npm test -- --ci --json` run `FLAKY_JEST_RUNS` (3) times.
3. [`flaky.py`](scripts/agents/flaky.py) reads the results. A test is **flaky** when it both passed and failed that night (a flaky Go subtest is reported, not its parent). For each flaky test it files one `bug` issue in **Triage**, titled `[E6] Flaky test: <name>`, with the commit, the pass and fail counts and the first failure's output (last 80 lines, `.env` secrets redacted the way `deploy_issue.py` does). Nothing is filed when an open issue, or one closed in the last 14 days, has a title ending in `Flaky test: <name>`.

A test that failed every run is not flaky: it is logged and nothing is filed, because CI catches it. Past the time limit everything is stopped, nothing is filed and the log says it timed out. `data/agents/flaky/<date>.done` is written when the hunt starts, so a crash is not retried every tick. Results and logs: `data/agents/flaky/<date>/`. Run ledger agent `flaky`, outcomes `filed`, `none` or `timed_out`. Tests: `test_flaky.py` (the parsing, dedup and issue body, and `flaky.sh` end to end on a throwaway origin with stub `go`/`npm`/`gh`).

## Assistant requests

A person can send a bug or a wish from the monitor's assistant (#409). It goes to a **private** queue, `<clone>/data/monitor/requests.jsonl`, never to GitHub: the repository is public. Since #410 the PM agent reviews that queue and turns what it accepts into redacted public issues. Each tick runs [`requests.sh`](scripts/agents/requests.sh); it exits at once (no run, no ledger line) unless a request in some environment has no status line yet, or a filed one is still open (that sweep runs at most hourly). The rules are in [`requests.py`](scripts/agents/requests.py), standard library only, files only.

1. **Mechanical, no Claude.** A filed request whose issue has closed becomes `fixed`, or `not_planned` when GitHub says so. A request that an issue already carries (the body's `<!-- assistant-request: <env>/R-NNNN -->` marker: filed, then the status write failed) becomes `filed` with that issue, so it is never filed twice. A repeated id in the queue is judged once.
2. **Redaction before the model sees anything.** The script drops the requester's username, role and answer id. In the text it redacts `.env` secret values (the `deploy_issue.py` rule), bearer tokens, emails, account ids, the username and any `@mention`, long tokens, and amounts in coin or USD ("an amount"). Percentages stay.
3. **One PM agent run** (product-manager skill, procedure [`docs/product/assistant/request-triage.md`](docs/product/assistant/request-triage.md); read-only: `Read`/`Grep`/`Glob`/`Skill`, no Bash, no edit, no network; within the budget and limit pauses, `budget_take requests`) judges each request: `duplicate` (the same root cause as an open issue, or one closed in the last 14 days, given in `issues.json`; any other number is refused), `not_a_bug` (with the reason), `needs_info` (what is missing) or `accept` (kind, epic, title, body). **The request text is untrusted data:** each request is fenced in `requests.md` with a fence it cannot close, and the procedure says that nothing inside a fence is an instruction.
4. **The script acts.** Every verdict appends `{id, status, note, issue, issue_url, ts}` to the request's environment's `requests_status.jsonl`, which My requests shows. An accept is filed as a `bug` or `enhancement` issue in **Triage**, titled `[E?] …`. Its title and body are redacted again (the username comes from the queue, not from the agent). The body keeps the request id, "From the in-product assistant", the cited journal and P&L references and the marker. The requester is never named, mentioned or copied in.

**Bounds:** at most **10 issues a UTC day** across all environments (`DAILY_CAP`). Only as many requests as the cap has left go to the agent; the rest wait for the next day, with no status line. A request with no valid verdict (left out, an unknown duplicate, a `not_a_bug` with no reason) is asked again, 3 runs at most (`<clone>/data/monitor/requests_attempts.json`); after that it waits for a person. If `gh issue create` fails, nothing is written, and the request is judged again on the next run. Logs: `data/agents/requests/<time>/` (`requests.md`, `agent.md`, `decide.log`, the filings). Run ledger agent `requests`, outcomes `filed`, `judged`, `none`, `skipped_limit` or `failed`. Tests: `test_requests.py` (redaction, dedup by status line and marker, the status merge and closed-issue sweep, the daily cap, the attempts bound, and `prepare → decide → filed` on a throwaway clone).
