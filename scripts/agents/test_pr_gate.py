"""Tests of pr_gate.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import pr_gate  # noqa: E402

GREEN = [{"__typename": "CheckRun", "name": "ci", "status": "COMPLETED", "conclusion": "SUCCESS"}]


def pr(**kw):
    p = {"state": "OPEN", "isDraft": False, "labels": [{"name": "agent:dev"}], "headRefOid": "abc",
         "mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN", "statusCheckRollup": GREEN}
    p.update(kw)
    return p


CLEAN = ["sha=abc verdict=clean"]


class Decide(unittest.TestCase):
    def test_ready(self):
        self.assertEqual(pr_gate.decide(pr(), CLEAN, [])[0], "ready")

    def test_conflict_first(self):
        self.assertEqual(pr_gate.decide(pr(mergeable="CONFLICTING", mergeStateStatus="DIRTY"), CLEAN, [])[0], "conflict")

    def test_behind_updates(self):
        self.assertEqual(pr_gate.decide(pr(mergeStateStatus="BEHIND"), CLEAN, [])[0], "update")

    def test_review_of_an_older_commit_is_not_enough(self):
        a, why = pr_gate.decide(pr(headRefOid="new"), CLEAN, [])
        self.assertEqual(a, "wait")
        self.assertIn("current commit", why)

    def test_latest_verdict_on_head_counts(self):
        self.assertEqual(pr_gate.decide(pr(), ["sha=abc verdict=blocking", "sha=abc verdict=clean"], [])[0], "ready")
        self.assertEqual(pr_gate.decide(pr(), ["sha=abc verdict=blocking"], [])[0], "wait")

    def test_open_dependency_waits(self):
        a, why = pr_gate.decide(pr(), CLEAN, [170])
        self.assertEqual((a, why), ("wait", "its ticket waits for #170"))

    def test_ci_running_waits(self):
        running = [{"__typename": "CheckRun", "name": "ci", "status": "IN_PROGRESS"}]
        self.assertEqual(pr_gate.decide(pr(statusCheckRollup=running), CLEAN, [])[0], "wait")

    def test_needs_human_and_draft_wait(self):
        self.assertEqual(pr_gate.decide(pr(labels=[{"name": "agent:needs-human"}]), CLEAN, [])[0], "wait")
        self.assertEqual(pr_gate.decide(pr(isDraft=True), CLEAN, [])[0], "wait")
