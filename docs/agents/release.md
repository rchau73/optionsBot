# Automatic release

Part of [Agentic development](../../AGENTIC.md).

Every release since v0.1.0 has been the newest mvp build that soaked 3 days, so the agents make it (#471, epic E6, product owner 2026-10-28); the soak, the `sme:not-fixed` check and the rollback stay in [`promote.sh`](../../scripts/promote.sh).
After a healthy deploy step, each tick runs [`release.sh`](../../scripts/agents/release.sh) (rules in pure `release.py`, no Claude), which calls `promote.sh <next patch> --commit <sha> --yes` from the mvp clone:

- **Weekly:** the first tick of an ISO week (Monday UTC, `data/agents/release/<week>.done`) releases the newest healthy mvp deploy (never a `rollback` line) at least 3 days old that stable does not contain yet.
  Nothing to release, or a block, is a one-line note on the E6 epic saying why.
  Once a week either way.
- **Hotfix** (any tick): a merged PR labelled `bug` + `release:hotfix`, or one that closes an issue with both labels, is released once the first mvp deploy containing it has been live 1 h (`PROMOTE_HOTFIX_HOURS`) — still tested on mvp.
  The newer of a hotfix and the weekly build goes.
- **Escalation (admin only):** a person runs `scripts/promote.sh <tag> --commit <sha> --escalate "<reason>"`; the soak is skipped, the reason is required.
  (From the monitor, the audited Admin route is a later story; it will pass the admin as `PROMOTE_BY`.)
- **Every path** refuses a commit that never ran in mvp and stops on an open `sme:not-fixed`.
  Each healthy release writes `stable … release <tag> <trigger> <who> <why>` to `deploys.tsv` (trigger `manual`, `weekly`, `hotfix` or `escalation`), which the delivery report reads; `release.sh` notifies and comments on E6.
  A failed promotion (stable went back) is not retried for that commit (`data/agents/release/failed-<sha>`).
- **Off:** `AUTO_RELEASE=0` in `agents.env`.
  `RELEASE_DRY_RUN=1 scripts/agents/release.sh` prints what it would do and changes nothing.
  Ledger agent `release`.

Tests: `scripts/agents/test_release.py` (the rules, and `release.sh` on a throwaway origin and stable clone) and `test_promote.py`.
