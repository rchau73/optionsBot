"""Tests of deploy_issue.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import deploy_issue  # noqa: E402

SHA = "b" * 40
# Fake values, low-entropy on purpose: CI's gitleaks must not take them for keys.
ENV = """# the mvp .env
DERIBIT_CLIENT_ID=clientclient
DERIBIT_CLIENT_SECRET="secretsecret"   # comment
MONITOR_API_TOKEN='tokentoken'
export AUTH_SECRET=authauthauth
ADMIN_PASSWORD=passpasspass
ANALYTICS_SALT=saltsaltsalt
API_KEY=short
DERIBIT_ENV=testnet
"""


class Title(unittest.TestCase):
    def test_names_the_short_sha_and_subject(self):
        self.assertEqual(deploy_issue.title(SHA, " Fix it (#1)\n"), "[E6] mvp deploy failed: bbbbbbb (Fix it (#1))")


class Find(unittest.TestCase):
    def test_finds_an_issue_for_the_sha(self):
        issues = [{"number": 3, "title": "other"}, {"number": 7, "title": deploy_issue.title(SHA, "x")}]
        self.assertEqual(deploy_issue.find(SHA, issues), 7)

    def test_another_sha_is_not_a_duplicate(self):
        self.assertIsNone(deploy_issue.find(SHA, [{"number": 7, "title": deploy_issue.title("c" * 40, "x")}]))
        self.assertIsNone(deploy_issue.find(SHA, []))


class Redaction(unittest.TestCase):
    def test_secrets_by_name(self):
        names = {n for n, _ in deploy_issue.secrets(ENV)}
        self.assertEqual(names, {"DERIBIT_CLIENT_ID", "DERIBIT_CLIENT_SECRET", "MONITOR_API_TOKEN", "AUTH_SECRET", "ADMIN_PASSWORD", "ANALYTICS_SALT"})

    def test_quotes_and_comments_are_not_part_of_the_value(self):
        self.assertIn(("DERIBIT_CLIENT_SECRET", "secretsecret"), deploy_issue.secrets(ENV))
        self.assertIn(("MONITOR_API_TOKEN", "tokentoken"), deploy_issue.secrets(ENV))

    def test_scenario_no_secret_in_the_ticket(self):
        log = "\n".join([
            "bot-btc | auth client_id=clientclient secret=secretsecret",
            "monitor | Authorization: Bearer tokentoken",
            "monitor | curl -H 'Authorization: Bearer zzzzzzzzzzzz'",
            "monitor | auth=authauthauth admin=passpasspass",
            "bot-btc | testnet",
        ])
        out = deploy_issue.body(SHA, "health check", "rolled back", log, "abc1234 Fix", deploy_issue.secrets(ENV))
        for value in ("clientclient", "secretsecret", "tokentoken", "zzzzzzzzzzzz", "authauthauth", "passpasspass"):
            self.assertNotIn(value, out)
        self.assertIn("[redacted DERIBIT_CLIENT_SECRET]", out)
        self.assertIn("[redacted DERIBIT_CLIENT_ID]", out)
        self.assertIn("bot-btc | testnet", out)

    def test_value_inside_a_longer_value_is_redacted_whole(self):
        pairs = deploy_issue.secrets("A_TOKEN=abcdef\nB_TOKEN=abcdefghij\n")
        self.assertEqual(deploy_issue.redact("x abcdefghij y", pairs), "x [redacted B_TOKEN] y")


class Body(unittest.TestCase):
    def test_keeps_the_last_80_lines(self):
        log = "\n".join(f"line {i}" for i in range(200))
        out = deploy_issue.body(SHA, "docker compose up --build", "none", log, "", [])
        self.assertIn("line 199", out)
        self.assertIn("line 120", out)
        self.assertNotIn("line 119\n", out)

    def test_has_the_step_rollback_range_and_marker(self):
        out = deploy_issue.body(SHA, "health check", "mvp rolled back to `aaaaaaa`, healthy", "boom", "bbbbbbb Break it", [])
        self.assertIn("**Failed step:** health check", out)
        self.assertIn("rolled back to `aaaaaaa`", out)
        self.assertIn("bbbbbbb Break it", out)
        self.assertIn(f"<!-- deploy-failed: {SHA} -->", out)

    def test_backticks_in_the_log_do_not_break_the_fence(self):
        out = deploy_issue.body(SHA, "s", "r", "```\ninjected", "", [])
        self.assertIn("````text\n```\ninjected\n````", out)

    def test_long_lines_are_cut(self):
        out = deploy_issue.body(SHA, "s", "r", "x" * 5000, "", [])
        self.assertNotIn("x" * (deploy_issue.MAX_LINE + 1), out)

    def test_empty_log(self):
        self.assertIn("(empty)", deploy_issue.body(SHA, "s", "r", "", "", []))


if __name__ == "__main__":
    unittest.main()
