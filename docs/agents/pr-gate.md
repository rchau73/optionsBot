# Only ready PRs reach you

Part of [Agentic development](../../AGENTIC.md).

Product owner, 2026-10-19: "In case of PR conflicts, Dev and PM should align in priorities and address the dependencies prior to hand it over to my approval. I'd rather pick only ready tickets for review."
Every tick runs [`pr_gate.sh`](../../scripts/agents/pr_gate.sh) (no Claude; rules in `pr_gate.py`) over each open Dev agent PR:

| PR state | What happens |
|---|---|
| its card is **In progress** | wait, "a Dev agent is working on it", before every other rule: no update, no send-back, never `ready-to-merge` or merged (#478) |
| a draft whose card is not **In review** (Blocked, Ready) | wait: a person or the Dev agent has it |
| CI **red** on the head, card **In review** (#472) | only tests the PR does not touch failed → the failed jobs are re-run (3 times a head at most; green afterwards → each named **flaky** on the PR and its flaky-test issue). Anything else → back to **Ready** with `needs-fix` and the failed jobs' log tails, once per head; the card's second check failure → **Blocked**, `agent:needs-human`. Never `ready-to-merge` or merged |
| a **draft** In review, CI green | marked ready (`gh pr ready`): the Reviewer starts on it now |
| behind `main`, no conflict | the script updates the branch (`gh pr update-branch`). A clean review carries over GitHub's own update merges (`pr_gate.py equiv`: only 2-parent "Merge branch 'main' into …" commits by GitHub after the reviewed one), so the Reviewer reads only commits with new code |
| conflicts with `main` only in generated files (`.gitattributes` `linguist-generated`, #465) | the gate merges `main` into the branch itself, reruns the generators (`scripts/regen.sh --resolve`) and pushes; the Reviewer checks the new commit |
| conflicts with `main` | the card goes back to **Ready** with `needs-rebase`; the Dev agent merges `main` into the branch (a conflict in a generated file is regenerated, never edited), resolves keeping both changes' intent, re-runs every check and pushes; the Reviewer checks it again |
| its ticket waits for an open issue | not ready |
| its review on the current commit is `incomplete` (a changed file unread, #476) | wait, "not fully reviewed": not reviewed at all, whatever else it says |
| reviewed clean on its **current** commit, CI green, GitHub `CLEAN`, and it **changes a business rule or policy** (a path in `POLICY_PATHS`, or the Reviewer's `policy=yes`; a review without the declaration counts as yes), or `PR_AUTO_MERGE=0` | label **`ready-to-merge`** on the PR and its card, one notification naming why it is yours |
| the same, **policy-free** | updated with `main` first if it is behind; then **merged by the gate** (`gh pr merge --merge --match-head-commit`), at most one per run, so the next one is tested on the new `main`. Comment and notification; ledger outcome `merged`. mvp redeploys it on its next tick |

**A PR that merged while the Dev agent fixed it** (#478: PR #474 merged at 01:48 UTC while the agent fixed two review findings; the fixes were pushed to its branch and never reached `main`): before pushing, and again after the checks and after the push, `dev_agent.sh` re-reads the PR's state.
Merged → `rescue_commits.sh` cherry-picks the run's new commits (not merges, not what `main` holds) onto `agent/issue-<n>-r<k>` from `origin/main`, the checks run again there, and a new PR `Refs #n` carries the same `## Review response`;
the issue and the merged PR get a comment linking it (a fix that does not apply on `main` stops and asks; nothing left to move → a comment, no PR).
Closed unmerged → stop and ask.
"Pushed changes" is commented only once the PR's head on GitHub is the pushed commit.
The Dev agent, the Reviewer (`lib.sh open_dev_pr`) and the gate treat an `agent/issue-<n>-r<k>` PR as issue n's.

**Your queue is the `ready-to-merge` label:** only PRs that change a policy (In review shows those cards first, by priority).
Merge them top-down: whatever then conflicts is rebased by the agents before it comes back to you.

**Policy paths** (`POLICY_PATHS`): trading rules (`internal/strategy/`), margin policy (`internal/risk/`), settings (`internal/config/`, `config_*`, the effective-config goldens), `OptionStrategy.md`,
the order path (`internal/orders/`, `internal/gateway/`, `internal/exchange/`, `cmd/bot/`), the assistant's guard, roles (`frontend/lib/auth/`),
`docs/product/`, `docs/legal/`, `.github/`, `.githooks/`, `.claude/skills/`, releases (`scripts/promote.sh`), the coverage floor, the merge rules (`pr_gate.*`), deploys and the agents' budget.
The Reviewer's own judgement covers what a path can't, such as a POST route.
