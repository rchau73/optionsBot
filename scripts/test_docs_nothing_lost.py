import tempfile
import unittest
from pathlib import Path

import docs_nothing_lost as d

OLD = """# Title

First sentence. Second sentence, e.g. with an abbreviation.

| Area | Shows |
|---|---|
| Menu | It lists pages. It is a bar. |

- A bullet. Another one.
"""


class NothingLost(unittest.TestCase):
    def test_a_split_that_moves_every_sentence_loses_none(self):
        new_a = "# Title\n\nFirst sentence.\nSecond sentence, e.g. with an abbreviation.\n"
        new_b = "### Menu\n\nIt lists pages.\nIt is a bar.\n\n- A bullet.\n- Another one.\n"
        self.assertEqual(d.missing(OLD, [new_a, new_b]), [])

    def test_a_dropped_sentence_is_listed(self):
        new = "# Title\n\nFirst sentence.\n\n### Menu\n\nIt lists pages.\n\n- A bullet.\n"
        self.assertEqual(d.missing(OLD, [new]), ["Second sentence, e.g. with an abbreviation.", "It is a bar.", "Another one."])

    def test_a_lead_in_label_is_not_a_loss(self):
        new = "# Title\n\nFirst sentence.\nSecond sentence, e.g. with an abbreviation.\n### Menu\n**Does:** It lists pages.\nIt is a bar.\n- A bullet.\n- Another one.\n"
        self.assertEqual(d.missing(OLD, [new]), [])

    def test_a_reworded_sentence_is_listed(self):
        new = OLD.replace("It is a bar.", "It is a strip.")
        self.assertEqual(d.missing(OLD, [new]), ["It is a bar."])

    def test_an_unescaped_pipe_in_a_cell_stays_in_that_cell(self):
        old = "| A | B |\n|---|---|\n| x | the |Δ| of a leg. Next. |\n"
        self.assertEqual(d.units(old), ["x", "the |Δ| of a leg.", "Next."])

    def test_links_count_by_anchor_or_file_name(self):
        old = "See [the page](docs/a.md) and [more](#income-page)."
        new = "See [the page](a.md) and [more](menu.md#income-page)."
        self.assertEqual(d.missing(old, [new]), [])


class Links(unittest.TestCase):
    def test_a_missing_file_and_a_missing_heading_are_reported(self):
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp, "a.md"), Path(tmp, "b.md")
            b.write_text("# B\n\n## Real heading\n", encoding="utf-8")
            a.write_text("[ok](b.md#real-heading) [bad anchor](b.md#nope) [no file](c.md) [web](https://x.y/z)\n", encoding="utf-8")
            self.assertEqual(d.broken_links(a), [f"{a}: b.md#nope (no such heading)", f"{a}: c.md (no such file)"])


if __name__ == "__main__":
    unittest.main()
