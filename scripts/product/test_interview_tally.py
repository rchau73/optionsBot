"""Tests of the interview tally (#787): python3 -m unittest discover -s scripts/product -p 'test_*.py'"""
import io
import json
import os
import re
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import go_no_go as gng  # noqa: E402
import interview_tally as it  # noqa: E402

REPO = os.path.dirname(os.path.dirname(HERE))
GUIDE = os.path.join(REPO, "docs", "product", "discovery", "interview-guide.md")
LEGAL = os.path.join(REPO, "docs", "legal", "preliminary-answers.md")
HEADER = ",".join(it.COLUMNS)


def line(n, qualified="yes", delegation="no", codes="", happened="no", pay="unsure", trial="unsure"):
    return f"I{n:02d},2026-11-0{1 + n % 9},{qualified},{delegation},{codes},{happened},{pay},{trial}"


def sheet(*rows):
    return "\n".join([HEADER, *rows]) + "\n"


def run(text):
    """(exit code, stdout, stderr) of the tool on a sheet."""
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "interviews.csv")
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = it.main([path, "-json"])
        return code, out.getvalue(), err.getvalue()


def fifteen():
    return [line(n, delegation="yes still do" if n <= 6 else "no", codes="opaque_yield;fees",
                 happened="yes" if n <= 10 else "no", pay="subscription" if n % 2 else "nothing", trial="yes")
            for n in range(1, 16)]


class TallyTest(unittest.TestCase):
    def test_a_coded_interview_sheet_is_counted(self):
        code, out, _ = run(sheet(*fifteen()))
        self.assertEqual(code, 0)
        t = json.loads(out)
        self.assertEqual((t["qualified"], t["target"], t["small_sample"]), (15, 15, False))
        self.assertEqual((t["delegation"]["n"], t["delegation"]["share_pct"]), (15, 40.0))
        self.assertEqual(t["pains"]["codes"]["opaque_yield"], {"count": 10, "share_pct": 66.7})
        self.assertEqual(t["would_pay"]["options"]["subscription"]["count"], 8)
        self.assertEqual(t["testnet_trial"]["options"]["yes"]["share_pct"], 100.0)
        text = it.render_markdown(t)
        for must in ("15 of ~15", "40 %, n = 15", "opaque_yield", "subscription", "Trusted a testnet trial"):
            self.assertIn(must, text)
        self.assertNotIn("Small sample", text)

    def test_fewer_than_15_qualified_is_noted_not_hidden(self):
        code, out, _ = run(sheet(*fifteen()[:9], line(10, qualified="no", delegation="yes still do")))
        t = json.loads(out)
        self.assertEqual((code, t["qualified"], t["small_sample"], t["delegation"]["n"]), (0, 9, True, 9))
        self.assertIn("Small sample: fewer than 15 qualified", it.render_markdown(t))
        self.assertEqual(t["delegation"]["share_pct"], 66.7)  # the unqualified row is not in the figures

    def test_a_pain_that_did_not_happen_is_not_counted(self):
        _, out, _ = run(sheet(line(1, codes="opaque_yield", happened="no"), line(2, codes="fees", happened="yes")))
        t = json.loads(out)
        self.assertEqual(t["pains"]["codes"]["opaque_yield"]["count"], 0)
        self.assertEqual(t["pains"]["codes"]["fees"]["count"], 1)
        self.assertEqual(t["pains"]["not_counted"], 1)

    def test_bad_input_is_refused_naming_row_and_problem(self):
        bad = {
            "unknown pain code": (sheet(line(1), line(2, codes="opaque_yield;secret_thing")), "row 3, column pain_codes"),
            "duplicate participant": (sheet(line(1), line(1)), "row 3, column participant"),
            "e-mail": (sheet(line(1, codes="jane.doe@example.com")), "row 2, column pain_codes"),
            "amount": (sheet(line(1), line(2, pay="about 50 USD")), "row 3, column would_pay_bucket"),
            "currency sign": (sheet(line(1, pay="$20")), "row 2, column would_pay_bucket"),
            "a name as participant": (sheet(line(1).replace("I01", "Jane Doe")), "row 2, column participant"),
            "bad delegation": (sheet(line(1, delegation="maybe")), "row 2, column delegation"),
            "bad date": (sheet(line(1).replace("2026-11-02", "yesterday")), "row 2, column date"),
        }
        for name, (text, where) in bad.items():
            with self.subTest(name):
                code, out, err = run(text)
                self.assertEqual((code, out), (2, ""))
                self.assertIn(where, err)
                for personal in ("jane", "Jane", "example.com", "50 USD", "$20", "secret_thing", "maybe", "yesterday"):
                    self.assertNotIn(personal, err)

    def test_missing_and_extra_column_are_refused(self):
        code, _, err = run("participant,date,qualified,delegation,pain_happened,would_pay_bucket,trusted_testnet_trial\n")
        self.assertEqual(code, 2)
        self.assertIn("missing column pain_codes", err)
        code, _, err = run(HEADER + ",notes\n" + line(1) + ",free text\n")
        self.assertEqual(code, 2)
        self.assertIn("outside the coding sheet", err)
        self.assertNotIn("free text", err)

    def test_no_file_is_an_error_not_a_crash(self):
        with redirect_stderr(io.StringIO()):
            self.assertEqual(it.main(["/no/such/interviews.csv"]), 2)


class GuideTest(unittest.TestCase):
    def test_the_guide_and_the_script_agree(self):
        with open(GUIDE, encoding="utf-8") as f:
            coding = f.read().split("## Coding sheet", 1)[1]

        def cell(column):
            return re.search(rf"^\| `{column}` \| (.*) \|$", coding, re.M).group(1)

        self.assertEqual(set(re.findall(r"^\| `([a-z_]+)` \|", coding, re.M)), set(it.COLUMNS))
        self.assertEqual(set(re.findall(r"`([a-z_]+)`", cell("pain_codes"))), set(it.PAIN_CODES))
        for column, allowed in (("delegation", it.DELEGATION), ("would_pay_bucket", it.WOULD_PAY),
                                ("trusted_testnet_trial", it.TRIAL), ("qualified", it.YES_NO),
                                ("pain_happened", it.YES_NO)):
            self.assertEqual(set(re.findall(r"`([a-z_ ]+)`", cell(column))) - {"screener"} & set(allowed),
                             set(allowed), column)


class ScorecardTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def write(self, name, text):
        path = os.path.join(self.tmp.name, name)
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        return path

    def card(self, interviews, survey_share=None):
        tally = "none"
        if survey_share:
            tally = self.write("tally.json", json.dumps(
                {"delegation": {"cycle holder": {"n": 20, "share_pct": survey_share}}}))
        return gng.build(tally, "none", self.tmp.name, "btc", LEGAL, interviews)

    def test_the_scorecard_reads_interviews_without_letting_them_decide(self):
        rows = [line(n, delegation="yes still do" if n <= 2 else "no") for n in range(1, 11)]  # 20 %
        card = self.card(self.write("interviews.csv", sheet(*rows)), survey_share=45.0)
        info = card["informational"][0]
        self.assertEqual((info["status"], info["value"]), ("info", "20.0 % (n = 10)"))
        self.assertIn("survey 45.0 %", info["note"])
        self.assertEqual(card["overall"], "incomplete")  # set by the survey and E2 rows, as PRD §6 says
        self.assertEqual(len(card["rows"]), 4)
        self.assertIn("Informational (never decides)", gng.render(card))

    def test_interviews_never_make_a_kill(self):
        rows = [line(n) for n in range(1, 11)]  # 0 % delegation
        card = self.card(self.write("interviews.csv", sheet(*rows)), survey_share=60.0)
        self.assertNotEqual(card["overall"], "kill criterion met")

    def test_no_interview_file_reads_missing_and_nothing_fails(self):
        card = self.card(os.path.join(self.tmp.name, "interviews.csv"))
        self.assertEqual(card["informational"][0]["status"], gng.MISSING)
        self.assertEqual(card["overall"], "incomplete")
        self.assertEqual(self.card(None)["informational"][0]["status"], gng.MISSING)

    def test_a_refused_sheet_reads_missing_without_its_content(self):
        card = self.card(self.write("interviews.csv", sheet(line(1, codes="jane@example.com"))))
        info = card["informational"][0]
        self.assertEqual(info["status"], gng.MISSING)
        self.assertIn("row 2", info["note"])
        self.assertNotIn("jane", json.dumps(card))


if __name__ == "__main__":
    unittest.main()
