# Delivery report (DORA)

Part of [Agentic development](../../AGENTIC.md).

Do we deliver faster without breaking more (#470, epic E6)?
Once a UTC day, [`dora.sh`](../../scripts/agents/dora.sh) dumps `main`'s first-parent history (each merge with the author time of the earliest commit it brought in), the `v*` tags, the merged PRs and the issues (`gh … --json`) to `data/agents/dora/<date>/`, then runs `go run ./cmd/dora` (pure `internal/delivery`, files only) with `<envs>/history/deploys.tsv` and `promotions.tsv`.
It writes `<envs>/history/delivery/dora.json` (every week, every PR, every release and failure, and the input lines it skipped) and one `<week>.md` per week, back to 2026-W40 (`DORA_FROM`, 2026-10-02).
Once a week is over, its four numbers go on the E6 epic as one comment (`data/agents/dora/<week>.posted`; a failed post is retried the next day).
No Claude call, no order.
Ledger agent `dora`, outcomes `written`, `posted` or `failed`.

- **A PR** is a `Merge pull request #N` merge on `main`: number, branch, merge time, first commit, title and epic (the `[E#]` title prefix or an `E#` label).
  It is **live on mvp** at the first healthy mvp line of `deploys.tsv` whose commit contains it ("not recorded" when merged before the first line, 2026-10-17), and **live on stable** at the first release that does: a tag's first promotion (`promotions.tsv`, or a `stable … release <tag>` line, which `promote.sh` writes from #470 on, with `stable … rollback <tag>` when a failed promotion went back).
  A tag promoted again is a rollback re-run, not a release.
  `main`'s first-parent line is linear, so "contains" is "at or after it on that line".
- **Failures:** a `rollback` line; an `[E6] mvp deploy failed: <sha>` issue (filed within 6 h after a rollback line, it is that failure); a merged `Revert "…"` PR (the reverted PR from its `revert-<N>-…` branch, else by title); a bug labelled `regression` with a `Caused by #N` line in its body or in the body of the PR that closes it.
  The Dev agent writes that line in its PR body when it found the cause, and `dev_agent.sh` then labels the issue; the SME agent sets `caused_by` on a finding and `sme_review.sh` adds the label and the line.
  A revert or regression marks the deploy (and release) that first carried PR N as failed, on each environment where N went live before its fix.
- **Per ISO week and environment:** deployment frequency over the recorded part of the week (mvp: healthy deploys a day, `per_day`; stable: new releases a week, `per_week`, banded per week — 1 a week is high);
  lead time, median and p90, first commit → merge, merge → live and first commit → live (by the week it went live);
  change failure rate (failed attempts + failed deploys out of deploys + failed attempts);
  time to restore, a failure → the next healthy deploy or release, or the fix PR live (a regression with no fix PR found: until the issue closed; a regression filed after its fix went live starts at the fix's live time, the earlier of detected and fix live, so a restore is never negative).
  Each figure carries its DORA band (frequency: elite ≥ 1 a day, high ≥ 1 a week, medium ≥ 1 a month; lead time: < 1 day, < 1 week, < 1 month; failure rate: ≤ 5, 10, 15 %; restore: < 1 h, < 1 day, < 1 week); under 3 samples it says "too few samples" instead.
  A week before recording began is "deploy not recorded" (stable: "release not recorded"), never 0 and never an estimate; a recorded week with no deploy is 0.
- **Skipped, never counted:** a malformed `deploys.tsv` line, a merge with no PR number, a deploy, release or tag on a commit not on `main`, a regression that names no merged PR — each listed as a problem in `dora.json` and on `cmd/dora`'s stderr.

Tests: `tests/delivery_test.go` on the fixture history in `tests/testdata/delivery/`.
