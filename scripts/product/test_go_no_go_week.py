"""Tests of the weekly go / no-go reading (#738).
python3 -m unittest discover -s scripts/product -p 'test_*.py'"""
import json
import os
import shutil
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import go_no_go as gng  # noqa: E402
import go_no_go_week as week  # noqa: E402

LEGAL = os.path.join(os.path.dirname(os.path.dirname(HERE)), "docs", "legal", "preliminary-answers.md")


def card_for(root, share=None, bot="btc"):
    """The scorecard on a temp tree: a tally only when share is given, no fake door, no weekly reports."""
    tally = None
    if share is not None:
        tally = os.path.join(root, "tally.json")
        with open(tally, "w") as f:
            json.dump({"delegation": {"cycle holder": {"n": 20, "share_pct": share}}}, f)
    return gng.build(tally, None, root, bot, LEGAL)


class WeeklyReading(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = self.tmp.name
        self.history = os.path.join(self.root, "data", "product", "go_no_go_history.jsonl")
        self.alerts = os.path.join(self.root, "alert")

    def read(self, day, **kw):
        return week.run(card_for(self.root, **kw), self.history, day, self.root, self.alerts)

    def lines(self):
        with open(self.history) as f:
            return [json.loads(line) for line in f]

    # Scenario: a weekly reading is kept
    def test_a_weekly_reading_is_kept_with_rows_missing_and_no_issue(self):
        self.assertFalse(self.read("2026-11-09"))  # a fresh checkout: no data/product/ at all
        line, = self.lines()
        self.assertEqual(line["date"], "2026-11-09")
        self.assertEqual(line["overall"], "incomplete")
        self.assertEqual(sorted({r["status"] for r in line["rows"]}), ["missing"])
        self.assertFalse(os.path.exists(self.alerts))

    # Scenario: a kill row turns to fails
    def test_a_kill_row_turning_to_fails_opens_one_issue(self):
        self.read("2026-11-09")
        self.assertTrue(self.read("2026-11-16", share=20.0))
        self.assertEqual(self.lines()[-1]["overall"], "kill criterion met")
        with open(os.path.join(self.alerts, "title.txt")) as f:
            title = f.read()
        with open(os.path.join(self.alerts, "body.md")) as f:
            body = f.read()
        self.assertIn("kill criterion met", title)
        self.assertIn("Survey: cycle holders who would delegate", body)
        self.assertIn("missing → **fails**", body)
        self.assertIn("incomplete → **kill criterion met**", body)
        self.assertIn("`<env clone>/tally.json`", body)  # the source file, never an absolute path
        self.assertTrue(body.startswith(week.HEADER))
        self.assertTrue(body.rstrip().endswith(week.MARKER))
        self.assertNotIn(self.root, body)

    # Scenario: nothing changed
    def test_nothing_changed_keeps_the_line_and_opens_no_issue(self):
        self.read("2026-11-09", share=50.0)  # missing → meets: this first reading alerts
        shutil.rmtree(self.alerts)
        self.assertFalse(self.read("2026-11-16", share=50.0))
        self.assertEqual(len(self.lines()), 2)
        self.assertFalse(os.path.exists(self.alerts))

    # Scenario: no amounts
    def test_no_amounts_in_a_reading_or_an_issue(self):
        dirty = card_for(self.root, share=20.0)
        dirty["rows"][2]["value"] = "engine 1.2 vs hold 0.9 on 1.2345 BTC"
        dirty["rows"][2]["threshold"] = "worth $70000"
        dirty["rows"][2]["status"] = "meets"
        week.run(dirty, self.history, "2026-11-09", self.root, self.alerts)
        with open(os.path.join(self.alerts, "body.md")) as f:
            text = json.dumps(self.lines()) + f.read()
        for amount in ("BTC", "$", "USD", "1.2345", "70000"):
            self.assertNotIn(amount, text)

    def test_overall_to_no_kill_criterion_met_alerts_without_a_row_change(self):
        old = {"date": "d1", "overall": "incomplete", "rows": [{"input": "a", "status": "meets"}]}
        new = {"date": "d2", "overall": "no kill criterion met",
               "rows": [{"input": "a", "status": "meets", "value": "1", "threshold": "t", "source": "s", "date": "d"}]}
        title, body = week.alert(old, new)
        self.assertIn("no kill criterion met", title)
        self.assertIn("incomplete → **no kill criterion met**", body)
        self.assertIn("the product owner's", body)

    def test_a_row_change_without_a_verdict_still_alerts_and_unchanged_does_not(self):
        row = {"input": "a", "value": "1", "threshold": "t", "source": "s", "date": "d"}
        old = {"date": "d1", "overall": "incomplete", "rows": [dict(row, status="missing")]}
        new = {"date": "d2", "overall": "incomplete", "rows": [dict(row, status="meets")]}
        self.assertIn("a row changed status", week.alert(old, new)[0])
        self.assertIsNone(week.alert(new, dict(new, date="d3")))

    def test_a_damaged_history_line_is_skipped(self):
        os.makedirs(os.path.dirname(self.history))
        with open(self.history, "w") as f:
            f.write(json.dumps({"date": "d", "overall": "incomplete", "rows": []}) + "\nnot json\n")
        self.assertEqual(week.last_reading(self.history)["date"], "d")


if __name__ == "__main__":
    unittest.main()
