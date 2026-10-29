# Run ledger

Part of [Agentic development](../../AGENTIC.md).

With up to 3 Dev and 3 Reviewer runs in parallel, the logs under `data/agents/<agent>/<run>/` have no single index.
Since #206 every SME, PM, PM-answer, Dev, Reviewer, PR-gate, deploy, flaky-test-hunt, backup, usage-report and assistant-request run appends a `start` and an `end` JSON line to `<envs>/history/agent_runs.jsonl` (beside `deploys.tsv`, outside both clones):

```json
{"ts":"2026-10-22T23:10:04Z","agent":"dev","run_id":"dev-20261022T225348Z-4242-1","issue":206,"pr":271,"event":"end","outcome":"pr_opened","detail":"checks passed","log":"data/agents/dev/issue-206-20261022-225348","commit":"1a2b3c4"}
```

- **Helpers** (`lib.sh`): `run_begin <agent> [issue] [pr] [log]`, then `run_end <outcome> [detail]`; a script sets `RUN_PR`/`RUN_COMMIT` when it learns them (the PR it opened, the commit it pushed, reviewed or deployed; otherwise `commit` is the agents' own code).
  Each line is built by `runs.py line` and written with one `printf` under the `agent-runs` mutex, so parallel runs never interleave.
  The ledger never stops an agent: a line it cannot write is logged and skipped.
- **Every exit ends the run:** the `EXIT` trap writes `failed` (detail `exit <code>`) for a run that dies under `set -e`, and `done` for one that exits 0 without an outcome.
  Only a hard kill leaves a run open: `runs.py --open` lists those (and the runs still going), and the sweep below closes them.
- **The sweep** (#291): each tick, before any new run starts, [`sweep_runs.sh`](../../scripts/agents/sweep_runs.sh) runs `runs.py --sweep` under the `agent-runs` mutex.
  An open run whose pid (the one in its `run_id`, `<agent>-<time>-<pid>-<n>`) no longer exists gets an `end` line, outcome `abandoned`, detail `process gone`, built by the same `make_line` as `run_end`.
  A claim holds that same pid, so a dead run's claim is free too, and a live pid is a live run, claimed or not.
  A live run past its agent's time limit (`TIME_LIMIT_MIN` in `runs.py`: dev 120 min, review 60, sme 60, pm and pm_answer 30, deploy 30, backup 30, flaky 45, usage 20, requests 30, pr_gate 20; tune them from the weekly metrics' p90) is reported **once** — a marker `data/agents/runs/flagged/<run_id>` and a notification naming the agent, the card and the log.
  **Nothing is ever killed.**
  A sweep failure is logged (`data/agents/tick-sweep.log`) and the tick goes on.
- **Outcomes:** Dev `pr_opened`, `pr_updated`, `sent_back` (its checks failed: Ready with `needs-fix`, #472), `blocked`, `skipped_limit`;
  Reviewer `reviewed_clean`, `changes_requested`, `incomplete`, `failed`, `skipped_limit`;
  PM `triaged`, `stories_filed`, `failed`, `skipped_limit`;
  PM answer `replied`, `prd_pr_opened`, `prd_pr_updated`, `prd_merged`, `blocked`, `skipped_limit`;
  SME `reported`, `failed`, `skipped_limit`;
  PR gate `sent_back`, `marked_ready` (a green draft), `ci_rerun` (untouched tests failed), `branch_updated`, `ready_to_merge`, `ready_removed`, `merged`, `failed`;
  deploy `deployed`, `rolled_back`, `skipped`, `failed`;
  flaky-test hunt `filed`, `none`, `timed_out`, `failed`;
  usage report `posted`, `skipped`;
  backup `backed_up`, `failed`;
  assistant requests `filed`, `judged`, `none`, `skipped_limit`, `failed`;
  any agent `abandoned` (the sweep: its process was gone).
- **Only work is a run:** a tick where an agent finds nothing to do writes nothing.
  The PM agent is a run when it asks Claude (triage or planning), the PR gate once per PR it changed, the deploy once per new commit it tries.
- **Paths and ids only:** no secrets, no amounts, no prompt text; the detail is a short fixed phrase (≤ 200 characters).

```bash
python3 scripts/agents/runs.py --issue 206          # every run that touched #206, oldest first
python3 scripts/agents/runs.py --agent dev --date 2026-10-22
python3 scripts/agents/runs.py --open               # crashed (killed) or still running
python3 scripts/agents/runs.py --pr 271 --json
```

Tests: `test_runs.py` (line format, queries, the helpers sourced for real — a crash, six parallel runs; the sweep: a killed run, a live run under and over its limit, a claimed pid, idempotence, `sweep_runs.sh` beside parallel writers) and the ledger cases in `test_deploy.py`.
