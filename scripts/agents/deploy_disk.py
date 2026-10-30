#!/usr/bin/env python3
"""deploy_disk.py — the Docker disk warning deploy.sh raises after a healthy
deploy (#548: the VM disk filled with old images and build cache, and every
monitor write failed with ENOSPC). Pure text here; deploy.sh runs docker and gh.

    deploy_disk.py check < df.txt                 → the use % when over WARN_PCT, else nothing
    deploy_disk.py find < issues.json             → number of an open disk warning, or nothing
    deploy_disk.py title <pct>                    → the issue title
    deploy_disk.py body <pct> <df.txt> <sysdf.txt> → the issue body (markdown)

df.txt: `df -P` of the monitor's data volume, which lives on the Docker disk.
issues.json: gh issue list --json number,title. sysdf.txt: `docker system df`.

Standard library only.
"""
import json
import sys

from deploy_issue import fence  # a code fence longer than any backtick run

WARN_PCT = 85  # warn above this: at 100 % the monitor can no longer write its volume
TITLE = "[E6] Docker disk over"


def use_pct(df_text):
    """The use % on the last data line of `df -P` output, or None."""
    for line in reversed(df_text.strip().splitlines()[1:]):
        for field in line.split():
            if field.endswith("%") and field[:-1].isdigit():
                return int(field[:-1])
    return None


def over(pct, limit=WARN_PCT):
    return pct is not None and pct > limit


def title(pct):
    return f"{TITLE} {WARN_PCT} % full ({pct} %)"


def find(issues):
    """Number of an open issue that already warns about the disk."""
    for i in issues:
        if i.get("title", "").startswith(TITLE):
            return i["number"]
    return None


def body(pct, df_text, sysdf_text):
    df_text = df_text.strip() or "(empty)"
    sysdf_text = sysdf_text.strip() or "(empty)"
    f, g = fence(df_text), fence(sysdf_text)
    return f"""🚀 **Deploy** — the Docker disk is **{pct} %** full (warning above {WARN_PCT} %).

- At 100 % each monitor's own volume (sign-ins, assistant log, analytics, surveys) can no longer be written (#548: ENOSPC).
- `deploy.sh` already removed untagged images and build cache older than 72 h after this deploy; what is left is in use or newer.
- Look at `docker system df -v`: unused volumes are never pruned automatically (a monitor's data lives in one), remove one only by hand once you know whose it is.
- Or give the Docker VM a bigger disk (Docker Desktop → Settings → Resources).

### Monitor volume (`df -P`)
{f}text
{df_text}
{f}

### `docker system df`
{g}text
{sysdf_text}
{g}

Filed by `scripts/agents/deploy.sh` (#548); one open warning at a time.
"""


def _read(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            return f.read()
    except OSError:
        return ""


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "check" and len(argv) == 2:
        pct = use_pct(sys.stdin.read())
        if over(pct):
            print(pct)
    elif cmd == "find" and len(argv) == 2:
        n = find(json.load(sys.stdin))
        if n is not None:
            print(n)
    elif cmd == "title" and len(argv) == 3:
        print(title(argv[2]))
    elif cmd == "body" and len(argv) == 5:
        sys.stdout.write(body(argv[2], _read(argv[3]), _read(argv[4])))
    else:
        print(__doc__, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
