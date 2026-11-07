"""Tests of the E3 usability round scorer (#701).
python3 -m unittest scripts/product/test_score_usability.py"""
import io
import os
import re
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import score_usability as su  # noqa: E402

HEADER = ("| Code | Layout | Scenario (default / mixed / drawdown) | Q1 | Q1 time | Q2 | Q2 time | Q3 | Q3 time "
          "| Q4 | Q4 time | Errors (sign / unit / coin / time) | Passed |\n"
          "|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
ROUND_LINE = "Round: loss round · Date 2026-11-07 · Monitor version 16439ab\n\n"


def row(code, q2="success", q4_time="04:10", errors="", scenario="default", passed="", layout="desktop"):
    return (f"| {code} | {layout} | {scenario} | success | 00:40 | {q2} | 01:20 | success | 02:30 "
            f"| success | {q4_time} | {errors} | {passed} |")


def table(*rows, before=""):
    """Five rows, the given ones first; the rest are clean passes."""
    rows = list(rows)
    codes = {r.split("|")[1].strip() for r in rows}
    rows += [row(c) for c in su.CODES if c not in codes]
    return before + HEADER + "\n".join(rows) + "\n"


def score(text, name="baseline"):
    return su.score_round(name, su.parse_results(text))


def refused_lines(text):
    try:
        su.parse_results(text)
    except su.Refused as e:
        return e.problems
    raise AssertionError("expected the table to be refused")


class PassMarkTest(unittest.TestCase):
    def test_round_passes_at_4_of_5(self):
        s = score(table(row("P1", q2="partial")))
        self.assertTrue(s["round_passed"])
        self.assertEqual(s["passed"], ["P2", "P3", "P4", "P5"])
        self.assertIn("The round passed: 4 of 5", su.render_summary(s, "- **Result:** _"))

    def test_round_fails_at_3_of_5_and_lists_failing_questions(self):
        s = score(table(row("P1", q2="partial"), row("P2", q4_time="05:00")))
        self.assertFalse(s["round_passed"])
        text = su.render_summary(s, "- **Result:** _")
        self.assertIn("The round failed: 3 of 5", text)
        self.assertIn("- Q2: 4 of 5 success; P1 partial", text)
        self.assertIn("- Q4: 5 of 5 success; P2 at or after 5:00", text)
        self.assertNotIn("- Q1:", text)

    def test_participant_rule(self):
        for name, text_row, want in (
            ("all success before the limit", row("P1", q4_time="04:59"), True),
            ("the fourth answer at the limit", row("P1", q4_time="05:00"), False),
            ("past the limit", row("P1", q4_time="05:01"), False),
            ("partial counts as not passed", row("P1", q2="partial"), False),
            ("fail, unanswered, no time", row("P1", q2="fail"), False),
        ):
            with self.subTest(name):
                self.assertEqual("P1" in score(table(text_row))["passed"], want)

    def test_unanswered_fourth_question_needs_no_time(self):
        line = row("P1").replace("| success | 04:10 |", "| fail | — |")
        s = score(table(line))
        self.assertNotIn("P1", s["passed"])
        self.assertEqual(s["failing"]["Q4"], ["P1 fail"])
        self.assertEqual(s["median_q4_sec"], 250)

    def test_passed_column_that_disagrees_is_reported_not_trusted(self):
        s = score(table(row("P1", q2="partial", passed="yes"), row("P2", passed="yes"), row("P3", passed="no")))
        self.assertEqual(s["table_differs"], ["P1", "P3"])
        self.assertNotIn("P1", s["passed"])


class ErrorsTest(unittest.TestCase):
    def test_counts_by_type_per_question(self):
        s = score(table(row("P1", errors="Q2 sign, Q3 unit"), row("P2", errors="Q2 sign; q2 coin"),
                        row("P3", errors="none"), row("P4", errors="—")))
        self.assertEqual(s["errors"]["sign"], {"Q1": 0, "Q2": 2, "Q3": 0, "Q4": 0})
        self.assertEqual(s["errors"]["unit"]["Q3"], 1)
        self.assertEqual(s["errors"]["coin"]["Q2"], 1)
        self.assertEqual(sum(s["errors"]["time"].values()), 0)

    def test_sign_error_is_first(self):
        s = score(table(row("P1", errors="Q3 unit, Q1 time", scenario="mixed"),
                        row("P2", errors="Q2 sign", scenario="drawdown", q2="fail")), "loss")
        text = su.render_summary(s, "- **Result:** _")
        errors = text[text.index("## Errors"):]
        self.assertIn("**Sign errors in a loss scenario: 1** (Q2 P2)", errors)
        self.assertLess(errors.index("Sign errors in a loss scenario"), errors.index("| unit |"))
        self.assertLess(errors.index("Sign errors in a loss scenario"), errors.index("| sign |"))

    def test_sign_error_on_the_profit_scenario_is_not_a_loss_sign_error(self):
        s = score(table(row("P1", errors="Q2 sign", scenario="default")))
        self.assertEqual(s["loss_sign"], [])
        self.assertNotIn("loss scenario", su.render_summary(s, "- **Result:** _"))

    def test_loss_round_without_a_sign_error_says_so(self):
        s = score(table(row("P1", scenario="mixed")))
        self.assertIn("**Sign errors in a loss scenario: 0** (none)", su.render_summary(s, "- **Result:** _"))


class CompareTest(unittest.TestCase):
    def test_two_rounds_compared(self):
        baseline = score(table(row("P1", errors="Q3 unit")), "baseline")
        loss = score(table(row("P1", errors="Q2 sign, Q2 sign", scenario="mixed", q2="fail"),
                           row("P2", q2="partial", errors="Q1 time")), "loss")
        text = su.render_comparison(baseline, loss)
        self.assertIn("| | baseline | loss | Change |", text)
        self.assertIn("| Participants passed | 5 | 3 | -2 |", text)
        self.assertIn("| Sign errors in a loss scenario | 0 | 2 | +2 |", text)
        self.assertIn("| sign errors | 0 | 2 | +2 |", text)
        self.assertIn("| unit errors | 1 | 0 | -1 |", text)
        self.assertIn("| coin errors | 0 | 0 | +0 |", text)
        self.assertIn("| time errors | 0 | 1 | +1 |", text)


class PersonalDataTest(unittest.TestCase):
    def test_refused_with_the_line_number(self):
        name_row = row("P2").replace("| desktop |", "| Maria |")
        for name, text, line, why in (
            ("e-mail in a free cell", table(row("P2", errors="ana.souza@example.com")), 3, su.IS_EMAIL),
            ("e-mail in a code cell", table(row("P1")).replace("| P5 |", "| p@x.io |"), 7, su.IS_EMAIL),
            ("phone in a cell", table(row("P1", errors="+55 11 91234-5678")), 3, su.IS_PHONE),
            ("local phone number", table(row("P3", errors="9123-4567")), 3, su.IS_PHONE),
            ("a name in a cell", table(name_row), 3, su.NAME_LIKE),
            ("a name as the code", table(row("P1")).replace("| P5 |", "| Maria |"), 7, su.NAME_LIKE),
            ("a code past P5", table(row("P1")).replace("| P5 |", "| P6 |"), 7, su.NAME_LIKE),
            ("a lower-case code", table(row("P1")).replace("| P5 |", "| p5 |"), 7, su.NAME_LIKE),
            ("a free-text error", table(row("P1", errors="Joao was confused")), 3, su.NAME_LIKE),
            ("a labelled line", table(before="Name: Maria Silva\n"), 1, su.NAME_LIKE),
            ("a note below", table() + "called Maria\n", 8, su.NAME_LIKE),
            ("a name on the round line", table(before="Round: baseline · Date 2026-11-07 · Monitor version 16439ab"
                                                      " · Moderator Maria\n"), 1, su.NAME_LIKE),
            ("an extra column", table().replace("| Passed |", "| Passed | Name |", 1), 1, su.NAME_LIKE),
        ):
            with self.subTest(name):
                self.assertIn((line, why), refused_lines(text))

    def test_every_bad_line_is_named(self):
        text = table(row("P1", errors="a@b.co"), row("P2", errors="c@d.co"))
        self.assertEqual([p[0] for p in refused_lines(text)], [3, 4])

    def test_not_personal_data(self):
        for line in ("Round: baseline · Date 2026-11-07 · Monitor version 16439ab",
                     "Round: release · Date (2026-11-07) · Monitor version (0123456789abcdef0123456789abcdef01234567)",
                     "Participants passed: 4 of 5 · Round passed (≥ 4 of 5): yes"):
            with self.subTest(line):
                self.assertIsNone(su.personal_data(line))
                self.assertEqual(len(su.parse_results(table(before=line + "\n"))), 5)

    def test_incomplete_tables_are_refused(self):
        unscored = row("P1").replace("| success | 00:40 |", "|  |  |")
        no_time = row("P1", q4_time="")
        for name, text, want in (
            ("a question not scored", table(unscored), (3, su.NOT_SCORED)),
            ("a success without a time", table(no_time), (3, su.NOT_SCORED)),
            ("no scenario", table(row("P1", scenario="")), (3, su.NOT_SCORED)),
            ("a code twice", table(row("P1")) + row("P1") + "\n", (8, "the same participant code twice")),
            ("a participant missing", table().replace(row("P4") + "\n", ""), (None, "P4 has no row")),
            ("no table", "\n", (None, "P1 has no row")),
        ):
            with self.subTest(name):
                self.assertIn(want, refused_lines(text))


class TemplateTest(unittest.TestCase):
    """The kit is the source: the script reads what section 8 prints, and section 7 points to it."""

    def setUp(self):
        with open(su.PROTOCOL, encoding="utf-8") as f:
            self.doc = f.read()

    def test_blank_template_is_only_unscored(self):
        start = self.doc.index("### Results table")
        problems = refused_lines(self.doc[start:self.doc.index("### Synthesis")])
        self.assertEqual({why for _, why in problems}, {su.NOT_SCORED})
        self.assertEqual(len(problems), 5)

    def test_section_7_points_to_the_script(self):
        section = self.doc[self.doc.index("## 7. Scoring"):self.doc.index("## 8. Templates")]
        self.assertIn("scripts/product/score_usability.py", section)
        self.assertIn("`Q2 sign, Q3 unit`", section)

    def test_pass_mark_matches_section_7(self):
        self.assertIn(f"at least {su.ROUND_PASS_MARK} of the 5 participants pass", self.doc)
        self.assertIn(f"before {su.Q4_LIMIT_SEC // 60}:00", self.doc)


class CommandTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def write(self, rel, text):
        path = os.path.join(self.tmp.name, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        return path

    def run_main(self, *args):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = su.main(list(args))
        return code, out.getvalue(), err.getvalue()

    def summary(self, path):
        with open(os.path.join(os.path.dirname(path), "summary.md"), encoding="utf-8") as f:
            return f.read()

    def test_summary_beside_the_table_with_the_synthesis_skeleton(self):
        path = self.write("baseline/results.md", table(row("P1", q2="partial")))
        code, out, _ = self.run_main(path)
        self.assertEqual(code, 0)
        written = self.summary(path)
        self.assertIn("# E3 usability round: baseline", out)
        self.assertIn("The round passed: 4 of 5", written)
        self.assertIn("- **Result:** 4 of 5 passed; success per question: Q1 5/5, Q2 4/5, Q3 5/5, Q4 5/5; "
                      "median time to the fourth answer: 04:10.", written)
        self.assertIn("- **Top confusions**", written)
        self.assertIn("- **What to change before the next round**", written)

    def test_a_failed_round_is_scored_not_an_error(self):
        path = self.write("r/results.md", table(*[row(c, q2="fail") for c in su.CODES]))
        self.assertEqual(self.run_main(path)[0], 0)

    def test_filled_in_summary_stays_unless_forced(self):
        path = self.write("baseline/results.md", table())
        mine = self.write("baseline/summary.md", "my synthesis")
        code, out, _ = self.run_main(path)
        self.assertEqual(code, 0)
        self.assertIn(f"kept {mine}", out)
        self.assertEqual(self.summary(path), "my synthesis")
        self.run_main(path, "--force")
        self.assertIn("The round passed", self.summary(path))

    def test_two_rounds_print_and_write_the_comparison(self):
        first = self.write("baseline/results.md", table())
        second = self.write("loss/results.md", table(row("P1", q2="fail", errors="Q2 sign", scenario="mixed")))
        code, out, _ = self.run_main(first, second)
        self.assertEqual(code, 0)
        self.assertIn("| Participants passed | 5 | 4 | -1 |", out)
        with open(os.path.join(os.path.dirname(second), "comparison.md"), encoding="utf-8") as f:
            self.assertIn("| sign errors | 0 | 1 | +1 |", f.read())

    def test_personal_data_refused(self):
        secret = "ana.souza@example.com"
        good = self.write("baseline/results.md", table())
        path = self.write("loss/results.md", table(row("P1", errors=secret)))
        code, out, err = self.run_main(good, path)
        self.assertEqual(code, 2)
        self.assertIn("line 3: refused: e-mail address", err)
        self.assertNotIn(secret, out + err)
        self.assertEqual(out, "")
        self.assertEqual(sorted(os.listdir(os.path.dirname(good))), ["results.md"])
        self.assertEqual(sorted(os.listdir(os.path.dirname(path))), ["results.md"])

    def test_no_refusal_quotes_the_file(self):
        path = self.write("r/results.md", table(row("P1", errors="Maria Silva"), before="Maria Silva\n"))
        code, out, err = self.run_main(path)
        self.assertEqual(code, 2)
        self.assertNotIn("Maria", out + err)
        self.assertTrue(re.fullmatch(r"(.*line \d+: refused: .*\n)+nothing written\n", err), err)

    def test_unreadable_inputs(self):
        path = self.write("r/results.md", table())
        self.assertEqual(self.run_main(os.path.join(self.tmp.name, "absent.md"))[0], 2)
        no_template = self.write("kit.md", "no templates here")
        code, _, err = self.run_main(path, "--protocol", no_template)
        self.assertEqual(code, 2)
        self.assertIn("nothing written", err)
        self.assertFalse(os.path.exists(os.path.join(os.path.dirname(path), "summary.md")))


if __name__ == "__main__":
    unittest.main()
