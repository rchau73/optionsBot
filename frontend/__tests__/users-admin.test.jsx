import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import UsersAdmin from "@/components/UsersAdmin";
import UserMenu from "@/components/UserMenu";
import { confirmText, lockedLabel, newUserProblem, passwordFormProblem, UNUSED_DAYS, unusedLabel } from "@/lib/users";

// #306: the Users page. The routes enforce the role; these check what the page shows and sends.

const reply = (status, body = {}) => ({ ok: status < 300, status, headers: new Headers(), json: async () => body });

const USERS = [
  { id: 1, username: "admin", role: "admin", email: null, created_at: "2026-10-01T10:00:00Z", last_sign_in_at: "2026-10-24T08:00:00Z" },
  { id: 2, username: "bob", role: "manager", email: "bob@example.com", created_at: "2026-10-20T10:00:00Z", last_sign_in_at: null },
];

/** fetch: GET of the list answers USERS; any other call answers `answer`. */
function mockFetch(answer = reply(200, {})) {
  global.fetch = jest.fn(async (url, init = {}) => (url === "/api/admin/users" && !init.method ? reply(200, { users: USERS }) : answer));
}
const sent = () => global.fetch.mock.calls.filter(([, init]) => init?.method);

/**
 * Clicks a button that sends a request and waits, inside act, until the mocked
 * reply is rendered (#592): a findBy* polls against a 1 s wall-clock timer that
 * a busy or frozen runner can fire before React renders the reply.
 */
const clickAndSettle = (button) =>
  act(async () => {
    fireEvent.click(button);
  });

const realFetch = global.fetch;
afterEach(() => {
  global.fetch = realFetch;
});

describe("Users page", () => {
  test("lists each person with role, email, created and last sign-in; marks you and keeps your own delete and reset off", async () => {
    mockFetch();
    render(<UsersAdmin me="admin" />);
    const bob = await screen.findByTestId("user-bob");
    expect(bob).toHaveTextContent("bob@example.com");
    expect(bob).toHaveTextContent("never");
    expect(within(bob).getByLabelText("Role of bob")).toHaveValue("manager");
    const me = screen.getByTestId("user-admin");
    expect(me).toHaveTextContent("you");
    expect(within(me).getByRole("button", { name: "Delete" })).toBeDisabled();
    expect(within(me).getByRole("button", { name: "Reset password" })).toBeDisabled();
    expect(within(bob).getByRole("button", { name: "Delete" })).toBeEnabled();
  });

  test("a load error is shown, never an empty list", async () => {
    global.fetch = jest.fn(async () => reply(403, { error: "admin role required" }));
    render(<UsersAdmin me="admin" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Users could not be loaded: admin role required");
  });

  test("delete asks first: Cancel sends nothing, the confirmation sends DELETE", async () => {
    mockFetch(reply(200, { events_removed: 3 }));
    render(<UsersAdmin me="admin" />);
    fireEvent.click(within(await screen.findByTestId("user-bob")).getByRole("button", { name: "Delete" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("cannot be undone");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(sent()).toEqual([]);

    fireEvent.click(within(screen.getByTestId("user-bob")).getByRole("button", { name: "Delete" }));
    await clickAndSettle(screen.getByRole("button", { name: "Delete user" }));
    expect(screen.getByRole("status")).toHaveTextContent("bob is deleted (3 analytics events removed)");
    expect(sent()).toEqual([["/api/admin/users/2", expect.objectContaining({ method: "DELETE" })]]);
  });

  test("a deletion whose survey answers could not be removed says so (#363)", async () => {
    mockFetch(reply(200, { events_removed: 1, survey_answers_error: "the user is deleted but their survey answers are not (disk full): the monitor deletes them at its next start" }));
    render(<UsersAdmin me="admin" />);
    fireEvent.click(within(await screen.findByTestId("user-bob")).getByRole("button", { name: "Delete" }));
    await clickAndSettle(screen.getByRole("button", { name: "Delete user" }));
    expect(screen.getByRole("status")).toHaveTextContent("bob is deleted, but: the user is deleted but their survey answers are not (disk full)");
  });

  test("a role change asks first and sends PATCH with the new role; the server's refusal is shown", async () => {
    mockFetch(reply(409, { error: "the last admin cannot be removed or demoted: there must always be an admin" }));
    render(<UsersAdmin me="admin" />);
    const me = await screen.findByTestId("user-admin");
    expect(within(me).queryByRole("button", { name: "Change role" })).toBeNull();
    fireEvent.change(within(me).getByLabelText("Role of admin"), { target: { value: "user" } });
    fireEvent.click(within(me).getByRole("button", { name: "Change role" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("admin: admin → user");
    await clickAndSettle(within(screen.getByRole("dialog")).getByRole("button", { name: "Change role" }));
    expect(screen.getByRole("alert")).toHaveTextContent("there must always be an admin");
    const [[url, init]] = sent();
    expect(url).toBe("/api/admin/users/1");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(init.body)).toEqual({ role: "user" });
  });

  test("a reset asks for the new password twice and refuses a short one before sending", async () => {
    mockFetch(reply(200, {}));
    render(<UsersAdmin me="admin" />);
    fireEvent.click(within(await screen.findByTestId("user-bob")).getByRole("button", { name: "Reset password" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent("signs them out on every browser");
    fireEvent.change(within(dialog).getByLabelText("New password"), { target: { value: "short" } });
    fireEvent.change(within(dialog).getByLabelText("Confirm new password"), { target: { value: "short" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Reset password" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("at least 12 characters");
    expect(sent()).toEqual([]);

    fireEvent.change(within(dialog).getByLabelText("New password"), { target: { value: "a long new password" } });
    fireEvent.change(within(dialog).getByLabelText("Confirm new password"), { target: { value: "a long new password" } });
    await clickAndSettle(within(dialog).getByRole("button", { name: "Reset password" }));
    expect(screen.getByRole("status")).toHaveTextContent("bob's password is reset");
    const [[url, init]] = sent();
    expect(url).toBe("/api/admin/users/2/password");
    expect(JSON.parse(init.body)).toEqual({ password: "a long new password" });
  });

  test.each(["too common", "contains your username"])("a reset and Add user show the server's reason (%s)", async (error) => {
    mockFetch(reply(400, { error }));
    render(<UsersAdmin me="admin" />);
    fireEvent.click(within(await screen.findByTestId("user-bob")).getByRole("button", { name: "Reset password" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("New password"), { target: { value: "a long new password" } });
    fireEvent.change(within(dialog).getByLabelText("Confirm new password"), { target: { value: "a long new password" } });
    await clickAndSettle(within(dialog).getByRole("button", { name: "Reset password" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent(error);
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

    const form = screen.getByRole("form", { name: "Add user" });
    fireEvent.change(within(form).getByLabelText("Username"), { target: { value: "carol" } });
    fireEvent.change(within(form).getByLabelText("Password"), { target: { value: "a long new password" } });
    fireEvent.change(within(form).getByLabelText("Confirm password"), { target: { value: "a long new password" } });
    await clickAndSettle(within(form).getByRole("button", { name: "Add user" }));
    expect(within(form).getByRole("alert")).toHaveTextContent(error);
  });

  test("sign out everywhere asks first, then POSTs", async () => {
    mockFetch(reply(200, {}));
    render(<UsersAdmin me="admin" />);
    fireEvent.click(within(await screen.findByTestId("user-bob")).getByRole("button", { name: "Sign out everywhere" }));
    await clickAndSettle(within(screen.getByRole("dialog")).getByRole("button", { name: "Sign out everywhere" }));
    expect(screen.getByRole("status")).toHaveTextContent("bob is signed out everywhere");
    expect(sent()[0][0]).toBe("/api/admin/users/2/signout");
  });

  test("Add user checks the form, posts it and reloads the list", async () => {
    mockFetch(reply(201, { user: { id: 3, username: "carol", role: "user" } }));
    render(<UsersAdmin me="admin" />);
    await screen.findByTestId("user-bob");
    const form = screen.getByRole("form", { name: "Add user" });
    fireEvent.change(within(form).getByLabelText("Username"), { target: { value: "carol" } });
    fireEvent.change(within(form).getByLabelText("Password"), { target: { value: "carol's password" } });
    fireEvent.change(within(form).getByLabelText("Confirm password"), { target: { value: "different one!!" } });
    fireEvent.click(within(form).getByRole("button", { name: "Add user" }));
    expect(within(form).getByRole("alert")).toHaveTextContent("do not match");
    expect(sent()).toEqual([]);

    fireEvent.change(within(form).getByLabelText("Confirm password"), { target: { value: "carol's password" } });
    await clickAndSettle(within(form).getByRole("button", { name: "Add user" }));
    expect(screen.getByRole("status")).toHaveTextContent("carol (user) is added");
    const [[url, init]] = sent();
    expect(url).toBe("/api/admin/users");
    expect(JSON.parse(init.body)).toEqual({ username: "carol", email: null, role: "user", password: "carol's password" });
    await waitFor(() => expect(global.fetch.mock.calls.filter(([u, i]) => u === "/api/admin/users" && !i?.method)).toHaveLength(2));
  });
});

describe("Locked usernames (#846)", () => {
  // 17:32 UTC = 14:32 BRT.
  const LOCKED = [USERS[0], { ...USERS[1], locked_until: "2026-11-18T17:32:00.000Z" }];

  function mockLocked(answer) {
    global.fetch = jest.fn(async (url, init = {}) => (url === "/api/admin/users" && !init.method ? reply(200, { users: LOCKED }) : answer));
  }

  test("a locked row shows Locked until with the time in words and Unlock; a row not locked shows neither", async () => {
    mockLocked(reply(200, { unlocked: true }));
    render(<UsersAdmin me="admin" />);
    const bob = await screen.findByTestId("user-bob");
    expect(bob).toHaveTextContent("Locked until 14:32 BRT");
    expect(within(bob).getByRole("button", { name: "Unlock bob" })).toHaveClass("min-h-11");
    const me = screen.getByTestId("user-admin");
    expect(me).not.toHaveTextContent("Locked");
    expect(within(me).queryByRole("button", { name: /Unlock/ })).toBeNull();
  });

  test("Unlock asks first with what follows, then POSTs and refreshes the list", async () => {
    mockLocked(reply(200, { unlocked: true }));
    render(<UsersAdmin me="admin" />);
    fireEvent.click(within(await screen.findByTestId("user-bob")).getByRole("button", { name: "Unlock bob" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent("They can sign in again now. Their failed attempts are forgotten. If someone else is guessing this password, reset it instead.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(sent()).toEqual([]);

    fireEvent.click(within(screen.getByTestId("user-bob")).getByRole("button", { name: "Unlock bob" }));
    await clickAndSettle(within(screen.getByRole("dialog")).getByRole("button", { name: "Unlock" }));
    expect(screen.getByRole("status")).toHaveTextContent("bob is unlocked and can sign in again now.");
    expect(sent()).toEqual([["/api/admin/users/2/unlock", expect.objectContaining({ method: "POST" })]]);
    await waitFor(() => expect(global.fetch.mock.calls.filter(([u, i]) => u === "/api/admin/users" && !i?.method)).toHaveLength(2));
  });

  test("a lock that already ended says there was nothing to unlock", async () => {
    mockLocked(reply(200, { unlocked: false }));
    render(<UsersAdmin me="admin" />);
    fireEvent.click(within(await screen.findByTestId("user-bob")).getByRole("button", { name: "Unlock bob" }));
    await clickAndSettle(within(screen.getByRole("dialog")).getByRole("button", { name: "Unlock" }));
    expect(screen.getByRole("status")).toHaveTextContent("bob was not locked: nothing to unlock.");
  });

  test("lockedLabel", () => {
    expect(lockedLabel({ locked_until: "2026-11-18T17:32:59.000Z" })).toBe("Locked until 14:32 BRT");
    expect(lockedLabel({})).toBeNull();
    expect(lockedLabel(null)).toBeNull();
  });
});

describe("Accounts column and dialog (#760)", () => {
  const ACCOUNTS = [
    { account_id: "acct-a", label: "Account A", environment: "testnet" },
    { account_id: "acct-b", label: "Account B", environment: "testnet" },
  ];
  const LINKED = [...USERS, { id: 3, username: "ana", role: "manager", email: null, created_at: "2026-10-21T10:00:00Z", last_sign_in_at: null, accounts: ["acct-a"] }];

  /** fetch: GET of the list answers `users` and `accounts`; any other call answers `answer`. */
  function mockList({ users = LINKED, accounts = ACCOUNTS, answer = reply(200, {}) } = {}) {
    global.fetch = jest.fn(async (url, init = {}) => (url === "/api/admin/users" && !init.method ? reply(200, { users, accounts }) : answer));
  }

  test("each row says which accounts the person may act on; only managers have Edit accounts", async () => {
    mockList({ users: [...LINKED, { id: 4, username: "uma", role: "user", email: null, created_at: "2026-10-21T10:00:00Z", last_sign_in_at: null, accounts: [] }] });
    render(<UsersAdmin me="admin" />);
    expect(await screen.findByTestId("user-ana")).toHaveTextContent("Account A (acct-a)");
    expect(screen.getByRole("columnheader", { name: "Accounts" })).toBeInTheDocument();
    expect(screen.getByTestId("user-bob")).toHaveTextContent("All accounts (no links)");
    expect(screen.getByTestId("user-admin")).toHaveTextContent("All accounts");
    expect(screen.getByTestId("user-uma")).toHaveTextContent("None: sees only");
    expect(screen.getByRole("button", { name: "Edit accounts of ana" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit accounts of bob" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit accounts of admin" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Edit accounts of uma" })).toBeNull();
  });

  test("the dialog says the consequence, Cancel sends nothing, Save sends the whole list with PUT", async () => {
    mockList({ answer: reply(200, { accounts: ["acct-a", "acct-b"], added: ["acct-b"], removed: [] }) });
    render(<UsersAdmin me="admin" />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit accounts of ana" }));
    const dialog = screen.getByRole("dialog", { name: "Accounts ana manages" });
    expect(within(dialog).getByRole("checkbox", { name: /Account A/ })).toBeChecked();
    expect(within(dialog).getByRole("button", { name: "Save accounts" })).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Account B/ }));
    expect(dialog).toHaveTextContent("ana may close legs and rebalance only on Account A (acct-a), Account B (acct-b); the monitor refuses them on any other account.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(sent()).toEqual([]);

    fireEvent.click(screen.getByRole("button", { name: "Edit accounts of ana" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("checkbox", { name: /Account B/ }));
    await clickAndSettle(screen.getByRole("button", { name: "Save accounts" }));
    expect(screen.getByRole("status")).toHaveTextContent("ana's accounts: added acct-b.");
    const [[url, init]] = sent();
    expect(url).toBe("/api/admin/users/3/accounts");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body)).toEqual({ accounts: ["acct-a", "acct-b"] });
  });

  test("unticking every account says the manager acts on every account again", async () => {
    mockList();
    render(<UsersAdmin me="admin" />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit accounts of ana" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Account A/ }));
    expect(dialog).toHaveTextContent("ana may close legs and rebalance on every account of this deployment, as without links.");
  });

  test("the server's refusal is shown in the dialog", async () => {
    mockList({ answer: reply(400, { error: "account acct-b is not in the account registry" }) });
    render(<UsersAdmin me="admin" />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit accounts of ana" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("checkbox", { name: /Account B/ }));
    await clickAndSettle(screen.getByRole("button", { name: "Save accounts" }));
    expect(within(screen.getByRole("dialog")).getByRole("alert")).toHaveTextContent("account acct-b is not in the account registry");
  });

  test("without a registry the dialog says no account can be linked; a link the registry dropped can still be removed", async () => {
    mockList({ accounts: [] });
    render(<UsersAdmin me="admin" />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit accounts of bob" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("No account registry found, so no account can be linked.");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Edit accounts of ana" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent("no longer in the registry");
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /acct-a/ }));
    expect(within(dialog).getByRole("button", { name: "Save accounts" })).toBeEnabled();
  });

  test("Escape closes the dialog", async () => {
    mockList();
    render(<UsersAdmin me="admin" />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit accounts of ana" }));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("Unused accounts (#910)", () => {
  const NOW = Date.parse("2026-11-27T12:00:00Z");
  const ago = (days) => new Date(NOW - days * 24 * 60 * 60 * 1000).toISOString();
  const ROWS = [
    { id: 1, username: "admin", role: "admin", email: null, created_at: ago(400), last_sign_in_at: ago(0) },
    { id: 2, username: "recent", role: "user", email: null, created_at: ago(200), last_sign_in_at: ago(3) },
    { id: 3, username: "stale", role: "manager", email: null, created_at: ago(300), last_sign_in_at: ago(91) },
    { id: 4, username: "ghost", role: "user", email: null, created_at: ago(120), last_sign_in_at: null },
    { id: 5, username: "newbie", role: "user", email: null, created_at: ago(10), last_sign_in_at: null },
  ];
  const show = async () => {
    global.fetch = jest.fn(async (url, init = {}) => (url === "/api/admin/users" && !init.method ? reply(200, { users: ROWS, accounts: [] }) : reply(200, {})));
    render(<UsersAdmin me="admin" now={NOW} />);
    await screen.findByTestId("user-stale");
  };
  const names = () => screen.getAllByTestId(/^user-/).map((r) => r.dataset.testid.slice(5));

  test("Scenario: a person who signed in recently — the row shows that date and no unused tag", async () => {
    await show();
    const row = screen.getByTestId("user-recent");
    expect(row).not.toHaveTextContent("never");
    expect(row).not.toHaveTextContent(/Unused|Never signed in/);
  });

  test("Scenario: an account unused for 90 days — the tag, and the Unused filter lists that row in the list's order", async () => {
    await show();
    expect(screen.getByTestId("user-stale")).toHaveTextContent("Unused for 90 days");
    const chip = screen.getByRole("button", { name: "Unused (2)" });
    expect(chip).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(chip);
    expect(chip).toHaveAttribute("aria-pressed", "true");
    expect(names()).toEqual(["stale", "ghost"]);
    fireEvent.click(chip);
    expect(names()).toEqual(["admin", "recent", "stale", "ghost", "newbie"]);
  });

  test("Scenario: an account that never signed in — tagged once older than 90 days, not before", async () => {
    await show();
    const ghost = screen.getByTestId("user-ghost");
    expect(ghost).toHaveTextContent("never");
    expect(ghost).toHaveTextContent("Never signed in, created 120 days ago");
    const newbie = screen.getByTestId("user-newbie");
    expect(newbie).toHaveTextContent("never");
    expect(newbie).not.toHaveTextContent(/Unused|Never signed in/);
  });

  test("Scenario: nothing changes by itself — showing and filtering send no request beyond the list", async () => {
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Unused (2)" }));
    expect(sent()).toEqual([]);
    expect(global.fetch).toHaveBeenCalledTimes(1);
  });

  test("without a `now` the tags use the time the list was read", async () => {
    const real = Date.now();
    const old = { id: 9, username: "old", role: "user", email: null, created_at: new Date(real - 400 * 86400000).toISOString(), last_sign_in_at: null };
    global.fetch = jest.fn(async () => reply(200, { users: [old], accounts: [] }));
    render(<UsersAdmin me="admin" />);
    expect(await screen.findByTestId("user-old")).toHaveTextContent("Never signed in, created 400 days ago");
  });

  test("the Unused filter with nothing flagged says so in words, never an empty table", async () => {
    global.fetch = jest.fn(async () => reply(200, { users: ROWS.slice(0, 2), accounts: [] }));
    render(<UsersAdmin me="admin" now={NOW} />);
    fireEvent.click(await screen.findByRole("button", { name: "Unused (0)" }));
    expect(screen.getByText(/No account is unused/)).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  test("unusedLabel: the 90-day boundary, never signed in, unreadable times", () => {
    expect(unusedLabel({ last_sign_in_at: ago(89), created_at: ago(500) }, NOW)).toBeNull();
    expect(unusedLabel({ last_sign_in_at: ago(90), created_at: ago(500) }, NOW)).toBe("Unused for 90 days");
    expect(unusedLabel({ last_sign_in_at: null, created_at: ago(89) }, NOW)).toBeNull();
    expect(unusedLabel({ last_sign_in_at: null, created_at: ago(90) }, NOW)).toBe("Never signed in, created 90 days ago");
    expect(unusedLabel({ last_sign_in_at: "garbage", created_at: ago(500) }, NOW)).toBeNull();
    expect(unusedLabel({ last_sign_in_at: null, created_at: null }, NOW)).toBeNull();
    expect(unusedLabel(null, NOW)).toBeNull();
    expect(UNUSED_DAYS).toBe(90);
  });
});

describe("header link", () => {
  test("the Account menu shows Users to admins only", () => {
    const { unmount } = render(<UserMenu role="admin" />);
    fireEvent.click(screen.getByRole("button", { name: "Account ▾" }));
    expect(screen.getByRole("menuitem", { name: "Users" })).toHaveAttribute("href", "/admin/users");
    unmount();
    for (const role of ["manager", "user", null]) {
      const r = render(<UserMenu role={role} />);
      fireEvent.click(screen.getByRole("button", { name: "Account ▾" }));
      expect(screen.queryByRole("menuitem", { name: "Users" })).toBeNull();
      r.unmount();
    }
  });
});

describe("lib/users", () => {
  test("form checks", () => {
    const ok = { username: "carol", role: "user", password: "x".repeat(12), confirm: "x".repeat(12) };
    expect(newUserProblem(ok)).toBeNull();
    expect(newUserProblem({ ...ok, username: "ab" })).toMatch(/Username/);
    expect(newUserProblem({ ...ok, username: "no spaces" })).toMatch(/Username/);
    expect(newUserProblem({ ...ok, role: "root" })).toBe("Pick a role");
    expect(passwordFormProblem({ password: "x".repeat(11), confirm: "x".repeat(11) })).toMatch(/Too short/);
    expect(passwordFormProblem({ password: "x".repeat(12), confirm: "y".repeat(12) })).toMatch(/do not match/);
  });

  test("each confirmation says what follows, and speaks to you about your own account", () => {
    const bob = { id: 2, username: "bob", role: "manager" };
    expect(confirmText("role", bob, { role: "user", me: "admin" }).body).toBe("bob: manager → user. All of bob's sessions end: they sign in again with the new role.");
    expect(confirmText("role", { ...bob, username: "admin" }, { role: "user", me: "admin" }).body).toMatch(/All your sessions end: you sign in again/);
    expect(confirmText("delete", bob).button).toBe("Delete user");
    expect(confirmText("signout", bob).title).toBe("Sign bob out everywhere");
    expect(() => confirmText("nope", bob)).toThrow();
  });
});
