"""Tests of pm_board.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import contextlib
import io
import json
import os
import sys
import tempfile
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


class TestAgentPMTickets(unittest.TestCase):
    def test_never_to_ready(self):
        pm = {"decisions": [{"issue": 1, "epic": "E2", "priority": "P1", "action": "ready", "rule_area": "none"}]}
        a = pm_board.gate(pm, [card(1, labels=["agent:pm"])], epics(), {})[0]
        self.assertNotIn("move", a)
        self.assertIn("PM agent's own work", a["refused"])

    def test_placed_ones_leave_triage_to_the_answer_step(self):
        board = [card(1, labels=["agent:pm"], parent=100), card(2, labels=["agent:pm"])]
        self.assertEqual(pm_board.pending(board, {}, epics())["triage"], [2])


def ticket(n=149, msgs=(), status="Triage", labels=("agent:pm",), body="🧭 **PM** question", state="OPEN"):
    return {"number": n, "state": state, "status": status, "labels": list(labels), "created": "2026-10-18T01:00:00Z",
            "body": body, "messages": [{"at": at, "body": b} for at, b in msgs]}


class TestAnswered(unittest.TestCase):
    PM = "🧭 **PM agent** — epic E5"
    DEV = "🤖 **Dev agent** picked this up"

    def test_a_person_answer_after_the_pm(self):
        t = ticket(msgs=[("2026-10-18T01:57:00Z", self.PM), ("2026-10-18T02:40:00Z", "1. Yes"),
                         ("2026-10-18T02:43:00Z", self.DEV), ("2026-10-18T03:08:00Z", "Yes, for the PM")])
        self.assertEqual(pm_board.answered([t], {}), 149)

    def test_other_agents_do_not_hide_an_answer(self):
        t = ticket(msgs=[("2026-10-18T01:57:00Z", self.PM), ("2026-10-18T02:40:00Z", "1. Yes"),
                         ("2026-10-18T02:43:00Z", self.DEV)])
        self.assertEqual(pm_board.answered([t], {}), 149)

    def test_nothing_after_the_pm_reply(self):
        t = ticket(msgs=[("2026-10-18T02:40:00Z", "1. Yes"), ("2026-10-18T03:00:00Z", "x\n<!-- agent:pm -->")])
        self.assertIsNone(pm_board.answered([t], {}))

    def test_a_handled_run_is_not_repeated(self):
        t = ticket(msgs=[("2026-10-18T02:40:00Z", "1. Yes")])
        self.assertIsNone(pm_board.answered([t], {"answered": {"149": "2026-10-18T02:40:00Z"}}))

    def test_a_person_filed_request_counts(self):
        self.assertEqual(pm_board.answered([ticket(body="Add an epic for X")], {}), 149)
        self.assertIsNone(pm_board.answered([ticket()], {}))

    def test_skips_other_tickets_and_columns(self):
        msgs = [("2026-10-18T02:40:00Z", "answer")]
        for t in (ticket(labels=()), ticket(labels=("agent:pm", "epic")), ticket(status="In progress"),
                  ticket(status="Done"), ticket(state="CLOSED")):
            self.assertIsNone(pm_board.answered([dict(t, messages=[{"at": a, "body": b} for a, b in msgs])], {}))

    def test_oldest_answer_first(self):
        a = ticket(n=1, msgs=[("2026-10-18T05:00:00Z", "late")])
        b = ticket(n=2, msgs=[("2026-10-18T04:00:00Z", "early")])
        self.assertEqual(pm_board.answered([a, b], {}), 2)


def prd_with(marker_e1, extra=""):
    return f"## Epics\n### E1 · Engine\n<!-- epic: {marker_e1} -->\n{extra}"


class TestPolicyReasons(unittest.TestCase):
    MAIN = pm_board.parse_epics(prd_with('{"id": "E1", "status": "approved", "rule_areas": ["exits"]}'))

    def reasons(self, after_marker, declared="no", extra=""):
        return pm_board.policy_reasons(self.MAIN, pm_board.parse_epics(prd_with(after_marker, extra)), declared)

    def test_wording_change_without_policy_may_self_merge(self):
        self.assertEqual(self.reasons('{"id": "E1", "status": "approved", "rule_areas": ["exits"]}'), [])

    def test_narrowing_rule_areas_may_self_merge(self):
        self.assertEqual(self.reasons('{"id": "E1", "status": "approved", "rule_areas": []}'), [])

    def test_new_rule_area_needs_a_person(self):
        r = self.reasons('{"id": "E1", "status": "approved", "rule_areas": ["exits", "margin"]}')
        self.assertEqual(len(r), 1)
        self.assertIn("['margin']", r[0])

    def test_new_approved_epic_with_rule_areas_needs_a_person(self):
        extra = '### E7 · New\n<!-- epic: {"id": "E7", "status": "approved", "rule_areas": ["entry"]} -->\n'
        self.assertIn("E7", self.reasons('{"id": "E1", "status": "approved", "rule_areas": ["exits"]}', extra=extra)[0])

    def test_parked_epic_with_rule_areas_may_self_merge(self):
        extra = '### E7 · New\n<!-- epic: {"id": "E7", "status": "parked", "rule_areas": ["entry"]} -->\n'
        self.assertEqual(self.reasons('{"id": "E1", "status": "approved", "rule_areas": ["exits"]}', extra=extra), [])

    def test_unparking_an_epic_with_rule_areas_needs_a_person(self):
        main = pm_board.parse_epics(prd_with('{"id": "E1", "status": "parked", "rule_areas": ["exits"]}'))
        after = pm_board.parse_epics(prd_with('{"id": "E1", "status": "approved", "rule_areas": ["exits"]}'))
        self.assertEqual(len(pm_board.policy_reasons(main, after, "no")), 1)

    def test_declared_or_missing_declaration_needs_a_person(self):
        same = '{"id": "E1", "status": "approved", "rule_areas": ["exits"]}'
        self.assertIn("declared", self.reasons(same, "yes")[0])
        self.assertIn("did not say", self.reasons(same, None)[0])
        self.assertIn("did not say", self.reasons(same, "maybe")[0])


def pr(labels=("agent:pm", "pm:self-merge"), state="OPEN", merge="CLEAN", rollup=None):
    if rollup is None:
        rollup = [{"__typename": "CheckRun", "name": "check", "status": "COMPLETED", "conclusion": "SUCCESS"}]
    return {"state": state, "isDraft": False, "labels": [{"name": l} for l in labels],
            "mergeStateStatus": merge, "statusCheckRollup": rollup}


class TestMergeRefusal(unittest.TestCase):
    FILES = ["docs/product/PRD.md"]

    def test_clean_prd_pr_is_merged(self):
        self.assertEqual(pm_board.merge_refusal(pr(), self.FILES, [], False), "")

    def test_label_removed_by_a_person_stops_it(self):
        self.assertIn("pm:self-merge", pm_board.merge_refusal(pr(labels=("agent:pm",)), self.FILES, [], False))

    def test_needs_human_stops_it(self):
        labels = ("agent:pm", "pm:self-merge", "agent:needs-human")
        self.assertIn("needs-human", pm_board.merge_refusal(pr(labels=labels), self.FILES, [], False))

    def test_files_outside_docs_product_stop_it(self):
        why = pm_board.merge_refusal(pr(), self.FILES + ["internal/risk/risk.go"], [], False)
        self.assertIn("internal/risk/risk.go", why)
        self.assertIn("no changed files", pm_board.merge_refusal(pr(), [], [], False))

    def test_strategy_policy_stops_it(self):
        self.assertIn("delegates", pm_board.merge_refusal(pr(), self.FILES, ["epic E1 delegates x"], False))

    def test_a_waiting_person_message_stops_it(self):
        self.assertIn("waiting", pm_board.merge_refusal(pr(), self.FILES, [], True))

    def test_ci_must_be_green(self):
        running = [{"__typename": "CheckRun", "name": "check", "status": "IN_PROGRESS", "conclusion": ""}]
        failed = [{"__typename": "CheckRun", "name": "check", "status": "COMPLETED", "conclusion": "FAILURE"}]
        status = [{"__typename": "StatusContext", "context": "ci", "state": "PENDING"}]
        for rollup, word in (([], "no CI"), (running, "running"), (failed, "FAILURE"), (status, "PENDING")):
            self.assertIn(word, pm_board.merge_refusal(pr(rollup=rollup), self.FILES, [], False))
        ok = [{"__typename": "StatusContext", "context": "ci", "state": "SUCCESS"},
              {"__typename": "CheckRun", "name": "gitleaks", "status": "COMPLETED", "conclusion": "SKIPPED"}]
        self.assertEqual(pm_board.merge_refusal(pr(rollup=ok), self.FILES, [], False), "")

    def test_github_must_report_clean(self):
        for st in ("BEHIND", "BLOCKED", "DIRTY", "UNKNOWN"):
            self.assertIn(st, pm_board.merge_refusal(pr(merge=st), self.FILES, [], False))

    def test_closed_pr_is_left_alone(self):
        self.assertIn("not an open", pm_board.merge_refusal(pr(state="MERGED"), self.FILES, [], False))


class TestDeclaredPolicy(unittest.TestCase):
    def test_reads_the_marker_pm_answer_writes(self):
        self.assertEqual(pm_board.declared_policy("Closes #149\n<!-- pm:policy=no -->\n_Opened_"), "no")
        self.assertEqual(pm_board.declared_policy("<!-- pm:policy=yes -->"), "yes")

    def test_missing_marker_is_no_declaration(self):
        for body in (None, "", "Closes #149", "<!-- pm:policy=maybe -->"):
            self.assertIsNone(pm_board.declared_policy(body))

    def test_any_yes_wins(self):
        self.assertEqual(pm_board.declared_policy("<!-- pm:policy=no -->\n<!-- pm:policy=yes -->"), "yes")
        self.assertEqual(pm_board.declared_policy("<!-- pm:policy=yes -->\n<!-- pm:policy=no -->"), "yes")


class TestSelfMergeCLI(unittest.TestCase):
    """pm_board.py selfmerge as pm_answer.sh calls it: files in, one line out."""
    SAME = '{"id": "E1", "status": "approved", "rule_areas": ["exits"]}'

    def run_cli(self, body="<!-- pm:policy=no -->", tickets=None, state=None, files="docs/product/PRD.md\n",
                pr_prd=None):
        if tickets is None:
            tickets = [ticket(msgs=[("2026-10-18T02:40:00Z", "1. Yes"), ("2026-10-18T03:00:00Z", "🧭 PRD PR opened")],
                              status="In review")]
        with tempfile.TemporaryDirectory() as d:
            def put(name, text):
                p = os.path.join(d, name)
                with open(p, "w") as f:
                    f.write(text)
                return p
            args = ["pm_board.py", "selfmerge",
                    put("pr.json", json.dumps(dict(pr(), body=body))), put("files.txt", files),
                    put("main.md", prd_with(self.SAME)), put("pr.md", pr_prd or prd_with(self.SAME)),
                    put("tickets.json", json.dumps(tickets)),
                    put("state.json", json.dumps(state)) if state is not None else os.path.join(d, "none.json"),
                    "149"]
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                pm_board.main(args)
            return out.getvalue().strip()

    def test_clean_case_prints_an_empty_line(self):
        self.assertEqual(self.run_cli(), "")

    def test_a_person_message_newer_than_the_pm_comment_is_refused(self):
        t = ticket(msgs=[("2026-10-18T03:00:00Z", "🧭 PRD PR opened"), ("2026-10-18T03:10:00Z", "wait")],
                   status="In review")
        self.assertIn("waiting", self.run_cli(tickets=[t]))

    def test_a_ticket_missing_from_tickets_is_refused(self):
        self.assertIn("not an open agent:pm ticket", self.run_cli(tickets=[ticket(n=7)]))

    def test_declared_policy_in_the_body_is_refused(self):
        # Even with the label still on (its removal failed), the body says "yes": no merge.
        self.assertIn("declared", self.run_cli(body="<!-- pm:policy=yes -->"))

    def test_body_without_a_declaration_is_refused(self):
        self.assertIn("did not say", self.run_cli(body="Closes #149"))

    def test_new_rule_areas_are_refused(self):
        grown = prd_with('{"id": "E1", "status": "approved", "rule_areas": ["exits", "margin"]}')
        self.assertIn("delegates", self.run_cli(pr_prd=grown))

    def test_files_outside_docs_product_are_refused(self):
        self.assertIn("internal/x.go", self.run_cli(files="docs/product/PRD.md\ninternal/x.go\n"))


if __name__ == "__main__":
    unittest.main()
