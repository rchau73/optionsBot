#!/usr/bin/env python3
"""Usability round scorer for epic E3 (#701): a results table in, pass/fail, errors and a round comparison out.

Reads the results table of docs/usability/e3-investor-monitor-test.md section 8, copied to
data/usability/e3/<round>/results.md (gitignored, private), and applies section 7: a participant
passes with four successes and the fourth answer before 5:00, a round at 4 of 5. Prints a summary and
writes it, with the synthesis skeleton, to summary.md beside the table; an existing summary.md may hold
a filled-in synthesis, so it stays unless --force. With two rounds it also prints the change in passes
and in each error type, and writes comparison.md beside the later round.

Participant codes only: every cell must be a value the template allows and every line outside the table
a line of the template, so an e-mail, a phone number or a name stops the run. The refusal names the line
number, never its content, and nothing is written. Exit 0 when scored (a failed round too), 2 when refused.

    python3 scripts/product/score_usability.py data/usability/e3/baseline/results.md
    python3 scripts/product/score_usability.py data/usability/e3/baseline/results.md data/usability/e3/loss/results.md
"""
import argparse
import os
import re
import statistics
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PROTOCOL = os.path.join(REPO, "docs", "usability", "e3-investor-monitor-test.md")

# Section 7's pass mark, quoted (changing it is a change to the protocol, not to this script).
ROUND_PASS_MARK = 4         # participants of 5 who must pass
Q4_LIMIT_SEC = 5 * 60       # the fourth answer must come before 5:00

CODES = ("P1", "P2", "P3", "P4", "P5")
QUESTIONS = ("Q1", "Q2", "Q3", "Q4")
ERROR_TYPES = ("sign", "unit", "coin", "time")
LOSS_SCENARIOS = ("mixed", "drawdown")
SUCCESS = "success"

# Section 8's table, one entry per column: its title and, after the code, the values its cells may hold.
SCORE_CELL = "(success|partial|fail)?"
TIME_CELL = r"(\d{1,2}:[0-5]\d|-|—)?"
ERROR = "q[1-4] (sign|unit|coin|time)"
COLUMNS = ["code", "layout", "scenario (default / mixed / drawdown)",
           "q1", "q1 time", "q2", "q2 time", "q3", "q3 time", "q4", "q4 time",
           "errors (sign / unit / coin / time)", "passed"]
CELLS = [re.compile(p) for p in (
    "(desktop|mobile)?", "(default|mixed|drawdown)?",
    SCORE_CELL, TIME_CELL, SCORE_CELL, TIME_CELL, SCORE_CELL, TIME_CELL, SCORE_CELL, TIME_CELL,
    rf"(none|-|—|{ERROR}(\s*[,;]\s*{ERROR})*)?", "(yes|no)?")]

# The only lines allowed outside the table: section 8's, blank or filled in.
TEMPLATE_LINES = [re.compile(p) for p in (
    r"", r"### Results table",
    r"Round: (baseline / prototype / release / loss round|baseline|prototype|release|loss round)"
    r" · Date \(?(YYYY-MM-DD|\d{4}-\d{2}-\d{2})\)? · Monitor version \(?(commit|[0-9a-f]{7,40})\)?",
    r"Participants passed: (_|\d) of 5 · Round passed \(≥ 4 of 5\): (yes / no|yes|no)")]

EMAIL = re.compile(r"[\w.+-]+@[\w-]+")
ISO_DATE = re.compile(r"(?<!\d)\d{4}-\d{2}-\d{2}(?!\d)")
PHONE = re.compile(r"(?<![0-9A-Za-z])\+?\(?\d[\d\s().-]{6,}\d(?![0-9A-Za-z])")
PHONE_MIN_DIGITS = 8
TABLE_RULE = re.compile(r"\|(\s*:?-+:?\s*\|)+")
SYNTHESIS = re.compile(r"### Synthesis\n\n(.*?)\n\n<!-- templates: end -->", re.S)

IS_EMAIL, IS_PHONE = "e-mail address", "phone number"
NAME_LIKE = "name-like field (not a value the template allows)"
NOT_SCORED = "not scored (a layout, scenario or score is empty, or a success has no time)"


class Refused(Exception):
    """A table that cannot be scored. problems: (line number or None, reason); no reason quotes the file."""

    def __init__(self, problems):
        super().__init__(f"{len(problems)} problem(s)")
        self.problems = problems


# --- reading the table --------------------------------------------------------

def personal_data(line):
    """What personal data the line looks like holding, or None. Dates are not phone numbers."""
    if EMAIL.search(line):
        return IS_EMAIL
    for m in PHONE.finditer(ISO_DATE.sub(" ", line)):
        if sum(c.isdigit() for c in m.group()) >= PHONE_MIN_DIGITS:
            return IS_PHONE
    return None


def seconds(cell):
    """A mm:ss cell in seconds, None when empty."""
    if ":" not in cell:
        return None
    minutes, secs = cell.split(":")
    return int(minutes) * 60 + int(secs)


def parse_errors(cell):
    """The cell's errors as (question, type), one per error counted; Q2 sign, Q2 unit gives two."""
    return [(m[0].upper(), m[1]) for m in re.findall(r"(q[1-4]) (sign|unit|coin|time)", cell)]


def parse_row(cells):
    """(participant, None) or (None, why the row is refused)."""
    if len(cells) != len(COLUMNS) or cells[0] not in CODES:
        return None, NAME_LIKE
    fields = [c.lower() for c in cells[1:]]
    if not all(p.fullmatch(c) for p, c in zip(CELLS, fields)):
        return None, NAME_LIKE
    layout, scenario, *answers, errors, passed = fields
    scores = dict(zip(QUESTIONS, answers[0::2]))
    times = dict(zip(QUESTIONS, map(seconds, answers[1::2])))
    unscored = not layout or not scenario or not all(scores.values())
    if unscored or any(s == SUCCESS and times[q] is None for q, s in scores.items()):
        return None, NOT_SCORED
    return {"code": cells[0], "scenario": scenario, "scores": scores, "times": times,
            "errors": parse_errors(errors), "table_passed": passed}, None


def parse_results(text):
    """The five participants, in code order. Raises Refused naming every line that stops the scoring."""
    problems, found, header_seen = [], {}, False
    for no, raw in enumerate(text.splitlines(), 1):
        line = raw.strip()
        why = personal_data(line)
        if why:
            problems.append((no, why))
        elif not line.startswith("|"):
            if not any(p.fullmatch(line) for p in TEMPLATE_LINES):
                problems.append((no, NAME_LIKE))
        elif not TABLE_RULE.fullmatch(line):
            cells = [c.strip() for c in line.strip("|").split("|")]
            if not header_seen:
                header_seen = [c.lower() for c in cells] == COLUMNS
                if not header_seen:
                    problems.append((no, NAME_LIKE))
                continue
            participant, why = parse_row(cells)
            if why or participant["code"] in found:
                problems.append((no, why or "the same participant code twice"))
            else:
                found[participant["code"]] = participant
    if not problems:
        problems = [(None, f"{code} has no row") for code in CODES if code not in found]
    if problems:
        raise Refused(problems)
    return [found[code] for code in CODES]


# --- scoring ------------------------------------------------------------------

def participant_passes(p):
    """Section 7: all four questions a success, the fourth answer before 5:00."""
    in_time = p["times"]["Q4"] is not None and p["times"]["Q4"] < Q4_LIMIT_SEC
    return in_time and all(s == SUCCESS for s in p["scores"].values())


def question_misses(participants, question):
    """Why each participant lost the question: the score, or a Q4 success that came too late."""
    out = []
    for p in participants:
        if p["scores"][question] != SUCCESS:
            out.append(f"{p['code']} {p['scores'][question]}")
        elif question == "Q4" and p["times"]["Q4"] >= Q4_LIMIT_SEC:
            out.append(f"{p['code']} at or after 5:00")
    return out


def error_counts(participants):
    """Errors per type and question, every type and question present."""
    counts = {t: dict.fromkeys(QUESTIONS, 0) for t in ERROR_TYPES}
    for p in participants:
        for question, kind in p["errors"]:
            counts[kind][question] += 1
    return counts


def loss_sign_errors(participants):
    """(question, code) of each sign error made on a loss scenario, the loss round's main finding."""
    return [(question, p["code"]) for p in participants if p["scenario"] in LOSS_SCENARIOS
            for question, kind in p["errors"] if kind == "sign"]


def score_round(name, participants):
    """The round's score, from the parsed participants."""
    passed = [p["code"] for p in participants if participant_passes(p)]
    q4_times = [p["times"]["Q4"] for p in participants if p["times"]["Q4"] is not None]
    misses = {q: question_misses(participants, q) for q in QUESTIONS}
    return {"round": name, "total": len(participants), "passed": passed,
            "round_passed": len(passed) >= ROUND_PASS_MARK,
            "not_passed": [p["code"] for p in participants if p["code"] not in passed],
            "success": {q: sum(p["scores"][q] == SUCCESS for p in participants) for q in QUESTIONS},
            "failing": {q: m for q, m in misses.items() if m},
            "errors": error_counts(participants),
            "has_loss_scenario": any(p["scenario"] in LOSS_SCENARIOS for p in participants),
            "loss_sign": loss_sign_errors(participants),
            "median_q4_sec": int(statistics.median(q4_times)) if q4_times else None,
            "table_differs": [p["code"] for p in participants if p["table_passed"]
                              and (p["table_passed"] == "yes") != participant_passes(p)]}


def compare_rounds(earlier, later):
    """Passes and each error type side by side: (what, earlier count, later count)."""
    def total(score, kind):
        return sum(score["errors"][kind].values())
    return ([("Participants passed", len(earlier["passed"]), len(later["passed"])),
             ("Sign errors in a loss scenario", len(earlier["loss_sign"]), len(later["loss_sign"]))]
            + [(f"{kind} errors", total(earlier, kind), total(later, kind)) for kind in ERROR_TYPES])


# --- output -------------------------------------------------------------------

def fmt_time(sec):
    return "—" if sec is None else f"{sec // 60:02d}:{sec % 60:02d}"


def result_line(score):
    """The synthesis' Result line, filled in."""
    per_question = ", ".join(f"{q} {score['success'][q]}/{score['total']}" for q in QUESTIONS)
    return (f"- **Result:** {len(score['passed'])} of {score['total']} passed; success per question: {per_question}; "
            f"median time to the fourth answer: {fmt_time(score['median_q4_sec'])}.")


def fill_synthesis(skeleton, score):
    return re.sub(r"^- \*\*Result:\*\*.*$", lambda _: result_line(score), skeleton, count=1, flags=re.M)


def render_errors(score):
    """A sign error on a loss scenario comes first: section 6 calls it the main thing to watch."""
    out = []
    if score["has_loss_scenario"]:
        where = ", ".join(f"{q} {code}" for q, code in score["loss_sign"]) or "none"
        out += [f"**Sign errors in a loss scenario: {len(score['loss_sign'])}** ({where})", ""]
    out += ["| Error | " + " | ".join(QUESTIONS) + " | Total |", "|---|" + "---|" * (len(QUESTIONS) + 1)]
    for kind in ERROR_TYPES:
        counts = score["errors"][kind]
        cells = " | ".join(str(counts[q]) for q in QUESTIONS)
        out.append(f"| {kind} | {cells} | {sum(counts.values())} |")
    return out


def render_summary(score, skeleton):
    n, total = len(score["passed"]), score["total"]
    verdict = "passed" if score["round_passed"] else "failed"
    out = [f"# E3 usability round: {score['round']}", "",
           f"**The round {verdict}: {n} of {total} participants passed (at least {ROUND_PASS_MARK} needed).**", "",
           "- Passed: " + (", ".join(score["passed"]) or "none"),
           "- Not passed: " + (", ".join(score["not_passed"]) or "none")]
    if score["table_differs"]:
        out.append("- The table's Passed column differs from section 7's rule for: " + ", ".join(score["table_differs"]))
    out += ["", "## Failing questions", ""]
    out += [f"- {q}: {score['success'][q]} of {total} success; {', '.join(misses)}"
            for q, misses in score["failing"].items()] or ["None."]
    out += ["", "## Errors", ""] + render_errors(score)
    out += ["", "## Synthesis", "", fill_synthesis(skeleton, score), ""]
    return "\n".join(out)


def render_comparison(earlier, later):
    out = [f"# E3 usability rounds: {earlier['round']} and {later['round']}", "",
           f"| | {earlier['round']} | {later['round']} | Change |", "|---|---|---|---|"]
    for what, before, after in compare_rounds(earlier, later):
        out.append(f"| {what} | {before} | {after} | {after - before:+d} |")
    out.append("")
    return "\n".join(out)


def read_round(path):
    """The round's score; its name is the folder's (data/usability/e3/<round>/)."""
    # errors="replace": a stray byte is refused as a bad cell, never echoed by a traceback.
    with open(path, encoding="utf-8", errors="replace") as f:
        participants = parse_results(f.read())
    return score_round(os.path.basename(os.path.dirname(os.path.abspath(path))), participants)


def write_beside(results_path, name, text, overwrite):
    """Write name next to the table; an existing file stays unless overwrite. Returns what happened."""
    out = os.path.join(os.path.dirname(os.path.abspath(results_path)), name)
    if os.path.exists(out) and not overwrite:
        return f"kept {out}: it may hold your synthesis (--force replaces it)"
    with open(out, "w", encoding="utf-8") as f:
        f.write(text)
    return f"wrote {out}"


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("results", help="the round's results table (data/usability/e3/<round>/results.md)")
    ap.add_argument("later", nargs="?", help="a later round's table, to compare the two")
    ap.add_argument("--protocol", default=PROTOCOL, help="the usability kit, source of the synthesis skeleton")
    ap.add_argument("--force", action="store_true", help="replace an existing summary.md")
    args = ap.parse_args(argv)

    paths = [args.results] + ([args.later] if args.later else [])
    scores, refused = [], False
    for path in paths:
        try:
            scores.append(read_round(path))
        except Refused as e:
            refused = True
            for line, why in e.problems:
                print(f"{path}: {f'line {line}: ' if line else ''}refused: {why}", file=sys.stderr)
        except OSError as e:
            refused = True
            print(f"{path}: unreadable: {e.strerror}", file=sys.stderr)
    if refused:
        print("nothing written", file=sys.stderr)
        return 2
    try:
        with open(args.protocol, encoding="utf-8") as f:
            skeleton = SYNTHESIS.search(f.read()).group(1)
    except (OSError, AttributeError):
        print(f"{args.protocol}: no synthesis template; nothing written", file=sys.stderr)
        return 2

    notes = []
    for path, score in zip(paths, scores):
        summary = render_summary(score, skeleton)
        print(summary)
        notes.append(write_beside(path, "summary.md", summary, args.force))
    if len(scores) == 2:
        comparison = render_comparison(*scores)
        print(comparison)
        notes.append(write_beside(paths[1], "comparison.md", comparison, True))
    print("\n".join(notes))
    return 0


if __name__ == "__main__":
    sys.exit(main())
