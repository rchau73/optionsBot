/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as ask } from "@/app/api/assistant/route";
import { goldenInputs } from "@/lib/assistant/goldenrun";
import { check } from "@/lib/assistant/guard";
import { readAnswers } from "@/lib/assistant/log";
import { myAccess, setAssistantModel } from "@/lib/assistant/server";
import { ACCESS_ACTIONS, accessPassages, asksAboutAccess } from "@/lib/assistant/service";
import { actionRoles } from "@/lib/assistant/access";
import { READ_ROUTES, STATE_CHANGING_ROUTES } from "@/lib/auth/routeRoles";
import { NEEDS_INFO_TEXT } from "@/lib/assistantRequests";
import { signedIn, useTestAuth } from "@/test/auth";

// The assistant answers "what can I do with my role, and why can't I see or
// do X?" from the asking person's own role (#911): you:role and one
// you:can/<action> per action, built from the role matrix
// (lib/auth/routeRoles.js) and, for a manager, their own account links —
// never anyone else's. The fixtures are the golden set's access cases. A
// fake model: no test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const byId = Object.fromEntries(golden.map((q) => [q.id, q]));
const ACCESS_IDS = ["access-admin-what-can-i-do", "access-manager-unlinked-account", "access-user-why-cannot-close", "access-vague-does-not-work"];
const [adminCase, unlinked, userClose, vague] = ACCESS_IDS.map((id) => byId[id]);
const ROWS = new Map([...STATE_CHANGING_ROUTES, ...READ_ROUTES].map((r) => [r.route, r]));
const text = (ps) => ps.map((p) => `${p.id} ${p.title} ${p.text}`).join("\n");

let root, file;
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-access-"));
  file = path.join(root, "assistant.jsonl");
  process.env.ASSISTANT_LOG_PATH = file;
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
});
afterEach(() => {
  setAssistantModel(undefined);
  jest.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
  delete process.env.ASSISTANT_LOG_PATH;
});

/** A model that always replies `reply` (and `verdict` to a request check), recording what it was sent. */
function fakeModel(reply, verdict = null) {
  const m = {
    model: "fake",
    calls: [],
    ask: async (args) => (m.calls.push(args), structuredClone(reply)),
    checkRequest: async (args) => (m.calls.push(args), structuredClone(verdict)),
  };
  return m;
}

/** POST /api/assistant as `username` with `role` (and a manager's `links`): the answer, the passages sent and the log line. */
async function askAs(username, role, question, reply, { links = [], verdict = null } = {}) {
  const model = fakeModel(reply, verdict);
  setAssistantModel(model);
  const { cookie, user, auth } = await signedIn(username, role);
  if (links.length) auth.store.changeAccountLinks(user.id, { add: links }, "admin");
  const res = await ask(
    new Request("http://monitor/api/assistant", {
      method: "POST",
      headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie },
      body: JSON.stringify({ question }),
    }),
  );
  const sent = model.calls[0]?.passages ?? [];
  return { body: await res.json(), sent, ids: sent.map((p) => p.id), line: [...readAnswers(file).values()].at(-1) };
}

describe("the access passages follow the role matrix", () => {
  test("every action's routes are rows of the matrix, and allow the same roles", () => {
    for (const a of ACCESS_ACTIONS) {
      for (const r of a.routes) expect([a.key, r, ROWS.has(r)]).toEqual([a.key, r, true]);
      const roles = a.routes.map((r) => ROWS.get(r).roles.filter((x) => x !== "anonymous").sort().join(","));
      expect([a.key, new Set(roles).size]).toEqual([a.key, 1]);
    }
  });

  test("a linked action is one the matrix marks linked (#760)", () => {
    for (const a of ACCESS_ACTIONS.filter((x) => x.linked)) {
      for (const r of a.routes) expect([r, ROWS.get(r).linked]).toEqual([r, true]);
    }
  });

  test("close and rebalance are for managers and admins; the hedge switch, Users, Audit and Analytics for admins; seeing for everyone", () => {
    const roles = Object.fromEntries(ACCESS_ACTIONS.map((a) => [a.key, actionRoles(a).sort()]));
    expect(roles).toMatchObject({
      see: ["admin", "manager", "user"],
      close: ["admin", "manager"],
      rebalance: ["admin", "manager"],
      hedge: ["admin"],
      lab: ["admin", "manager"],
      "lab-confirm": ["admin", "manager"],
      users: ["admin"],
      audit: ["admin"],
      analytics: ["admin"],
    });
  });

  test("one you:role and one you:can/<action> per action, for each role", () => {
    for (const role of ["admin", "manager", "user"]) {
      expect(accessPassages({ role }).map((p) => p.id)).toEqual(["you:role", ...ACCESS_ACTIONS.map((a) => `you:can/${a.key}`)]);
    }
  });

  test("no passage for an unknown role, a test run or no role", () => {
    for (const role of ["test_run", "", undefined, "owner"]) expect(accessPassages({ role })).toEqual([]);
  });

  test("a user: close is refused with who may and who to ask, never a way around it", () => {
    const close = accessPassages({ role: "user" }).find((p) => p.id === "you:can/close");
    expect(close.text).toBe(
      "Close a position: buy back a short leg at market from the Positions page (manual close): managers and admins can do it; your role is user, so you cannot. An admin can give you another role on the Users page.",
    );
  });

  test("a user's you:role lists what it may and may not do", () => {
    const role = accessPassages({ role: "user" })[0].text;
    expect(role).toContain("Your role is user: you are signed in as a user.");
    expect(role).toContain("With it you may: see the monitor and ask the assistant.");
    expect(role).toContain("close a position (manual close) (managers and admins)");
    expect(role).toContain("read the audit trail on the Audit page (only admins)");
    expect(role).toContain("Only an admin can give you another role or other account links, on the Users page; the assistant cannot.");
  });

  test("an admin may do everything, on every account", () => {
    const ps = accessPassages({ role: "admin" });
    expect(ps[0].text).toContain("There is nothing in the monitor your role may not do.");
    expect(ps.find((p) => p.id === "you:can/close").text).toContain("Admins act on every account.");
  });

  test("a manager with links: their own account ids, on every linked action; without links, every account", () => {
    const linked = accessPassages({ role: "manager", links: ["acct-b", "acct-a", "acct-a"] });
    expect(linked[0].text).toContain("you act only on the bots of the accounts you are linked to: acct-a, acct-b.");
    for (const a of ACCESS_ACTIONS.filter((x) => x.linked && actionRoles(x).includes("manager"))) {
      expect(linked.find((p) => p.id === `you:can/${a.key}`).text).toContain("acct-a, acct-b");
    }
    // The hedge switch is admin-only: a manager is refused it, links or not.
    expect(linked.find((p) => p.id === "you:can/hedge").text).toContain("only admins can do it; your role is manager, so you cannot.");
    expect(linked.find((p) => p.id === "you:can/lab").text).not.toContain("acct-a");
    const none = accessPassages({ role: "manager" });
    expect(none[0].text).toContain("You are linked to no account, so as a manager you act on every account of this monitor.");
  });

  test("links given for a user or an admin are never named", () => {
    for (const role of ["user", "admin"]) expect(text(accessPassages({ role, links: ["acct-a"] }))).not.toContain("acct-a");
  });
});

describe("Scenario: a user asks why they cannot close a position", () => {
  test("the answer says closing is for managers and admins, cites you:role, and says an admin can change their role", async () => {
    const { body, ids } = await askAs("user-asker", "user", userClose.question, userClose.access.answer);
    expect(ids).toEqual(expect.arrayContaining(["you:role", "you:can/close"]));
    expect(body).toMatchObject({ outcome: "answered", answer: userClose.access.answer.text });
    for (const s of ["managers and admins", "your role is user", "An admin can give you another role"]) expect(body.answer).toContain(s);
    expect(body.citations.map((c) => c.id)).toEqual(["you:can/close", "you:role"]);
  });
});

describe("Scenario: a manager asks about an account they are not linked to", () => {
  test("the answer says they act only on linked accounts and cites the passage; the other account is not read or named", async () => {
    const other = await signedIn("someone-else", "manager");
    other.auth.store.changeAccountLinks(other.user.id, { add: ["acct-b"] }, "admin");
    const { body, sent, ids } = await askAs("mgr-asker", "manager", unlinked.question, unlinked.access.answer, { links: ["acct-a"] });
    expect(ids).toContain("you:role");
    expect(body).toMatchObject({ outcome: "answered", answer: unlinked.access.answer.text });
    expect(body.answer).toContain("only on the bots of the accounts you are linked to: acct-a");
    expect(body.citations.map((c) => c.id)).toEqual(["you:role", "you:can/lab-confirm"]);
    for (const s of ["acct-b", "someone-else"]) {
      expect(text(sent)).not.toContain(s);
      expect(JSON.stringify(body)).not.toContain(s);
    }
  });

  test("myAccess reads only the asker's own links", async () => {
    const { auth, user } = await signedIn("mgr-one", "manager");
    auth.store.changeAccountLinks(user.id, { add: ["acct-a"] }, "admin");
    const other = await signedIn("mgr-two", "manager");
    auth.store.changeAccountLinks(other.user.id, { add: ["acct-b"] }, "admin");
    const reads = jest.spyOn(auth.store, "accountLinks");
    const t = text(myAccess(auth, user));
    expect(reads.mock.calls).toEqual([[user.id]]);
    expect(t).toContain("acct-a");
    expect(t).not.toContain("acct-b");
  });

  test("a links read that fails gives no access passage, never 'every account'", async () => {
    const { auth, user } = await signedIn("mgr-broken", "manager");
    jest.spyOn(auth.store, "accountLinks").mockImplementation(() => {
      throw new Error("database is locked");
    });
    expect(myAccess(auth, user)).toEqual([]);
  });

  test("a user's or an admin's links are never read", async () => {
    for (const role of ["user", "admin"]) {
      const { auth, user } = await signedIn(`${role}-x`, role);
      const reads = jest.spyOn(auth.store, "accountLinks");
      myAccess(auth, user);
      expect(reads).not.toHaveBeenCalled();
      reads.mockRestore();
    }
  });
});

describe("Scenario: an admin asks what they can do", () => {
  test("the answer lists what an admin may do from the role tables, with citations", async () => {
    const { body, ids } = await askAs("admin-asker", "admin", adminCase.question, adminCase.access.answer);
    expect(ids).toEqual(expect.arrayContaining(["you:role", ...ACCESS_ACTIONS.map((a) => `you:can/${a.key}`)]));
    expect(body).toMatchObject({ outcome: "answered" });
    for (const a of ACCESS_ACTIONS) expect(body.answer).toContain(a.short);
    expect(body.citations.map((c) => c.id)).toEqual(["you:role", "you:can/close"]);
  });
});

describe("Scenario: a vague question", () => {
  test("'it does not work' gets the checklist back and drafts nothing", async () => {
    const verdict = byId["req-01"].request.verdict;
    const { body } = await askAs("user-vague", "user", vague.question, { text: "x", citations: [] }, { verdict });
    expect(body).toMatchObject({ outcome: "needs_info", answer: NEEDS_INFO_TEXT });
    expect(body.draft).toBeUndefined();
    const unticked = body.checklist.filter((c) => !c.ok).map((c) => c.key);
    expect(unticked).toEqual(expect.arrayContaining(["what", "where"]));
  });
});

describe("Scenario: no leakage", () => {
  test("no other person's username, email or role, and not the asker's own name, in the passages or the answer", async () => {
    const { auth } = await signedIn("hidden-admin", "admin");
    auth.store.createUser({ username: "hidden-user", passwordHash: "not a real hash", role: "user", email: "hidden@example.org" });
    for (const role of ["user", "manager", "admin"]) {
      const asker = `asker-${role}`;
      const { body, sent } = await askAs(asker, role, "What is my role and what can I do?", { text: `Your role is ${role}.`, citations: ["you:role"] });
      const shown = `${text(sent.filter((p) => p.id.startsWith("you:")))}\n${JSON.stringify(body)}`;
      for (const s of ["hidden-admin", "hidden-user", "hidden@example.org", asker]) expect([role, s, shown.includes(s)]).toEqual([role, s, false]);
      expect(body.outcome).toBe("answered");
    }
  });

  test("other questions do not pay: no you: passage for a question about a rule", async () => {
    const { ids, line } = await askAs("user-rule", "user", "What is a delta exit?", { text: "x", citations: [] });
    expect(ids.some((id) => id.startsWith("you:"))).toBe(false);
    expect(line.sources.some((id) => id.startsWith("you:"))).toBe(false);
  });

  test("an answer claiming a citation the person has no passage for is never shown", async () => {
    const { body } = await askAs("user-fake", "user", "Can I close a position?", { text: "Yes, you can.", citations: ["you:can/everything"] });
    expect(body.outcome).toBe("rejected");
  });
});

describe("Scenario: guard and golden set", () => {
  test("four access cases: an admin, a manager and an unlinked account, a user who cannot close, a vague report", () => {
    expect(golden.filter((q) => q.category === "access").map((q) => q.id)).toEqual(ACCESS_IDS);
  });

  test("every golden access answer passes the guard on the passages built from its fixture", () => {
    for (const q of ACCESS_IDS.map((id) => byId[id]).filter((x) => x.access.answer)) {
      const { access } = goldenInputs(q);
      expect([q.id, check(q.access.answer, access)]).toEqual([q.id, { ok: true, reasons: [], details: [] }]);
      for (const c of q.access.answer.citations) expect([q.id, c, access.some((p) => p.id === c)]).toEqual([q.id, c, true]);
    }
  });

  test("every access question asks about access; the vague one reads as a report instead", () => {
    for (const q of [adminCase, unlinked, userClose]) expect([q.id, asksAboutAccess(q.question)]).toEqual([q.id, true]);
    expect(asksAboutAccess(vague.question)).toBe(false);
  });
});

describe("asksAboutAccess", () => {
  test.each([
    ["Why can't I close this position?", true],
    ["What can I do with my role?", true],
    ["Can I change a setting?", true],
    ["Who can see the Audit page?", true],
    ["What is my role?", true],
    ["I don't have permission to rebalance", true],
    ["Why is the Users page missing for me?", true],
    ["How does manual close work?", true],
    ["Which accounts am I linked to?", true],
    ["What is a delta exit?", false],
    ["How did I do this month?", false],
    ["What did my last comparison show?", false],
    ["Why is nothing open?", false],
    ["Should I sell more puts?", false],
  ])("%s → %s", (q, want) => {
    expect(asksAboutAccess(q)).toBe(want);
  });
});
