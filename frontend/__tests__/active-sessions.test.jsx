import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import ActiveSessionsModal, { TAKES_EFFECT } from "@/components/ActiveSessions";
import UserMenu from "@/components/UserMenu";

// #584: Account ▾ → Active sessions.

const HERE = { id: "aaaaaaaaaaaaaaaa", browser: "Chrome on Windows", created_at: "2026-11-01T12:00:00Z", last_used_at: "2026-11-03T09:00:00Z", current: true };
const PHONE = { id: "bbbbbbbbbbbbbbbb", browser: "Safari on iOS", created_at: "2026-10-30T08:00:00Z", last_used_at: "2026-11-02T20:00:00Z", current: false };

function deps(sessions = [HERE, PHONE], over = {}) {
  let rows = sessions;
  return {
    listSessions: jest.fn(async () => rows),
    endSession: jest.fn(async (id) => {
      rows = rows.filter((s) => s.id !== id);
    }),
    ...over,
  };
}

describe("Active sessions dialog", () => {
  test("the person's menu opens it and closes the menu", async () => {
    const realFetch = global.fetch;
    global.fetch = jest.fn(async () => ({ ok: true, status: 200, headers: new Headers(), json: async () => ({ sessions: [HERE] }) }));
    render(<UserMenu username="bob" />);
    fireEvent.click(screen.getByRole("button", { name: "bob ▾" }));
    fireEvent.click(within(screen.getByRole("menu")).getByRole("menuitem", { name: "Active sessions" }));
    expect(screen.queryByRole("menu")).toBeNull();
    expect(await screen.findByText("You are signed in only here.")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Active sessions" })).toHaveTextContent("This browser");
    expect(global.fetch).toHaveBeenCalledWith("/api/auth/sessions", expect.objectContaining({ cache: "no-store" }));
    global.fetch = realFetch;
  });

  test("Scenario: list own sessions — two rows, the current one marked and without a Sign out button", async () => {
    render(<ActiveSessionsModal onClose={() => {}} deps={deps()} />);
    const list = await screen.findByRole("list", { name: "Your sessions" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Chrome on Windows");
    expect(rows[0]).toHaveTextContent("This browser");
    expect(within(rows[0]).queryByRole("button")).toBeNull();
    expect(within(rows[1]).getByRole("button", { name: /^Sign out Safari on iOS/ })).toHaveClass("min-h-11");
    expect(rows[1]).not.toHaveTextContent("This browser");
    expect(screen.getByRole("dialog")).toHaveTextContent("No address is kept or shown.");
  });

  test("Scenario: end another session — says when it takes effect and lists what is left", async () => {
    const d = deps();
    render(<ActiveSessionsModal onClose={() => {}} deps={d} />);
    fireEvent.click(await screen.findByRole("button", { name: /^Sign out Safari on iOS/ }));
    expect(await screen.findByRole("status")).toHaveTextContent("goes to the sign-in page within 15 minutes");
    expect(d.endSession).toHaveBeenCalledWith(PHONE.id);
    await waitFor(() => expect(within(screen.getByRole("list", { name: "Your sessions" })).getAllByRole("listitem")).toHaveLength(1));
    expect(screen.getByRole("dialog")).toHaveTextContent(TAKES_EFFECT);
  });

  test("a refused sign-out shows the server's reason and keeps the row", async () => {
    const d = deps([HERE, PHONE], {
      endSession: jest.fn(async () => {
        throw new Error("no such session");
      }),
    });
    render(<ActiveSessionsModal onClose={() => {}} deps={d} />);
    fireEvent.click(await screen.findByRole("button", { name: /^Sign out Safari on iOS/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("no such session");
    expect(within(screen.getByRole("list", { name: "Your sessions" })).getAllByRole("listitem")).toHaveLength(2);
  });

  test("loading, then an error the person can read", async () => {
    let fail;
    const d = deps([], { listSessions: jest.fn(() => new Promise((_, reject) => (fail = reject))) });
    render(<ActiveSessionsModal onClose={() => {}} deps={d} />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading your sessions…");
    await act(async () => fail(new Error("503")));
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load your sessions: 503");
  });

  test("Escape and Close window close it", async () => {
    const onClose = jest.fn();
    render(<ActiveSessionsModal onClose={onClose} deps={deps()} />);
    await screen.findByRole("list", { name: "Your sessions" });
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: "Close window" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
