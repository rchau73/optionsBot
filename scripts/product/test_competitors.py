"""Tests of the competitor map checker (#304).
python3 -m unittest discover -s scripts/product -p 'test_*.py'"""
import io
import os
import re
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import competitors as cm  # noqa: E402

EXAMPLE = os.path.join(HERE, "example_competitor.yaml")


def run(*argv):
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = cm.main(list(argv))
    return code, out.getvalue(), err.getvalue()


def example_text():
    with open(EXAMPLE, encoding="utf-8") as f:
        return f.read()


class Entry:
    """Writes a variant of the example entry in a temporary directory."""

    def __init__(self, text):
        self.dir = tempfile.TemporaryDirectory()
        self.path = os.path.join(self.dir.name, "acme.yaml")
        with open(self.path, "w", encoding="utf-8") as f:
            f.write(text)

    def __enter__(self):
        return self.path

    def __exit__(self, *exc):
        self.dir.cleanup()


class ExampleTest(unittest.TestCase):
    def test_example_is_valid(self):
        code, out, err = run(EXAMPLE, "--today", "2026-10-23")
        self.assertEqual(code, 0, err)
        self.assertIn("1 entry ok", out)

    def test_example_lives_outside_private_data(self):
        example = os.path.realpath(EXAMPLE)
        private = os.path.realpath(os.path.join(cm.REPO, "data"))
        self.assertNotEqual(os.path.commonpath([example, private]), private)
        self.assertEqual(os.path.dirname(example), os.path.realpath(HERE))


class RefuseTest(unittest.TestCase):
    def test_scenario_a_fact_without_a_source_is_refused(self):
        # The tier's price loses its source_url.
        text = example_text().replace(
            "      period: month\n      source_url: https://example.invalid/pricing\n",
            "      period: month\n", 1)
        with Entry(text) as path:
            code, _, err = run(path, "--today", "2026-10-23")
        self.assertNotEqual(code, 0)
        self.assertIn(path, err)
        self.assertIn("pricing.tiers.standard.source_url", err)

    def test_missing_seen_on_is_refused(self):
        text = example_text().replace(
            "  text: users say it is simple\n    source_url: https://example.invalid/reviews\n    seen_on: 2026-10-20\n",
            "  text: users say it is simple\n    source_url: https://example.invalid/reviews\n", 1)
        with Entry(text) as path:
            code, _, err = run(path, "--today", "2026-10-23")
        self.assertEqual(code, 1)
        self.assertIn("praise.simple.seen_on", err)

    def test_bad_date_future_date_and_bad_values_are_refused(self):
        text = re.sub(r"(risk_shown:.*\n  value:) partly", r"\1 maybe", example_text())
        text = (text
                .replace("category: exchange_yield", "category: casino", 1)
                .replace("seen_on: 2026-10-20\n\nweaknesses", "seen_on: 2026-13-01\n\nweaknesses", 1)
                .replace("seen_on: 2026-10-20\n\ncomplaints", "seen_on: 2027-01-01\n\ncomplaints", 1))
        with Entry(text) as path:
            code, _, err = run(path, "--today", "2026-10-23")
        self.assertEqual(code, 1)
        self.assertIn("risk_shown.value", err)
        self.assertIn("category: 'casino' is not one of", err)
        self.assertIn("strengths.onboarding.seen_on: '2026-13-01' is not a date", err)
        self.assertIn("praise.simple.seen_on: 2027-01-01 is after today", err)

    def test_unknown_and_missing_fields_are_refused(self):
        text = example_text().replace("segment: retail", "segmnt: retail", 1)
        with Entry(text) as path:
            code, _, err = run(path, "--today", "2026-10-23")
        self.assertEqual(code, 1)
        self.assertIn("segmnt: unknown field", err)
        self.assertIn("segment: missing", err)

    def test_missing_positioning_claim_is_refused(self):
        text = example_text().split("  public_track_record:")[0]
        with Entry(text) as path:
            code, _, err = run(path, "--today", "2026-10-23")
        self.assertEqual(code, 1)
        self.assertIn("against_positioning.public_track_record: missing", err)

    def test_errors_print_no_summary(self):
        text = example_text().replace("    source_url: https://example.invalid/terms\n", "", 1)
        with Entry(text) as path:
            code, out, _ = run(path, "--summary", "--today", "2026-10-23")
        self.assertEqual(code, 1)
        self.assertEqual(out, "")

    def test_no_entries(self):
        with tempfile.TemporaryDirectory() as d:
            code, _, err = run(d)
        self.assertEqual(code, 2)
        self.assertIn("no entries", err)


class StaleTest(unittest.TestCase):
    def test_scenario_stale_facts_are_flagged(self):
        code, out, _ = run(EXAMPLE, "--today", "2026-10-23")
        self.assertEqual(code, 0)
        self.assertIn("STALE:", out)
        self.assertIn("recent_moves.new_product: seen_on 2026-06-01 is 144 days old (> 90)", out)
        self.assertIn("1 stale fact(s)", out)

    def test_ninety_days_is_not_stale(self):
        # Every other fact seen that day, so none is after "today".
        text = example_text().replace("2026-10-20", "2026-08-30")
        with Entry(text) as path:
            code, out, err = run(path, "--today", "2026-08-30")  # 2026-06-01 + 90 days
        self.assertEqual(code, 0, err)
        self.assertNotIn("STALE:", out)


class SummaryTest(unittest.TestCase):
    def test_scenario_the_summary_is_safe_to_publish(self):
        code, out, err = run(EXAMPLE, "--summary", "--today", "2026-10-23")
        self.assertEqual(code, 0, err)
        for forbidden in ("123.45", "123", "USD", "$", "http", "example.invalid", "data/", EXAMPLE,
                          "Example Yield Co", "STALE:"):
            self.assertNotIn(forbidden, out)

    def test_summary_counts_per_category(self):
        code, out, _ = run(EXAMPLE, "--summary", "--today", "2026-10-23")
        self.assertEqual(code, 0)
        self.assertIn("## Exchange-native structured yield\ncompetitors: 1\n", out)
        self.assertIn("risk shown beside the yield: yes 0, partly 1, no 0", out)
        self.assertIn("- shows the reason for every trade: open (none does)", out)
        self.assertIn("- shows the worst case before every risk: contested: yes 0, partly 1 of 1", out)
        self.assertIn("stale facts (> 90 days): 1", out)
        self.assertIn("## DeFi option vaults\nnot mapped yet", out)


if __name__ == "__main__":
    unittest.main()
