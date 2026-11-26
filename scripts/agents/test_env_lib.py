"""Tests of env_lib.sh's env_check (#129, #538): every environment directory of
the platform has its own Deribit account. The real library, sourced by bash,
against throwaway environment folders.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import json
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

    # ── The SOL and XRP bots (#837): BOT_LINEAR=1, each on its own account ──

    def _linear(self, sol="sol-id", xrp="xrp-id", secrets=True, flag="1"):
        d = os.path.join(self.envs, "mvp")
        self._write(os.path.join(d, "deploy.env"), f"OPTIONSBOT_ENV=mvp\nMONITOR_PORT=3100\nBOT_LINEAR={flag}\n")
        lines = ["DERIBIT_CLIENT_ID=mvp-id", "DERIBIT_ENV=testnet"]
        for coin, cid in (("SOL", sol), ("XRP", xrp)):
            if cid:
                lines.append(f"{coin}_DERIBIT_CLIENT_ID={cid}")
                if secrets:
                    lines.append(f"{coin}_DERIBIT_CLIENT_SECRET=s-{coin.lower()}")
        self._write(os.path.join(d, ".env"), "\n".join(lines) + "\n")
        return d

    def test_linear_bots_on_their_own_accounts_pass(self):
        self._linear()
        r = self._bash("env_check mvp")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_sol_and_xrp_on_one_account_are_refused(self):
        # Scenario: two coins never share an account — bot-sol and bot-xrp configured with one account.
        d = self._linear(sol="usdc-id", xrp="usdc-id")
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(r.stdout.strip(), f"{d} (SOL_DERIBIT_CLIENT_ID) and {d} (XRP_DERIBIT_CLIENT_ID) use the same Deribit account: "
                                           "each bot needs its own subaccount")

    def test_a_linear_bot_on_the_btc_eth_account_is_refused(self):
        d = self._linear(sol="mvp-id")
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(f"{d} (DERIBIT_CLIENT_ID) and {d} (SOL_DERIBIT_CLIENT_ID) use the same Deribit account", r.stdout)

    def test_a_linear_bot_on_another_environments_account_is_refused(self):
        self._linear(xrp="stable-id")
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(f"{os.path.join(self.envs, 'stable')} (DERIBIT_CLIENT_ID)", r.stdout)
        self.assertIn(f"{os.path.join(self.envs, 'mvp')} (XRP_DERIBIT_CLIENT_ID)", r.stdout)
        self.assertIn("each bot needs its own subaccount", r.stdout)

    def test_linear_bots_without_their_keys_are_refused(self):
        d = self._linear(xrp="")
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(f"{d}/deploy.env has BOT_LINEAR=1 but .env has no XRP_DERIBIT_CLIENT_ID and XRP_DERIBIT_CLIENT_SECRET", r.stdout)
        self._linear(secrets=False)
        self.assertIn("no SOL_DERIBIT_CLIENT_ID and SOL_DERIBIT_CLIENT_SECRET", self._bash("env_check mvp").stdout)

    def test_without_the_switch_nothing_is_asked(self):
        # BOT_LINEAR missing or 0: the SOL/XRP keys are not needed (stable, an mvp before the person adds them).
        self._linear(sol="", xrp="", flag="0")
        self.assertEqual(self._bash("env_check mvp").returncode, 0)

    def test_compose_vars_follow_the_switch(self):
        self._linear()
        r = self._bash("env_compose_vars mvp")
        self.assertEqual(r.stdout.splitlines(), ["COMPOSE_PROFILES=linear",
                                                 "MONITOR_LINEAR_APIS=,sol=http://bot-sol:8083,xrp=http://bot-xrp:8084"])
        r = self._bash("env_compose_vars stable")
        self.assertEqual(r.stdout.splitlines(), ["COMPOSE_PROFILES=", "MONITOR_LINEAR_APIS="])


class RegistryCheck(EnvCheck):
    """env_check's account-registry step (#800, scripts/env_registry.py): the
    real cmd/accounts, built once, on registries written to the temp folders.
    It inherits EnvCheck's tests, so each still holds with the step in place."""

    @classmethod
    def setUpClass(cls):
        cls.bin_dir = tempfile.mkdtemp()
        cls.accounts_bin = os.path.join(cls.bin_dir, "accounts")
        subprocess.run(["go", "build", "-o", cls.accounts_bin, "./cmd/accounts"], cwd=os.path.dirname(SCRIPTS), check=True)
        # A docker that only records it was called: envctl up must not reach it on a refusal.
        cls.docker_log = os.path.join(cls.bin_dir, "docker.log")
        stub = os.path.join(cls.bin_dir, "docker")
        with open(stub, "w") as f:
            f.write(f'#!/bin/sh\necho "$@" >> "{cls.docker_log}"\n')
        os.chmod(stub, 0o755)

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.bin_dir, True)

    def setUp(self):
        super().setUp()
        if os.path.exists(self.docker_log):
            os.remove(self.docker_log)

    def _bash(self, code, go_run=False):
        env = dict(os.environ, OPTIONSBOT_ENVS=self.envs)
        if not go_run:
            env["ACCOUNTS_BIN"] = self.accounts_bin
        return subprocess.run(["bash", "-c", f'set -euo pipefail; source "{LIB}"; {code}'],
                              env=env, capture_output=True, text=True, timeout=300)

    def _envctl(self, name, *args):
        env = dict(os.environ, OPTIONSBOT_ENVS=self.envs, ACCOUNTS_BIN=self.accounts_bin,
                   PATH=self.bin_dir + os.pathsep + os.environ["PATH"])
        return subprocess.run(["bash", os.path.join(SCRIPTS, "envctl.sh"), name, *args],
                              env=env, capture_output=True, text=True, timeout=300)

    def _private(self, path, doc):
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            json.dump(doc, f)
        os.chmod(path, 0o600)

    def _registry(self, name, accounts, keys):
        """accounts: (id, key id, [(currency, config)]); keys: key id → (status, scope)."""
        d = os.path.join(self.envs, name)
        self._private(os.path.join(d, "data", "accounts.json"), {"version": 1, "customers": [{
            "id": "c-001", "accounts": [{"id": a, "exchange": "deribit", "environment": "testnet", "label": a,
                                         "key_id": k, "bots": [{"currency": c, "config": cfg} for c, cfg in bots]}
                                        for a, k, bots in accounts]}]})
        self._private(os.path.join(d, "data", "keystore.json"), {"version": 1, "accounts": {
            k: {"label": k, "status": s, "key_scope": scope, "updated_at": "2026-11-02T00:00:00Z"}
            for k, (s, scope) in keys.items()}})

    def _one(self, name, status="active", scope="trade_only", account=None):
        account = account or f"acct-{name}"
        self._registry(name, [(account, f"key-{name}", [("BTC", "config_btc.yaml"), ("ETH", "config_eth.yaml")])],
                       {f"key-{name}": (status, scope)})

    def test_no_registry_changes_nothing(self):
        # Scenario: no data/accounts.json — the same result, and nothing printed.
        r = self._bash("env_check mvp")
        self.assertEqual((r.returncode, r.stdout, r.stderr), (0, "", ""))

    def test_revoked_key_refuses_envctl_up(self):
        # Scenario: the environment's account key is revoked — envctl up refuses, no container starts.
        self._one("mvp", status="revoked")
        r = self._envctl("mvp", "up")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("refusing to start mvp:", r.stderr)
        self.assertIn("runs on account acct-mvp, and the key is revoked", r.stderr)
        self.assertFalse(os.path.exists(self.docker_log), "docker was called")

    def test_key_that_can_move_funds_refuses(self):
        # Scenario: key_status can_move_funds — refused, says the key may move funds.
        self._one("mvp", scope="can_move_funds")
        r = self._envctl("mvp", "up")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("acct-mvp", r.stderr)
        self.assertIn("the key may move funds", r.stderr)
        self.assertFalse(os.path.exists(self.docker_log))

    def test_missing_key_refuses(self):
        self._registry("mvp", [("acct-mvp", "key-mvp", [("BTC", "config_btc.yaml")])], {})
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("the key is missing from the key store", r.stdout)

    def test_two_folders_on_one_account_are_refused(self):
        # Scenario: two environment folders map to the same registry account — both named.
        self._one("stable", account="acct-shared")
        self._one("mvp", account="acct-shared")
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(f"{os.path.join(self.envs, 'mvp')} and {os.path.join(self.envs, 'stable')} "
                      "both run on registry account acct-shared", r.stdout)

    def test_clean_registry_starts_as_before(self):
        # Scenario: a clean registry and key ok — envctl up starts, through the real go run once.
        self._one("stable")
        self._one("mvp")
        r = self._bash("env_check mvp", go_run=True)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        r = self._envctl("mvp", "up")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        with open(self.docker_log) as f:
            self.assertIn("compose", f.read())

    def test_mvp_btc_config_from_deploy_env(self):
        # mvp runs config_btc.mvp.yaml (BOT_CONFIG_BTC): its BTC bot is matched by that name.
        d = os.path.join(self.envs, "mvp")
        self._write(os.path.join(d, "deploy.env"), "OPTIONSBOT_ENV=mvp\nMONITOR_PORT=3100\nBOT_CONFIG_BTC=./config_btc.mvp.yaml\n")
        self._registry("mvp", [("acct-mvp", "key-mvp", [("BTC", "config_btc.mvp.yaml"), ("ETH", "config_eth.yaml")])],
                       {"key-mvp": ("active", "trade_only")})
        r = self._bash("env_check mvp")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_bot_not_in_registry(self):
        # Not listed and its currency nowhere in the registry: said, not refused.
        self._registry("mvp", [("acct-mvp", "key-mvp", [("BTC", "config_btc.yaml")])], {"key-mvp": ("active", "trade_only")})
        r = self._bash("env_check mvp")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("the ETH bot (config_eth.yaml) is not in data/accounts.json", r.stderr)
        # Its currency listed under another account (another config): refused.
        self._registry("mvp", [("acct-mvp", "key-mvp", [("BTC", "config_btc.yaml")]),
                               ("acct-other", "key-other", [("ETH", "config_eth.other.yaml")])],
                       {"key-mvp": ("active", "trade_only"), "key-other": ("active", "trade_only")})
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("the ETH bot (config_eth.yaml) is not in data/accounts.json, which lists ETH under account acct-other",
                      r.stdout)

    def test_envctl_up_refuses_sol_and_xrp_on_one_account(self):
        # Scenario: two coins never share an account — envctl up refuses, no container starts.
        self._linear(sol="usdc-id", xrp="usdc-id")
        r = self._envctl("mvp", "up")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("refusing to start mvp:", r.stderr)
        self.assertIn("use the same Deribit account: each bot needs its own subaccount", r.stderr)
        self.assertFalse(os.path.exists(self.docker_log), "docker was called")

    def test_registry_matches_the_linear_bots(self):
        # With BOT_LINEAR=1 the SOL and XRP bots are matched too: one of them under
        # the BTC/ETH account's currency elsewhere is refused like any bot.
        self._linear()
        self._registry("mvp", [("acct-mvp", "key-mvp", [("BTC", "config_btc.yaml"), ("ETH", "config_eth.yaml")]),
                               ("acct-sol", "key-sol", [("SOL", "config_sol.yaml")]),
                               ("acct-xrp", "key-xrp", [("XRP", "config_xrp.yaml")])],
                       {k: ("active", "trade_only") for k in ("key-mvp", "key-sol", "key-xrp")})
        r = self._bash("env_check mvp")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self._registry("mvp", [("acct-mvp", "key-mvp", [("BTC", "config_btc.yaml"), ("ETH", "config_eth.yaml")]),
                               ("acct-sol", "key-sol", [("SOL", "config_sol.yaml")]),
                               ("acct-xrp", "key-xrp", [("XRP", "config_xrp.yaml")])],
                       {"key-mvp": ("active", "trade_only"), "key-sol": ("active", "trade_only"), "key-xrp": ("revoked", "trade_only")})
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("the XRP bot (config_xrp.yaml) runs on account acct-xrp, and the key is revoked", r.stdout)

    def test_rule_break_refuses_and_prints_no_key_id(self):
        # Two accounts on one key: a rule break, named by rule; the key id never printed.
        self._registry("mvp", [("acct-mvp", "key-secret-7", [("BTC", "config_btc.yaml")]),
                               ("acct-two", "key-secret-7", [("ETH", "config_eth.yaml")])],
                       {"key-secret-7": ("active", "trade_only")})
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("breaks a rule: one_account_per_key", r.stdout)
        self.assertNotIn("key-secret-7", r.stdout + r.stderr)

    def test_unreadable_registry_refuses_without_quoting_it(self):
        self._write(os.path.join(self.envs, "mvp", "data", "accounts.json"), '{"key_id": "key-secret-9"')
        os.chmod(os.path.join(self.envs, "mvp", "data", "accounts.json"), 0o600)
        r = self._bash("env_check mvp")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("could not be checked", r.stdout)
        self.assertNotIn("key-secret-9", r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
