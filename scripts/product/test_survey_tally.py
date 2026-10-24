"""Tests of the survey tally (#247). Every answer here is synthetic, made inside the test.
python3 -m unittest discover -s scripts/product -p 'test_*.py'"""
import csv
import io
import json
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import survey_tally as st  # noqa: E402

SECRET_TEXT = "SYNTHETIC-FREE-TEXT-MARKER"
SECRET_MAIL = "synthetic-person@example.invalid"

HOLDER = {"S1": "both", "S2": "2018–2021", "S3": "kept them", "S4": "dual investment", "S5": "never",
          "S6": "no", "S7": "no", "S8": "yes"}
TRADER = dict(HOLDER, S5="monthly", S4="none")
SCREENED = [dict(HOLDER, S1="neither"), dict(HOLDER, S2="2022 or later"), dict(HOLDER, S3="sold most"),
            dict(HOLDER, S7="yes"), dict(HOLDER, S8="no")]


def row(base, **over):
    r = {item: "" for item in st.ITEMS}
    r.update(base)
    r.update(over)
    return r


def fifteen_and_five():
    """12 cycle holders + 3 active option traders qualify; 5 are screened out, one per rule."""
    holder_q1 = ["never tried"] * 2 + ["dual investment"] * 6 + ['Exchange “earn” / savings'] * 4
    holder_n1 = ["8", "8", "10", "9", "9", "8", "7", "6", "5", "0", "10", "3"]  # the first two are skipped
    holder_q6 = ["yes, and still do"] * 3 + ["yes, and took it back"] + ["no"] * 8
    holder_f1 = [("yes, add me (leave an e-mail)", SECRET_MAIL)] * 2 + [("yes, add me", "")] + \
        [("not now", "")] + [("no", "")] * 8
    rows = []
    for i in range(12):
        rows.append(row(HOLDER, Q1=holder_q1[i], N1=holder_n1[i], Q6=holder_q6[i], F1=holder_f1[i][0],
                        F1_email=holder_f1[i][1], Q4=SECRET_TEXT if i < 5 else "", F2=SECRET_TEXT if i < 3 else "",
                        Q8="signals; a tool subscription" if i < 2 else "nothing"))
    for i, (n1, q6) in enumerate([("9", "yes, and still do"), ("7", "yes, and still do"), ("2", "no")]):
        rows.append(row(TRADER, Q1="options sold by hand", N1=n1, Q6=q6,
                        F1="yes, add me" if i == 0 else "no", F1_email=SECRET_MAIL if i == 0 else ""))
    for r in SCREENED:
        rows.append(row(r, Q1="lending", N1="0", Q6="yes, and still do", F1="no"))
    for i, r in enumerate(rows, 1):
        r["ID"] = f"P{i:02d}"
    return rows


def to_csv(rows, columns=None):
    columns = columns or st.ITEMS
    buf = io.StringIO()
    w = csv.DictWriter(buf, fieldnames=columns, extrasaction="ignore")
    w.writeheader()
    w.writerows(rows)
    return buf.getvalue()


def tally_csv(text, mapping=None):
    return st.tally(st.read_rows(io.StringIO(text), mapping))


def run_main(args):
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = st.main(args)
    return code, out.getvalue(), err.getvalue()


class FifteenQualifiedFiveScreenedOutTest(unittest.TestCase):
    """Scenario: a CSV with 15 qualified and 5 screened-out responses."""

    @classmethod
    def setUpClass(cls):
        cls.t = tally_csv(to_csv(fifteen_and_five()))
        cls.md = st.render_markdown(cls.t)

    def test_prints_15_qualified_and_5_screened_out(self):
        self.assertEqual((self.t["qualified"], self.t["screened_out"]), (15, 5))
        self.assertEqual(self.t["segments"], {st.CYCLE_HOLDER: 12, st.ACTIVE_TRADER: 3})
        self.assertIn("| 20 | 15 | 12 | 3 | 5 |", self.md)

    def test_counts_each_q1_to_q8_option(self):
        q1 = self.t["questions"]["Q1"]
        self.assertEqual(q1["answered"], 15)
        self.assertEqual(q1["options"]["never tried"]["count"], 2)
        self.assertEqual(q1["options"]["dual investment"]["count"], 6)
        self.assertEqual(q1["options"]['exchange "earn" / savings']["count"], 4)  # curly quotes, case
        self.assertEqual(q1["options"]["options sold by hand"]["count"], 3)
        self.assertEqual(q1["options"]["lending"]["count"], 0)  # screened-out answers are not counted
        self.assertAlmostEqual(q1["options"]["dual investment"]["share_pct"], 40.0)
        q8 = self.t["questions"]["Q8"]
        self.assertTrue(q8["multi"])
        self.assertEqual((q8["options"]["signals"]["count"], q8["options"]["a tool subscription"]["count"],
                          q8["options"]["nothing"]["count"]), (2, 2, 10))
        self.assertEqual(set(self.t["questions"]), {"Q1", "Q2", "Q3", "Q5", "Q6", "Q7", "Q8"})
        for item in ("Q1", "Q2", "Q3", "Q5", "Q6", "Q7", "Q8"):
            self.assertIn(f"### {item} ", self.md)

    def test_nps_on_n1_skips_never_tried(self):
        n = self.t["nps"]
        # holders 3–12: 10 9 9 8 7 6 5 0 10 3; traders: 9 7 2
        self.assertEqual(n["n"], 13)
        self.assertEqual((n["promoters"], n["passives"], n["detractors"]), (5, 3, 5))
        self.assertEqual(n["nps"], 0.0)
        self.assertFalse(n["small_sample"])

    def test_fake_door_outcomes_of_f1(self):
        f = self.t["fake_door"]
        q = f["qualified"]
        self.assertEqual((q["n"], q["Joined"]["count"], q["Declined"]["count"], q["Undecided"]["count"]),
                         (15, 3, 10, 2))
        h = f[st.CYCLE_HOLDER]
        self.assertEqual((h["Joined"]["count"], h["Declined"]["count"], h["Undecided"]["count"]), (2, 8, 2))

    def test_delegation_share_with_n_and_threshold(self):
        d = self.t["delegation"]["qualified"]
        self.assertEqual((d["delegated"], d["n"], d["share_pct"]), (6, 15, 40.0))
        self.assertFalse(d["below_threshold"])
        self.assertFalse(d["small_sample"])
        h = self.t["delegation"][st.CYCLE_HOLDER]
        self.assertEqual((h["delegated"], h["n"], h["share_pct"]), (4, 12, 33.3))
        self.assertIn("| qualified | 6 | 15 | 40 % | < 30 % | at or above |", self.md)
        self.assertNotIn("Small sample: qualified", self.md)
        self.assertIn("Small sample: cycle holder n = 12", self.md)


class FakeDoorTest(unittest.TestCase):
    def test_yes_without_email_counts_as_undecided(self):
        self.assertEqual(st.fake_door(row(HOLDER, F1="yes, add me (leave an e-mail)", F1_email="")), "Undecided")
        self.assertEqual(st.fake_door(row(HOLDER, F1="yes, add me", F1_email="   ")), "Undecided")
        self.assertEqual(st.fake_door(row(HOLDER, F1="yes, add me", F1_email=SECRET_MAIL)), "Joined")
        self.assertEqual(st.fake_door(row(HOLDER, F1="Not now")), "Undecided")
        self.assertEqual(st.fake_door(row(HOLDER, F1="no")), "Declined")
        self.assertIsNone(st.fake_door(row(HOLDER, F1="")))


class FreeTextTest(unittest.TestCase):
    def test_free_text_never_printed_only_counted(self):
        rows = fifteen_and_five()
        rows[0]["Q1"] = "other: " + SECRET_TEXT  # "other" with a typed detail
        rows[1]["Q7"] = SECRET_TEXT              # an answer that matches no option
        text = to_csv(rows)
        t = tally_csv(text)
        self.assertEqual(t["free_text_answers"], {"Q4": 5, "F2": 3})
        self.assertEqual(t["questions"]["Q7"]["options"][st.UNRECOGNISED]["count"], 1)
        md = st.render_markdown(t)
        js = json.dumps(t)
        for out in (md, js):
            self.assertNotIn(SECRET_TEXT, out)
            self.assertNotIn(SECRET_MAIL, out)
        self.assertIn("- Q4: 5 answers", md)
        self.assertIn("- F2: 3 answers", md)


class SmallSampleTest(unittest.TestCase):
    def test_fewer_than_15_qualified_carry_a_note(self):
        rows = [row(HOLDER, ID=f"P{i:02d}", Q6="yes, and still do" if i < 2 else "no", F1="no")
                for i in range(10)]
        t = tally_csv(to_csv(rows))
        d = t["delegation"]["qualified"]
        self.assertEqual((d["n"], d["delegated"], d["share_pct"]), (10, 2, 20.0))
        self.assertTrue(d["small_sample"])
        self.assertTrue(d["below_threshold"])
        md = st.render_markdown(t)
        self.assertIn("Small sample: qualified n = 10 < 15", md)
        self.assertIn("| 20 % | < 30 % | below |", md)

    def test_nps_under_5_shows_the_scores(self):
        rows = [row(HOLDER, ID=f"P{i}", Q1="lending", N1=s, F1="no", Q6="no") for i, s in enumerate(["9", "4"])]
        md = st.render_markdown(tally_csv(to_csv(rows)))
        self.assertIn("read the scores, not the NPS — 4: 1, 9: 1.", md)


class RulesTest(unittest.TestCase):
    def test_option_matching_is_tolerant_but_exact_on_words(self):
        self.assertEqual(st.match_option("S2", "2018 - 2021"), "2018-2021")
        self.assertEqual(st.match_option("Q2", "1–6 Months Ago"), "1-6 months ago")
        self.assertEqual(st.match_option("F1", "not now"), "not now")  # not "no"
        self.assertEqual(st.match_option("Q7", "never gave anyone access"), "never gave anyone access")
        self.assertEqual(st.match_option("Q7", "maybe"), st.UNRECOGNISED)
        self.assertIsNone(st.match_option("Q7", "  "))

    def test_screener_segments(self):
        self.assertEqual(st.segment_of(row(HOLDER)), st.CYCLE_HOLDER)
        self.assertEqual(st.segment_of(row(HOLDER, S4="staking", S6="yes, only looked")), st.CYCLE_HOLDER)
        self.assertEqual(st.segment_of(row(HOLDER, S4="staking", S6="no")), st.SCREENED_OUT)
        self.assertEqual(st.segment_of(row(HOLDER, S4="lending; covered calls or other options sold by hand")),
                         st.CYCLE_HOLDER)
        self.assertEqual(st.segment_of(row(TRADER)), st.ACTIVE_TRADER)
        self.assertEqual(st.segment_of(row(TRADER, S7="yes")), st.SCREENED_OUT)
        self.assertEqual(st.segment_of(row(HOLDER, S3="did not hold then")), st.SCREENED_OUT)
        for r in SCREENED:
            self.assertEqual(st.segment_of(row(r)), st.SCREENED_OUT, r)

    def test_nps_score_bounds(self):
        self.assertEqual(st.nps_score(row(HOLDER, Q1="lending", N1="10")), 10)
        self.assertIsNone(st.nps_score(row(HOLDER, Q1="lending", N1="11")))
        self.assertIsNone(st.nps_score(row(HOLDER, Q1="lending", N1="7.5")))
        self.assertIsNone(st.nps_score(row(HOLDER, Q1="", N1="7")))
        self.assertIsNone(st.nps_score(row(HOLDER, Q1="never tried", N1="7")))

    def test_duplicate_ids_are_flagged(self):
        rows = [row(HOLDER, ID="P01", F1="no", Q6="no"), row(HOLDER, ID="P01", F1="no", Q6="no")]
        t = tally_csv(to_csv(rows))
        self.assertEqual(t["duplicate_ids"], ["P01"])
        self.assertIn("duplicate IDs P01", st.render_markdown(t))


class InputTest(unittest.TestCase):
    def test_mapping_renames_form_columns(self):
        rows = fifteen_and_five()
        named = {item: f"Question {item}" for item in st.ITEMS}
        renamed = [{named[k]: v for k, v in r.items()} for r in rows]
        text = to_csv(renamed, [named[i] for i in st.ITEMS])
        with self.assertRaises(st.TallyError):
            tally_csv(text)
        mapping = {v: k for k, v in named.items()}
        t = tally_csv(text, mapping)
        self.assertEqual((t["qualified"], t["screened_out"]), (15, 5))

    def test_map_file_with_unknown_item_refused(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "map.json")
            with open(path, "w") as f:
                json.dump({"col": "Q99"}, f)
            with self.assertRaises(st.TallyError):
                st.load_map(path)

    def test_main_prints_markdown_and_json(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "export.csv")
            with open(path, "w", encoding="utf-8") as f:
                f.write(to_csv(fifteen_and_five()))
            code, out, _ = run_main([path])
            self.assertEqual(code, 0)
            self.assertIn("# Survey tally", out)
            code, out, _ = run_main([path, "-json"])
            self.assertEqual(code, 0)
            self.assertEqual(json.loads(out)["qualified"], 15)

    def test_out_outside_private_dir_refused(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "export.csv")
            with open(path, "w", encoding="utf-8") as f:
                f.write(to_csv(fifteen_and_five()))
            code, out, err = run_main([path, "-out", os.path.join(d, "tally.md")])
            self.assertEqual(code, 2)
            self.assertIn("refused", err)
            self.assertEqual(out, "")

    def test_out_inside_private_dir_accepted(self):
        with tempfile.TemporaryDirectory() as d:
            target = os.path.join(d, "interviews", "tally.md")
            self.assertEqual(st.check_out_path(target, os.path.join(d, "interviews")), os.path.realpath(target))
            with self.assertRaises(st.TallyError):
                st.check_out_path(os.path.join(d, "interviews"), os.path.join(d, "interviews"))


if __name__ == "__main__":
    unittest.main()
