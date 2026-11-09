"""Tests of the comprehension-round scorer (#718). Every answer here is synthetic.
python3 -m unittest discover -s scripts/product -p 'test_*.py'"""
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import comprehension_score as cs  # noqa: E402

KEYS = {"delta": None, "stop_loss_multiplier": "video.change_setting", "strangle": None,
        "repair_cooldown_hours": "video.change_setting"}
EXAMPLE = os.path.join(cs.REPO, "docs", "product", "education", "comprehension-example.csv")


def rows_for(key, answers, rnd="R1"):
    return [{"participant": f"P{i:02d}", "key": key, "answer": a, "seconds": None, "round": rnd}
            for i, a in enumerate(answers, 1)]


def report(rows, **kw):
    return cs.build_report(rows, KEYS, **kw)


class ScenarioTest(unittest.TestCase):
    def test_a_tip_passes(self):
        s = report(rows_for("delta", ["correct"] * 4 + ["wrong"]))["tips"]["delta"]
        self.assertEqual((s["result"], s["share_correct_pct"], s["participants"]), (cs.PASS, 80.0, 5))

    def test_a_tip_fails_and_lands_on_the_rewrite_list(self):
        rep = report(rows_for("stop_loss_multiplier", ["correct"] * 2 + ["wrong"] * 3)
                     + rows_for("delta", ["correct"] * 4 + ["partly"]))
        self.assertEqual(rep["tips"]["stop_loss_multiplier"]["result"], cs.FAIL)
        self.assertEqual(rep["rewrite"], [{"key": "stop_loss_multiplier", "share_wrong_pct": 60.0,
                                           "video": "video.change_setting"}])

    def test_rewrite_list_is_ordered_by_share_wrong(self):
        rep = report(rows_for("delta", ["wrong"] * 2 + ["partly"] * 3)
                     + rows_for("stop_loss_multiplier", ["wrong"] * 4 + ["correct"])
                     + rows_for("repair_cooldown_hours", ["wrong"] * 2 + ["partly"] * 3))
        self.assertEqual([r["key"] for r in rep["rewrite"]],
                         ["stop_loss_multiplier", "delta", "repair_cooldown_hours"])
        self.assertIsNone(rep["rewrite"][1]["video"])

    def test_too_few_people(self):
        rep = report(rows_for("strangle", ["wrong"] * 3))
        self.assertEqual(rep["tips"]["strangle"]["result"], cs.TOO_FEW)
        self.assertEqual((rep["passed"], rep["failed"], rep["not_enough_people"]), (0, 0, 1))
        self.assertEqual(rep["rewrite"], [])

    def test_round_over_round(self):
        rows = (rows_for("delta", ["correct"] * 3 + ["wrong"] * 2, "R1")
                + rows_for("delta", ["correct"] * 4 + ["wrong"], "R2")
                + rows_for("stop_loss_multiplier", ["correct"] * 4 + ["wrong"], "R1")
                + rows_for("stop_loss_multiplier", ["correct"] * 3 + ["wrong"] * 2, "R2")
                + rows_for("repair_cooldown_hours", ["correct"] * 3 + ["wrong"] * 2, "R1")
                + rows_for("repair_cooldown_hours", ["correct"] * 3 + ["wrong"] * 2, "R2")
                + rows_for("strangle", ["correct"] * 5, "R2"))
        rep = report(rows)
        self.assertEqual(rep["previous_round"], "R1")
        t = rep["tips"]
        self.assertEqual(t["delta"]["change"]["direction"], "up")
        self.assertEqual(t["stop_loss_multiplier"]["change"]["direction"], "down")
        self.assertEqual(t["repair_cooldown_hours"]["change"]["direction"], "flat")
        self.assertIsNone(t["strangle"]["change"])  # not asked in R1
        self.assertIn("up (+20 pts from 60 %)", cs.render_markdown(rep))

    def test_first_round_has_no_change_column(self):
        rep = report(rows_for("delta", ["correct"] * 5))
        self.assertIsNone(rep["previous_round"])
        self.assertNotIn("Change since", cs.render_markdown(rep))

    def test_the_bar_is_one_constant(self):
        rows = rows_for("delta", ["correct"] * 3 + ["wrong"] * 2)
        self.assertEqual(report(rows)["tips"]["delta"]["result"], cs.FAIL)
        self.assertEqual(report(rows, bar_pct=60)["tips"]["delta"]["result"], cs.PASS)
        self.assertEqual(cs.PASS_BAR_PCT, 80)


class RefusalTest(unittest.TestCase):
    def table(self, header, line):
        d = tempfile.TemporaryDirectory()
        self.addCleanup(d.cleanup)
        path = os.path.join(d.name, "r.csv")
        with open(path, "w", encoding="utf-8") as f:
            f.write(header + "\n" + line + "\n")
        return path

    def run_main(self, args):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = cs.main(args)
        return code, out.getvalue(), err.getvalue()

    def test_private_data_is_refused_and_nothing_is_written(self):
        path = self.table("participant,name,key,answer,round", "P01,Ana,delta,correct,R1")
        with tempfile.TemporaryDirectory() as d:
            code, out, err = self.run_main([path, "--out-dir", os.path.join(d, "x")])
            self.assertEqual(code, 2)
            self.assertIn("'name'", err)
            self.assertEqual(out, "")
            self.assertEqual(os.listdir(d), [])

    def test_free_text_column_in_json_is_refused(self):
        with self.assertRaises(cs.ScoreError) as e:
            cs.validate([{"participant": "P01", "key": "delta", "answer": "correct", "round": "R1",
                          "notes": "x"}], KEYS)
        self.assertIn("'notes'", str(e.exception))

    def test_unknown_key_is_named(self):
        with self.assertRaises(cs.ScoreError) as e:
            cs.validate([{"participant": "P01", "key": "no_such_tip", "answer": "correct", "round": "R1"}], KEYS)
        self.assertIn("'no_such_tip'", str(e.exception))

    def test_bad_rows(self):
        base = {"participant": "P01", "key": "delta", "answer": "correct", "round": "R1"}
        for bad in ({"participant": "Ana"}, {"answer": "maybe"}, {"seconds": "12s"}, {"round": ""}):
            with self.assertRaises(cs.ScoreError, msg=bad):
                cs.validate([dict(base, **bad)], KEYS)
        with self.assertRaises(cs.ScoreError):
            cs.validate([base, dict(base)], KEYS)  # the same person twice
        with self.assertRaises(cs.ScoreError):
            report(rows_for("delta", ["correct"]), round_id="R9")

    def test_out_dir_outside_private_area_refused(self):
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(cs.ScoreError):
                cs.check_out_dir(os.path.join(d, "out"))
            inside = os.path.join(d, "comprehension", "R1")
            self.assertEqual(cs.check_out_dir(inside, os.path.join(d, "comprehension")), os.path.realpath(inside))


class PrivateAndExampleTest(unittest.TestCase):
    def test_results_stay_private(self):
        target = os.path.join(cs.PRIVATE_DIR, "2026-R2", "score.md")
        r = subprocess.run(["git", "check-ignore", "-q", target], cwd=cs.REPO)
        self.assertEqual(r.returncode, 0, f"{target} is not gitignored")

    def test_example_scores_with_the_real_keys(self):
        keys = cs.known_keys()
        rep = cs.build_report(cs.validate(cs.read_table(EXAMPLE), keys), keys)
        self.assertEqual((rep["round"], rep["previous_round"]), ("2026-R2", "2026-R1"))
        self.assertEqual(rep["tips"]["delta"]["result"], cs.PASS)
        self.assertEqual(rep["tips"]["strangle"]["result"], cs.TOO_FEW)
        self.assertEqual([r["key"] for r in rep["rewrite"]], ["repair_cooldown_hours", "stop_loss_multiplier"])
        self.assertEqual(rep["rewrite"][0]["video"], "video.change_setting")
        json.dumps(rep)


if __name__ == "__main__":
    unittest.main()
