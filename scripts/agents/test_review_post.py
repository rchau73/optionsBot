"""Tests of review_post.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import review_post  # noqa: E402


def review(block, files=None, nxt="clean"):
    d = tempfile.mkdtemp()
    md, rj, out, ft = (os.path.join(d, n) for n in ("agent.md", "review.json", "post.json", "files.txt"))
    open(md, "w").write(f"answer\n<!-- REVIEW-JSON\n{json.dumps(block)}\n-->\n")
    if files is not None:
        open(ft, "w").write("".join(f + "\n" for f in files))
    review_post.parse(md, rj, ft if files is not None else None)
    review_post.body(rj, out, "o/r", "abcdef1234", "1", nxt, "agent-review")
    return json.load(open(rj)), json.load(open(out))["body"]


# PR #474's shape: a diff over 2,000 lines across several files.
FILES = ["internal/delivery/markdown.go", "internal/delivery/read.go", "internal/delivery/write.go", "tests/delivery_test.go"]


def read_all(files=FILES, **override):
    return [{"file": f, "read": override.get(f, "yes")} for f in files]


class Completeness(unittest.TestCase):
    """#476: a review that leaves a changed file unread is incomplete, never clean."""

    def test_every_file_read_is_clean_and_the_body_lists_each(self):
        r, body = review({"summary": "s", "checked": read_all(), "ran": "- go test: ok", "findings": [],
                          "policy": {"changes": "no", "why": "x"}}, FILES)
        self.assertEqual((r["verdict"], r["unread"]), ("clean", []))
        for f in FILES:
            self.assertIn(f"- `{f}` — read: yes", body)
        self.assertIn("- go test: ok", body)

    def test_a_file_read_no_is_incomplete(self):
        r, body = review({"summary": "s", "checked": read_all(**{"internal/delivery/write.go": "no"}), "findings": [],
                          "policy": {"changes": "no", "why": "x"}}, FILES, "incomplete")
        self.assertEqual((r["verdict"], r["unread"]), ("incomplete", ["internal/delivery/write.go"]))
        self.assertIn("verdict=incomplete policy=no -->", body)
        self.assertIn("**Not fully reviewed:** 1 changed file(s) not read: `internal/delivery/write.go`", body)
        self.assertNotIn("the PR gate merges it", body)

    def test_a_missing_file_or_the_old_free_text_form_is_incomplete(self):
        for checked in (read_all(FILES[:-1]), "- read everything", None, [{"file": FILES[0]}]):
            block = {"summary": "s", "findings": [], "policy": {"changes": "no", "why": "x"}}
            if checked is not None:
                block["checked"] = checked
            r, _ = review(block, FILES[:1] if checked == [{"file": FILES[0]}] else FILES)
            self.assertEqual(r["verdict"], "incomplete", checked)

    def test_diff_file_paths_and_prefixes_name_the_changed_file(self):
        checked = [{"file": ".agent-review/diff/internal/delivery/read.go.diff", "read": "yes"},
                   {"file": "b/tests/delivery_test.go", "read": "Yes"}, {"file": "`internal/delivery/write.go`", "read": "yes"},
                   {"file": "./internal/delivery/markdown.go", "read": "yes"}]
        r, _ = review({"summary": "s", "checked": checked, "findings": []}, FILES)
        self.assertEqual(r["verdict"], "clean")

    def test_a_blocking_finding_still_blocks_when_files_are_unread(self):
        f = {"severity": "blocking", "file": FILES[0], "line": 3, "title": "t", "scenario": "s", "fix": "f"}
        r, _ = review({"summary": "s", "checked": [], "findings": [f]}, FILES, "ready")
        self.assertEqual(r["verdict"], "blocking")

    def test_the_second_try_is_left_for_a_person(self):
        _, body = review({"summary": "s", "checked": [], "findings": []}, FILES, "incomplete-human")
        self.assertIn("a person reviews this PR", body)


class Due(unittest.TestCase):
    """#476: an incomplete review is retried once on the next tick, then left for a person."""

    def test_due(self):
        cases = [([], True),
                 (["sha=old verdict=clean policy=no"], True),
                 (["sha=abc verdict=incomplete policy=no"], True),
                 (["sha=abc verdict=incomplete policy=no", "sha=abc verdict=incomplete policy=no"], False),
                 (["sha=abc verdict=clean policy=no"], False),
                 (["sha=abc verdict=blocking policy=no"], False),
                 (["sha=abc verdict=failed"], False),
                 (["sha=abcd verdict=clean policy=no"], True)]
        for markers, want in cases:
            self.assertEqual(review_post.due("abc", markers), want, markers)


class Policy(unittest.TestCase):
    """#468: the Reviewer declares whether a PR changes a policy; anything but "no" is yes."""

    def test_no_is_kept_and_the_gate_merges(self):
        r, body = review({"summary": "s", "findings": [], "policy": {"changes": "no", "why": "copy only"}})
        self.assertEqual(r["policy"], {"changes": "no", "why": "copy only"})
        self.assertIn("<!-- agent-review sha=abcdef1234 verdict=clean policy=no -->", body)
        self.assertIn("the PR gate merges it", body)

    def test_missing_or_unclear_counts_as_yes_and_a_person_merges(self):
        for pol in (None, {"changes": "maybe"}, "no"):
            block = {"summary": "s", "findings": []}
            if pol is not None:
                block["policy"] = pol
            r, body = review(block)
            self.assertEqual(r["policy"]["changes"], "yes", pol)
            self.assertIn("verdict=clean policy=yes -->", body)
            self.assertIn("a person merges it", body)
