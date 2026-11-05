# Nightly backup

Part of [Agentic development](../../AGENTIC.md).

The journal is the bots' only history, so the tick copies it every night (#328, epic E6).
On the first tick after 00:00 UTC, [`backup_data.sh`](../../scripts/agents/backup_data.sh) copies, for each environment (stable, mvp) and bot (`BACKUP_BOTS`, default `btc eth`) whose folder exists,
`orders.log`, `data/pnl_history.jsonl`, `data/regime_history.jsonl`, `data/direction_history.jsonl` and `data/archive/`
to `<envs>/history/backups/<date>/<env>/<bot>/`.
Each environment's monitor files go to `<date>/<env>/monitor/` (#650), read from the running monitor's `monitor-auth` volume through `scripts/envctl.sh <env> cp monitor:/app/data/monitor/. <temp>` (read-only), else — the monitor not running, the log naming the source — from the clone's `data/monitor/`.
The monitor's allow-list (`MONITOR_FILES` in the script, pinned by `test_backup.py`): `auth.db` (users), `events.jsonl` (analytics), `assistant.jsonl` (#438), `requests.jsonl` and `requests_status.jsonl` (#409), `audit.jsonl` and its archives `audit.<UTC time>.jsonl` (#619); plain files only, nothing else of the volume.
Every copied `.jsonl` must parse (`backupcheck -lines`; a torn last line is allowed) and the audit trail's hash chain must hold on the copy (`backupcheck -audit`, `internal/backup.CheckAuditChain`: the Audit page's `verifyChain`, both tested on `tests/testdata/audit_chain/`).
Named files only: no `.env`, no secret (an `.env*` found in the archive is dropped).
It reads the live files and never writes to them or to the volume.
No Claude call, no git, no order.

**Restore check:** `go run ./cmd/backupcheck` (`internal/backup.Check`) replays each copied journal, archived ones included, with `orders.ReplayFile`.
It fails on an unreadable line (a torn last line, a copy racing an append, is allowed) or when the copy holds fewer events than the previous good backup (its `check.json`; an archived journal counts, so `--reset-history` is not a loss).
A bot folder the previous backup had and that is now gone fails too.
The copy is built in `.<date>.partial` and becomes `<date>` only when every check passes; then the newest `BACKUP_KEEP_DAYS` (14) are kept.
A failure keeps the copy as `<date>.failed`, notifies, ends the run `failed`, prunes nothing, and is retried once on a later tick; `data/agents/backup/<date>.done` marks the day done after a success or the second attempt.
Run ledger: agent `backup`, outcome `backed_up` or `failed`, only when there was something to copy.
Log: `data/agents/backup/<date>.log`.
Tests: `test_backup.py` (end to end on throwaway environments) and `tests/backup_check_test.go`.
Restore steps: [operations](../operations.md#nightly-backup-and-restore).
