"""env_registry.py — env_check's account-registry step (#800, env_lib.sh
env_registry_check). Read-only.

    python3 scripts/env_registry.py <env dir> <platform dir>...

When <env dir>/data/accounts.json does not exist it does nothing. Otherwise it
runs `cmd/accounts -check -json` on each folder's registry and key store
(data/keystore.json beside it) and fails, printing why on stdout, when:
  - the registry breaks a rule (its `violations`);
  - the account a bot of the environment runs on has a key_status other than ok;
  - a bot of the environment is not in the registry while the registry lists
    its currency under an account (or lists it under two);
  - another platform folder's registry maps one of its bots to the same account.

A bot is matched by its currency and config file name, as docker-compose.yml
runs them: BTC on BOT_CONFIG_BTC of deploy.env (default config_btc.yaml), ETH
on config_eth.yaml. The status document holds no key, key id, client id or
scope string, and cmd/accounts' own stderr (a parse error may quote the file)
is never printed. ACCOUNTS_BIN runs a built cmd/accounts instead of go run.
Notes that do not refuse go to stderr."""
import json
import os
import subprocess
import sys

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REGISTRY = os.path.join("data", "accounts.json")
KEYSTORE = os.path.join("data", "keystore.json")

# key_status (accounts.KeyState) in plain words.
KEY_WORDS = {
    "missing": "the key is missing from the key store",
    "revoked": "the key is revoked",
    "can_move_funds": "the key may move funds (a trade-only key is required)",
    "unknown_scope": "the key's scope is unknown",
}


class Unreadable(Exception):
    pass


def dotenv_var(path, key):
    """The last value of key in a dotenv file, quotes stripped ("" if none)."""
    value = ""
    try:
        with open(path) as f:
            for line in f:
                name, sep, rest = line.strip().partition("=")
                if sep and name.strip() == key:
                    value = rest.strip().strip("\"'")
    except FileNotFoundError:
        pass
    return value


def env_bots(env_dir):
    """The (currency, config file name) of each bot the folder's stack runs."""
    btc = dotenv_var(os.path.join(env_dir, "deploy.env"), "BOT_CONFIG_BTC")
    return [("BTC", os.path.basename(btc) if btc else "config_btc.yaml"), ("ETH", "config_eth.yaml")]


def status(env_dir):
    """The registry's status document (accounts.StatusDocument) for a folder."""
    cmd = [os.environ["ACCOUNTS_BIN"]] if os.environ.get("ACCOUNTS_BIN") else ["go", "run", "./cmd/accounts"]
    cmd += ["-check", "-json", "-registry", os.path.join(env_dir, REGISTRY), "-keystore", os.path.join(env_dir, KEYSTORE)]
    try:
        r = subprocess.run(cmd, cwd=REPO, capture_output=True, text=True, timeout=300)
        return json.loads(r.stdout)
    except (OSError, subprocess.TimeoutExpired, ValueError):
        raise Unreadable(f"{env_dir}/{REGISTRY} could not be checked: run go run ./cmd/accounts -check "
                         f"-registry {REGISTRY} -keystore {KEYSTORE} in {env_dir} to see why")


def match(doc, bot):
    """The accounts listing this bot, and those listing its currency at all."""
    currency, config = bot
    exact, same_currency = [], []
    for a in doc.get("accounts") or []:
        for b in a.get("bots") or []:
            if b.get("currency") == currency:
                same_currency.append(a)
                if b.get("config") == config:
                    exact.append(a)
    return exact, same_currency


def accounts_of(env_dir, doc):
    """The account ids the folder's bots map to (each bot on one account)."""
    ids = set()
    for bot in env_bots(env_dir):
        exact, _ = match(doc, bot)
        if len(exact) == 1:
            ids.add(exact[0]["account_id"])
    return ids


def ids(accounts):
    return ", ".join(sorted(a["account_id"] for a in accounts))


def check(env_dir, platform_dirs):
    """Why the environment may not start ("" when it may)."""
    if not os.path.isfile(os.path.join(env_dir, REGISTRY)):
        return ""
    doc = status(env_dir)
    if doc.get("violations"):
        breaks = "; ".join(v["rule"] + (f" (account {v['account_id']})" if v.get("account_id") else "")
                           for v in doc["violations"])
        return f"{env_dir}/{REGISTRY} breaks a rule: {breaks} (go run ./cmd/accounts -check names each)"
    for currency, config in env_bots(env_dir):
        exact, same_currency = match(doc, (currency, config))
        bot = f"the {currency} bot ({config})"
        if len(exact) > 1:
            return (f"{env_dir}: {bot} is listed under accounts {ids(exact)} in {REGISTRY}: "
                    "each environment's bot belongs to one account")
        if not exact:
            if same_currency:
                return (f"{env_dir}: {bot} is not in {REGISTRY}, which lists {currency} under account "
                        f"{ids(same_currency)}: add this bot to its own account")
            print(f"{env_dir}: {bot} is not in {REGISTRY}", file=sys.stderr)
            continue
        acct = exact[0]
        state = acct.get("key_status")
        if state != "ok":
            words = KEY_WORDS.get(state, f"its key status is {state}")
            return f"{env_dir}: {bot} runs on account {acct['account_id']}, and {words}"
    mine = accounts_of(env_dir, doc)
    for other in platform_dirs:
        if os.path.realpath(other) == os.path.realpath(env_dir):
            continue
        if not os.path.isfile(os.path.join(other, REGISTRY)):
            continue
        try:
            odoc = status(other)
        except Unreadable as e:
            print(f"{e} (its own env_check refuses it)", file=sys.stderr)
            continue
        shared = mine & accounts_of(other, odoc)
        if shared:
            return (f"{env_dir} and {other} both run on registry account {', '.join(sorted(shared))}: "
                    "each environment needs its own account")
    return ""


def main(argv):
    if len(argv) < 2:
        print("usage: env_registry.py <env dir> <platform dir>...")
        return 2
    try:
        why = check(argv[1], argv[2:])
    except Unreadable as e:
        why = str(e)
    if why:
        print(why)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
