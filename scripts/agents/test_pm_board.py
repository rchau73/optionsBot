"""Tests of pm_board.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import pm_board  # noqa: E402

PRD = """# PRD

## Epics

### E1 · Reliable engine
<!-- epic: {"id": "E1", "status": "approved", "rank": 1, "rule_areas": ["exits", "repair"], "ui": false} -->
Outcome: fewer stops.

### E2 · Monitor for investors
<!-- epic: {"id": "E2", "status": "approved", "rank": 2, "ui": true} -->
Outcome: value seen in the first session.

### E3 · Parked idea
<!-- epic: {"id": "E3", "status": "parked"} -->

## Out of scope
### Not an epic
No marker here.
"""


def card(n, status="Triage", labels=(), updated="2026-10-17T10:00:00Z", parent=None, priority=None):
    return {"item": f"I{n}", "id": f"N{n}", "number": n, "title": f"t{n}", "status": status, "state": "OPEN",
            "labels": list(labels), "updatedAt": updated, "parent": parent, "priority": priority}


def epics(prototype_approved=False):
    es = pm_board.parse_epics(PRD)
    for i, e in enumerate(es):
        e["number"] = 100 + i
        e["prototype_approved"] = prototype_approved
    return es


class ParseEpics(unittest.TestCase):
    def test_reads_marked_sections_only(self):
        es = pm_board.parse_epics(PRD)
        self.assertEqual([e["id"] for e in es], ["E1", "E2", "E3"])
        self.assertEqual(es[0]["name"], "Reliable engine")
        self.assertEqual(es[0]["rule_areas"], ["exits", "repair"])
        self.assertTrue(es[1]["ui"])
        self.assertEqual(es[2]["status"], "parked")
        self.assertIn("fewer stops", es[0]["body"])
        self.assertNotIn("Out of scope", es[1]["body"] + es[2]["body"])

    def test_rejects_bad_markers(self):
        for bad in ('{"id": "X1"}', '{"id": "E1", "status": "maybe"}', '{"id": "E1", "rule_areas": ["everything"]}'):
            with self.assertRaises(ValueError):
                pm_board.parse_epics(f"### E1 · x\n<!-- epic: {bad} -->\n")
        with self.assertRaises(ValueError):
            pm_board.parse_epics(PRD + '\n### E1 · again\n<!-- epic: {"id": "E1"} -->\n')


class Pending(unittest.TestCase):
    def test_new_or_changed_triage_and_unparented(self):
        board = [card(1), card(2, updated="2026-10-17T08:00:00Z"), card(3, "Ready"),
                 card(4, "Ready", parent=100), card(5, "Done"), card(6, labels=["epic"])]
        state = {"seen": {"2": "2026-10-17T09:00:00Z"}}
        self.assertEqual(pm_board.pending(board, state, epics()), {"triage": [1], "unparented": [3]})


class Gate(unittest.TestCase):
    def decide(self, d, c=None, es=None, state=None):
        c = c or card(1)
        d = {"issue": c["number"], "priority": "P1", **d}
        return pm_board.gate({"decisions": [d]}, [c], es or epics(), state or {})[0]

    def test_ready_inside_an_approved_epic(self):
        a = self.decide({"epic": "E1", "action": "ready", "rule_area": "none"})
        self.assertEqual((a.get("move"), a["epic"], a["priority"]), ("Ready", "E1", "P1"))

    def test_trading_rule_area_must_be_covered(self):
        self.assertEqual(self.decide({"epic": "E1", "action": "ready", "rule_area": "exits"})["move"], "Ready")
        a = self.decide({"epic": "E1", "action": "ready", "rule_area": "margin"})
        self.assertNotIn("move", a)
        self.assertIn("does not cover", a["refused"])

    def test_sme_finding_must_name_its_rule_area(self):
        a = self.decide({"epic": "E1", "action": "ready"}, card(1, labels=["agent:sme"]))
        self.assertNotIn("move", a)
        self.assertIn("trading-rule area", a["refused"])

    def test_no_epic_or_parked_epic_stays_in_triage(self):
        self.assertIn("fits no epic", self.decide({"epic": "E9", "action": "ready"})["refused"])
        self.assertIn("parked", self.decide({"epic": "E3", "action": "ready", "rule_area": "none"})["refused"])

    def test_ui_story_waits_for_the_prototype(self):
        a = self.decide({"epic": "E2", "action": "ready", "rule_area": "none", "ui": True})
        self.assertIn("prototype", a["refused"])
        a = self.decide({"epic": "E2", "action": "ready", "rule_area": "none", "ui": True}, es=epics(True))
        self.assertEqual(a["move"], "Ready")

    def test_never_moves_cards_outside_triage(self):
        for st in ("In progress", "In review", "Blocked", "Ready"):
            a = self.decide({"epic": "E1", "action": "ready", "rule_area": "none"}, card(1, st))
            self.assertNotIn("move", a)

    def test_cards_in_flight_get_an_epic_but_keep_their_priority(self):
        a = self.decide({"epic": "E1", "priority": "P0"}, card(1, "In progress"))
        self.assertEqual(a["epic"], "E1")
        self.assertNotIn("priority", a)

    def test_unready_only_what_the_pm_put_in_ready(self):
        board = [card(1, "Ready"), card(2, "Ready"), card(3, "In progress")]
        pm = {"unready": [{"issue": 1}, {"issue": 2}, {"issue": 3}]}
        acts = pm_board.gate(pm, board, epics(), {"pm_ready": [1, 3]})
        self.assertEqual([(a["issue"], a["move"]) for a in acts], [(1, "Triage")])

    def test_priorities_only_on_triage_and_ready(self):
        board = [card(1, "Ready", priority="P2"), card(2, "In progress"), card(3, "Ready", priority="P1")]
        pm = {"priorities": [{"issue": 1, "priority": "P0"}, {"issue": 2, "priority": "P0"},
                             {"issue": 3, "priority": "P1"}, {"issue": 1, "priority": "P9"}]}
        acts = pm_board.gate(pm, board, epics(), {})
        self.assertEqual([(a["issue"], a["priority"]) for a in acts], [(1, "P0")])

    def test_epics_and_closed_issues_are_ignored(self):
        closed = card(2)
        closed["state"] = "CLOSED"
        pm = {"decisions": [{"issue": 1, "epic": "E1", "action": "ready", "priority": "P0"},
                            {"issue": 2, "epic": "E1", "action": "ready", "priority": "P0"}]}
        self.assertEqual(pm_board.gate(pm, [card(1, labels=["epic"]), closed], epics(), {}), [])


if __name__ == "__main__":
    unittest.main()
