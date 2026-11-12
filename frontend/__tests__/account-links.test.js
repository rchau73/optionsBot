/**
 * @jest-environment node
 */
// #760: manager–account links — the pure rules (which account a bot trades
// for, who is limited, what a link request may name), the status file reader
// and AuthService.setAccountLinks with its audit lines. The routes, end to
// end and in the role matrix, are in role-matrix.test.js.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { AuthService } from "@/lib/auth/service";
import { AuthStore } from "@/lib/auth/store";
import { authKey } from "@/lib/auth/tokens";
import { accountLinkProblem, botAccount, checkLinkRequest, linkChanges, MAX_LINKS, readAccounts } from "@/lib/auth/accountLinks";
import { TEST_SECRET } from "@/test/auth";

const A = { account_id: "acct-a", label: "Account A", environment: "testnet", bots: [{ currency: "BTC" }] };
const B = { account_id: "acct-b", label: "acct-b", environment: "testnet", bots: [{ currency: "ETH" }] };
const ACCOUNTS = [A, B];
const manager = { id: 2, username: "ana", role: "manager" };

beforeEach(() => {
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => jest.restoreAllMocks());

describe("botAccount", () => {
  test.each([
    ["btc", ACCOUNTS, "acct-a"],
    ["ETH", ACCOUNTS, "acct-b"],
    ["sol", ACCOUNTS, null],
    ["btc", [], null],
    // Two accounts trade BTC: the monitor cannot tell which, so none.
    ["btc", [A, { ...B, account_id: "acct-c", bots: [{ currency: "BTC" }] }], null],
  ])("%s → %s", (bot, accounts, want) => {
    expect(botAccount(bot, accounts)?.account_id ?? null).toBe(want);
  });
});

describe("accountLinkProblem", () => {
  test.each([
    ["an admin is never limited", { ...manager, role: "admin" }, ["acct-b"], "btc", null],
    ["a manager without a link acts on every account", manager, [], "btc", null],
    ["a linked manager on their account", manager, ["acct-a"], "btc", null],
    ["a linked manager on another account", manager, ["acct-a"], "eth", "you do not manage account acct-b, which bot eth trades for: ask an admin to link it to you on the Users page"],
    ["the account is named with its label", manager, ["acct-b"], "btc", "you do not manage account Account A (acct-a), which bot btc trades for: ask an admin to link it to you on the Users page"],
    ["a bot no account trades for fails closed", manager, ["acct-a"], "sol", expect.stringContaining("cannot tell which account bot sol trades for")],
  ])("%s", (_, user, links, bot, want) => {
    expect(accountLinkProblem({ user, links, bot, accounts: ACCOUNTS })).toEqual(want);
  });

  test("a linked manager is refused on every bot when the registry is empty", () => {
    expect(accountLinkProblem({ user: manager, links: ["acct-a"], bot: "btc", accounts: [] })).toMatch(/cannot tell/);
  });
});

describe("checkLinkRequest and linkChanges", () => {
  test.each([
    ["not a list", "acct-a", { ok: false, error: `accounts: a list of at most ${MAX_LINKS} account ids` }],
    ["a malformed id", ["Acct A"], { ok: false, error: `accounts: a list of at most ${MAX_LINKS} account ids` }],
    ["one over the limit", Array.from({ length: MAX_LINKS + 1 }, (_, i) => `a${i}`), { ok: false, error: `accounts: a list of at most ${MAX_LINKS} account ids` }],
    ["an id the registry does not list", ["acct-z"], { ok: false, error: "account acct-z is not in the account registry" }],
    ["known ids, repeated and unsorted", ["acct-b", "acct-a", "acct-b"], { ok: true, ids: ["acct-a", "acct-b"] }],
    ["an id already linked that the registry no longer lists may stay", ["acct-gone"], { ok: true, ids: ["acct-gone"] }],
    ["none: every account again", [], { ok: true, ids: [] }],
  ])("%s", (_, raw, want) => {
    expect(checkLinkRequest(raw, { current: ["acct-gone"], accounts: ACCOUNTS })).toEqual(want);
  });

  test("the changes from one list to another", () => {
    expect(linkChanges(["acct-a", "acct-gone"], ["acct-b", "acct-a"])).toEqual({ add: ["acct-b"], remove: ["acct-gone"] });
    expect(linkChanges([], [])).toEqual({ add: [], remove: [] });
  });
});

describe("readAccounts", () => {
  let dir;
  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "account-links-"));
  });
  afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));
  const write = (doc) => {
    const file = path.join(dir, "accounts_status.json");
    fs.writeFileSync(file, typeof doc === "string" ? doc : JSON.stringify(doc));
    return file;
  };

  test("reads ids, labels, environments and bot currencies, nothing else", () => {
    const file = write({
      version: 1,
      accounts: [{ account_id: "acct-a", label: "Main", exchange: "deribit", environment: "live", bots: [{ currency: "BTC", config: "config_btc.yaml" }], key_status: "ok" }],
      violations: [],
    });
    expect(readAccounts(file)).toEqual([{ account_id: "acct-a", label: "Main", environment: "live", bots: [{ currency: "BTC" }] }]);
  });

  test.each([
    ["no file", null],
    ["not JSON", "{"],
    ["an unknown version", { version: 2, accounts: [{ account_id: "acct-a", bots: [] }] }],
    ["a registry that breaks a rule lists no account", { version: 1, accounts: [], violations: [{ rule: "opaque_id" }] }],
  ])("%s → no account known", (_, doc) => {
    expect(readAccounts(doc === null ? path.join(dir, "missing.json") : write(doc))).toEqual([]);
  });
});

describe("AuthService.setAccountLinks", () => {
  let auth, lines, admin, ana, user;
  beforeEach(async () => {
    lines = [];
    auth = new AuthService({ store: await AuthStore.open(":memory:"), key: authKey(TEST_SECRET), audit: (e) => lines.push(e) });
    admin = auth.store.createUser({ username: "root", passwordHash: "x", role: "admin" });
    ana = auth.store.createUser({ username: "ana", passwordHash: "x", role: "manager" });
    user = auth.store.createUser({ username: "uma", passwordHash: "x", role: "user" });
  });

  test("adds and removes links, one audit line per account with the admin, the manager and the account", () => {
    expect(auth.setAccountLinks(admin, ana.id, ["acct-a", "acct-b"], ACCOUNTS)).toEqual({ ok: true, accounts: ["acct-a", "acct-b"], added: ["acct-a", "acct-b"], removed: [] });
    expect(auth.setAccountLinks(admin, ana.id, ["acct-b"], ACCOUNTS)).toEqual({ ok: true, accounts: ["acct-b"], added: [], removed: ["acct-a"] });
    expect(lines.map(({ action, username, role, target, account_id }) => [action, username, role, target, account_id])).toEqual([
      ["account_link_added", "root", "admin", "ana", "acct-a"],
      ["account_link_added", "root", "admin", "ana", "acct-b"],
      ["account_link_removed", "root", "admin", "ana", "acct-a"],
    ]);
    expect(auth.listUsers().map((u) => [u.username, u.accounts])).toEqual([
      ["root", []],
      ["ana", ["acct-b"]],
      ["uma", []],
    ]);
  });

  test("the same list again changes and audits nothing", () => {
    auth.setAccountLinks(admin, ana.id, ["acct-a"], ACCOUNTS);
    lines.length = 0;
    expect(auth.setAccountLinks(admin, ana.id, ["acct-a"], ACCOUNTS)).toEqual({ ok: true, accounts: ["acct-a"], added: [], removed: [] });
    expect(lines).toEqual([]);
  });

  test.each([
    ["an unknown user", () => 999, ["acct-a"], { ok: false, status: 404, error: "no such user" }],
    ["an admin", () => admin.id, ["acct-a"], { ok: false, status: 409, error: "root is an admin: account links are for managers only" }],
    ["a user", () => user.id, ["acct-a"], { ok: false, status: 409, error: "uma is a user: account links are for managers only" }],
    ["an account outside the registry", () => ana.id, ["acct-z"], { ok: false, status: 400, error: "account acct-z is not in the account registry" }],
    ["no list", () => ana.id, undefined, { ok: false, status: 400, error: `accounts: a list of at most ${MAX_LINKS} account ids` }],
  ])("refuses %s and writes nothing", (_, id, ids, want) => {
    expect(auth.setAccountLinks(admin, id(), ids, ACCOUNTS)).toEqual(want);
    expect(lines).toEqual([]);
    expect(auth.store.allAccountLinks()).toEqual({});
  });

  test("a linked manager is refused on another account; deleting the manager removes their links", () => {
    auth.setAccountLinks(admin, ana.id, ["acct-a"], ACCOUNTS);
    const load = jest.fn(() => ACCOUNTS);
    expect(auth.accountRefusal(ana, "btc", load)).toBeNull();
    expect(auth.accountRefusal(ana, "eth", load)).toMatch(/do not manage account acct-b/);
    // An admin or an unlinked manager never reads the registry.
    load.mockClear();
    expect(auth.accountRefusal(admin, "eth", load)).toBeNull();
    expect(load).not.toHaveBeenCalled();
    auth.deleteUser(admin, ana.id);
    expect(auth.store.allAccountLinks()).toEqual({});
  });
});
