import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import reflow  # noqa: E402


class ReflowTest(unittest.TestCase):
    def test_splits_prose_into_one_sentence_per_line(self):
        self.assertEqual(reflow.reflow("One is here. Two is here! Three?"), "One is here.\nTwo is here!\nThree?")

    def test_list_item_continues_under_its_text(self):
        self.assertEqual(reflow.reflow("- First. Second."), "- First.\n  Second.")
        self.assertEqual(reflow.reflow("  1. First. Second."), "  1. First.\n     Second.")
        self.assertEqual(reflow.reflow("> Quote. More."), "> Quote.\n> More.")

    def test_leaves_code_tables_headings_and_links_alone(self):
        text = "# Title. Not split.\n| a. B | c |\n```\nA. B. C.\n```\nSee `a. B` and [Link. Text](x.md#a-b). Next."
        want = "# Title. Not split.\n| a. B | c |\n```\nA. B. C.\n```\nSee `a. B` and [Link. Text](x.md#a-b).\nNext."
        self.assertEqual(reflow.reflow(text), want)

    def test_abbreviations_numbers_and_lowercase_do_not_split(self):
        for line in ["Use e.g. Deribit here.", "Limit 1.5 Then.", "a. b is fine", "Dr J. Smith ran it", "Values: vs. Other."]:
            self.assertEqual(reflow.reflow(line), line)

    def test_reflow_is_idempotent_and_loses_nothing(self):
        text = "- A thing. Another thing.\n\nPlain. Text (see e.g. this). More.\n| x. Y |"
        once = reflow.reflow(text)
        self.assertEqual(reflow.reflow(once), once)
        self.assertEqual(reflow.missing(text, [once]), [])


class MissingTest(unittest.TestCase):
    def test_lists_a_dropped_or_reworded_sentence(self):
        old = "Keep this. Drop this.\n\n# Head"
        self.assertEqual(reflow.missing(old, ["Keep this.\n\n# Head"]), ["Drop this."])
        self.assertEqual(reflow.missing(old, ["Keep this.\nDrop that.\n# Head"]), ["Drop this."])

    def test_a_sentence_may_move_to_another_file(self):
        self.assertEqual(reflow.missing("A one. B two.", ["A one.", "B two."]), [])

    def test_a_lost_heading_or_table_row_is_listed(self):
        self.assertEqual(reflow.missing("# A\n| x |\nText.", ["# A\nText."]), ["| x |"])


if __name__ == "__main__":
    unittest.main()
