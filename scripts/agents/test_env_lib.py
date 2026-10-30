"""Tests of env_lib.sh's env_check (#129, #538): every environment directory of
the platform has its own Deribit account. The real library, sourced by bash,
against throwaway environment folders.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import os
import shutil
import subprocess
import tempfile
import unittest

SCRIPTS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LIB = os.path.join(SCRIPTS, "env_lib.sh")


class EnvCheck(unittest.TestCase):
    def setUp(self):
        self.envs = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.envs, True)
        self._env("stable", "stable-id", port=3000)
        self._env("mvp", "mvp-id", port=3100)

    def _write(self, path, text):
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(text)

    def _env(self, name, client_id, port=None):
        d = os.path.join(self.envs, name)
        os.makedirs(os.path.join(d, ".git"), exist_ok=True)
        self._write(os.path.join(d, ".env"), f"DERIBIT_CLIENT_ID={client_id}\nDERIBIT_ENV=testnet\n")
        if port is not None:
            self._write(os.path.join(d, "deploy.env"), f"OPTIONSBOT_ENV={name}\nMONITOR_PORT={port}\n")
        return d

    def _bash(self, code):
        env = dict(os.environ, OPTIONSBOT_ENVS=self.envs)
        return subprocess.run(["bash", "-c", f'set -euo pipefail; source "{LIB}"; {code}'],
                              env=env, capture_output=True, text=True, timeout=30)

    def test_separate_accounts_pass(self):
        for name in ("stable", "mvp"):
            r = self._bash(f"env_check {name}")
            self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_stable_and_mvp_on_one_account_are_refused(self):
        self._env("mvp", "stable-id", port=3100)
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("use the same Deribit account (DERIBIT_CLIENT_ID): each environment needs its own subaccount", r.stdout)
        self.assertIn(os.path.join(self.envs, "stable"), r.stdout)
        self.assertIn(os.path.join(self.envs, "mvp"), r.stdout)

    def test_shared_account_with_another_bot_of_the_platform(self):
        # Scenario: shared account — any environment folder, not only stable and mvp.
        other = self._env("customer-a", "mvp-id")
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(os.path.join(self.envs, "mvp"), r.stdout)
        self.assertIn(other, r.stdout)

    def test_any_two_directories_passed(self):
        a = self._env("a", "same")
        b = self._env("b", "same")
        c = self._env("c", "other")
        r = self._bash(f'env_accounts_distinct "{c}" "{a}" "{b}"')
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(r.stdout.strip(), f"{a} and {b} use the same Deribit account (DERIBIT_CLIENT_ID): "
                                           "each environment needs its own subaccount")
        self.assertEqual(self._bash(f'env_accounts_distinct "{a}" "{c}"').returncode, 0)
        self.assertEqual(self._bash("env_accounts_distinct").returncode, 0)

    def test_a_folder_without_credentials_is_not_an_environment(self):
        os.makedirs(os.path.join(self.envs, "history"))
        self._write(os.path.join(self.envs, "empty", ".env"), "DERIBIT_ENV=testnet\n")
        self.assertEqual(self._bash("env_check stable").returncode, 0)

    def test_shared_port_is_still_refused(self):
        self._env("mvp", "mvp-id", port=3000)
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("mvp and stable use the same MONITOR_PORT", r.stdout)


if __name__ == "__main__":
    unittest.main()
