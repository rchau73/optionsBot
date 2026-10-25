"""Tests of metrics.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import metrics  # noqa: E402


def c(at, body):
    return {"createdAt": at, "body": body, "author": {"login": "rchau73"}}


def lab(*names):
    return [{"name": n} for n in names]


def rv(verdict):
    return {"body": f"review\n<!-- agent-review sha=abc123 verdict={verdict} -->"}


PICKUP = "🤖 **Dev agent** picked this up on branch `agent/issue-{}` — **attempt 1**."
READY = "🧭 **PM agent** — epic **E6 · Agents** (#9) · priority **P2** · → **Ready**\n\nwhy"

# Week 2026-W42 = 2026-10-12 (Mon) → 2026-10-18. Expected values worked out by hand
# beside each fixture.
ISSUES = [
    {"number": 1, "title": "a", "labels": lab("agent:sme"), "createdAt": "2026-10-12T10:00:00Z", "comments": [
        c("2026-10-12T12:00:00Z", READY),                                   # triaged + Ready
        c("2026-10-12T14:00:00Z", PICKUP.format(1)),
        c("2026-10-13T09:00:00Z", "🤖 **Dev agent stopped — a person is needed.**\n\nWhich threshold?\n\n_Move the card back_"),
        c("2026-10-13T15:00:00Z", PICKUP.format(1)),
        c("2026-10-15T02:00:00Z", "🚀 Deployed to **mvp** (testnet) in abc (x)."),  # merge 10-15 00:00 → 2 h
        c("2026-10-16T09:00:00Z", "**SME verification (2026-10-16): fixed**\n\nok"),
    ]},
    {"number": 2, "title": "b", "labels": [], "createdAt": "2026-10-05T10:00:00Z", "comments": [
        c("2026-10-05T11:00:00Z", "🧭 **PM agent** — stays in **Triage**: needs a person"),  # before the week
        c("2026-10-14T08:00:00Z", PICKUP.format(2)),                         # a person moved it to Ready: Ready = pickup
    ]},
    {"number": 3, "title": "c", "labels": lab("agent:sme"), "createdAt": "2026-10-17T00:00:00Z", "comments": [
        c("2026-10-17T01:00:00Z", "🧭 **PM agent** — epic **E1** · stays in **Triage**: no rule area"),
        c("2026-10-18T09:00:00Z", "**SME verification (2026-10-18): not_fixed**\n\nstill there"),
    ]},
    {"number": 4, "title": "d", "labels": [], "createdAt": "2026-10-14T00:00:00Z", "comments": [
        c("2026-10-15T00:00:00Z", READY),
        c("2026-10-15T01:00:00Z", PICKUP.format(4)),
        c("2026-10-19T01:00:00Z", "🚀 Deployed to **mvp** (testnet) in def (y)."),  # next week
    ]},
]
PRS = [
    # Ready 10-12 12:00 → opened 10-13 18:00 = 30 h; → merged 10-15 00:00 = 30 h
    {"number": 11, "headRefName": "agent/issue-1", "createdAt": "2026-10-13T18:00:00Z",
     "mergedAt": "2026-10-15T00:00:00Z", "closedAt": "2026-10-15T00:00:00Z", "state": "MERGED",
     "reviews": [rv("blocking"), rv("clean")]},
    # pickup 10-14 08:00 → opened 10-14 10:00 = 2 h; closed unmerged
    {"number": 12, "headRefName": "agent/issue-2", "createdAt": "2026-10-14T10:00:00Z",
     "mergedAt": None, "closedAt": "2026-10-16T10:00:00Z", "state": "CLOSED",
     "reviews": [rv("blocking"), rv("blocking"), rv("blocking")]},
    # Ready 10-15 00:00 → opened 10-16 00:00 = 24 h; → merged 10-18 12:00 = 60 h
    {"number": 13, "headRefName": "agent/issue-4", "createdAt": "2026-10-16T00:00:00Z",
     "mergedAt": "2026-10-18T12:00:00Z", "closedAt": "2026-10-18T12:00:00Z", "state": "MERGED",
     "reviews": [rv("clean")]},
    # a person's PR: not the agents'
    {"number": 14, "headRefName": "feature/x", "createdAt": "2026-10-13T00:00:00Z",
     "mergedAt": "2026-10-13T01:00:00Z", "closedAt": "2026-10-13T01:00:00Z", "state": "MERGED", "reviews": []},
]
EVENTS = [
    {"event": "labeled", "label": "agent:needs-human", "created_at": "2026-10-13T09:00:00Z", "issue": 1},    # 6 h, ended
    {"event": "unlabeled", "label": "agent:needs-human", "created_at": "2026-10-13T15:00:00Z", "issue": 1},
    {"event": "labeled", "label": "prototype:review", "created_at": "2026-10-10T00:00:00Z", "issue": 5},     # 60 h, 12 h in the week
    {"event": "unlabeled", "label": "prototype:review", "created_at": "2026-10-12T12:00:00Z", "issue": 5},
    {"event": "labeled", "label": "agent:needs-human", "created_at": "2026-10-18T00:00:00Z", "issue": 6},    # open: 24 h
    {"event": "labeled", "label": "agent:needs-human", "created_at": "2026-10-20T00:00:00Z", "issue": 7},    # after the week
    {"event": "labeled", "label": "agent:needs-human", "created_at": "2026-10-01T00:00:00Z", "issue": 8},    # before it
    {"event": "unlabeled", "label": "agent:needs-human", "created_at": "2026-10-02T00:00:00Z", "issue": 8},
    {"event": "labeled", "label": "E6", "created_at": "2026-10-13T00:00:00Z", "issue": 1},                  # not a wait
]
GITLOG = "\n".join([
    "2026-10-14T10:00:00+02:00\tRevert \"Add x (#5)\"",
    "2026-10-11T23:00:00Z\tRevert \"old\"",
    "2026-10-15T00:00:00Z\tMerge pull request #11 from rchau73/agent/issue-1",
])


class AWeekOfFixtures(unittest.TestCase):
    """Scenario: a week of fixtures — the report's counts, medians and waits match hand-computed values."""

    @classmethod
    def setUpClass(cls):
        cls.dir = tempfile.mkdtemp()
        for name, data in (("issues.json", ISSUES), ("prs.json", PRS)):
            json.dump(data, open(os.path.join(cls.dir, name), "w"))
        # events as JSON lines, the way metrics.sh writes them
        open(os.path.join(cls.dir, "events.json"), "w").write("\n".join(json.dumps(e) for e in EVENTS))
        open(os.path.join(cls.dir, "gitlog.txt"), "w").write(GITLOG)
        cls.out = metrics.report("2026-W42", cls.dir)

    def test_header(self):
        self.assertIn("# Agent metrics 2026-W42 (2026-10-12 → 2026-10-18)", self.out)

    def test_stage_counts(self):
        # SME 1,3 · triaged 1,3,4 · Ready 1,4 · pickups 1,1,2,4 · PRs 11,12,13 · merged 11,13 · deployed 1 · fixed 1 · reopened 3
        self.assertIn("| 2 | 3 | 2 | 4 | 3 | 2 | 1 | 1 | 1 |", self.out)

    def test_cycle_times(self):
        self.assertIn("| Ready → PR | 3 | 24.0 h | 30.0 h |", self.out)
        self.assertIn("| PR → merge | 2 | 45.0 h | 60.0 h |", self.out)
        self.assertIn("| merge → deploy | 1 | 2.0 h | 2.0 h |", self.out)

    def test_quality(self):
        self.assertIn("(PRs merged or closed in the week: 3): median 2, max 3; #11 2 (1 blocking), #12 3 (3 blocking), #13 1 (0 blocking)", self.out)
        self.assertIn("- PRs closed unmerged: 1 — #12", self.out)
        self.assertIn("- Reverts on main: 1\n  - Revert \"Add x (#5)\"", self.out)
        self.assertIn("- Dev agent blocks (needs a person): 1\n  - #1 10-13 09:00: Which threshold?", self.out)

    def test_waits(self):
        self.assertIn("`agent:needs-human` 30.0 h, `prototype:review` 12.0 h", self.out)
        self.assertIn("- Waits that ended in the week: 2, median 33.0 h, p90 60.0 h", self.out)
        self.assertIn("- Still waiting at the week's end: 1\n  - #6 `agent:needs-human` for 24.0 h", self.out)


def write_dir(issues=(), prs=(), events=(), gitlog=""):
    d = tempfile.mkdtemp()
    json.dump(list(issues), open(os.path.join(d, "issues.json"), "w"))
    json.dump(list(prs), open(os.path.join(d, "prs.json"), "w"))
    open(os.path.join(d, "events.json"), "w").write("\n".join(json.dumps(e) for e in events))
    open(os.path.join(d, "gitlog.txt"), "w").write(gitlog)
    return d


def run_line(ts, agent, run_id, event, outcome=None, issue=None):
    return json.dumps({"ts": ts, "agent": agent, "run_id": run_id, "issue": issue, "pr": None, "event": event,
                       "outcome": outcome, "detail": "free text never shown" if outcome else None,
                       "log": None, "commit": None})


# Week 2026-W42. dev: 30, 10 and 2 min ended + one open; review: 20 (failed) and 40 (abandoned) min.
LEDGER = "\n".join([
    run_line("2026-10-11T23:00:00Z", "dev", "dev-old", "start"),          # started the week before
    run_line("2026-10-12T00:30:00Z", "dev", "dev-old", "end", "pr_opened"),
    run_line("2026-10-13T10:00:00Z", "dev", "dev-1", "start", issue=1),
    run_line("2026-10-13T10:30:00Z", "dev", "dev-1", "end", "pr_opened", issue=1),
    run_line("2026-10-14T10:00:00Z", "dev", "dev-2", "start"),
    run_line("2026-10-14T10:10:00Z", "dev", "dev-2", "end", "blocked"),
    run_line("2026-10-15T10:00:00Z", "dev", "dev-3", "start"),
    run_line("2026-10-15T10:02:00Z", "dev", "dev-3", "end", "skipped_limit"),
    run_line("2026-10-17T10:00:00Z", "dev", "dev-4", "start"),           # open: no end line
    run_line("2026-10-16T09:00:00Z", "review", "rev-1", "start"),
    run_line("2026-10-16T09:20:00Z", "review", "rev-1", "end", "failed"),
    run_line("2026-10-16T12:00:00Z", "review", "rev-2", "start"),
    run_line("2026-10-16T12:40:00Z", "review", "rev-2", "end", "abandoned"),
    "not a ledger line",
]) + "\n"


class RunsSection(unittest.TestCase):
    """#290: the Runs table from the run ledger."""

    def setUp(self):
        self.dir = write_dir()
        self.ledger = os.path.join(self.dir, "agent_runs.jsonl")
        open(self.ledger, "w").write(LEDGER)
        self.out = metrics.report("2026-W42", self.dir, runs_file=self.ledger, stuck_file="")

    # Scenario: a week with runs
    def test_a_week_with_runs(self):
        self.assertIn("| agent | runs | outcomes | failed % | limit % | median | p90 | open at week end |", self.out)
        self.assertIn("| dev | 4 | blocked 1, pr_opened 1, skipped_limit 1 | 0 % | 25 % | 10.0 min | 30.0 min | 1 |", self.out)
        self.assertIn("| review | 2 | abandoned 1, failed 1 | 100 % | 0 % | 30.0 min | 40.0 min | 0 |", self.out)
        self.assertIn("- 1 ledger line(s) skipped", self.out)
        self.assertNotIn("free text", self.out)  # outcome names only, never a detail

    # Scenario: a run still open — counted in "open at week end", not in the durations
    def test_a_run_still_open(self):
        open(self.ledger, "w").write(run_line("2026-10-17T10:00:00Z", "dev", "dev-4", "start") + "\n")
        out = metrics.report("2026-W42", self.dir, runs_file=self.ledger, stuck_file="")
        self.assertIn("| dev | 1 | – | 0 % | 0 % | – | – | 1 |", out)

    def test_a_run_ending_after_the_week_is_open_at_its_end(self):
        open(self.ledger, "w").write(run_line("2026-10-18T23:00:00Z", "dev", "d", "start") + "\n"
                                     + run_line("2026-10-19T01:00:00Z", "dev", "d", "end", "pr_opened") + "\n")
        out = metrics.report("2026-W42", self.dir, runs_file=self.ledger, stuck_file="")
        self.assertIn("| dev | 1 | – | 0 % | 0 % | – | – | 1 |", out)

    # Scenario: runs outside the week — a run that started the week before is not counted
    def test_runs_outside_the_week(self):
        open(self.ledger, "w").write("\n".join(LEDGER.splitlines()[:2]) + "\n")
        out = metrics.report("2026-W42", self.dir, runs_file=self.ledger, stuck_file="")
        self.assertIn("- no run started in the week", out)
        self.assertNotIn("| dev |", out)

    # Scenario: no ledger — the report says ledger unavailable and every other section is unchanged
    def test_no_ledger(self):
        d = tempfile.mkdtemp()
        for name, data in (("issues.json", ISSUES), ("prs.json", PRS)):
            json.dump(data, open(os.path.join(d, name), "w"))
        open(os.path.join(d, "events.json"), "w").write("\n".join(json.dumps(e) for e in EVENTS))
        open(os.path.join(d, "gitlog.txt"), "w").write(GITLOG)
        open(os.path.join(d, "agent_runs.jsonl"), "w").write(LEDGER)
        missing = metrics.report("2026-W42", d, runs_file=os.path.join(d, "none.jsonl"), stuck_file="")
        present = metrics.report("2026-W42", d, runs_file=os.path.join(d, "agent_runs.jsonl"), stuck_file="")
        self.assertIn("## Runs (run ledger, started in the week)\n\n- ledger unavailable: none.jsonl not found\n", missing)
        self.assertNotIn(d, missing)  # no local path: the report goes to a public repository
        cut = lambda s: (s.split("## Runs")[0], s.split("## Overruled")[1])  # noqa: E731
        self.assertEqual(cut(missing), cut(present))

    def test_unreadable_ledger(self):
        out = metrics.report("2026-W42", self.dir, runs_file=self.dir, stuck_file="")  # a directory
        self.assertIn("- ledger unavailable:", out)

    def test_cli_passes_the_ledger(self):
        import io
        from contextlib import redirect_stdout
        buf = io.StringIO()
        with redirect_stdout(buf):
            metrics.main(["metrics.py", "report", "2026-W42", self.dir, "--runs", self.ledger, "--stuck", "",
                          "--repo", "o/r"])
        self.assertIn("| review | 2 |", buf.getvalue())


def person(at, body):
    return {"createdAt": at, "body": body, "author": {"login": "rchau73"}}


OV_ISSUES = [
    {"number": 21, "labels": lab("agent:sme"), "state": "CLOSED", "stateReason": "NOT_PLANNED",
     "createdAt": "2026-10-10T00:00:00Z", "closedAt": "2026-10-14T00:00:00Z", "comments": []},
    {"number": 22, "labels": lab("agent:sme"), "state": "CLOSED", "stateReason": "COMPLETED",
     "createdAt": "2026-10-10T00:00:00Z", "closedAt": "2026-10-15T00:00:00Z",
     "comments": [person("2026-10-14T23:00:00Z", "Not a bug: the bot is meant to wait here.")]},
    {"number": 23, "labels": lab("agent:sme"), "state": "CLOSED", "stateReason": "COMPLETED",  # fixed by an agent PR
     "createdAt": "2026-10-10T00:00:00Z", "closedAt": "2026-10-15T00:00:00Z",
     "comments": [person("2026-10-14T23:00:00Z", "Thanks, merged.")]},
    {"number": 24, "labels": lab("agent:sme"), "state": "CLOSED", "stateReason": "COMPLETED",  # an agent's last word
     "createdAt": "2026-10-10T00:00:00Z", "closedAt": "2026-10-16T00:00:00Z",
     "comments": [c("2026-10-15T23:00:00Z", "🚀 Deployed to **mvp** (testnet) in abc (x).")]},
    {"number": 25, "labels": lab("agent:sme"), "state": "CLOSED", "stateReason": "NOT_PLANNED",  # the week before
     "createdAt": "2026-10-01T00:00:00Z", "closedAt": "2026-10-05T00:00:00Z", "comments": []},
    {"number": 30, "labels": [], "state": "OPEN", "createdAt": "2026-10-10T00:00:00Z", "comments": []},
    {"number": 31, "labels": [], "state": "OPEN", "createdAt": "2026-10-10T00:00:00Z", "comments": [
        c("2026-10-15T08:00:00Z", "🧭 **PM agent** — what this card waited for is done. → **Ready**. <!-- agent:pm -->")]},
    {"number": 32, "labels": [], "state": "OPEN", "createdAt": "2026-10-10T00:00:00Z", "comments": []},
]
OV_EVENTS = [
    {"event": "unlabeled", "label": "E3", "created_at": "2026-10-14T10:00:00Z", "issue": 30},          # a person
    {"event": "unlabeled", "label": "pm:waiting", "created_at": "2026-10-15T08:00:30Z", "issue": 31},  # the PM's release
    {"event": "labeled", "label": "E6", "created_at": "2026-10-15T09:00:00Z", "issue": 32},            # the PM's retag
    {"event": "unlabeled", "label": "E2", "created_at": "2026-10-15T09:00:01Z", "issue": 32},
    {"event": "unlabeled", "label": "agent:needs-human", "created_at": "2026-10-15T10:00:00Z", "issue": 32},  # not a PM label
    {"event": "unlabeled", "label": "E4", "created_at": "2026-10-20T10:00:00Z", "issue": 30},          # after the week
]


def pr(number, issue, reviews, merged=None, closed=None, head="abc123ffff"):
    return {"number": number, "headRefName": f"agent/issue-{issue}", "headRefOid": head,
            "createdAt": "2026-10-13T00:00:00Z", "mergedAt": merged, "closedAt": closed or merged,
            "state": "MERGED" if merged else "CLOSED", "reviews": reviews}


OV_PRS = [
    pr(41, 23, [rv("blocking"), rv("clean")], merged="2026-10-14T00:00:00Z"),  # reverted below
    pr(42, 30, [rv("clean")], closed="2026-10-15T00:00:00Z"),                  # closed unmerged
    pr(43, 31, [rv("clean")], merged="2026-10-16T00:00:00Z"),                  # stays merged
    pr(44, 32, [rv("clean"), rv("blocking")], closed="2026-10-15T00:00:00Z"),  # last review blocking
    pr(45, 33, [rv("clean")], closed="2026-10-15T00:00:00Z", head="def4560000"),  # clean on an older head
]
OV_GITLOG = "2026-10-16T10:00:00Z\tRevert \"Merge pull request #41 from rchau73/agent/issue-23\""


class OverruledSection(unittest.TestCase):
    """#292: what a person overruled."""

    @classmethod
    def setUpClass(cls):
        d = write_dir(OV_ISSUES, OV_PRS, OV_EVENTS, OV_GITLOG)
        cls.out = metrics.report("2026-W42", d, runs_file="", stuck_file="", repo="o/r")

    # Scenario: an SME finding closed as not planned
    def test_sme_finding_closed_as_not_planned(self):
        self.assertIn("- SME findings rejected: 2 of 4 SME findings closed in the week"
                      "\n  - [#21](https://github.com/o/r/issues/21) closed as not planned"
                      "\n  - [#22](https://github.com/o/r/issues/22) closed after a person's comment", self.out)
        self.assertNotIn("issues/23)", self.out)
        self.assertNotIn("issues/24)", self.out)
        self.assertNotIn("issues/25)", self.out)

    # Scenario: a clean-reviewed PR reverted (and one closed unmerged)
    def test_clean_reviewed_pr_reverted(self):
        self.assertIn("2 of 3 clean-reviewed PRs merged or closed in the week"
                      "\n  - [#41](https://github.com/o/r/pull/41) reverted: Revert \"Merge pull request #41"
                      , self.out)
        self.assertIn("\n  - [#42](https://github.com/o/r/pull/42) closed unmerged", self.out)
        for n in (43, 44, 45):
            self.assertNotIn(f"pull/{n})", self.out)

    # Scenario: a label removed by a person — agent-made removals do not count
    def test_label_removed_by_a_person(self):
        self.assertIn("1 of 1 PM comments in the week\n  - [#30](https://github.com/o/r/issues/30) `E3` removed 10-14 10:00",
                      self.out)
        self.assertNotIn("issues/31) `pm:waiting`", self.out)
        self.assertNotIn("`E2` removed", self.out)
        self.assertNotIn("`agent:needs-human` removed", self.out)
        self.assertIn("board moves", self.out)

    # Scenario: nothing overruled — the section says none, with the totals
    def test_nothing_overruled(self):
        out = metrics.report("2026-W42", write_dir(OV_ISSUES[3:4], OV_PRS[2:3]), runs_file="", stuck_file="")
        self.assertIn("## Overruled by a person\n\n- none in the week\n", out)
        self.assertIn("- SME findings rejected: 0 of 1 SME findings closed in the week\n", out)
        self.assertIn("0 of 1 clean-reviewed PRs merged or closed in the week", out)


class StuckSection(unittest.TestCase):
    """#329: the stuck-card line of the weekly report."""

    def test_cards_found_in_the_week(self):
        d = write_dir()
        f = os.path.join(d, "reports.jsonl")
        open(f, "w").write("\n".join(json.dumps(r) for r in [
            {"ts": "2026-10-13T06:00:00Z", "issue": 12, "column": "In progress", "hours_idle": 8.0, "reason": "no agent run", "fault": True},
            {"ts": "2026-10-13T12:00:00Z", "issue": 12, "column": "In progress", "hours_idle": 14.0, "reason": "no agent run", "fault": True},
            {"ts": "2026-10-14T12:00:00Z", "issue": 13, "column": "In review", "hours_idle": 30.0, "reason": "paused", "fault": False},
            {"ts": "2026-10-20T12:00:00Z", "issue": 14, "column": "In review", "hours_idle": 30.0, "reason": "paused", "fault": False},
        ]) + "\nbad line\n")
        out = metrics.report("2026-W42", d, runs_file="", stuck_file=f)
        self.assertIn("- Cards found stuck in the week: 2 (1 with no reason, commented on)"
                      "\n  - #12 In progress 14.0 h (no agent run)\n  - #13 In review 30.0 h (paused)", out)

    def test_no_log(self):
        out = metrics.report("2026-W42", write_dir(), runs_file="", stuck_file="")
        self.assertIn("## Stuck cards\n\n- no stuck-card check recorded", out)


class EmptyWeek(unittest.TestCase):
    def test_no_data_renders_dashes_not_zeros(self):
        d = tempfile.mkdtemp()
        for name in ("issues.json", "prs.json"):
            open(os.path.join(d, name), "w").write("[]")
        open(os.path.join(d, "events.json"), "w").write("")
        open(os.path.join(d, "gitlog.txt"), "w").write("")
        out = metrics.report("2026-W42", d)
        self.assertIn("| Ready → PR | 0 | – | – |", out)
        self.assertIn("no PR merged or closed in the week", out)
        self.assertIn("Waits that ended in the week: 0, median –", out)


class Helpers(unittest.TestCase):
    def test_previous_week(self):
        import datetime as dt
        self.assertEqual(metrics.previous_week(dt.date(2026, 10, 19)), "2026-W42")
        self.assertEqual(metrics.previous_week(dt.date(2026, 1, 5)), "2026-W01")
        self.assertEqual(metrics.previous_week(dt.date(2026, 1, 1)), "2025-W52")

    def test_percentiles(self):
        self.assertIsNone(metrics.p90([]))
        self.assertEqual(metrics.p90([5]), 5)
        self.assertEqual(metrics.p90(list(range(1, 11))), 9)
        self.assertEqual(metrics.median([1, 2, 3, 10]), 2.5)

    def test_events_as_array(self):
        f = os.path.join(tempfile.mkdtemp(), "e.json")
        open(f, "w").write(json.dumps(EVENTS[:2]))
        self.assertEqual(len(metrics.load_events(f)), 2)


if __name__ == "__main__":
    unittest.main()
