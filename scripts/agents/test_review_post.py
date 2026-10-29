"""Tests of review_post.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import review_post  # noqa: E402


def review(block):
    d = tempfile.mkdtemp()
    md, rj, out = (os.path.join(d, n) for n in ("agent.md", "review.json", "post.json"))
    open(md, "w").write(f"answer\n<!-- REVIEW-JSON\n{json.dumps(block)}\n-->\n")
    review_post.parse(md, rj)
    review_post.body(rj, out, "o/r", "abcdef1234", "1", "clean", "agent-review")
    return json.load(open(rj)), json.load(open(out))["body"]


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
