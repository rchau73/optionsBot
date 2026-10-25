import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import AuditPage from "@/app/admin/audit/page";
import AuditTrail from "@/components/AuditTrail";
import ResetPasswordForm from "@/components/ResetPasswordForm";
import UserMenu from "@/components/UserMenu";
import UsersAdmin from "@/components/UsersAdmin";
import { viewer } from "@/lib/auth/viewer";

// #307 (+#214) the reset link on the Users page and the /reset form; #308 (+#249) the Audit page.

jest.mock("@/lib/auth/viewer", () => ({ viewer: jest.fn() }));

const reply = (status, body = {}) => ({ ok: status < 300, status, headers: new Headers(), json: async () => body });
const realFetch = global.fetch;
afterEach(() => {
  global.fetch = realFetch;
});
const sent = () => global.fetch.mock.calls.filter(([, init]) => init?.method);

const USERS = [
  { id: 1, username: "root", role: "admin", email: null, created_at: "2026-10-01T10:00:00Z", last_sign_in_at: null },
  { id: 2, username: "ana", role: "user", email: null, created_at: "2026-10-20T10:00:00Z", last_sign_in_at: null },
];
const LINK = { url: "http://localhost:3000/reset?token=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ", expires_at: "2026-10-25T12:30:00.000Z" };

describe("Users page: Reset link", () => {
  test("asks first, then shows the link once with its expiry and a copy button; your own row has it off", async () => {
    global.fetch = jest.fn(async (url, init = {}) => (url === "/api/admin/users" && !init.method ? reply(200, { users: USERS }) : reply(200, LINK)));
    const writeText = jest.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    render(<UsersAdmin me="root" />);

    expect(within(await screen.findByTestId("user-root")).getByRole("button", { name: "Reset link" })).toBeDisabled();
    fireEvent.click(within(screen.getByTestId("user-ana")).getByRole("button", { name: "Reset link" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent("works once, for 30 minutes");
    expect(sent()).toEqual([]);

    fireEvent.click(within(dialog).getByRole("button", { name: "Create link" }));
    expect(await screen.findByDisplayValue(LINK.url)).toBeInTheDocument();
    expect(dialog).toHaveTextContent("2026-10-25 09:30:00 BRT");
    expect(sent()).toEqual([["/api/admin/users/2/reset-link", expect.objectContaining({ method: "POST" })]]);

    fireEvent.click(within(dialog).getByRole("button", { name: "Copy link" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(LINK.url));
    fireEvent.click(within(dialog).getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByDisplayValue(LINK.url)).toBeNull();
    expect(await screen.findByText(/A reset link for ana was created/)).toBeInTheDocument();
  });

  test("the server's refusal is shown in the dialog", async () => {
    global.fetch = jest.fn(async (url, init = {}) => (url === "/api/admin/users" && !init.method ? reply(200, { users: USERS }) : reply(404, { error: "no such user" })));
    render(<UsersAdmin me="root" />);
    fireEvent.click(within(await screen.findByTestId("user-ana")).getByRole("button", { name: "Reset link" }));
    fireEvent.click(screen.getByRole("button", { name: "Create link" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("no such user");
  });
});

describe("/reset form", () => {
  const fill = (password, confirm = password) => {
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: password } });
    fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: confirm } });
    fireEvent.click(screen.getByRole("button", { name: "Set password" }));
  };

  test("without a token it says the link is incomplete and has no form", () => {
    render(<ResetPasswordForm token="" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Ask an admin for a new reset link");
    expect(screen.queryByLabelText("New password")).toBeNull();
  });

  test("a short or mismatched password is refused before sending; a good one sends token and password, then says sign in", async () => {
    global.fetch = jest.fn(async () => reply(200, {}));
    window.history.replaceState(null, "", "/reset?token=tok");
    render(<ResetPasswordForm token="tok" />);
    expect(window.location.search).toBe("");

    fill("short");
    expect(screen.getByRole("alert")).toHaveTextContent("at least 12 characters");
    fill("a long enough password", "another one entirely");
    expect(screen.getByRole("alert")).toHaveTextContent("do not match");
    expect(global.fetch).not.toHaveBeenCalled();

    fill("a long enough password");
    expect(await screen.findByRole("status")).toHaveTextContent("sign in with the new password");
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
    const [[url, init]] = global.fetch.mock.calls;
    expect(url).toBe("/api/auth/reset");
    expect(JSON.parse(init.body)).toEqual({ token: "tok", next: "a long enough password" });
  });

  test("a used or expired link shows the server's one answer", async () => {
    global.fetch = jest.fn(async () => reply(400, { error: "link expired or already used" }));
    render(<ResetPasswordForm token="tok" />);
    fill("a long enough password");
    expect(await screen.findByRole("alert")).toHaveTextContent("link expired or already used");
  });
});

describe("Audit page", () => {
  const ENTRIES = [
    { time: "2026-10-21T09:00:00.000Z", action: "manual_close", username: "alice", role: "manager", bot: "btc", position_ids: ["p1"], status: 200 },
    { time: "2026-10-20T10:00:00.000Z", action: "role_changed", username: "root", role: "admin", target: "bob", from: "user", to: "manager" },
  ];

  test("lists the lines newest first with time in BRT, person, action, target or bot and detail; filters go to the query", async () => {
    global.fetch = jest.fn(async () => reply(200, { off: false, entries: ENTRIES, skipped: 0, next_before: null }));
    render(<AuditTrail />);
    const rows = await screen.findAllByTestId("audit-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("2026-10-21 06:00:00 BRT");
    expect(rows[0]).toHaveTextContent("alice");
    expect(rows[0]).toHaveTextContent("manual_close");
    expect(rows[0]).toHaveTextContent("btc");
    expect(rows[0]).toHaveTextContent('position_ids=["p1"] status=200');
    expect(rows[1]).toHaveTextContent("bob");
    expect(global.fetch.mock.calls[0][0]).toBe("/api/admin/audit");

    fireEvent.change(screen.getByLabelText("Person"), { target: { value: "alice" } });
    fireEvent.change(screen.getByLabelText("Action"), { target: { value: "manual_close" } });
    fireEvent.change(screen.getByLabelText("From (UTC day)"), { target: { value: "2026-10-20" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(global.fetch).toHaveBeenCalledTimes(2));
    expect(global.fetch.mock.calls[1][0]).toBe("/api/admin/audit?user=alice&action=manual_close&from=2026-10-20");
  });

  test("damaged lines are counted, Older loads the next page with the cursor", async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValueOnce(reply(200, { off: false, entries: [ENTRIES[0]], skipped: 1, next_before: 512 }))
      .mockResolvedValueOnce(reply(200, { off: false, entries: [ENTRIES[1]], skipped: 0, next_before: null }));
    render(<AuditTrail />);
    expect(await screen.findByText("1 damaged line was skipped.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Older" }));
    await waitFor(() => expect(screen.getAllByTestId("audit-row")).toHaveLength(2));
    expect(global.fetch.mock.calls[1][0]).toBe("/api/admin/audit?before=512");
    expect(screen.queryByRole("button", { name: "Older" })).toBeNull();
  });

  test("an in-memory database says the audit trail is off, not empty", async () => {
    global.fetch = jest.fn(async () => reply(200, { off: true, entries: [], skipped: 0, next_before: null }));
    render(<AuditTrail />);
    expect(await screen.findByRole("status")).toHaveTextContent("The audit trail is off");
    expect(screen.queryByText("No audit lines match these filters.")).toBeNull();
  });

  test("an empty trail says so; a refusal is shown as an error", async () => {
    global.fetch = jest.fn(async () => reply(200, { off: false, entries: [], skipped: 0, next_before: null }));
    const r = render(<AuditTrail />);
    expect(await screen.findByText("No audit lines match these filters.")).toBeInTheDocument();
    r.unmount();
    global.fetch = jest.fn(async () => reply(403, { error: "admin role required" }));
    render(<AuditTrail />);
    expect(await screen.findByRole("alert")).toHaveTextContent("could not be loaded: admin role required");
  });

  test("a manager or a user sees no audit section; the menu links Audit for admins only", async () => {
    global.fetch = jest.fn(async () => reply(200, { off: false, entries: [], skipped: 0, next_before: null }));
    for (const role of ["manager", "user"]) {
      viewer.mockResolvedValue({ username: "mia", role });
      const r = render(await AuditPage());
      expect(screen.getByRole("alert")).toHaveTextContent("Only an admin reads the audit trail");
      expect(screen.queryByText("Audit trail")).toBeNull();
      r.unmount();
    }
    expect(global.fetch).not.toHaveBeenCalled();

    const admin = render(<UserMenu role="admin" />);
    fireEvent.click(screen.getByRole("button", { name: "Account ▾" }));
    expect(screen.getByRole("menuitem", { name: "Audit" })).toHaveAttribute("href", "/admin/audit");
    admin.unmount();
    for (const role of ["manager", "user", null]) {
      const r = render(<UserMenu role={role} />);
      fireEvent.click(screen.getByRole("button", { name: "Account ▾" }));
      expect(screen.queryByRole("menuitem", { name: "Audit" })).toBeNull();
      r.unmount();
    }
  });
});
