"""One sentence per line for Markdown prose (#463), and the "nothing lost" check.

    python3 -I scripts/docs/reflow.py write FILE...        reflow the files in place
    python3 -I scripts/docs/reflow.py compare REF FILE...  list every sentence of REF:FILE missing from FILE

Prose is everything outside code fences, tables, headings, HTML and blank lines.
A sentence ends at . ! or ? followed by a space and a capital; never inside `code`,
[link text](url) or after an abbreviation. Only lines with more than one sentence change,
so the rendered text is the same (a soft line break renders as a space).
"""
import re
import subprocess
import sys
from collections import Counter

ABBREVIATIONS = ("e.g.", "i.e.", "vs.", "etc.", "approx.", "cf.", "incl.", "No.", "Fig.", "St.")
PREFIX = re.compile(r"^((?:\s*>)*\s*(?:(?:[-*+]|\d+[.)])\s+)?)")
NEXT_SENTENCE = re.compile(r"""[*_"'(\[]*([A-Z]|`)""")


def is_prose(line):
    s = line.strip()
    return bool(s) and not s.startswith(("|", "#", "<", "```", "---", "===")) and not re.fullmatch(r"[-*_ ]{3,}", s)


def split_prefix(line):
    """The list/quote marker of a line and the text after it."""
    prefix = PREFIX.match(line).group(1)
    return prefix, line[len(prefix):]


def continuation_indent(prefix):
    """What starts the following sentences of a line: quotes repeated, a list marker as spaces."""
    quotes = re.match(r"^((?:\s*>)*\s*)", prefix).group(1)
    return quotes + " " * (len(prefix) - len(quotes))


def split_sentences(text):
    """Splits text into sentences, keeping code spans and link texts whole."""
    out, start, in_code, depth = [], 0, False, 0
    for i, ch in enumerate(text):
        if ch == "`":
            in_code = not in_code
        elif in_code:
            continue
        elif ch in "[(":
            depth += 1
        elif ch in "])":
            depth = max(0, depth - 1)
        elif ch in ".!?" and depth == 0 and _ends_sentence(text, i):
            out.append(text[start:i + 1].strip())
            start = i + 1
    rest = text[start:].strip()
    if rest:
        out.append(rest)
    return out


def _ends_sentence(text, i):
    """True when the mark at text[i] closes a sentence: a space and a capital follow."""
    after = text[i + 1:]
    if not after.startswith(" "):
        return False
    if not NEXT_SENTENCE.match(after.lstrip()):
        return False
    word = text[:i + 1].split(" ")[-1]
    return not (word.endswith(ABBREVIATIONS) or re.fullmatch(r"[A-Z]\.", word))


def reflow(text):
    lines, out, in_code = text.split("\n"), [], False
    for line in lines:
        if line.strip().startswith("```"):
            in_code = not in_code
            out.append(line)
            continue
        if in_code or not is_prose(line):
            out.append(line)
            continue
        prefix, body = split_prefix(line)
        sentences = split_sentences(body)
        if len(sentences) < 2:
            out.append(line)
            continue
        indent = continuation_indent(prefix)
        out.append(prefix + sentences[0])
        out.extend(indent + s for s in sentences[1:])
    return "\n".join(out)


def _normal(s):
    return " ".join(s.split())


def sentences_and_lines(text):
    """The prose sentences of a text (paragraphs joined, so line breaks don't matter)
    and its other lines (headings, tables, code), as two counters."""
    sentences, other = Counter(), Counter()
    paragraph, in_code = [], False

    def flush():
        if paragraph:
            for s in split_sentences(" ".join(paragraph)):
                sentences[_normal(s)] += 1
            paragraph.clear()

    for line in text.split("\n"):
        if line.strip().startswith("```"):
            flush()
            in_code = not in_code
            other[line.rstrip()] += 1
        elif in_code or not is_prose(line):
            flush()
            if line.strip():
                other[line.rstrip()] += 1
        else:
            prefix, body = split_prefix(line)
            if prefix.strip():
                flush()
            paragraph.append(body.strip())
    flush()
    return sentences, other


def missing(old, new_texts):
    """Sentences and other lines of old that no text of new_texts has (empty = nothing lost)."""
    old_s, old_o = sentences_and_lines(old)
    new_s, new_o = Counter(), Counter()
    for t in new_texts:
        s, o = sentences_and_lines(t)
        new_s.update(s)
        new_o.update(o)
    lost = [s for s in (old_s - new_s).elements()]
    lost += [l for l in (old_o - new_o).elements()]
    return lost


def main(argv):
    if len(argv) >= 3 and argv[1] == "write":
        for path in argv[2:]:
            text = open(path, encoding="utf-8").read()
            with open(path, "w", encoding="utf-8") as f:
                f.write(reflow(text))
        return 0
    if len(argv) >= 4 and argv[1] == "compare":
        ref, status = argv[2], 0
        for path in argv[3:]:
            old = subprocess.check_output(["git", "show", f"{ref}:{path}"], text=True)
            lost = missing(old, [open(path, encoding="utf-8").read()])
            print(f"{path}: {len(lost)} missing")
            for s in lost:
                print("  -", s)
            status |= bool(lost)
        return status
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
