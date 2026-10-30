"""Tests of deploy_disk.py (#548): python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import deploy_disk  # noqa: E402

DF = """Filesystem     1024-blocks     Used Available Capacity Mounted on
/dev/vda1         61255492 57000000   4255492      {}% /app/data/monitor
"""


class Threshold(unittest.TestCase):
    def test_use_pct_from_df(self):
        self.assertEqual(deploy_disk.use_pct(DF.format(93)), 93)
        self.assertEqual(deploy_disk.use_pct(DF.format(100)), 100)

    def test_unreadable_df_is_unknown(self):
        for text in ("", "Filesystem 1024-blocks Used Available Capacity Mounted on\n", "error: no such service\n"):
            self.assertIsNone(deploy_disk.use_pct(text), text)

    def test_warns_only_over_85(self):
        cases = {None: False, 0: False, 84: False, 85: False, 86: True, 100: True}
        for pct, want in cases.items():
            self.assertEqual(deploy_disk.over(pct), want, pct)

    def test_check_prints_only_when_over(self):
        import io
        for pct, want in ((85, ""), (86, "86\n")):
            sys.stdin, out = io.StringIO(DF.format(pct)), io.StringIO()
            old = sys.stdout
            sys.stdout = out
            try:
                self.assertEqual(deploy_disk.main(["deploy_disk.py", "check"]), 0)
            finally:
                sys.stdout, sys.stdin = old, sys.__stdin__
            self.assertEqual(out.getvalue(), want)


class Ticket(unittest.TestCase):
    def test_title_and_find(self):
        t = deploy_disk.title(91)
        self.assertEqual(t, "[E6] Docker disk over 85 % full (91 %)")
        self.assertEqual(deploy_disk.find([{"number": 3, "title": "other"}, {"number": 9, "title": t}]), 9)
        self.assertIsNone(deploy_disk.find([{"number": 3, "title": "[E6] mvp deploy failed: abc"}]))

    def test_body_never_suggests_pruning_volumes_automatically(self):
        out = deploy_disk.body(91, DF.format(91), "Local Volumes 87 3 4.5GB")
        self.assertIn("**91 %** full", out)
        self.assertIn("never pruned automatically", out)
        self.assertIn("Local Volumes 87", out)
        self.assertNotIn("volume prune", out)

    def test_backticks_do_not_break_the_fence(self):
        self.assertIn("````text\n```x\n````", deploy_disk.body(90, "```x", ""))


if __name__ == "__main__":
    unittest.main()
