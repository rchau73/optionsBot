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
