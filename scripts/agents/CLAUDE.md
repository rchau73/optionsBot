# scripts/agents

Folder notes for `CLAUDE.md`: the agentic workflow (`AGENTIC.md` is the overview, one file per agent in `docs/agents/`).

```bash
go run ./cmd/usage -events data/monitor/events.jsonl -week 2026-W43   # monitor usage and cohort report from the analytics events (#295): active users, retention, panels, first answers, help, funnels, trust — counts only, no user/session id → data/monitor/reports/<week>.md|json (the JSON carries the week's drop check as `drops` and the NPS reasons by score, `why_best`/`why_improve`, for the monitor's admin Analytics page, #445); `-alert` also prints that check (a `DROPS {json}` line, #341) (README "Usage report")
go run ./cmd/dora -main main.tsv -tags tags.tsv -prs prs.json -issues issues.json -deploys ~/dev/go/optionsBot-env/history/deploys.tsv -promotions ~/dev/go/optionsBot-env/history/promotions.tsv -out ~/dev/go/optionsBot-env/history/delivery   # delivery report (#470, pure internal/delivery, files only — dora.sh dumps git/gh daily): DORA metrics per ISO week × mvp/stable with bands ("too few samples" under 3; "not recorded" before deploys.tsv began, never 0) and every PR's merge, mvp-live and stable-live time and release → dora.json + <week>.md; failures = rollback lines, "[E6] mvp deploy failed" issues, Revert PRs, `regression` bugs with "Caused by #N"; malformed/off-main lines reported, never counted (docs/agents/dora.md "Delivery report (DORA)")
```

- **Only the owner's text** (#575): a script reading issue bodies, comments or reviews filters them with `mine_jq` (`lib.sh`: `mine`, `own_body`) — the repo is public;
  `test_owner_text.py` fails on a new read without it (report-only `dora.sh`, `metrics.sh`, `release.sh` are exempt).
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
    in a worktree with edit/test/commit permissions only, the script runs the checks the change touches and opens the PR (`agent:dev`, always a draft) or moves the card to Blocked with questions;
    - CI runs every suite (#472, docs/agents/dev.md "Check failures go back to the Dev agent"; pure rules in `checkfail.py`, `test_checkfail.py`):
      the gate marks the draft ready once CI is green, the Reviewer starts only on a ready PR with a green head (`pr_gate.py green`);
    - a check failure (the script's, or CI red on an In review head) → Ready with `needs-fix` and the failing log tails, read first by the next run; the second → Blocked, `agent:needs-human`
      (count: `<!-- dev:check-fail n=… -->` on the issue since the last `<!-- dev:blocked -->`; `lib.sh check_failed`);
    - a failing test the change does not touch is re-run (script: that test ×3; gate: the failed jobs, 3 times a head); a pass makes it flaky (`lib.sh note_flaky`: the PR and its flaky issue);
    - `checkfail.py suites` (CI's path filter and the script's) runs a suite on code it builds or a doc its tests open; any `.md` runs the doc-wide line checks, a CLAUDE.md only those;
      `phone` (#739, CI's "Phone viewport check", required) runs `npm run test:phone` on non-doc `frontend/` files, `cmd/monitor-demo/`, `internal/api/` and `ci.yml`, never on the Mac (`frontend/docs/testing.md` "In CI");
    - a user-visible change is done only with its monitor-guide section, a golden question and `go run ./cmd/helpindex`; the Reviewer blocks one without them (#483, both prompts);
    - a PR that merged while it fixed review findings (#478, PR #474): the new commits move to `agent/issue-<n>-r<k>` from main (`rescue_commits.sh`, tested on a throwaway clone in `test_rescue.py`)
      and a new PR `Refs #n`, linked on the issue; closed unmerged → Blocked with a question; "pushed changes" only once the PR head on GitHub is the pushed commit;
    - a run blocked with commits ahead of main and no open PR pushes its branch, and the next run resumes it; without a PR the commits counted are those ahead of main (#517, `branch_work.sh`, `test_branch_work.py`);
    - `lib.sh open_dev_pr` finds an issue's open PR on either branch form (Dev, Reviewer; the gate strips `-r<k>`);
  - the Reviewer agents (`review_agent.sh`, up to `REVIEW_PARALLEL` 1, developer skill in Review mode, read-only) reviews each new PR commit, verifies every finding, posts one comment review
    and sends blocking findings back to Ready (≤ 2 fix rounds, then `agent:needs-human`; clean → `agent:reviewed`);
  - **only ready PRs reach a person** (product owner 2026-10-19): `pr_gate.sh` (each tick, no Claude; rules in `pr_gate.py`) updates a PR behind main itself (`gh pr update-branch`),
    sends a PR that conflicts with main back to Ready with `needs-rebase` (the Dev agent always merges `origin/main` into an open PR's branch first and resolves conflicts as its first task),
    - except a conflict only in generated files or sorted line lists (#465, #596): the gate merges main in a scratch worktree and pushes,
      `scripts/regen.sh --resolve` keeping both sides' lines of `golden.jsonl` (`scripts/merge_lines.py`) and rerunning the generators; the Dev agent's merge step does the same before the agent starts;
    never acts on a PR whose card is In progress (wait "a Dev agent is working on it", #478),
    and labels `ready-to-merge` (PR and card) only a PR reviewed clean on its current head, CI green, `CLEAN`, with no open dependency
    — a PR that changes a business rule or policy waits there for the person, who approves it with the label `merge:approved` (#596): the gate merges approved PRs one per run, each up to date with main and green first, so several approved at once never conflict in turn;
    - the approval holds for the head it was given on (and GitHub's update merges of it); a new commit of the PR's own takes the label off and asks again;
    - the gate's own regenerated-files merge keeps the approval and the clean review too (#634): it records the sha (`<pr>:regen`) when `pr_gate.py settled` finds it differs from git's merge only in generated files and sorted lists, and `equiv` walks it;
    - policy = a path in `pr_gate.py` `POLICY_PATHS` (less its `WORKING_DOCS`, and `golden.jsonl` only when a line is removed or changed: `ADD_ONLY`, #596), or the Reviewer's `policy=yes` (none declared = yes);
    - every agent session runs without GitHub credentials (`lib.sh` `no_github`, #596): code a session runs cannot call `gh` as the owner, so only the person and the scripts label or merge;
    - a policy-free one, up to date with main, is merged by the gate itself, one per run (#468, product owner 2026-10-28);
    - `PR_AUTO_MERGE=0` in `agents.env` turns it off;
  - a human merges every policy change (ruleset: PR + green CI); a review that left a changed file unread is `incomplete`, never clean, and counts as no review (#476);
  - `deploy.sh` redeploys the **mvp** environment only (testnet, its clean `main` clone), health-checks, records `~/dev/go/optionsBot-env/history/deploys.tsv` (the soak clock);
    - a failed compose or health check (#204) resets the clone to `mvp.deployed`, rebuilds and re-checks it (a `rollback` line in `deploys.tsv`; unhealthy too → loud notify, no retry),
      keeps the bad commit in `mvp.failed` and files one `bug` issue `[E6] mvp deploy failed: <sha> (<subject>)` in Triage
      (step, last 80 log lines with `.env` secrets redacted, commit range — `deploy_issue.py`; deduplicated by the sha, re-tried each tick until filed)
      and moves every closed issue's card to Done (each tick too, for issues closed by hand: the board's "Item closed" workflow is off);
    - a health check whose log shows only Deribit refusing the connect (5xx handshake, refused/timed-out dial; `deploy_issue.py outage`, #631) waits `DEPLOY_OUTAGE_WAIT_SEC` (600) more,
      then, still down, is an exchange outage: no rollback, no `mvp.failed`, no issue, one notify, ledger `exchange_outage`; the next tick redeploys the commit;
    - a healthy deploy prunes untagged images and build cache older than 72 h, never volumes (#548: the Docker disk filled, the monitors' writes failed with ENOSPC),
      and over 85 % full (the monitor volume's `df`) warns: log, notify, one open `bug` `[E6] Docker disk over 85 % full (<pct> %)` (`deploy_disk.py`);
  - stable moves only by `scripts/promote.sh` (3-day soak, no open `sme:not-fixed`
    — the SME agent adds it when it reopens a not-fixed issue and verifies fixes on mvp's data, `data/sme/<date>/mvp/`),
    run by a person or by the automatic release below.
- Automatic release (#471, docs/agents/release.md "Automatic release"):
  - `release.sh` (from the tick, only after a healthy deploy step; rules in pure `release.py`, no Claude) runs `promote.sh <next patch> --commit <sha> --yes` from the mvp clone:
    - weekly: the first tick of an ISO week (`data/agents/release/<week>.done`) releases the newest healthy mvp deploy ≥ 3 days old that stable does not contain;
      nothing to release → a one-line note on the E6 epic saying why;
    - hotfix (any tick): a merged PR labelled, or closing an issue labelled, `bug` + `release:hotfix`, once its first mvp deploy has been live 1 h (`--hotfix "#PR"`);
    - an open `sme:not-fixed` blocks both; a commit whose promotion failed is not retried (`failed-<sha>`); every release notifies and comments on E6;
    - `RELEASE_DRY_RUN=1` decides and prints only; `AUTO_RELEASE=0` in `agents.env` turns it off; ledger agent `release`.
  - a person (an admin) escalates: `promote.sh <tag> --commit <sha> --escalate "<reason>"` skips the soak — still a commit that ran in mvp, still no `sme:not-fixed`;
  - every healthy release is a `stable … release <tag> <trigger> <who> <why>` line in `deploys.tsv` (trigger `manual | weekly | hotfix | escalation`, who = `PROMOTE_BY` else the git user, `agent:release` for the agent).
- Run ledger (#206, docs/agents/run-ledger.md "Run ledger"):
  - every agent run appends a `start` and an `end` JSON line to `<envs>/history/agent_runs.jsonl`
    (`lib.sh` `run_begin <agent> [issue] [pr] [log]` / `run_end <outcome> [detail]`, `RUN_PR`/`RUN_COMMIT` set on the way; one `printf` under the `agent-runs` mutex;
    the `EXIT` trap ends an open run `failed` on a non-zero exit)
  - agent names (`runs.py` `AGENTS`): `sme`, `pm`, `pm_answer`, `dev`, `review`, `pr_gate`, `deploy`, `flaky`, `backup`, `usage`, `requests`, `assistant_review`, `assistant_golden`, `dora`, `release`, `metrics`, `evidence`, `go_no_go`, `requeue`, `stuck`, `security`, `recording`;
  - — a new agent script or exit path needs them too; only a tick that does work is a run; paths and ids only;
  - `tests/ledger_coverage_test.go` (#722) fails on a `scripts/agents/*.sh` without `run_begin`/`run_end` that is not on its `ledgerExempt` list (one reason per entry, the file must exist),
    and on an agent name missing from `runs.py`, `docs/agents/run-ledger.md` or the list above;
  - `runs.py --issue N | --agent | --date | --open` queries it;
  - each tick `sweep_runs.sh` (`runs.py --sweep`, #291) closes open runs whose pid is gone as `abandoned` and notifies once of a live run past its agent's `TIME_LIMIT_MIN` (never kills).
- Stuck cards (#329, docs/agents/stuck-cards.md "Stuck cards"):
  - `stuck.sh` (tick, at most every 6 h; pure `stuck.py`) comments once on a card In progress > 6 h or In review > 24 h with no open run and no waiting label
    — a pause, budget or recent limit is listed as the reason instead; moves nothing.
- Weekly agent metrics (#180, docs/agents/metrics.md "Agent metrics"):
  - `metrics.sh` (from the tick, first tick of a Monday UTC) → `data/agents/metrics/<week>.md` and a comment on the E6 epic;
  - pure `metrics.py` reads stages from the agents' comment markers (a changed comment wording needs its marker there too),
    plus the Runs table from the ledger (#290), what a person overruled (#292) and the stuck cards;
  - its Trend section (#707) puts the week beside the 3 before it from the same dumps (`metrics.sh` fetches 4 weeks): an arrow when worse than their mean, a base under 3 "too few to judge", a week with no data "–";
  - its Funnel section follows the issues filed in the week with an `E#` label through to verified fixed: numbers and counts only.
  - its "Flaky repeat offenders (30 days)" section (#747) lists each test file with 2 or more flaky issues (`flaky.repeat_offenders`; the dumps reach 30 days before the week's end), else "none".
  - agent health (#755, pure `health`/`health_issues`): an agent with 4 runs or more whose failed % or limit % reached 25 % gets one `bug` `[E6] Agent health: <agent> <metric> <week>` in Triage
    (counts, run ids, the report's link; more than 3 crossings → one combined issue; dedup by title, open or closed); the Runs section names them, else "no agent crossed a threshold".
- Weekly evidence (#244, docs/agents/evidence.md "Weekly evidence report"):
  - `evidence.sh` (from the tick, first tick of a Monday UTC, `data/agents/evidence/<week>.done`, retried once) runs `stress_inputs.sh` + `cmd/evidence` for each env × bot (mvp with stable's books)
    and posts `evidence_week.py`'s summary on the E2 epic — percentages only for an env whose `.env` is `DERIBIT_ENV=live`;
  - it also runs the direction trial's scorecard (`cmd/evidence -compare-direction`, #440: last week plus every week since 2026-W43 without one)
    and adds a "Direction trial" block (% and counts: replay net/drawdown/sheds, live sheds, live − replay, the brief's counters), naming the first week with a GEX shed on the comment's first line;
  - `agent:needs-human` decision tickets (first shed, early stop, review due — each kind once ever, by title) go to Triage, never a config change.
- Daily recording check (#716, docs/agents/evidence.md "Daily recording check"):
  - `recording_check.sh` (from the tick, first tick of a UTC day, `data/agents/recording/<yesterday>.done`, no Claude) runs `cmd/datacoverage -yesterday -json` for each env that records the market (`BOT_RECORD_MARKET`, stable) × bot;
  - a market recording under `evidence.ReplayFullCoveragePct` (90) of yesterday's hours or with a gap over `LongGapHours` (3) → one notify and one comment on the E2 epic (pure `recording_day.py`, hours and % only); all at the floor → silent;
  - ledger agent `recording`.
- Weekly go / no-go reading (#738, docs/product/go-no-go.md §6):
  - `go_no_go.sh` (from the tick, first tick of a Monday UTC after the evidence report, else from Wednesday; `data/agents/go_no_go/<week>.done`, no Claude)
    runs `scripts/product/go_no_go_week.py`: `go_no_go.py -json` on stable's clone and the newest tally and fake-door files, one line appended to the gitignored `data/product/go_no_go_history.jsonl` (shares and ratios only);
  - a row changing status, or the overall line becoming `kill criterion met` / `no kill criterion met`, files one `agent:pm` + `agent:needs-human` issue in Triage (old and new reading, source file; recommends nothing beyond PRD §6); ledger agent `go_no_go`;
  - an issue that cannot be filed (`gh` failed) is notified and kept in `data/agents/go_no_go/<week>/alert` with the week not done, retried each tick without re-reading — the next reading's baseline is the new one, so the change would never alert again.
- Nightly flaky-test hunt (#248, docs/agents/flaky.md "Nightly flaky-test hunt"):
  - `flaky.sh` (from the tick, first tick of a UTC night from 03:00, `data/agents/flaky/<date>.done`, no Claude)
    runs `go test ./tests/... -race -count=5 -json` and `npm test -- --ci` ×3 on a clean `origin/main` worktree within 30 min (a time-out files nothing)
    and files one `bug` `[E6] Flaky test: <name>` in Triage per test that both passed and failed
    (pure `flaky.py`: dedup against open issues and those closed in 14 days, secrets redacted as `deploy_issue.py`; always-failing tests are only logged);
  - a jest timeout that fired over a minute late is `stalled` (the process was frozen: the Mac slept, #587), logged and not counted; `caffeinate -is` keeps the Mac awake during the hunt.
  - repeat offenders (#747, `repeat_offenders`): flaky issues open or closed in 30 days grouped by test file (jest: before ` › `; Go: the top-level `Test…`);
    the metrics report lists files with 2 or more, and a new issue of a file reaching 3 carries a "fix the shared cause" line (body only).
- Weekly usage report (#340, #341, #252, docs/agents/usage.md "Weekly usage report"):
  - `usage.sh` (from the tick, first tick of a Monday UTC, `data/agents/usage/<week>.done`, no Claude) runs `cmd/usage -alert` in each env with a non-empty `data/monitor/events.jsonl`
    (none with events in the week → nothing posted, no marker, re-looked at hourly),
  - files one `bug` `[E13] Engagement drop: <metrics> <week>` in Triage when pure `usage.Drops`
    (`internal/usage/drops.go`: active users or help used ≥ 30 % under the 3 weeks' mean, a funnel ≥ 20 points under, a base under 10 = too few to judge, a week without events left out)
    finds one (dedup by week as `flaky.py`),
  - and posts `usage_week.py`'s summary on the E13 epic — named figures only, so no user, session or amount field; percentages only for a live env.
  - after the assistant quality review (`assistant_review.sh`, #438) it runs `assistant_golden.sh` (#536): in `ASSISTANT_GOLDEN_ENV` (mvp) when its `.env` has `ANTHROPIC_API_KEY`,
    `scripts/product/assistant_run.sh` asks the real assistant every golden question (a test run: its own log, `test_run`), grades it against `golden_baseline.json`
    and posts counts and % per category on the E10 epic (`<week>.done`, ledger agent `assistant_golden`; a regression is reported, not a failure).
  - `assistant_review.sh` also posts for a week with no answers while a row is open, with the overdue count and, above 0, a line that a person reads the private review list (#704: `closures.jsonl`, never on GitHub).
- Security digest and alerts (#717, docs/agents/usage.md "Weekly usage report"):
  - `security.sh` (from the tick, no Claude, no GitHub) reads each env's audit trail (the running monitor's volume, audit files only, else the clone's) and runs `cmd/security` (pure `internal/security`);
  - alerts at most every 30 min (`SECURITY_ALERT_MIN`): 10 failed sign-ins for one username within an hour, 3 lockouts in a UTC day, a role change to admin outside the first-admin setup — one notify each, IDs in `data/agents/security/<env>.alerts` (never twice);
  - the weekly digest (`<week>.done`, retried once): counts this week vs the week before, the chain check first, "No data yet" for an empty trail
    → `data/agents/security/<week>/<env>/digest.md|json`, one notify, the usage report's "Security" section (`lib.sh security_section`, also called by `usage.sh`);
  - counts only, never a name, IP, password, token or reset link; ledger agent `security`.
- Daily delivery report (#470, docs/agents/dora.md "Delivery report (DORA)"):
  - `dora.sh` (from the tick, once a UTC day, `data/agents/dora/<date>.done`, no Claude) dumps main's first-parent history, tags, merged PRs and issues
    and runs `cmd/dora` → `<envs>/history/delivery/dora.json|<week>.md`, and posts each finished week's four numbers on the E6 epic once (`<week>.posted`);
  - `promote.sh` appends `stable … release <tag> <trigger> <who> <why>` / `stable … rollback <tag>` lines to `deploys.tsv` (mvp readers skip them; `internal/delivery` ignores the fields after the tag);
  - the `regression` label + a "Caused by #N" line (Dev: in its PR body, `dev_agent.sh` labels the issue; SME: a finding's `caused_by`) feed its change failure rate;
  - ledger agent `dora`.
- Nightly backup (#328, docs/agents/backup.md "Nightly backup"):
  - `backup_data.sh` (from the tick, first tick after 00:00 UTC, `data/agents/backup/<date>.done`, retried once) copies each env × bot's `orders.log`, P&L/regime/direction histories and `data/archive/`
    (named files only, never `.env`) to `<envs>/history/backups/<date>/<env>/<bot>/`, keeps 14 days,
    and checks each copy with `cmd/backupcheck` (`internal/backup.Check`: `orders.ReplayFile` decodes it, no fewer events than the previous good copy's `check.json`);
  - each env's monitor files → `<date>/<env>/monitor/` (#650): an allow-list (`MONITOR_FILES`: users `auth.db`, events, assistant log, request queue, audit trail and archives) read from the running monitor's volume (`envctl.sh … cp`, read-only),
    else from the clone's `data/monitor/` (logged), plus the clone's `assistant_reviews/closures.jsonl` (#704); every `.jsonl` must parse (`-lines`) and the audit chain hold (`-audit`, `CheckAuditChain` = the Audit page's `verifyChain`);
  - a failure notifies, keeps `<date>.failed` and prunes nothing; ledger agent `backup`.
- Assistant requests (#410, docs/agents/requests.md "Assistant requests"):
  - `requests.sh` (each tick; exits at once unless a request in some env's `data/monitor/requests.jsonl` has no status line, or a filed one is open
    — that sweep at most hourly; rules in pure `requests.py`, files only)
  - marks a filed request `fixed`/`not_planned` when its issue closes, `filed` when an issue already carries its `<!-- assistant-request: <env>/R-NNNN -->` marker (never filed twice),
  - redacts the rest (`.env` secrets as `deploy_issue.py`, emails, account ids, the requester's username and @mentions, long tokens, coin/USD amounts → "an amount"; no user, role or answer id)
  - and hands at most the day's cap left (10 issues a UTC day, all envs) to one read-only PM agent run (`docs/product/assistant/request-triage.md`, request text fenced as untrusted data):
    duplicate (an issue open or closed in 14 days, else refused), not_a_bug (reason), needs_info,
    accept → a `bug`/`enhancement` issue `[E?] …` in Triage, redacted again, with the request id and "From the in-product assistant";
  - every verdict appends `{id, status, note, issue, issue_url, ts}` to that env's `requests_status.jsonl` (My requests; `not_planned` added to `STATUSES`);
  - no valid verdict → retried, 3 runs at most (`requests_attempts.json`); ledger agent `requests`.
- Token savers (2026-10-27, AGENTIC.md "Parallel runs and limits"):
  - the Dev pick order skips a card whose epic another instance is building (`pm_board.py ready`);
    except `blocker` cards: up to 3 of one epic at once (`BLOCKER_PARALLEL`, #459), and while any is open only blocker cards are picked;
    with none open, normal picking and one per epic again;
    in any epic, a card that names a file or field (`**Files:**` line, `files_named`) of a card being built or In progress, or depends on one, waits (#467);
  - documents/research cards (label `documentation` or `DEV_LIGHT_EPICS`) run on `DEV_LIGHT_MODEL` unless `DEV_MODEL` is set (`pm_board.py model`);
  - the agent and the script run only the tests the change touches, CI every suite (#472),
    plus the checks that read every file (`checkfail.py repo-wide`: glossary, responsive audit, empty states, menu, help index; #467) after merging `origin/main` last.
- Parallel runs:
  - `tick.sh` (every 6 min) starts them in the background, within the person's budget `<envs>/agents.env`
    (pause, agent hours, Claude runs per day, parallelism 1 each by default, models — `limit_lib.sh` `budget_reason`, AGENTIC.md "Parallel runs and limits"; 2026-10-23: the agents had used the person's whole Claude plan);
  - each claims its card (`lib.sh claim`, pid-held), git in the shared clone goes through `git_locked`;
  - after a usage/rate limit (`note_limit`, `LIMIT_PATTERN` in `limit_lib.sh`; `note_stop` also counts a run that fails in under 60 s with no answer block, so a limit never labels a card `agent:needs-human`)
    no new Claude run for 30 min, then one at a time for an hour (`parallel_max`);
  - each tick, outside a pause, `requeue.sh` (rules in `requeue.py`) puts back in Ready the Blocked cards whose Dev stop was a limit and nobody answered (#237).
- Setup: `setup_github.sh`; schedule: `install.sh`.
- A pipe never hides a failure (#683): a script or CI step that pipes a command into a filter sets `pipefail` or carries `# pipe-ok: <why>` on that line or the one before (`tests/pipe_guard_test.go`).
- Agents never deploy live or trade (release.sh promotes a testnet stable only: `promote.sh` refuses a live one); they merge only policy-free work: the PR gate merges Dev agent PRs reviewed clean, CI green, up to date and policy-free (#468), and the PM merges its own policy-free PRD PRs.
