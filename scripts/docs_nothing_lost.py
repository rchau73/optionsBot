#!/usr/bin/env python3
"""Lists every sentence of an old markdown file that is missing from the new files (#462).

Splitting one big document into several must move content, not lose it.
The old text and the new files are cut into the same units — a heading, a
table cell, a list item or a sentence of a paragraph — and every old unit must
be a unit of some new file (a new unit may only add a lead-in such as "Default: ").

    python3 scripts/docs_nothing_lost.py HEAD:frontend/README.md frontend/README.md frontend/docs/*.md

The first argument is a file or a `git show` spec (`<rev>:<path>`).
Exit 0 and no output when nothing is missing; otherwise one missing sentence per line.
With --links it also checks that every relative link and `#anchor` of the new files resolves.
"""
import re
import subprocess
import sys
from pathlib import Path

# A sentence ends at . ! ? followed by a space and a capital, a quote, a backtick or a bracket;
# "e.g." "i.e." and "vs." do not end one.
SENTENCE_END = re.compile(r"(?<!e\.g\.)(?<!i\.e\.)(?<!vs\.)(?<=[.!?])\s+(?=[A-Z*`\[\"“])")
_CELL_SPLIT = re.compile(r"(?<!\\)\|")
_LINK = re.compile(r"\]\(([^)\s]+)\)")
_LIST_MARK = re.compile(r"^(?:[-*]|\d+\.)\s+")


def split_sentences(text):
    return [s.strip() for s in SENTENCE_END.split(text) if s.strip()]


def table_cells(line, ncols=None):
    """The cells of a table row, with the `\\|` of a cell turned back into `|`.

    A row with more cells than the header (an unescaped `|` in the text) keeps the surplus in its last cell.
    """
    inner = line.strip()[1:]
    if inner.endswith("|") and not inner.endswith("\\|"):
        inner = inner[:-1]
    cells = _CELL_SPLIT.split(inner)
    if ncols and len(cells) > ncols:
        cells = cells[: ncols - 1] + ["|".join(cells[ncols - 1 :])]
    return [c.strip().replace("\\|", "|") for c in cells]


def _link_key(match):
    """A link counts by its anchor, else by its file name: moving a file changes the path, not the link."""
    target = match.group(1)
    key = target.split("#", 1)[1] if "#" in target else target.rstrip("/").rsplit("/", 1)[-1]
    return f"]({key})"


def normalise(text):
    text = _LINK.sub(_link_key, text.replace("\\|", "|"))
    return re.sub(r"\s+", " ", text.replace("*", "")).strip()


def units(markdown):
    """Every heading, table cell, code line and sentence of a markdown text, normalised."""
    out = []
    in_code = False
    ncols = None
    for raw in markdown.splitlines():
        line = raw.strip()
        if not line.startswith("|"):
            ncols = None
        if line.startswith("```"):
            in_code = not in_code
            continue
        if in_code:
            parts = [line]
        elif line.startswith("|"):
            if re.fullmatch(r"\|[\s:|-]+\|?", line):
                continue
            cells = table_cells(line, ncols)
            if ncols is None:
                # The header row: its words become the labels of each row's lines, not content.
                ncols = len(cells)
                continue
            parts = [s for cell in cells for s in split_sentences(cell)]
        else:
            line = re.sub(r"^#+\s+", "", line)
            line = re.sub(r"^>\s?", "", line)
            line = _LIST_MARK.sub("", line)
            parts = split_sentences(line)
        out.extend(n for n in (normalise(p) for p in parts) if re.search(r"[A-Za-z0-9]", n))
    return out


def missing(old_markdown, new_markdowns):
    """The old units that are not (the end of) a unit of the new texts, in order, once each."""
    new_units = [u for text in new_markdowns for u in units(text)]
    exact = set(new_units)
    lost = []
    for unit in units(old_markdown):
        if unit in exact or unit in lost:
            continue
        if any(n.endswith(unit) for n in new_units):
            continue
        lost.append(unit)
    return lost


def slug(heading):
    """GitHub's anchor of a heading."""
    text = re.sub(r"[^\w\s-]", "", heading.strip().lower())
    return text.replace(" ", "-")


def anchors(markdown):
    out, in_code = set(), False
    for line in markdown.splitlines():
        if line.startswith("```"):
            in_code = not in_code
        elif not in_code and (m := re.match(r"#{1,6}\s+(.+)", line)):
            out.add(slug(m.group(1)))
    return out


def broken_links(path):
    """`file: target` for each relative link of a markdown file whose file or anchor does not exist."""
    path = Path(path)
    text = path.read_text(encoding="utf-8")
    out = []
    in_code = False
    for line in text.splitlines():
        if line.startswith("```"):
            in_code = not in_code
            continue
        if in_code:
            continue
        for m in _LINK.finditer(line):
            target = m.group(1)
            if re.match(r"[a-z]+:", target):
                continue
            file_part, _, anchor = target.partition("#")
            dest = path if not file_part else (path.parent / file_part)
            if not dest.exists():
                out.append(f"{path}: {target} (no such file)")
            elif anchor and dest.suffix == ".md" and anchor not in anchors(dest.read_text(encoding="utf-8")):
                out.append(f"{path}: {target} (no such heading)")
    return out


def read_old(spec):
    if Path(spec).exists():
        return Path(spec).read_text(encoding="utf-8")
    return subprocess.run(["git", "show", spec], check=True, capture_output=True, text=True).stdout


def main(argv):
    args = [a for a in argv if a != "--links"]
    if len(args) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    lost = missing(read_old(args[0]), [Path(p).read_text(encoding="utf-8") for p in args[1:]])
    problems = list(lost)
    if "--links" in argv:
        problems += [b for p in args[1:] for b in broken_links(p)]
    for p in problems:
        print(p)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
