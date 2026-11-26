#!/usr/bin/env python3
"""request_filter.py — the mechanical review of an assistant request before
any model reads it (#852, E10). Pure: no file, no network, no clock.

requests.py prepare runs `review` on every request waiting for a status, in
queue order; the first rule that holds decides, and its status line is written
with a plain-words note:
  1. duplicate  — same kind, and a title or `what` close to a card of the
                  board (open or closed) or to an earlier request;
  2. needs_info — no cited evidence and fewer than two steps to see it again;
  3. not_a_bug  — `explained_by` names a help passage that explains it;
otherwise None: the request goes on to the PM agent's review (requests.sh),
which may file it.

Standard library only.
"""
import difflib
import re

KINDS = ("bug", "enhancement")
# An earlier request with one of these statuses still stands: a new one close
# to it is a duplicate. One sent back (needs_info, not_a_bug) may be sent again.
STANDING = ("in_review", "filed", "fixed", "not_planned", "duplicate")
CLOSE_RATIO = 0.85  # difflib ratio of the normalised texts
CLOSE_WORDS = 0.8  # share of shared words (Jaccard), texts of 3 words or more
MIN_STEPS = 2

_PREFIX = re.compile(r"^\s*\[E\d{1,2}\]\s*")
_WORD = re.compile(r"[a-z0-9]+")


def norm(text):
    """Lower-case words of a title, without its "[E#]" prefix: "" for nothing."""
    return " ".join(_WORD.findall(_PREFIX.sub("", str(text or "")).lower()))


def close(a, b):
    """True when two titles say the same thing in nearly the same words."""
    na, nb = norm(a), norm(b)
    if not na or not nb:
        return False
    if na == nb or difflib.SequenceMatcher(None, na, nb).ratio() >= CLOSE_RATIO:
        return True
    wa, wb = set(na.split()), set(nb.split())
    return min(len(wa), len(wb)) >= 3 and len(wa & wb) / len(wa | wb) >= CLOSE_WORDS


def _fields(req):
    f = req.get("fields")
    return f if isinstance(f, dict) else {}


def texts(req):
    """The request's title and `what`, the texts a duplicate is matched on."""
    f = _fields(req)
    return [t for t in (f.get("title"), f.get("what")) if isinstance(t, str) and t.strip()]


def kind_of(req):
    return req.get("kind") if req.get("kind") in KINDS else "bug"


def _labels(card):
    return {(l.get("name") if isinstance(l, dict) else l) for l in card.get("labels") or []}


def duplicate_card(req, cards):
    """The first card of the same kind whose title is close to the request's title or what, else None."""
    mine = texts(req)
    for c in cards:
        if kind_of(req) in _labels(c) and any(close(t, c.get("title")) for t in mine):
            return c
    return None


def duplicate_request(req, earlier):
    """The first earlier request (dict) of the same kind close to this one, else None."""
    mine = texts(req)
    for e in earlier:
        if kind_of(e) == kind_of(req) and any(close(a, b) for a in mine for b in texts(e)):
            return e
    return None


def cited(req):
    """True when the request cites a record: an evidence item with its source, or a journal reference."""
    ev = _fields(req).get("evidence")
    items = ev if isinstance(ev, list) else []
    if any(isinstance(e, dict) and str(e.get("citation") or "").strip() for e in items):
        return True
    return any(isinstance(r, str) and r.strip() for r in req.get("journal_refs") or [])


def steps(req):
    s = _fields(req).get("steps")
    return [x for x in s if isinstance(x, str) and x.strip()] if isinstance(s, list) else []


def explained_by(req):
    """The help passages the assistant said explain it (the request's own, else its fields')."""
    v = req.get("explained_by")
    if not isinstance(v, list):
        v = _fields(req).get("explained_by")
    return [x for x in v if isinstance(x, str) and x.strip()] if isinstance(v, list) else []


def review(req, cards, earlier):
    """(status, note, issue number or None, earlier request id or None) of the
    first rule that holds, else None (the request goes on to the PM review).
    cards: issues {number, title, labels}; earlier: the requests before it
    that still stand (STANDING, or waiting in this run)."""
    c = duplicate_card(req, cards)
    if c is not None:
        return ("duplicate", f"Same as #{c.get('number')}: {str(c.get('title') or '')[:120]}", c.get("number"), None)
    e = duplicate_request(req, earlier)
    if e is not None:
        return ("duplicate", f"Same as the earlier request {e.get('id')}", None, e.get("id"))
    if not cited(req) and len(steps(req)) < MIN_STEPS:
        n = len(steps(req))
        return ("needs_info", f"Missing evidence: no cited journal entry or P&L point, and {n} step(s) to see it again"
                f" (at least {MIN_STEPS}). Send it again with one of them.", None, None)
    passages = explained_by(req)
    if passages:
        return ("not_a_bug", "The help already explains this: " + ", ".join(passages[:3]), None, None)
    return None
