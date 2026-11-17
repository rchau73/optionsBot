import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import UsersAdmin from "@/components/UsersAdmin";
import UserMenu from "@/components/UserMenu";
import { confirmText, newUserProblem, passwordFormProblem } from "@/lib/users";

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
