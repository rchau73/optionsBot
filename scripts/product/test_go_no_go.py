"""Tests of the go / no-go scorecard (#305) and the pricing-options document (#236).
python3 -m unittest discover -s scripts/product -p 'test_*.py'"""
import io
import json
import os
import re
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import go_no_go as gng  # noqa: E402

REPO = os.path.dirname(os.path.dirname(HERE))
PRICING = os.path.join(REPO, "docs", "product", "pricing-options.md")
GO_NO_GO_DOC = os.path.join(REPO, "docs", "product", "go-no-go.md")
LEGAL = os.path.join(REPO, "docs", "legal", "preliminary-answers.md")
SCOREBOARD_CASES = os.path.join(REPO, "tests", "testdata", "scoreboard", "cases.json")


def tally(share, n=20):
    return {"delegation": {"cycle holder": {"n": n, "share_pct": share, "small_sample": n < 15}}}


def evidence(week, ret, hold, coin=1.2345):
    """A weekly report shaped like cmd/evidence's JSON, with coin figures in it."""
    return {"week": week, "equity": coin * 10,
            "live": {"realised": coin, "fees": coin / 10,
                     "perf": {"NetPnL": coin, "NetPnLUSD": coin * 70000, "MaxDrawdown": -coin / 2,
                              "MaxDrawdownPct": -2.5}},
            "hold": {"known": True, "strategy_known": True, "spot_start": 70000.0, "spot_end": 71000.0,
                     "return_usd_pct": ret, "hold_return_usd_pct": hold}}


class Fixture:
    def __init__(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name

    def write(self, rel, data):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(data if isinstance(data, str) else json.dumps(data))
        return path

    def weeks(self, n, engine, hold, bot="btc"):
        for i in range(n):
            week = f"2026-W{10 + i:02d}"
            self.write(f"data/evidence/{week}/{bot}.json", evidence(week, engine(i), hold(i)))

    def path(self, rel):
        return os.path.join(self.root, rel)

    def card(self, tally_rel="tally.json", fakedoor_rel="result.json", legal=LEGAL):
        return gng.build(self.path(tally_rel), self.path(fakedoor_rel), self.root, "btc", legal)

    def close(self):
        self.tmp.cleanup()


class ScorecardTest(unittest.TestCase):
    def setUp(self):
        self.fx = Fixture()

    def tearDown(self):
        self.fx.close()

    def test_the_prd_kill_rule(self):
        # Scenario: a survey share of 25 % → PRD §6 kill criterion met, citing the tally file.
        tally_path = self.fx.write("tally.json", tally(25.0))
        card = self.fx.card()
        self.assertEqual(card["rows"][0]["status"], gng.FAILS)
        self.assertEqual(card["rows"][0]["value"], "25.0 %")
        self.assertEqual(card["overall"], "kill criterion met")
        self.assertIn(tally_path, card["overall_reason"])
        self.assertIn("PRD §6", card["overall_reason"])

    def test_survey_at_threshold_meets(self):
        self.fx.write("tally.json", tally(30.0))
        self.assertEqual(self.fx.card()["rows"][0]["status"], gng.MEETS)

    def test_missing_inputs_stay_missing(self):
        # Scenario: no fake-door result file → that row reads missing, overall incomplete, not go.
        self.fx.write("tally.json", tally(45.0))
        card = self.fx.card()
        fd = card["rows"][1]
        self.assertEqual((fd["value"], fd["status"]), ("not run", gng.MISSING))
        self.assertEqual(card["overall"], "incomplete")
        self.assertNotIn("go", card["overall"].split())
        md = gng.render(card)
        self.assertIn("**Overall: incomplete**", md)

    def test_no_tally_is_missing(self):
        card = self.fx.card()
        self.assertEqual(card["rows"][0]["status"], gng.MISSING)
        self.assertEqual(card["overall"], "incomplete")

    def test_no_cycle_holder_answer_is_missing(self):
        self.fx.write("tally.json", tally(None, n=0))
        self.assertEqual(self.fx.card()["rows"][0]["status"], gng.MISSING)

    def test_fakedoor_outcomes(self):
        for outcome, status in (("Success", gng.MEETS), ("Kill", gng.FAILS), ("Iterate", gng.FAILS),
                                ("Not enough traffic", gng.MISSING)):
            self.fx.write("result.json", {"outcome": outcome, "overall": {"rate": 0.08, "visits": 500}})
            r = self.fx.card()["rows"][1]
            self.assertEqual(r["status"], status, outcome)
            self.assertEqual(r["note"], "sign-up rate 8.0 %")

    def test_no_amounts(self):
        # Scenario: an evidence JSON with coin figures → only percentages and pass/fail.
        self.fx.write("tally.json", tally(45.0))
        self.fx.weeks(gng.E2_MIN_WEEKS, engine=lambda i: 1.0 + (i % 3), hold=lambda i: (i % 4) - 1.5)
        card = self.fx.card()
        e2 = card["rows"][2]
        self.assertIn(e2["status"], (gng.MEETS, gng.FAILS))
        text = gng.render(card) + json.dumps(card["rows"])
        for amount in ("1.2345", "12.345", "86415", "70000", "71000", "0.61725"):
            self.assertNotIn(amount, text)
        self.assertIn("-2.5 %", e2["note"])

    def test_e2_not_enough_weeks(self):
        self.fx.weeks(gng.E2_MIN_WEEKS - 1, engine=lambda i: 2.0 + i, hold=lambda i: i)
        e2 = self.fx.card()["rows"][2]
        self.assertEqual((e2["value"], e2["status"]), ("not enough weeks", gng.MISSING))

    def test_e2_engine_beats_hold(self):
        self.fx.weeks(gng.E2_MIN_WEEKS, engine=lambda i: 1.0 + 0.1 * (i % 2), hold=lambda i: 5.0 * ((-1) ** i))
        self.assertEqual(self.fx.card()["rows"][2]["status"], gng.MEETS)

    def test_e2_engine_loses_to_hold_is_the_kill(self):
        self.fx.write("tally.json", tally(60.0))
        self.fx.weeks(gng.E2_MIN_WEEKS, engine=lambda i: -1.0 + 0.1 * (i % 2), hold=lambda i: 2.0 + 0.1 * (i % 2))
        card = self.fx.card()
        self.assertEqual(card["rows"][2]["status"], gng.FAILS)
        self.assertEqual(card["overall"], "kill criterion met")

    def test_e2_agrees_with_the_evidence_scoreboard(self):
        # Scenario: both tools agree — the fixture tests/evidence_scoreboard_test.go reads too (#537):
        # row 3 gives the scoreboard's status (its value names "not enough weeks" / "no spread") and ratios.
        with open(SCOREBOARD_CASES, encoding="utf-8") as f:
            cases = json.load(f)["cases"]
        for c in cases:
            with self.subTest(c["name"]):
                fx = Fixture()
                try:
                    for week, engine, hold in c["weeks"]:
                        fx.write(f"data/evidence/{week}/btc.json", evidence(week, engine, hold))
                    e2 = gng.e2_row(fx.root, "btc")
                finally:
                    fx.close()
                status = e2["status"] if e2["status"] in (gng.MEETS, gng.FAILS) else e2["value"]
                self.assertEqual(status, c["status"])
                if c["status"] in (gng.MEETS, gng.FAILS):
                    self.assertEqual(e2["value"], f"engine {c['engine_ratio']:.2f} vs hold {c['hold_ratio']:.2f}")

    def test_e2_skips_non_week_folders_and_unknown_hold(self):
        self.fx.weeks(gng.E2_MIN_WEEKS, engine=lambda i: 1.0 + i, hold=lambda i: i)
        self.fx.write("data/evidence/track-record/btc.json", evidence("track-record", 1, 1))
        bad = evidence("2026-W40", 1, 1)
        bad["hold"]["known"] = False
        self.fx.write("data/evidence/2026-W40/btc.json", bad)
        reports = gng.weekly_reports(self.fx.root, "btc")
        self.assertEqual(len(reports), gng.E2_MIN_WEEKS + 1)
        self.assertEqual(self.fx.card()["rows"][2]["date"], f"2026-W{10 + gng.E2_MIN_WEEKS - 1:02d}")

    def test_legal_gate_preliminary_is_missing(self):
        r = self.fx.card()["rows"][3]
        self.assertEqual(r["status"], gng.MISSING)
        self.assertIn("0 of 7", r["value"])

    def test_legal_gate_counsel_answered_meets(self):
        lines = "\n".join(f"| **{q}** x | **Counsel (2026-11-01):** answered | high | x |" for q in gng.LEGAL_GATE)
        legal = self.fx.write("legal.md", "| ID | A |\n|---|---|\n" + lines + "\n")
        r = self.fx.card(legal=legal)["rows"][3]
        self.assertEqual(r["status"], gng.MEETS)

    def test_every_input_in_is_not_a_go(self):
        self.fx.write("tally.json", tally(60.0))
        self.fx.write("result.json", {"outcome": "Success", "overall": {"rate": 0.12}})
        self.fx.weeks(gng.E2_MIN_WEEKS, engine=lambda i: 1.0 + 0.1 * (i % 2), hold=lambda i: 5.0 * ((-1) ** i))
        lines = "\n".join(f"| **{q}** x | **Counsel (2026-11-01):** a | high | x |" for q in gng.LEGAL_GATE)
        legal = self.fx.write("legal.md", lines + "\n")
        card = self.fx.card(legal=legal)
        self.assertEqual(card["overall"], "no kill criterion met")
        self.assertIn("product owner", card["overall_reason"])

    def test_main_prints_json(self):
        out = io.StringIO()
        with redirect_stdout(out):
            code = gng.main(["--tally", self.fx.path("none.json"), "--fakedoor", "",
                             "--evidence-root", self.fx.root, "-json"])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out.getvalue())["overall"], "incomplete")

    def test_thresholds_quote_the_prd(self):
        self.assertEqual(gng.SURVEY_KILL_PCT, 30.0)
        with open(os.path.join(REPO, "docs", "product", "PRD.md"), encoding="utf-8") as f:
            self.assertIn("<30 % of interviewed cycle holders would let software trade their account", f.read())
        with open(GO_NO_GO_DOC, encoding="utf-8") as f:
            doc = f.read()
        self.assertIn(f"{gng.E2_MIN_WEEKS} weekly reports", doc)


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def section(text, heading):
    """The text of the section whose heading starts with heading, up to the next ## heading."""
    m = re.search(rf"^## {re.escape(heading)}.*?$(.*?)(?=^## |\Z)", text, re.S | re.M)
    return m.group(1) if m else ""


class PricingOptionsDocTest(unittest.TestCase):
    def setUp(self):
        self.doc = read(PRICING)

    def test_no_figures_in_public(self):
        # Scenario: no price, revenue, CAC or competitor price.
        money = re.compile(r"[$€£]\s*\d|\d\s*(?:USD|EUR|BRL|R\$|usd|reais|dollars?)\b|R\$\s*\d|"
                           r"\d+(?:[.,]\d+)?\s*%\s*(?:of|per|a)\s+(?:capital|aum|profit|year)", re.I)
        self.assertEqual(money.findall(self.doc), [])
        for word in re.findall(r"\b(?:CAC|revenue|MRR|ARPA|LTV)\b[^|\n]{0,40}?\d", self.doc):
            self.fail(f"a figure next to a unit-economics word: {word!r}")

    def test_legal_flags(self):
        # Scenario: any option that scales with capital or profits → needs legal review + its L-question.
        for line in self.doc.splitlines():
            low = line.lower()
            if ("capital" in low or "profit" in low) and ("scale" in low or "fee" in low) and line.startswith("|"):
                self.assertIn("needs legal review", low, line)
                self.assertRegex(line, r"L8", line)
        self.assertIn("legal-brief.md", self.doc)
        self.assertIn("preliminary-answers.md", self.doc)

    def test_decidable(self):
        # Scenario: ends with the decision, the options, the recommendation and the missing data.
        last = re.sub(r"^\d+\.\s*", "", self.doc.split("\n## ")[-1])
        self.assertTrue(last.lower().startswith("decision"), last[:60])
        for must in ("Decision to take", "Options", "Recommendation", "Still missing"):
            self.assertIn(must, last)

    def test_no_dark_patterns(self):
        # Scenario: every trial or plan rule says when it ends, what it costs after, and cancelling is as easy.
        rules = section(self.doc, "5.")
        self.assertTrue(rules, "section 5 (trial and plan rules) missing")
        blocks = [b for b in re.split(r"^### ", rules, flags=re.M)[1:]]
        self.assertGreaterEqual(len(blocks), 3)
        for b in blocks:
            title = b.splitlines()[0]
            self.assertIn("**Ends:**", b, title)
            self.assertIn("**After it ends:**", b, title)
            self.assertIn("**Cancelling:**", b, title)
            self.assertIn("as easy as signing up", b, title)


if __name__ == "__main__":
    unittest.main()
