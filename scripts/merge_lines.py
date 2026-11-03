"""merge_lines.py — settles a merge conflict in a sorted JSON-lines list (#596):
one record per line, kept in ascending order of its "id" (golden.jsonl,
tests/sorted_lists_test.go). Two branches that each add a record conflict
whenever their records land next to each other; both belong in the result.

    python3 scripts/merge_lines.py <path>    # during a merge, from the repository root

Reads the three sides from the index (base :1, ours :2, theirs :3). A line
stays when both sides kept it or one side added it; a line one side removed
is gone. The result is written sorted by id and staged. Exits 1, leaving the
file as it is, when two different records share an id (both sides changed
or added the same one: a person's or the Dev agent's call). Used by
scripts/regen.sh --resolve.
"""
import json
import subprocess
import sys


def side(stage, path):
    r = subprocess.run(["git", "show", f":{stage}:{path}"], capture_output=True, text=True)
    return [l for l in r.stdout.splitlines() if l.strip()] if r.returncode == 0 else []


def merge(base, ours, theirs):
    """The merged lines, sorted by id, or raises ValueError naming a shared id."""
    base, theirs_set, ours_set = set(base), set(theirs), set(ours)
    kept = [l for l in ours if l in theirs_set or l not in base]
    kept += [l for l in theirs if l not in ours_set and l not in base]
    by_id = {}
    for line in kept:
        rid = json.loads(line)["id"]
        if rid in by_id and by_id[rid] != line:
            raise ValueError(f'id "{rid}" is on two different lines')
        by_id[rid] = line
    return [by_id[k] for k in sorted(by_id)]


def main(path):
    try:
        lines = merge(side(1, path), side(2, path), side(3, path))
    except (ValueError, KeyError, json.JSONDecodeError) as e:
        print(f"merge_lines: {path}: {e}: resolve it by hand", file=sys.stderr)
        return 1
    with open(path, "w") as f:
        f.write("".join(l + "\n" for l in lines))
    subprocess.run(["git", "add", "--", path], check=True)
    print(f"merge_lines: {path}: {len(lines)} records, both sides kept")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
