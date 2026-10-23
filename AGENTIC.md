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
| ④⑤ | **Build** | Dev agents, every 6 min, up to 3 in parallel | Each takes the top free Ready issue (Priority, then oldest; one instance per card) and implements it on its own branch, in an isolated worktree, with tests and docs. Then it commits. |
| ⑥ | **Verify & propose** | script | Re-runs every check itself (gofmt, vet, race tests, frontend lint/test/build), pushes, and opens a PR that closes the issue → **In review**. If checks fail, or the agent stops with questions → **Blocked**, with the questions on the issue. |
| ⑦ | **Review** | Reviewer agents, every 6 min, up to 3 in parallel | Reviews each new commit of a Dev agent PR as a second engineer: correctness, the trading rules, whether every acceptance scenario has a real test. It checks each finding against the code before reporting it, then posts one review with inline comments. A **blocking** finding moves the card back to **Ready**, and the Dev agent fixes it on the same PR and answers each finding in the PR body. After 2 fix rounds the human decides. No blocking finding → the PR is labelled `agent:reviewed`. So when the human opens a PR, the review loop has already run. |
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

**Talking to the PM: answer on the ticket.** The PM asks on `agent:pm` tickets. To answer, or to give it new work, comment on that ticket (or on its PRD pull request), or file an issue labelled `agent:pm`. Within 6 minutes [`pm_answer.sh`](scripts/agents/pm_answer.sh) picks up the newest person's message on it. Claude runs with the same skill in a worktree, allowed to edit only `docs/product/`. The script checks that nothing else changed and that the epic markers parse. Then it either opens or revises the PRD pull request (`Closes #n`, card → In review) or replies on the ticket (questions add `agent:needs-human`). The merge of that pull request approves the PRD. **Who merges** (product owner on #151): the PM itself merges a PRD pull request that changes no strategy policy. It gets the label `pm:self-merge`, and the script merges it once CI is green and GitHub reports it clean (`pm_board.py selfmerge`, re-checked on the exact head commit). A **strategy policy** change gets `agent:needs-human` and waits for your merge: an approved epic gaining trading-rule areas (`pm_board.py policy`, checked by the script), or a change the agent declares on its `POLICY:` line (margin, exits, entry, repair, GEX, orders, kill switch, the autonomy line; a missing line counts as a policy change). The declaration is also written into the PR body (`<!-- pm:policy=yes|no -->`) and `selfmerge` reads it again at merge time, so a PR without `pm:policy=no` is never self-merged, whatever its labels say. Taking the label off before a reply, a revision or a failure is a precondition: if GitHub refuses, the run stops before your message is marked handled, and the waiting message keeps blocking the merge. To stop a self-merge, remove the `pm:self-merge` label. Commenting also stops it: the PM acts on your message first, and if it only replies, the label comes off. There is nothing to trigger from chat. Every comment comes from one GitHub account, so the agents' comments carry a header (🧭 PM, 🤖 Dev/Reviewer, 🚀 deploy), and anything without one is read as yours.

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
| Trigger | daily, launchd | every 6 min, a new or changed **Triage** card, a card with no epic, an epic with fewer than 2 moving stories, a `pm:waiting` card to release, or a person's answer on an `agent:pm` ticket (up to 2 answers in parallel) | every 6 min, a card in **Ready** (skipping one that waits: "Depends on #N" while #N is open, "Waits until <date>"), up to 3 in parallel | every 6 min, an `agent:dev` PR in **In review** with an unreviewed commit, up to 3 in parallel | every 6 min, `main` moved |
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
scripts/agents/install.sh             # launchd: SME daily at 09:00; tick every 6 min (deploy, then review/PM/Dev in parallel; Mondays also metrics and evidence)
launchctl kickstart gui/$(id -u)/com.optionsbot.sme-review   # run a review now
scripts/agents/install.sh --uninstall
```

Runs on the Mac that runs the bots, logged in to Claude Code and `gh`, **from the mvp clone** (`~/dev/go/optionsBot-env/mvp/scripts/agents/install.sh`), so every deploy also updates the agents' scripts. Logs: `data/sme/<date>/` (reports, collected data) and `data/agents/` (dev runs, reviews, deploys, launchd).

## Trade-offs and next steps

- **One GitHub identity.** The agents act with the human's `gh` login, so the separation of duties is enforced by tool permissions and the ruleset, not by GitHub identity. Next step: a GitHub App or a machine user for the agents, with "require one approval" on `main`, so GitHub itself enforces that someone other than the author approves.
- **One machine.** The bots, the monitor and the agents run on one Mac (launchd catches up after sleep). Moving the bots to a server would move the agents with them; moving only the Dev agent to GitHub Actions is possible, but deploy and the SME agent need the bots' local API.
- **Measure the agents.** Weekly report: see [Agent metrics](#agent-metrics) below. Still not measured: findings the human overrules.

---

*Educational study of an automated trading system — not investment advice. Options trading can lose more than the premium collected; crypto markets are highly volatile.*

## Parallel runs and limits

`tick.sh` runs every 6 minutes (launchd, `AbandonProcessGroup`) and starts in the background up to `DEV_PARALLEL` (3) Dev agents, `REVIEW_PARALLEL` (3) Reviewers and `PM_ANSWER_PARALLEL` (2) PM answers, plus one PM agent. Each instance claims its card (`data/agents/claims/<kind>-<n>`, holding its pid — free the moment the process ends), picks under a short mutex so two never take the same card, and serialises git fetch/worktree/push in the shared clone (`git_locked`). Independent cards are built at once; dependent ones wait in Blocked (`pm:waiting`).

**Limits adapt by themselves** (product owner, 2026-10-19: "find the optimum value"): when a run hits the Claude usage limit or GitHub's rate limit (`note_limit`), no new Claude run starts for 30 minutes, then one at a time for an hour, then the configured parallelism again (`parallel_max`). A Dev card that hit a limit goes back to Ready, not Blocked; a PM answer stays due. Set the `*_PARALLEL` variables in the launchd plist to change the ceilings.

**A limit never looks like a question** (#237: Claude's "You've hit your weekly limit · resets …" was not in the pattern, and 28 approved cards were labelled `agent:needs-human` and moved to Blocked in 2–5 s each). The wordings live in one place, `LIMIT_PATTERN` in [`limit_lib.sh`](scripts/agents/limit_lib.sh) (weekly, session and usage limits, the generic `hit your … limit` and `limit · resets` forms, GitHub's rate limits). As a second guard, `note_stop` treats any run that exits non-zero in under 60 s with no answer block (`RESULT:`, `REVIEW-JSON`, `PM-…`) like a limit: the pause starts, the Dev card goes back to Ready without `agent:needs-human`, a review or PM answer is retried. The Dev agent posts one limit comment per episode: none when the card's last comment (its own pickups aside) already is one. Each tick, before the Dev agents start and never inside a pause, [`requeue.sh`](scripts/agents/requeue.sh) re-queues the cards such a limit blocked before this guard existed (rules in `requeue.py`, tested in `test_requeue.py`): an open Blocked card with `agent:needs-human`, without `pm:waiting`, whose newest Dev stop says "the agent did not finish", with no person's comment after it, and whose newest run log (`data/agents/dev/issue-<n>-*/agent.md`) matches `LIMIT_PATTERN`, loses the label, moves to Ready and gets one comment ("🤖 Re-queued: the earlier stop was a usage limit, not a question."). A stop that carries the agent's questions, or that a person answered, stays with the person.

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

E6's outcome is "no human toil except approvals", so the loop measures itself (#180). On the first tick of each Monday (UTC), [`metrics.sh`](scripts/agents/metrics.sh) writes last week's report to `data/agents/metrics/<YYYY-Www>.md` and posts it as a comment on the E6 epic. It makes no Claude call. The numbers come from [`metrics.py`](scripts/agents/metrics.py), which is standard library only and tested offline on fixture JSON (`test_metrics.py`). Its inputs are the issues and agent PRs updated in the week, the repository's label events and `main`'s git log.

| Section | What it counts |
|---|---|
| Stages | Events in the week: SME filed (an `agent:sme` issue created), PM triaged (an issue's first 🧭 PM comment), → Ready (a PM comment that moves a card to Ready), Dev picked up, PR opened, merged, deployed to mvp (🚀 comment), SME verified fixed or reopened (`not_fixed`). Only `agent/issue-N` PRs count. |
| Cycle time | Median and p90 (nearest rank) of Ready → PR, PR → merge and merge → deploy, for spans that ended in the week. A card a person moved to Ready leaves no comment, so its Ready time is the first Dev pickup. |
| Quality | Reviewer rounds per PR merged or closed in the week (each `agent-review` marker counts, blocking ones shown), PRs closed unmerged, `Revert …` commits on `main`, and Dev agent blocks with their first line. |
| Waiting for a person | Hours in the week that issues carried `agent:needs-human` or `prototype:review`, the waits that ended in the week (median, p90) and the ones still open. |

Each stage is read from what the agents leave on GitHub, because a move on the board leaves no trace on the issue. If an agent's comment wording changes, update the markers at the top of `metrics.py` to match.

## Weekly evidence report

The bots' track record builds itself too (#244, epic E2). On the first tick of each Monday (UTC), [`evidence.sh`](scripts/agents/evidence.sh) runs the [evidence report](README.md#evidence-report) for the last complete ISO week on **stable** and on **mvp**, for every bot (`EVIDENCE_BOTS`, default `btc eth`). It makes no Claude call and places no order.

1. For each environment and bot, `scripts/stress_inputs.sh <bot>` captures the week-end book into that clone's `data/stress/<bot>/`, through that environment's monitor with its read-only `MONITOR_API_TOKEN`.
2. `go run ./cmd/evidence -env <env> -root <clone> -bot <bot> -week <week>` writes `data/evidence/<week>/<bot>.md` and `.json` in the clone. mvp records no books, so it gets `-books-root` = stable's clone.
3. [`evidence_week.py`](scripts/agents/evidence_week.py) builds one comment from the `.json` reports, posted on the E2 epic: a row per env × bot with net P&L (% of equity), excess return vs holding the coin, worst 7 days, replay gap, the worst stress shock and the missing days, then every failed step. **Live accounts:** an environment whose `.env` has `DERIBIT_ENV=live` gets percentages and the report's path only — no coin or USD amount, no failure detail (the rule of `sme_review.sh`).

A failed step (stress inputs, a report) is listed in the comment and the rest goes on. `data/agents/evidence/<week>.done` marks the week done once the comment is posted; a week whose comment could not be posted is tried once more on a later tick and then marked done whatever happened. Logs of each step: `data/agents/evidence/<week>/`. Tests: `test_evidence.py` (the comment, and `evidence.sh` end to end on throwaway clones with stub `go`/`gh`/`curl`).
