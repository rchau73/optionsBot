# scripts/agents

Folder notes for `CLAUDE.md`: the agentic workflow (`AGENTIC.md` is the full description).

```bash
go run ./cmd/usage -events data/monitor/events.jsonl -week 2026-W43   # monitor usage and cohort report from the analytics events (#295): active users, retention, panels, first answers, help, funnels, trust — counts only, no user/session id → data/monitor/reports/<week>.md|json (the JSON carries the week's drop check as `drops` and the NPS reasons by score, `why_best`/`why_improve`, for the monitor's admin Analytics page, #445); `-alert` also prints that check (a `DROPS {json}` line, #341) (README "Usage report")
go run ./cmd/dora -main main.tsv -tags tags.tsv -prs prs.json -issues issues.json -deploys ~/dev/go/optionsBot-env/history/deploys.tsv -promotions ~/dev/go/optionsBot-env/history/promotions.tsv -out ~/dev/go/optionsBot-env/history/delivery   # delivery report (#470, pure internal/delivery, files only — dora.sh dumps git/gh daily): DORA metrics per ISO week × mvp/stable with bands ("too few samples" under 3; "not recorded" before deploys.tsv began, never 0) and every PR's merge, mvp-live and stable-live time and release → dora.json + <week>.md; failures = rollback lines, "[E6] mvp deploy failed" issues, Revert PRs, `regression` bugs with "Caused by #N"; malformed/off-main lines reported, never counted (AGENTIC.md "Delivery report (DORA)")
```

- **Agentic workflow** (`AGENTIC.md`, `scripts/agents/`):
  - the SME agent (`sme_review.sh`, daily, read-only, option-bot-specialist skill) files findings as `agent:sme` issues on the project board's Triage column
    (never a duplicate: it sees every open issue — any label, with body and last comments — and every one closed in 14 days,
    and reports a known problem as `existing` + `update`, commented only when something is new, so tickets parked in Triage for data are not re-filed;
    `sme_issues.py` routes each finding: agent's `existing`, then `sme-fingerprint` on open/closed issues, then a near-identical-title guard; 2026-10-16 #106/#107 re-filed #86/#103;
    verifies closed issues, reopens if not fixed; amounts redacted if a bot is live);
  - the PM agent (`pm_agent.sh`, every 6 min in the tick, read-only, product-manager skill) reads every new Triage card first,
    places it in exactly one epic (sub-issue of an `epic` issue it keeps in sync with the approved PRD `docs/product/PRD.md` on main
    — a person's merge of a PRD PR is the approval of the roadmap and business requirements),
    sets a Priority (P0–P3) and moves to Ready only what `pm_board.py gate` allows:
    - an approved epic, a trading-rule area the epic lists (`rule_areas`), a `prototype:approved` epic for UI stories (a label only a person adds; prototypes come from the ux-designer skill as private Artifacts)
    - — everything else stays in Triage for a person (`agent:needs-human`);
    - it moves out of Ready only cards it put there and of In progress/In review/Blocked/Done touches only its own `pm:waiting` cards;
    - it drives every approved epic (product owner 2026-10-19): an epic with no moving story gets one PR-sized next story
      ("Epic planning", once a day at most, `pm_board.py plan`/`stories`; a title close to any card of the epic, open or closed, is dropped — `duplicate_of`;
      product owner 2026-10-24: related work is bundled in one ticket, one PR, to save the Claude plan),
    - a card that must wait ("Depends on #N" / "Waits until YYYY-MM-DD", `pm_board.py wait`) goes to Blocked + `pm:waiting` and is released to Ready when free (→ Triage if a dependency closed as not planned),
    - every card under an epic gets its `E#` label and `[E#]` title prefix (`tags`), Ready/In review/Triage are ordered (`order`: `ready-to-merge` first in In review, people last in Triage);
    - a person talks to it on the board, not in chat:
      `pm_answer.sh` (each tick, up to `PM_ANSWER_PARALLEL` 1 at once) takes the newest person's comment on an `agent:pm` ticket or its PRD PR
      (agents' comments are told apart by their 🧭/🤖/🚀 header — one GitHub account)
      and runs the skill in a worktree that may edit only `docs/product/` → a PRD PR `Closes #n` (script-checked: no other path, epic markers parse) or a ticket reply;
    - the PM merges its own PRD PR once CI is green (`pm:self-merge`, `pm_board.py selfmerge`) unless it touches a strategy policy
      — an approved epic gaining `rule_areas` (`pm_board.py policy`) or the agent's `POLICY: yes` (missing = yes) → `agent:needs-human`, a person merges;
      removing the label or a person's comment stops it (product owner on #151);
    - `agent:pm` tickets never go to Ready and the Dev agent skips them (2026-10-18, #151: it picked up the PRD questions #149);
    - tests: `python3 -m unittest discover -s scripts/agents -p 'test_*.py'`;
  - the Dev agents (`dev_agent.sh`, up to `DEV_PARALLEL` 1 at once, developer skill in Agent mode) each implement the top free Ready card
    (Priority, then oldest; never an epic, an `agent:pm` ticket, a card another instance holds, or one that must wait — `pm_board.py wait`)
    in a worktree with edit/test/commit permissions only, the script re-runs all checks and opens the PR (`agent:dev`) or moves the card to Blocked with questions;
    - a user-visible change is done only with its monitor-guide section, a golden question and `go run ./cmd/helpindex`; the Reviewer blocks one without them (#483, both prompts);
    - a PR that merged while it fixed review findings (#478, PR #474): the new commits move to `agent/issue-<n>-r<k>` from main (`rescue_commits.sh`, tested on a throwaway clone in `test_rescue.py`)
      and a new PR `Refs #n`, linked on the issue; closed unmerged → Blocked with a question; "pushed changes" only once the PR head on GitHub is the pushed commit;
    - `lib.sh open_dev_pr` finds an issue's open PR on either branch form (Dev, Reviewer; the gate strips `-r<k>`);
  - the Reviewer agents (`review_agent.sh`, up to `REVIEW_PARALLEL` 1, developer skill in Review mode, read-only) reviews each new PR commit, verifies every finding, posts one comment review
    and sends blocking findings back to Ready (≤ 2 fix rounds, then `agent:needs-human`; clean → `agent:reviewed`);
  - **only ready PRs reach a person** (product owner 2026-10-19): `pr_gate.sh` (each tick, no Claude; rules in `pr_gate.py`) updates a PR behind main itself (`gh pr update-branch`),
    sends a PR that conflicts with main back to Ready with `needs-rebase` (the Dev agent always merges `origin/main` into an open PR's branch first and resolves conflicts as its first task),
    never acts on a PR whose card is In progress (wait "a Dev agent is working on it", #478),
    and labels `ready-to-merge` (PR and card) only a PR reviewed clean on its current head, CI green, `CLEAN`, with no open dependency
    — a PR that changes a business rule or policy waits there for the person, who merges those top-down in priority order;
    - policy = a path in `pr_gate.py` `POLICY_PATHS`, or the Reviewer's `policy=yes` (none declared = yes);
    - a policy-free one, up to date with main, is merged by the gate itself, one per run (#468, product owner 2026-10-28);
    - `PR_AUTO_MERGE=0` in `agents.env` turns it off;
  - a human merges every policy change (ruleset: PR + green CI); a review that left a changed file unread is `incomplete`, never clean, and counts as no review (#476);
  - `deploy.sh` redeploys the **mvp** environment only (testnet, its clean `main` clone), health-checks, records `~/dev/go/optionsBot-env/history/deploys.tsv` (the soak clock);
    - a failed compose or health check (#204) resets the clone to `mvp.deployed`, rebuilds and re-checks it (a `rollback` line in `deploys.tsv`; unhealthy too → loud notify, no retry),
      keeps the bad commit in `mvp.failed` and files one `bug` issue `[E6] mvp deploy failed: <sha> (<subject>)` in Triage
      (step, last 80 log lines with `.env` secrets redacted, commit range — `deploy_issue.py`; deduplicated by the sha, re-tried each tick until filed)
      and moves every closed issue's card to Done (each tick too, for issues closed by hand: the board's "Item closed" workflow is off);
  - stable moves only by `scripts/promote.sh` (3-day soak, no open `sme:not-fixed`
    — the SME agent adds it when it reopens a not-fixed issue and verifies fixes on mvp's data, `data/sme/<date>/mvp/`),
    run by a person or by the automatic release below.
- Automatic release (#471, AGENTIC.md "Automatic release"):
  - `release.sh` (from the tick, only after a healthy deploy step; rules in pure `release.py`, no Claude) runs `promote.sh <next patch> --commit <sha> --yes` from the mvp clone:
    - weekly: the first tick of an ISO week (`data/agents/release/<week>.done`) releases the newest healthy mvp deploy ≥ 3 days old that stable does not contain;
      nothing to release → a one-line note on the E6 epic saying why;
    - hotfix (any tick): a merged PR labelled, or closing an issue labelled, `bug` + `release:hotfix`, once its first mvp deploy has been live 1 h (`--hotfix "#PR"`);
    - an open `sme:not-fixed` blocks both; a commit whose promotion failed is not retried (`failed-<sha>`); every release notifies and comments on E6;
    - `RELEASE_DRY_RUN=1` decides and prints only; `AUTO_RELEASE=0` in `agents.env` turns it off; ledger agent `release`.
  - a person (an admin) escalates: `promote.sh <tag> --commit <sha> --escalate "<reason>"` skips the soak — still a commit that ran in mvp, still no `sme:not-fixed`;
  - every healthy release is a `stable … release <tag> <trigger> <who> <why>` line in `deploys.tsv` (trigger `manual | weekly | hotfix | escalation`, who = `PROMOTE_BY` else the git user, `agent:release` for the agent).
- Run ledger (#206, AGENTIC.md "Run ledger"):
  - every SME, PM, PM-answer, Dev, Reviewer, PR-gate, deploy, flaky-test-hunt, usage-report and backup run appends a `start` and an `end` JSON line to `<envs>/history/agent_runs.jsonl`
    (`lib.sh` `run_begin <agent> [issue] [pr] [log]` / `run_end <outcome> [detail]`, `RUN_PR`/`RUN_COMMIT` set on the way; one `printf` under the `agent-runs` mutex;
    the `EXIT` trap ends an open run `failed` on a non-zero exit)
  - — a new agent script or exit path needs them too; only a tick that does work is a run; paths and ids only;
  - `runs.py --issue N | --agent | --date | --open` queries it;
  - each tick `sweep_runs.sh` (`runs.py --sweep`, #291) closes open runs whose pid is gone as `abandoned` and notifies once of a live run past its agent's `TIME_LIMIT_MIN` (never kills).
- Stuck cards (#329, AGENTIC.md "Stuck cards"):
  - `stuck.sh` (tick, at most every 6 h; pure `stuck.py`) comments once on a card In progress > 6 h or In review > 24 h with no open run and no waiting label
    — a pause, budget or recent limit is listed as the reason instead; moves nothing.
- Weekly agent metrics (#180, AGENTIC.md "Agent metrics"):
  - `metrics.sh` (from the tick, first tick of a Monday UTC) → `data/agents/metrics/<week>.md` and a comment on the E6 epic;
  - pure `metrics.py` reads stages from the agents' comment markers (a changed comment wording needs its marker there too),
    plus the Runs table from the ledger (#290), what a person overruled (#292) and the stuck cards.
- Weekly evidence (#244, AGENTIC.md "Weekly evidence report"):
  - `evidence.sh` (from the tick, first tick of a Monday UTC, `data/agents/evidence/<week>.done`, retried once) runs `stress_inputs.sh` + `cmd/evidence` for each env × bot (mvp with stable's books)
    and posts `evidence_week.py`'s summary on the E2 epic — percentages only for an env whose `.env` is `DERIBIT_ENV=live`;
  - it also runs the direction trial's scorecard (`cmd/evidence -compare-direction`, #440: last week plus every week since 2026-W43 without one)
    and adds a "Direction trial" block (% and counts: replay net/drawdown/sheds, live sheds, live − replay, the brief's counters), naming the first week with a GEX shed on the comment's first line;
  - `agent:needs-human` decision tickets (first shed, early stop, review due — each kind once ever, by title) go to Triage, never a config change.
- Nightly flaky-test hunt (#248, AGENTIC.md "Nightly flaky-test hunt"):
  - `flaky.sh` (from the tick, first tick of a UTC night from 03:00, `data/agents/flaky/<date>.done`, no Claude)
    runs `go test ./tests/... -race -count=5 -json` and `npm test -- --ci` ×3 on a clean `origin/main` worktree within 30 min (a time-out files nothing)
    and files one `bug` `[E6] Flaky test: <name>` in Triage per test that both passed and failed
    (pure `flaky.py`: dedup against open issues and those closed in 14 days, secrets redacted as `deploy_issue.py`; always-failing tests are only logged).
- Weekly usage report (#340, #341, #252, AGENTIC.md "Weekly usage report"):
  - `usage.sh` (from the tick, first tick of a Monday UTC, `data/agents/usage/<week>.done`, no Claude) runs `cmd/usage -alert` in each env with a non-empty `data/monitor/events.jsonl`
    (none with events in the week → nothing posted, no marker, re-looked at hourly),
  - files one `bug` `[E13] Engagement drop: <metrics> <week>` in Triage when pure `usage.Drops`
    (`internal/usage/drops.go`: active users or help used ≥ 30 % under the 3 weeks' mean, a funnel ≥ 20 points under, a base under 10 = too few to judge, a week without events left out)
    finds one (dedup by week as `flaky.py`),
  - and posts `usage_week.py`'s summary on the E13 epic — named figures only, so no user, session or amount field; percentages only for a live env.
- Daily delivery report (#470, AGENTIC.md "Delivery report (DORA)"):
  - `dora.sh` (from the tick, once a UTC day, `data/agents/dora/<date>.done`, no Claude) dumps main's first-parent history, tags, merged PRs and issues
    and runs `cmd/dora` → `<envs>/history/delivery/dora.json|<week>.md`, and posts each finished week's four numbers on the E6 epic once (`<week>.posted`);
  - `promote.sh` appends `stable … release <tag> <trigger> <who> <why>` / `stable … rollback <tag>` lines to `deploys.tsv` (mvp readers skip them; `internal/delivery` ignores the fields after the tag);
  - the `regression` label + a "Caused by #N" line (Dev: in its PR body, `dev_agent.sh` labels the issue; SME: a finding's `caused_by`) feed its change failure rate;
  - ledger agent `dora`.
- Nightly backup (#328, AGENTIC.md "Nightly backup"):
  - `backup_data.sh` (from the tick, first tick after 00:00 UTC, `data/agents/backup/<date>.done`, retried once) copies each env × bot's `orders.log`, P&L/regime/direction histories and `data/archive/`
    and each env's monitor request queue (`data/monitor/requests*.jsonl` → `<date>/<env>/monitor/`, #409) (named files only, never `.env`) to `<envs>/history/backups/<date>/<env>/<bot>/`, keeps 14 days,
    and checks each copy with `cmd/backupcheck` (`internal/backup.Check`: `orders.ReplayFile` decodes it, no fewer events than the previous good copy's `check.json`);
  - a failure notifies, keeps `<date>.failed` and prunes nothing; ledger agent `backup`.
- Assistant requests (#410, AGENTIC.md "Assistant requests"):
  - `requests.sh` (each tick; exits at once unless a request in some env's `data/monitor/requests.jsonl` has no status line, or a filed one is open
    — that sweep at most hourly; rules in pure `requests.py`, files only)
  - marks a filed request `fixed`/`not_planned` when its issue closes, `filed` when an issue already carries its `<!-- assistant-request: <env>/R-NNNN -->` marker (never filed twice),
  - redacts the rest (`.env` secrets as `deploy_issue.py`, emails, account ids, the requester's username and @mentions, long tokens, coin/USD amounts → "an amount"; no user, role or answer id)
  - and hands at most the day's cap left (10 issues a UTC day, all envs) to one read-only PM agent run (`docs/product/assistant/request-triage.md`, request text fenced as untrusted data):
    duplicate (an issue open or closed in 14 days, else refused), not_a_bug (reason), needs_info,
    accept → a `bug`/`enhancement` issue `[E?] …` in Triage, redacted again, with the request id and "From the in-product assistant";
  - every verdict appends `{id, status, note, issue, issue_url, ts}` to that env's `requests_status.jsonl` (My requests; `not_planned` added to `STATUSES`);
  - no valid verdict → retried, 3 runs at most (`requests_attempts.json`); ledger agent `requests`.
- Token savers (2026-10-27, AGENTIC.md):
  - the Dev pick order skips a card whose epic another instance is building (`pm_board.py ready`);
    except `blocker` cards: up to 3 of one epic at once (`BLOCKER_PARALLEL`, #459), and while any is open only blocker cards are picked;
    with none open, normal picking and one per epic again;
  - documents/research cards (label `documentation` or `DEV_LIGHT_EPICS`) run on `DEV_LIGHT_MODEL` unless `DEV_MODEL` is set (`pm_board.py model`);
  - the agent runs only the tests its change touches, the script every suite once.
- Parallel runs:
  - `tick.sh` (every 6 min) starts them in the background, within the person's budget `<envs>/agents.env`
    (pause, agent hours, Claude runs per day, parallelism 1 each by default, models — `limit_lib.sh` `budget_reason`, AGENTIC.md "The budget"; 2026-10-23: the agents had used the person's whole Claude plan);
  - each claims its card (`lib.sh claim`, pid-held), git in the shared clone goes through `git_locked`;
  - after a usage/rate limit (`note_limit`, `LIMIT_PATTERN` in `limit_lib.sh`; `note_stop` also counts a run that fails in under 60 s with no answer block, so a limit never labels a card `agent:needs-human`)
    no new Claude run for 30 min, then one at a time for an hour (`parallel_max`);
  - each tick, outside a pause, `requeue.sh` (rules in `requeue.py`) puts back in Ready the Blocked cards whose Dev stop was a limit and nobody answered (#237).
- Setup: `setup_github.sh`; schedule: `install.sh`.
- Agents never deploy live or trade (release.sh promotes a testnet stable only: `promote.sh` refuses a live one); they merge only policy-free work: the PR gate merges Dev agent PRs reviewed clean, CI green, up to date and policy-free (#468), and the PM merges its own policy-free PRD PRs.
