"""Tests of the fake-door result tool (#303).
python3 -m unittest discover -s scripts/product -p 'test_*.py'"""
import io
import json
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import fakedoor as fd  # noqa: E402

SIGNUP_HEADER = "submitted_at,email,confirmed,channel,country,first_bought,kept_most,first_wanted,fair_pay\n"


def signup_row(i, confirmed="yes", channel="forum-a", country="BR", bought="before 2022", kept="yes",
               wanted="testnet trial", pay="monthly subscription"):
    return (f"2026-11-0{1 + i % 9}T10:00:00Z,person{i}@example.com,{confirmed},{channel},{country},"
            f"{bought},{kept},{wanted},{pay}\n")


class RunDir:
    """A private data/product/ in a temp dir with one run in it."""

    def __init__(self, signups, visits):
        self.tmp = tempfile.TemporaryDirectory()
        self.private = os.path.join(self.tmp.name, "data", "product")
        self.run = os.path.join(self.private, "fakedoor", "run1")
        os.makedirs(self.run)
        with open(os.path.join(self.run, "signups.csv"), "w", encoding="utf-8") as f:
            f.write(SIGNUP_HEADER + "".join(signups))
        with open(os.path.join(self.run, "visits.csv"), "w", encoding="utf-8") as f:
            f.write("date,channel,visits\n" + "".join(visits))

    def main(self, *extra):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = fd.main([self.run, *extra], private_dir=self.private)
        return code, out.getvalue(), err.getvalue()

    def outputs(self):
        with open(os.path.join(self.run, "result.md"), encoding="utf-8") as f:
            md = f.read()
        with open(os.path.join(self.run, "result.json"), encoding="utf-8") as f:
            js = f.read()
        return md, js

    def close(self):
        self.tmp.cleanup()


def visits_over(days, per_day, channel="forum-a"):
    return [f"2026-11-{d + 1:02d},{channel},{per_day}\n" for d in range(days)]


class ThresholdsTest(unittest.TestCase):
    def test_thresholds_are_the_briefs(self):
        # Brief §5–§6 and PRD §6. Changing one is a new brief, not a code change.
        self.assertEqual(fd.thresholds(), {
            "min_visits": 400, "max_weeks": 4, "success_rate": 0.10, "success_lower_bound": 0.06,
            "cycle_holder_min_share": 0.5, "kill_rate": 0.03, "kill_upper_bound": 0.06,
            "survey_kill_share": 0.30,
        })


class WilsonTest(unittest.TestCase):
    def test_known_value(self):
        low, high = fd.wilson(48, 400)
        self.assertAlmostEqual(low, 0.0917, places=3)
        self.assertAlmostEqual(high, 0.1555, places=3)

    def test_zero_successes_has_a_positive_upper_bound(self):
        low, high = fd.wilson(0, 400)
        self.assertEqual(low, 0.0)
        self.assertGreater(high, 0)

    def test_no_visits_has_no_interval(self):
        self.assertIsNone(fd.wilson(0, 0))


class OutcomeTest(unittest.TestCase):
    def test_scenario_success_by_the_briefs_rule(self):
        self.assertEqual(fd.outcome(400, 48, 30, 50)[0], fd.SUCCESS)

    def test_success_rate_with_cycle_holders_under_half_iterates(self):
        self.assertEqual(fd.outcome(400, 48, 20, 50)[0], fd.ITERATE)

    def test_success_rate_with_no_segment_answers_iterates(self):
        self.assertEqual(fd.outcome(400, 48, 0, 0)[0], fd.ITERATE)

    def test_cycle_holders_exactly_half_is_success(self):
        self.assertEqual(fd.outcome(400, 48, 25, 50)[0], fd.SUCCESS)

    def test_between_kill_and_success_iterates(self):
        self.assertEqual(fd.outcome(400, 20, 10, 10)[0], fd.ITERATE)    # 5 %
        self.assertEqual(fd.outcome(400, 12, 10, 10)[0], fd.ITERATE)    # exactly 3 %

    def test_kill(self):
        self.assertEqual(fd.outcome(400, 8, 4, 8)[0], fd.KILL)          # 2 %

    def test_rate_exactly_ten_percent_is_success(self):
        self.assertEqual(fd.outcome(400, 40, 30, 40)[0], fd.SUCCESS)

    def test_scenario_not_enough_traffic_wins_over_any_rate(self):
        self.assertEqual(fd.outcome(250, 100, 50, 50)[0], fd.NOT_ENOUGH)
        self.assertEqual(fd.outcome(250, 0, 0, 0)[0], fd.NOT_ENOUGH)
        self.assertEqual(fd.outcome(399, 48, 30, 50)[0], fd.NOT_ENOUGH)


class SurveyTest(unittest.TestCase):
    def test_kill_with_survey_under_30_meets_the_prd_criterion(self):
        self.assertIn("kill criterion is met", fd.survey_sentence(fd.KILL, 0.25))

    def test_kill_with_survey_at_30_suspects_the_page(self):
        s = fd.survey_sentence(fd.KILL, 0.30)
        self.assertIn("not met", s)
        self.assertIn("the page, not the demand", s)

    def test_kill_without_survey_says_it_cannot_be_read(self):
        self.assertIn("cannot be read", fd.survey_sentence(fd.KILL, None))

    def test_no_sentence_outside_kill(self):
        self.assertIsNone(fd.survey_sentence(fd.SUCCESS, 0.10))


class ParseTest(unittest.TestCase):
    def test_first_bought_forms(self):
        self.assertTrue(fd.bought_before_2022("x", "2019"))
        self.assertTrue(fd.bought_before_2022("x", "before_2022"))
        self.assertFalse(fd.bought_before_2022("x", "2022 or later"))
        self.assertFalse(fd.bought_before_2022("x", "2023"))
        with self.assertRaises(fd.ExportError):
            fd.bought_before_2022("x", "a while ago")

    def test_bad_confirmed_value_is_refused(self):
        with self.assertRaises(fd.ExportError):
            fd.yes_no("confirmed", "maybe")


class EndToEndTest(unittest.TestCase):
    def test_scenario_success_from_the_export(self):
        # 48 confirmed (30 cycle holders of 40 that answered both, 8 left blank), 12 unconfirmed, 400 visits.
        rows = [signup_row(i) for i in range(30)]
        rows += [signup_row(i, bought="2023") for i in range(30, 40)]
        rows += [signup_row(i, bought="", kept="") for i in range(40, 48)]
        rows += [signup_row(i, confirmed="no") for i in range(48, 60)]
        d = RunDir(rows, visits_over(20, 15) + visits_over(10, 10, channel="content"))
        self.addCleanup(d.close)
        code, out, _ = d.main()
        self.assertEqual(code, 0)
        res = json.loads(d.outputs()[1])
        self.assertEqual(res["overall"]["visits"], 400)
        self.assertEqual(res["overall"]["confirmed"], 48)
        self.assertAlmostEqual(res["overall"]["rate"], 0.12)
        low, high = fd.wilson(48, 400)
        self.assertAlmostEqual(res["overall"]["low"], low)
        self.assertAlmostEqual(res["overall"]["high"], high)
        self.assertEqual(res["cycle_holders"], {"holders": 30, "answered_both": 40, "share": 0.75})
        self.assertAlmostEqual(res["confirmation"]["rate"], 0.8)
        self.assertEqual(res["outcome"], fd.SUCCESS)
        self.assertEqual(res["per_channel"]["forum-a"]["visits"], 300)
        self.assertEqual(res["per_channel"]["content"]["confirmed"], 0)
        md = d.outputs()[0]
        self.assertIn("12.0 % (Wilson 95 %: 9.2 %–15.6 %)", md)
        self.assertIn("**Outcome (§6): Success**", md)
        self.assertIn("Success", out)

    def test_scenario_not_enough_traffic_prints_the_interval(self):
        d2 = RunDir([signup_row(i) for i in range(30)], visits_over(25, 10))   # 250 visits
        self.addCleanup(d2.close)
        self.assertEqual(d2.main()[0], 0)
        res = json.loads(d2.outputs()[1])
        self.assertEqual(res["overall"]["visits"], 250)
        self.assertEqual(res["outcome"], fd.NOT_ENOUGH)
        self.assertIsNotNone(res["overall"]["low"])
        self.assertIn("Wilson 95 %", d2.outputs()[0])
        self.assertIn("under 4 weeks", d2.outputs()[0])

    def test_scenario_a_us_signup_is_a_guardrail(self):
        rows = [signup_row(0, country="US")] + [signup_row(i) for i in range(1, 10)]
        d = RunDir(rows, visits_over(28, 15))
        self.addCleanup(d.close)
        code, _, err = d.main()
        self.assertEqual(code, 0)
        md, js = d.outputs()
        self.assertTrue(md.startswith("**GUARDRAIL"), md[:80])
        self.assertIn("stop the run", md.splitlines()[0])
        self.assertEqual(json.loads(js)["guardrail_us_signups"], 1)
        self.assertIn("GUARDRAIL", err)

    def test_no_guardrail_line_without_us_signups(self):
        d = RunDir([signup_row(i) for i in range(10)], visits_over(28, 15))
        self.addCleanup(d.close)
        d.main()
        self.assertTrue(d.outputs()[0].startswith("## 10. Result"))

    def test_scenario_no_personal_data_out(self):
        d = RunDir([signup_row(i) for i in range(20)], visits_over(28, 15))
        self.addCleanup(d.close)
        d.main()
        md, js = d.outputs()
        for text in (md, js):
            self.assertNotIn("@", text)
            self.assertNotIn("example.com", text)

    def test_kill_reads_the_survey_share(self):
        d = RunDir([signup_row(i) for i in range(5)], visits_over(28, 15))   # 5 / 420
        self.addCleanup(d.close)
        d.main("--survey-share", "0.2")
        res = json.loads(d.outputs()[1])
        self.assertEqual(res["outcome"], fd.KILL)
        self.assertIn("kill criterion is met", res["survey_reading"])
        self.assertIn("With the survey", d.outputs()[0])

    def test_missing_column_is_refused(self):
        d = RunDir([], visits_over(1, 1))
        self.addCleanup(d.close)
        with open(os.path.join(d.run, "signups.csv"), "w", encoding="utf-8") as f:
            f.write("submitted_at,confirmed\n")
        code, _, err = d.main()
        self.assertEqual(code, 2)
        self.assertIn("missing column", err)

    def test_run_dir_outside_data_product_is_refused(self):
        d = RunDir([], visits_over(1, 1))
        self.addCleanup(d.close)
        err = io.StringIO()
        with redirect_stderr(err):
            code = fd.main([d.tmp.name], private_dir=d.private)
        self.assertEqual(code, 2)
        self.assertIn("refused", err.getvalue())

    def test_survey_share_out_of_range_is_refused(self):
        d = RunDir([], visits_over(1, 1))
        self.addCleanup(d.close)
        self.assertEqual(d.main("--survey-share", "25")[0], 2)

    def test_signup_on_a_channel_without_visits_has_no_rate(self):
        d = RunDir([signup_row(0, channel="dm")], visits_over(28, 15))
        self.addCleanup(d.close)
        d.main()
        res = json.loads(d.outputs()[1])
        self.assertIsNone(res["per_channel"]["dm"]["rate"])
        self.assertIn("no visits counted", d.outputs()[0])


if __name__ == "__main__":
    unittest.main()
