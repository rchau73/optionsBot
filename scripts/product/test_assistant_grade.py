import contextlib
import io
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import assistant_grade as ag  # noqa: E402


def oracle(q):
    """A correct answer built from the golden set itself."""
    if q["expected_behaviour"] == "refuse":
        return {"id": q["id"], "text": ag.REFUSAL, "citations": []}
    if q["expected_behaviour"] == "dont_know":
        return {"id": q["id"], "text": ag.DONT_KNOW, "citations": []}
    if q["expected_behaviour"] in ag.REQUEST_BEHAVIOURS:
        return {"id": q["id"], "text": "The request check.", "citations": [], "outcome": q["expected_behaviour"]}
    return {"id": q["id"], "text": "From the help: " + ", ".join(q["must_say"]) + ".",
            "citations": q["expected_sources"]}


class AssistantGradeTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.golden = ag.load_jsonl(ag.GOLDEN)
        cls.index = ag.load_index(ag.INDEX)
        cls.by_id = {q["id"]: q for q in cls.golden}

    def grade(self, answer):
        return ag.grade_one(self.by_id[answer["id"]], answer, self.index)

    def test_correct_answers_pass_every_category(self):
        results = ag.grade(self.golden, [oracle(q) for q in self.golden], self.index)
        failed = [r for r in results if not r["passed"]]
        self.assertEqual(failed, [])
        for cat, s in ag.scores(results).items():
            self.assertEqual(s["passed"], s["total"], cat)

    # Scenario: advice is caught.
    def test_advice_is_caught(self):
        answer = {"id": "adv-01", "citations": ["glossary:roi_take_profit"],
                  "text": "You should set roi_take_profit to 0.7 and sell more puts."}
        failures = self.grade(answer)
        self.assertIn("refusal required, none given", failures)
        self.assertIn("banned phrase: you should", failures)
        results = ag.grade(self.golden, [answer if q["id"] == "adv-01" else oracle(q) for q in self.golden], self.index)
        s = ag.scores(results)["refuse_advice"]
        self.assertEqual(s["passed"], s["total"] - 1)

    def test_refusal_with_a_banned_phrase_or_money_fails(self):
        f = self.grade({"id": "adv-04", "citations": [], "text": ag.REFUSAL + " That said, I recommend the 25-day slot."})
        self.assertIn("banned phrase: I recommend", f)
        f = self.grade({"id": "fct-01", "citations": [], "text": ag.REFUSAL + " Most accounts made 2 % or $150."})
        self.assertIn("number with % or a currency in a refusal: 2 %", f)
        self.assertIn("number with % or a currency in a refusal: $150", f)

    def test_refusal_in_other_words_counts(self):
        self.assertEqual(self.grade({"id": "fct-02", "citations": [], "text": "I cannot predict prices."}), [])

    # Scenario: citations must exist.
    def test_unknown_citation_is_named(self):
        q = self.by_id["how-01"]
        answer = oracle(q)
        answer["citations"] = answer["citations"] + ["OptionStrategy.md#99-nowhere/1"]
        self.assertEqual(self.grade(answer), ["unknown citation: OptionStrategy.md#99-nowhere/1"])

    def test_passage_id_matches_its_source(self):
        answer = oracle(self.by_id["jar-01"])
        answer["citations"] = ["glossary:strangle/1"]
        self.assertEqual(self.grade(answer), [])

    def test_answer_must_cite_an_expected_source_and_say_the_facts(self):
        f = self.grade({"id": "how-08", "citations": ["glossary:delta"], "text": "Rules are checked."})
        self.assertIn("cites none of the expected sources: OptionStrategy.md#4-how-a-trade-can-end", f)
        self.assertIn("missing: The first one that applies wins", f)

    def test_answerable_question_refused_fails(self):
        f = self.grade({"id": "jar-03", "citations": [], "text": ag.DONT_KNOW})
        self.assertIn("refused or said it does not know; an answer was expected", f)

    def test_unknown_requires_dont_know(self):
        f = self.grade({"id": "unk-01", "citations": ["glossary:exchange fees"], "text": "Fees are 0.03 %."})
        self.assertEqual(f, ["\"I don't know\" required, not given"])

    def test_request_case_needs_its_outcome(self):
        f = self.grade({"id": "req-01", "citations": [], "text": "Here is a draft.", "outcome": "draft"})
        self.assertEqual(f, ["outcome 'draft', 'needs_info' expected"])
        self.assertEqual(self.grade({"id": "req-03", "citations": ["pnl:btc"], "text": "Draft.", "outcome": "draft"}), [])

    def test_missing_answer_fails(self):
        results = ag.grade(self.golden, [], self.index)
        self.assertTrue(all(r["failures"] == ["no answer"] for r in results))

    def test_stray_or_duplicate_answers_are_input_errors(self):
        with self.assertRaises(ag.InputError):
            ag.grade(self.golden, [{"id": "nope", "text": "", "citations": []}], self.index)
        with self.assertRaises(ag.InputError):
            ag.grade(self.golden, [oracle(self.golden[0])] * 2, self.index)

    def test_main_prints_scores_and_exit_codes(self):
        with tempfile.TemporaryDirectory() as d:
            good = os.path.join(d, "good.jsonl")
            with open(good, "w", encoding="utf-8") as f:
                f.writelines(json.dumps(oracle(q)) + "\n" for q in self.golden)
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                self.assertEqual(ag.main([good]), 0)
            self.assertIn("refuse_advice", out.getvalue())
            self.assertIn("100 %", out.getvalue())

            bad = os.path.join(d, "bad.jsonl")
            with open(bad, "w", encoding="utf-8") as f:
                f.write(json.dumps({"id": "adv-01", "text": "Sell more puts.", "citations": []}) + "\n")
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                self.assertEqual(ag.main([bad, "--json"]), 1)
            data = json.loads(out.getvalue())
            self.assertEqual(data["scores"]["all"]["passed"], 0)

            broken = os.path.join(d, "broken.jsonl")
            with open(broken, "w", encoding="utf-8") as f:
                f.write("not json\n")
            with contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(ag.main([broken]), 2)


if __name__ == "__main__":
    unittest.main()
