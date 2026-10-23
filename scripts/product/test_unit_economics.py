"""Tests of the unit-economics calculator (#235).
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
import unit_economics as ue  # noqa: E402


def one_tier(**over):
    tier = {"monthly_price": 10, "monthly_churn_pct": 5, "cost_to_serve": 2}
    tier.update(over)
    return {"cac": 40, "tiers": {"solo": tier}}


class OneTierTest(unittest.TestCase):
    def test_scenario_one_tier(self):
        r = ue.analyse(one_tier())["base"]["solo"]
        self.assertAlmostEqual(r["contribution_margin"], 8)
        self.assertAlmostEqual(r["ltv"], 160)
        self.assertAlmostEqual(r["ltv_cac"], 4)
        self.assertAlmostEqual(r["payback_months"], 5)
        self.assertEqual(r["flags"], [])

    def test_annual_discount_and_fees_lower_arpa_and_contribution(self):
        r = ue.analyse(one_tier(annual_share_pct=50, annual_discount_pct=20, payment_fee_pct=10,
                                payment_fee_fixed=0.1,
                                cost_to_serve={"infrastructure": 1, "exchange_api": 0.5, "support": 0.5}))
        row = r["base"]["solo"]
        self.assertAlmostEqual(row["arpa"], 9)                 # 10 × (1 − 0.5 × 0.2)
        self.assertAlmostEqual(row["gross_margin"], 7.5)       # − infrastructure − exchange
        self.assertAlmostEqual(row["contribution_margin"], 7.5 - 0.5 - 0.9 - 0.1)

    def test_pessimistic_row_shifts_churn_and_cac(self):
        rep = ue.analyse(dict(one_tier(), pessimistic={"churn_factor": 2, "conversion_factor": 0.5}))
        p = rep["pessimistic"]["solo"]
        self.assertAlmostEqual(p["monthly_churn_pct"], 10)
        self.assertAlmostEqual(p["cac"], 80)
        self.assertAlmostEqual(p["ltv"], 80)
        self.assertAlmostEqual(p["payback_months"], 10)
        self.assertIn("LTV:CAC < 3", p["flags"])

    def test_cohort_mrr_after_n_months(self):
        rep = ue.analyse(dict(one_tier(), cohort_size=100, cohort_months=2))
        self.assertAlmostEqual(rep["base"]["solo"]["mrr_after_n"], 100 * 10 * 0.95 ** 2)

    def test_channel_cac_is_share_weighted(self):
        sc = one_tier()
        sc["cac"] = {"content": {"cac": 20, "share_pct": 50}, "paid": {"cac": 60, "share_pct": 50}}
        self.assertAlmostEqual(ue.analyse(sc)["base"]["solo"]["cac"], 40)


class BlendTest(unittest.TestCase):
    def test_scenario_blended_mix(self):
        sc = {"cac": 40, "tiers": {
            "a": {"monthly_price": 10, "monthly_churn_pct": 5, "cost_to_serve": 2, "mix_pct": 70},
            "b": {"monthly_price": 30, "monthly_churn_pct": 2, "cost_to_serve": 6, "mix_pct": 30},
        }}
        base = ue.analyse(sc)["base"]
        self.assertAlmostEqual(base["blended"]["arpa"], 0.7 * 10 + 0.3 * 30)
        self.assertAlmostEqual(base["blended"]["ltv"], 0.7 * base["a"]["ltv"] + 0.3 * base["b"]["ltv"])
        self.assertAlmostEqual(base["blended"]["ltv"], 0.7 * 160 + 0.3 * 1200)

    def test_mix_is_normalised(self):
        sc = {"cac": 40, "tiers": {
            "a": {"monthly_price": 10, "monthly_churn_pct": 5, "mix_pct": 7},
            "b": {"monthly_price": 30, "monthly_churn_pct": 5, "mix_pct": 3},
        }}
        self.assertAlmostEqual(ue.analyse(sc)["base"]["blended"]["arpa"], 16)

    def test_missing_mix_with_two_tiers_is_refused(self):
        sc = {"cac": 40, "tiers": {"a": {"monthly_price": 10, "monthly_churn_pct": 5},
                                   "b": {"monthly_price": 30, "monthly_churn_pct": 5}}}
        with self.assertRaises(ue.ScenarioError):
            ue.analyse(sc)


class FlagsTest(unittest.TestCase):
    def test_scenario_flags_payback_over_12_months(self):
        rep = ue.analyse(dict(one_tier(), cac=120))  # payback 15 months
        self.assertIn("payback > 12 months", rep["base"]["solo"]["flags"])
        self.assertIn("payback > 12 months", ue.render_text(rep))

    def test_negative_contribution_margin(self):
        flags = ue.analyse(one_tier(cost_to_serve=12))["base"]["solo"]["flags"]
        self.assertIn("negative contribution margin", flags)
        self.assertIn("CAC never paid back", flags)

    def test_gross_margin_below_target(self):
        sc = dict(one_tier(), gross_margin_target_pct=90)
        self.assertIn("gross margin < target 90 %", ue.analyse(sc)["base"]["solo"]["flags"])


class CapitalFeeTest(unittest.TestCase):
    def test_scenario_capital_scaled_fee_needs_legal_review(self):
        rep = ue.analyse(one_tier(capital_fee_pct_per_year=1.2, avg_capital=10000))
        self.assertAlmostEqual(rep["base"]["solo"]["arpa"], 10 + 10)  # 1.2 % × 10,000 ÷ 12
        self.assertTrue(any("needs legal review" in w for w in rep["warnings"]))
        self.assertIn("needs legal review", ue.render_text(rep))

    def test_no_warning_without_capital_fee(self):
        self.assertEqual(ue.analyse(one_tier())["warnings"], [])

    def test_performance_inputs_are_refused(self):
        for key in ("performance_fee_pct", "expected_return_pct", "profit_share_pct"):
            with self.subTest(key=key), self.assertRaisesRegex(ue.ScenarioError, "PRD §6"):
                ue.analyse(one_tier(**{key: 20}))


class ValidationTest(unittest.TestCase):
    def test_bad_inputs_are_refused(self):
        cases = {
            "zero churn": one_tier(monthly_churn_pct=0),
            "negative price": one_tier(monthly_price=-1),
            "unknown key": one_tier(colour="red"),
            "no cac": {"tiers": {"solo": {"monthly_price": 10, "monthly_churn_pct": 5}}},
            "no tiers": {"cac": 40, "tiers": {}},
            "text price": one_tier(monthly_price="ten"),
            "capital without fee": one_tier(avg_capital=1000),
        }
        for name, sc in cases.items():
            with self.subTest(name), self.assertRaises(ue.ScenarioError):
                ue.analyse(sc)


class YamlTest(unittest.TestCase):
    def test_nested_mappings_comments_and_scalars(self):
        text = ("# head\ncurrency: \"USD\"  # trailing\ntiers:\n  a:\n    monthly_price: 10   # placeholder, not a price\n"
                "    note: 'x # y'\n    on: true\n\ncac: 40\n")
        self.assertEqual(ue.parse_yaml(text), {"currency": "USD", "cac": 40, "tiers": {
            "a": {"monthly_price": 10, "note": "x # y", "on": True}}})

    def test_lists_and_bad_lines_are_refused(self):
        for text in ("tiers:\n  - a: 1\n", "just text\n", "a: 1\na: 2\n", "a: 1\n  b: 2\n"):
            with self.subTest(text=text), self.assertRaises(ue.ScenarioError):
                ue.parse_yaml(text)

    def test_example_scenario_runs_and_is_placeholder(self):
        path = os.path.join(HERE, "example_scenario.yaml")
        with open(path, encoding="utf-8") as f:
            text = f.read()
        self.assertIn("placeholder, not a price", text)
        rep = ue.analyse(ue.load_scenario(path))
        self.assertEqual(set(rep["base"]), {"basic", "pro", "blended"})
        self.assertEqual(rep["warnings"], [])

    def test_json_scenario(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "s.json")
            with open(path, "w") as f:
                json.dump(one_tier(), f)
            self.assertAlmostEqual(ue.analyse(ue.load_scenario(path))["base"]["solo"]["ltv"], 160)


class OutPathTest(unittest.TestCase):
    def test_scenario_private_data_stays_private(self):
        with tempfile.TemporaryDirectory() as d:
            private = os.path.join(d, "data", "product")
            os.makedirs(private)
            self.assertTrue(ue.check_out_path(os.path.join(private, "pricing", "a.md"), private))
            for bad in (os.path.join(d, "report.md"), os.path.join(private, "..", "x.md"),
                        os.path.join(d, "data", "product-x", "a.md"), private):
                with self.subTest(bad=bad), self.assertRaises(ue.ScenarioError):
                    ue.check_out_path(bad, private)

    def test_cli_refuses_out_outside_data_product(self):
        with tempfile.TemporaryDirectory() as d:
            target = os.path.join(d, "leak.txt")
            err = io.StringIO()
            with redirect_stderr(err), redirect_stdout(io.StringIO()):
                code = ue.main([os.path.join(HERE, "example_scenario.yaml"), "-out", target])
            self.assertEqual(code, 2)
            self.assertIn("refused", err.getvalue())
            self.assertFalse(os.path.exists(target))

    def test_cli_prints_text_report(self):
        out = io.StringIO()
        with redirect_stdout(out):
            code = ue.main([os.path.join(HERE, "example_scenario.yaml")])
        self.assertEqual(code, 0)
        self.assertIn("blended", out.getvalue())
        self.assertIn("pessimistic", out.getvalue())


if __name__ == "__main__":
    unittest.main()
