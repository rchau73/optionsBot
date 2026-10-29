"""release.py — the rules of the automatic release (#471), pure and tested
(test_release.py); release.sh gathers the inputs and runs promote.sh.

  release.py week [--now ISO]      → the ISO week, e.g. 2026-W44
  release.py decide --deploys F --promotions F --tags F --prs F --hotfix-issues F
                    --not-fixed F --git DIR [--weekly] [--now ISO]
                    [--soak-days 3] [--hotfix-hours 1]
                                   → one JSON object (decide's result)

A release candidate is always a commit with its own healthy `mvp` deploy line
in deploys.tsv (deploy.sh writes one only after the health check; a
`rollback` line re-runs an older build and is never a candidate) that stable
does not contain yet:
  - weekly (once a week, the first tick of a Monday UTC): the newest such
    deploy at least soak-days old — the build promote.sh would pick;
  - hotfix (any tick): a merged PR labelled, or closing an issue labelled,
    both `bug` and `release:hotfix` — the first mvp deploy containing its
    merge commit, once that has been live hotfix-hours.
The newer of the two goes; an open sme:not-fixed issue blocks both.
"""
import argparse
import datetime as dt
import json
import re
import subprocess
import sys

FMT = "%Y-%m-%dT%H:%M:%SZ"
HOTFIX_LABELS = {"bug", "release:hotfix"}
CLOSES = re.compile(r"\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+#(\d+)", re.I)
TAG = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")


def iso_week(now):
    y, w, _ = now.date().isocalendar()
    return f"{y}-W{w:02d}"


def parse_time(s):
    return dt.datetime.strptime(s, FMT).replace(tzinfo=dt.timezone.utc)


def mvp_deploys(text):
    """deploys.tsv → [(time, commit)] of the healthy mvp deploys, oldest first."""
    out = []
    for line in text.splitlines():
        p = line.rstrip("\n").split("\t")
        if len(p) < 3 or p[1] != "mvp" or (len(p) > 3 and p[3] != ""):
            continue
        try:
            out.append((parse_time(p[0]), p[2]))
        except ValueError:
            continue
    return sorted(out)


def stable_release(text):
    """promotions.tsv → (tag, commit) of stable's last promotion, or None."""
    rows = [p for p in (line.split("\t") for line in text.splitlines()) if len(p) >= 3]
    return (rows[-1][1], rows[-1][2].strip()) if rows else None


def next_patch(tags):
    """The newest vMAJOR.MINOR.PATCH with PATCH + 1, or None without a tag."""
    vs = sorted(tuple(int(x) for x in m.groups()) for m in (TAG.match(t.strip()) for t in tags) if m)
    if not vs:
        return None
    a, b, c = vs[-1]
    return f"v{a}.{b}.{c + 1}"


def hotfix_prs(prs, hotfix_issues):
    """[(PR number, merge commit)] of the merged PRs that are hotfixes: both
    labels on the PR, or "Closes #n" naming an issue with both labels."""
    issues = {i["number"] for i in hotfix_issues if HOTFIX_LABELS <= {l["name"] for l in i.get("labels", [])}}
    out = []
    for pr in prs:
        oid = (pr.get("mergeCommit") or {}).get("oid")
        if not oid:
            continue
        labels = {l["name"] for l in pr.get("labels", [])}
        closes = {int(n) for n in CLOSES.findall(f"{pr.get('title', '')}\n{pr.get('body') or ''}")}
        if HOTFIX_LABELS <= labels or closes & issues:
            out.append((pr["number"], oid))
    return sorted(out)


def _at(t):
    return f"{t:%Y-%m-%d %H:%M} UTC"


def decide(deploys, stable, tags, not_fixed, weekly, hotfixes, now, contains, soak_days=3.0, hotfix_hours=1.0):
    """What to release now.

    deploys: mvp_deploys(); stable: stable_release(); tags: every tag;
    not_fixed: the open sme:not-fixed issues ("#n title"); weekly: the weekly
    check is due; hotfixes: hotfix_prs(); contains(a, b): commit b contains a.
    → {"action": "release" | "none", "trigger", "commit", "tag", "ref", "note"}.
    """
    none = lambda note: {"action": "none", "trigger": "", "commit": "", "tag": "", "ref": "", "note": note}
    if stable is None:
        return none("no stable release yet: the first promotion is a person's (promote.sh --first)")
    stable_tag, stable_commit = stable
    tag = next_patch(tags)
    if tag is None:
        return none("no release tag yet: the first promotion is a person's (promote.sh --first)")
    new = [(t, c) for t, c in deploys if not contains(c, stable_commit)]

    ready, waiting = [], []  # hotfixes: (deploy time, commit, PR) / why each waits
    for pr, merge in hotfixes:
        if contains(merge, stable_commit):
            continue  # already released
        first = next(((t, c) for t, c in deploys if contains(merge, c)), None)
        if first is None:
            waiting.append(f"hotfix #{pr} has not run in mvp yet")
        elif now - first[0] < dt.timedelta(hours=hotfix_hours):
            waiting.append(f"hotfix #{pr} live on mvp since {_at(first[0])}, may go at "
                           f"{_at(first[0] + dt.timedelta(hours=hotfix_hours))}")
        else:
            ready.append((first[0], first[1], pr))
    soaked = [(t, c) for t, c in new if now - t >= dt.timedelta(days=soak_days)] if weekly else []

    if not ready and not weekly:
        return none("; ".join(waiting))
    if not_fixed:
        return none("blocked: open issues the SME agent found not fixed: " + ", ".join(not_fixed))
    hot = max(ready) if ready else None
    if hot and (not soaked or hot[0] > soaked[-1][0]):
        refs = sorted(pr for t, c, pr in ready if t <= hot[0])
        return {"action": "release", "trigger": "hotfix", "commit": hot[1], "tag": tag,
                "ref": ", ".join(f"#{n}" for n in refs), "note": f"hotfix {', '.join(f'#{n}' for n in refs)}"}
    if soaked:
        t, c = soaked[-1]
        return {"action": "release", "trigger": "weekly", "commit": c, "tag": tag, "ref": "",
                "note": f"newest mvp build soaked {soak_days:g} days: {c[:7]}, deployed {_at(t)}"}
    if not new:
        why = f"nothing new on mvp since {stable_tag}"
    else:
        t, c = new[-1]
        why = (f"nothing soaked {soak_days:g} days on mvp since {stable_tag}: the oldest new build "
               f"({new[0][1][:7]}, {_at(new[0][0])}) soaks at {_at(new[0][0] + dt.timedelta(days=soak_days))}")
    return none("; ".join([why + ", no hotfix ready"] + waiting))


def git_contains(repo):
    def contains(a, b):
        return subprocess.run(["git", "-C", repo, "merge-base", "--is-ancestor", a, b],
                              capture_output=True).returncode == 0
    return contains


def read(path, default=""):
    try:
        with open(path) as f:
            return f.read()
    except FileNotFoundError:
        return default


def main(argv):
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["week", "decide"])
    ap.add_argument("--now")
    for f in ("deploys", "promotions", "tags", "prs", "hotfix-issues", "not-fixed", "git"):
        ap.add_argument("--" + f)
    ap.add_argument("--weekly", action="store_true")
    ap.add_argument("--soak-days", type=float, default=3.0)
    ap.add_argument("--hotfix-hours", type=float, default=1.0)
    a = ap.parse_args(argv)
    now = parse_time(a.now) if a.now else dt.datetime.now(dt.timezone.utc)
    if a.cmd == "week":
        print(iso_week(now))
        return 0
    hot = hotfix_prs(json.loads(read(a.prs, "[]") or "[]"), json.loads(read(a.hotfix_issues, "[]") or "[]"))
    out = decide(mvp_deploys(read(a.deploys)), stable_release(read(a.promotions)), read(a.tags).split(),
                 [l for l in read(a.not_fixed).splitlines() if l.strip()], a.weekly, hot, now,
                 git_contains(a.git), a.soak_days, a.hotfix_hours)
    print(json.dumps(out))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
