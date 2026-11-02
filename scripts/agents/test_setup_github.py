"""Tests of setup_github.sh: python3 -m unittest scripts/agents/test_setup_github.py"""
import os
import re
import unittest

SCRIPT = os.path.join(os.path.dirname(__file__), "setup_github.sh")


class Labels(unittest.TestCase):
    def test_every_label_description_fits_githubs_limit(self):
        # GitHub refuses a description over 100 characters (HTTP 422), and the
        # script stops there: merge:approved's first one did (#596).
        labels = re.findall(r'^label "([^"]+)"\s+"[0-9a-f]{6}" "([^"]*)"', open(SCRIPT).read(), re.M)
        self.assertGreater(len(labels), 10)
        for name, desc in labels:
            self.assertLessEqual(len(desc), 100, name)


if __name__ == "__main__":
    unittest.main()
