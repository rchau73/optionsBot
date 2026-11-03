"""Tests of scripts/regen.sh --resolve (#465): a merge whose only conflicts are in
generated files finishes with no hand edits.
python3 -m unittest scripts/agents/test_regen.py"""
import json
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SCRIPT = os.path.join(ROOT, "scripts", "regen.sh")
MERGE_LINES = os.path.join(ROOT, "scripts", "merge_lines.py")
GOLDEN = "docs/product/assistant/golden.jsonl"
GLOSSARY = "frontend/content/glossary.json"
INDEX = "frontend/content/help-index.json"
ENV = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@x", "GIT_COMMITTER_NAME": "t",
       "GIT_COMMITTER_EMAIL": "t@x", "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}


def git(cwd, *args, check=True):
    return subprocess.run(["git", *args], cwd=cwd, env=ENV, check=check, capture_output=True, text=True)


def regen(cwd, *args):
    return subprocess.run(["bash", "scripts/regen.sh", *args], cwd=cwd, env=ENV, capture_output=True, text=True)


def add_term(repo, term, short):
    """Adds a glossary term in its alphabetical place, editing only its own lines."""
    path = os.path.join(repo, GLOSSARY)
    with open(path) as f:
        text = f.read()
    keys = list(json.loads(text))
    after = [k for k in keys if k > term]
    block = f'  "{term}": {{\n    "short": "{short}",\n    "long": "{short}",\n    "risk_income": "{short}",\n    "source": "OptionStrategy.md"\n  }}'
    if after:
        marker = f'  "{after[0]}": {{'
        text = text.replace(marker, block + ",\n" + marker, 1)
    else:
        text = text[: text.rindex("  }\n}")] + "  },\n" + block + "\n}\n"
    with open(path, "w") as f:
        f.write(text)


def unmerged(repo):
    return git(repo, "diff", "--name-only", "--diff-filter=U").stdout.split()


@unittest.skipUnless(shutil.which("go"), "needs Go to run the generators")
class MergeOfTwoGlossaryTerms(unittest.TestCase):
    """Two branches each add a glossary term and regenerate the help index (the acceptance)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.repo = os.path.join(self.tmp.name, "repo")
        # The repository as it is now (the Go module and every source the
        # generators read); the frontend's code is not needed.
        files = git(ROOT, "ls-files", "--cached", "--others", "--exclude-standard").stdout.splitlines()
        for rel in files:
            src = os.path.join(ROOT, rel)
            if (rel.startswith("frontend/") and not rel.startswith("frontend/content/")) or rel.startswith("data/") or not os.path.isfile(src):
                continue
            dst = os.path.join(self.repo, rel)
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            shutil.copy2(src, dst)
        git(self.repo, "init", "-q", "-b", "main")
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-q", "-m", "base")
        for branch, term in (("a", "kappa"), ("b", "rho")):
            git(self.repo, "checkout", "-q", "-b", branch, "main")
            add_term(self.repo, term, f"The {term} test term.")
            subprocess.run(["go", "run", "./cmd/helpindex"], cwd=self.repo, env=ENV, check=True, capture_output=True)
            git(self.repo, "commit", "-q", "-am", f"add {term}")
        git(self.repo, "checkout", "-q", "a")

    def tearDown(self):
        self.tmp.cleanup()

    def test_the_merge_finishes_with_no_hand_edits(self):
        self.assertNotEqual(git(self.repo, "merge", "-q", "--no-edit", "b", check=False).returncode, 0)
        self.assertEqual(unmerged(self.repo), [INDEX], "the terms sit apart: only the generated index conflicts")

        r = regen(self.repo, "--resolve")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(unmerged(self.repo), [])
        git(self.repo, "commit", "-q", "--no-edit")

        with open(os.path.join(self.repo, GLOSSARY)) as f:
            self.assertTrue({"kappa", "rho"} <= set(json.load(f)))
        with open(os.path.join(self.repo, INDEX)) as f:
            index = f.read()
        self.assertNotIn("<<<<<<<", index)
        self.assertIn("The kappa test term.", index)
        self.assertIn("The rho test term.", index)
        # The committed index is what the generator writes from the merged sources.
        subprocess.run(["go", "run", "./cmd/helpindex"], cwd=self.repo, env=ENV, check=True, capture_output=True)
        self.assertEqual(git(self.repo, "status", "--porcelain").stdout, "")


class ConflictInASource(unittest.TestCase):
    """A small repository: a source file and its generated file both conflict."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.repo = self.tmp.name
        os.makedirs(os.path.join(self.repo, "scripts"))
        shutil.copy2(SCRIPT, os.path.join(self.repo, "scripts", "regen.sh"))
        self.write(".gitattributes", "gen.json linguist-generated=true\n")
        self.write("src.txt", "base\n")
        self.write("gen.json", "base\n")
        git(self.repo, "init", "-q", "-b", "main")
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-q", "-m", "base")
        for branch in ("a", "b"):
            git(self.repo, "checkout", "-q", "-b", branch, "main")
            self.write("src.txt", f"{branch}\n")
            self.write("gen.json", f"{branch}\n")
            git(self.repo, "commit", "-q", "-am", branch)
        git(self.repo, "checkout", "-q", "a")
        git(self.repo, "merge", "-q", "--no-edit", "b", check=False)

    def tearDown(self):
        self.tmp.cleanup()

    def write(self, name, text):
        with open(os.path.join(self.repo, name), "w") as f:
            f.write(text)

    def test_another_conflict_is_left_to_resolve_first(self):
        self.assertEqual(sorted(unmerged(self.repo)), ["gen.json", "src.txt"])
        r = regen(self.repo, "--resolve")
        self.assertEqual(r.returncode, 3)
        self.assertIn("- src.txt", r.stderr)
        self.assertNotIn("- gen.json", r.stderr)
        with open(os.path.join(self.repo, "gen.json")) as f:
            self.assertEqual(f.read(), "a\n", "the generated file holds our side, no conflict markers")
        with open(os.path.join(self.repo, "src.txt")) as f:
            self.assertIn("<<<<<<<", f.read(), "a source conflict is never resolved by the script")

    def test_an_unknown_option_is_refused(self):
        self.assertEqual(regen(self.repo, "--bogus").returncode, 2)


def rec(rid, q="q"):
    return json.dumps({"id": rid, "question": q})


class SortedListConflict(unittest.TestCase):
    """#596: two branches each add a golden question next to each other."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.repo = self.tmp.name
        os.makedirs(os.path.join(self.repo, "scripts"))
        os.makedirs(os.path.join(self.repo, os.path.dirname(GOLDEN)))
        for src in (SCRIPT, MERGE_LINES):
            shutil.copy2(src, os.path.join(self.repo, "scripts"))
        self.write(GOLDEN, [rec("how-01"), rec("how-03"), rec("how-04")])
        self.write("src.txt", ["base"])
        git(self.repo, "init", "-q", "-b", "main")
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-q", "-m", "base")

    def tearDown(self):
        self.tmp.cleanup()

    def write(self, name, lines):
        with open(os.path.join(self.repo, name), "w") as f:
            f.write("".join(l + "\n" for l in lines))

    def read(self, name):
        with open(os.path.join(self.repo, name)) as f:
            return f.read()

    def branches(self, a, b, src=False):
        for branch, lines in (("a", a), ("b", b)):
            git(self.repo, "checkout", "-q", "-b", branch, "main")
            self.write(GOLDEN, lines)
            if src:
                self.write("src.txt", [branch])
            git(self.repo, "commit", "-q", "-am", branch)
        git(self.repo, "checkout", "-q", "a")
        self.assertNotEqual(git(self.repo, "merge", "-q", "--no-edit", "b", check=False).returncode, 0)

    def merge_lines(self):
        return subprocess.run(["python3", "scripts/merge_lines.py", GOLDEN], cwd=self.repo, env=ENV, capture_output=True, text=True)

    def test_both_new_lines_are_kept_in_order(self):
        self.branches([rec("how-01"), rec("how-02-a"), rec("how-03"), rec("how-04")],
                      [rec("how-01"), rec("how-02-b"), rec("how-03")])  # b also removed how-04
        r = self.merge_lines()
        self.assertEqual(r.returncode, 0, r.stderr)
        ids = [json.loads(l)["id"] for l in self.read(GOLDEN).splitlines()]
        self.assertEqual(ids, ["how-01", "how-02-a", "how-02-b", "how-03"], "both added; the removed line stays removed")
        self.assertEqual(unmerged(self.repo), [])

    def test_two_records_with_one_id_are_left_to_a_person(self):
        self.branches([rec("how-01"), rec("how-02", "a"), rec("how-03"), rec("how-04")],
                      [rec("how-01"), rec("how-02", "b"), rec("how-03"), rec("how-04")])
        r = self.merge_lines()
        self.assertEqual(r.returncode, 1)
        self.assertIn('"how-02"', r.stderr)
        self.assertIn("<<<<<<<", self.read(GOLDEN))
        self.assertEqual(unmerged(self.repo), [GOLDEN])

    def test_regen_resolve_settles_the_list_before_naming_other_conflicts(self):
        self.branches([rec("how-01"), rec("how-02-a"), rec("how-03"), rec("how-04")],
                      [rec("how-01"), rec("how-02-b"), rec("how-03"), rec("how-04")], src=True)
        r = regen(self.repo, "--resolve")
        self.assertEqual(r.returncode, 3)
        self.assertIn("- src.txt", r.stderr)
        self.assertNotIn(GOLDEN, r.stderr)
        self.assertEqual(unmerged(self.repo), ["src.txt"])


if __name__ == "__main__":
    unittest.main()
