"""Tests of the mechanical review of an assistant request (#852):
request_filter.py's duplicate, evidence and explained-by rules.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import request_filter as rf  # noqa: E402


def req(rid="R-0001", kind="bug", title="P&L chart wrong", what="The chart drops at 14:00",
        evidence=({"text": "Total P&L at 14:00", "citation": "pnl:btc/2026-10-27T14:00:00Z"},), steps=(), **extra):
    r = {"id": rid, "kind": kind, "fields": {"title": title, "what": what, "evidence": list(evidence), "steps": list(steps)},
         "journal_refs": []}
    r.update(extra)
    return r


def card(number, title, *labels):
    return {"number": number, "title": title, "labels": [{"name": l} for l in labels]}


class Close(unittest.TestCase):
    def test_close_titles(self):
        self.assertTrue(rf.close("P&L chart wrong", "[E3] P&L chart is wrong"))
        self.assertTrue(rf.close("Income page shows the wrong fee total", "Income page shows wrong fee total"))
        self.assertFalse(rf.close("P&L chart wrong", "Strikes too close to spot"))
        self.assertFalse(rf.close("", "anything"))
        self.assertFalse(rf.close(None, None))


class Review(unittest.TestCase):
    def test_duplicate_of_a_card_of_the_same_kind(self):
        v = rf.review(req(), [card(5, "Strikes too close", "bug"), card(77, "[E3] P&L chart is wrong", "bug", "E3")], [])
        self.assertEqual(v[0], "duplicate")
        self.assertEqual(v[2], 77)
        self.assertIn("#77", v[1])

    def test_a_card_of_the_other_kind_is_not_a_duplicate(self):
        self.assertIsNone(rf.review(req(), [card(77, "P&L chart wrong", "enhancement")], []))

    def test_matched_on_what_too(self):
        v = rf.review(req(title="Odd"), [card(9, "The chart drops at 14:00", "bug")], [])
        self.assertEqual((v[0], v[2]), ("duplicate", 9))

    def test_duplicate_of_an_earlier_request(self):
        v = rf.review(req("R-0002", title="P&L chart is wrong"), [], [req("R-0001")])
        self.assertEqual((v[0], v[2], v[3]), ("duplicate", None, "R-0001"))
        self.assertIsNone(rf.review(req("R-0002", kind="enhancement"), [], [req("R-0001")]))

    def test_no_evidence_and_fewer_than_two_steps(self):
        v = rf.review(req(evidence=(), steps=("Open Positions",)), [], [])
        self.assertEqual(v[0], "needs_info")
        self.assertIn("1 step", v[1])
        self.assertIn("journal", v[1])

    def test_evidence_without_a_source_does_not_count(self):
        self.assertEqual(rf.review(req(evidence=({"text": "It looked wrong", "citation": ""},)), [], [])[0], "needs_info")

    def test_two_steps_or_a_journal_reference_are_enough(self):
        self.assertIsNone(rf.review(req(evidence=(), steps=("Open Positions", "Sort by delta")), [], []))
        self.assertIsNone(rf.review(req(evidence=(), journal_refs=["journal:btc/42"]), [], []))

    def test_explained_by_the_help(self):
        v = rf.review(req(explained_by=["help:monitor-guide#income"]), [], [])
        self.assertEqual(v[0], "not_a_bug")
        self.assertIn("help:monitor-guide#income", v[1])
        self.assertIsNone(rf.review(req(explained_by=[""]), [], []))

    def test_order_duplicate_first(self):
        r = req(evidence=(), explained_by=["help:x"])
        self.assertEqual(rf.review(r, [card(1, "P&L chart wrong", "bug")], [])[0], "duplicate")
        self.assertEqual(rf.review(r, [], [])[0], "needs_info")

    def test_malformed_request(self):
        self.assertEqual(rf.review({"id": "R-1"}, [card(1, "x", "bug")], [])[0], "needs_info")


if __name__ == "__main__":
    unittest.main()
