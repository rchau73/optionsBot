import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import ActiveSessionsModal, { TAKES_EFFECT } from "@/components/ActiveSessions";
import UserMenu from "@/components/UserMenu";
import { formatDateTime } from "@/lib/format";
import { attemptsSentence, failedBucket, lockedLine, NOT_YOU, securityItem, summaryLine } from "@/lib/signInSummary";
import { track } from "@/lib/track";
import { longSentences, undefinedJargon } from "@/test/plainWords";

jest.mock("@/lib/track", () => ({ track: jest.fn(() => true) }));

// #584: Account ▾ → Active sessions; #803: its sign-in summary line.

const HERE = { id: "aaaaaaaaaaaaaaaa", browser: "Chrome on Windows", created_at: "2026-11-01T12:00:00Z", last_used_at: "2026-11-03T09:00:00Z", current: true };
const PHONE = { id: "bbbbbbbbbbbbbbbb", browser: "Safari on iOS", created_at: "2026-10-30T08:00:00Z", last_used_at: "2026-11-02T20:00:00Z", current: false };
const YESTERDAY = "2026-11-15T09:00:00Z";
const CLEAN = { previous_sign_in: YESTERDAY, first_sign_in: false, failed_since: 0, notice_id: null };
const THREE = { previous_sign_in: YESTERDAY, first_sign_in: false, failed_since: 3, notice_id: "security:1763280000:3" };
const FIRST = { previous_sign_in: null, first_sign_in: true, failed_since: 0, notice_id: null };

function deps(sessions = [HERE, PHONE], over = {}) {
  let rows = sessions;
  return {
    listSessions: jest.fn(async () => rows),
    endSession: jest.fn(async (id) => {
      rows = rows.filter((s) => s.id !== id);
    }),
    securitySummary: jest.fn(async () => CLEAN),
    markNoticeRead: jest.fn(async () => ({})),
    ...over,
  };
}

const summaryBox = () => screen.findByTestId("sign-in-summary");

beforeEach(() => track.mockClear());

describe("Active sessions dialog", () => {
  test("the person's menu opens it and closes the menu", async () => {
    const realFetch = global.fetch;
    global.fetch = jest.fn(async (url) => ({
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => (url === "/api/auth/security-summary" ? CLEAN : { sessions: [HERE] }),
    }));
    render(<UserMenu username="bob" />);
    fireEvent.click(screen.getByRole("button", { name: "bob ▾" }));
    fireEvent.click(within(screen.getByRole("menu")).getByRole("menuitem", { name: "Active sessions" }));
    expect(screen.queryByRole("menu")).toBeNull();
    expect(await screen.findByText("You are signed in only here.")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Active sessions" })).toHaveTextContent("This browser");
    expect(global.fetch).toHaveBeenCalledWith("/api/auth/sessions", expect.objectContaining({ cache: "no-store" }));
    expect(global.fetch).toHaveBeenCalledWith("/api/auth/security-summary", expect.objectContaining({ cache: "no-store" }));
    expect(await summaryBox()).toHaveTextContent("0 wrong-password attempts since.");
    // Change your password closes this dialog and opens that one.
    fireEvent.click(screen.getByRole("button", { name: "Change your password" }));
    expect(screen.queryByRole("dialog", { name: "Active sessions" })).toBeNull();
    expect(screen.getByRole("dialog")).toHaveTextContent(/password/i);
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

  test("Scenario: no failed attempts — the previous sign-in and 0 attempts, nothing marked read", async () => {
    const d = deps();
    render(<ActiveSessionsModal onClose={() => {}} deps={d} />);
    const box = await summaryBox();
    expect(box).toHaveTextContent(`Last sign-in: ${formatDateTime(YESTERDAY)}. 0 wrong-password attempts since.`);
    expect(box).toHaveTextContent(NOT_YOU);
    expect(d.markNoticeRead).not.toHaveBeenCalled();
    expect(track).toHaveBeenCalledWith("security_summary_seen", { failed_since: "0" });
  });

  test("Scenario: failed attempts since the last sign-in — 3 attempts, the advice, the bell's item marked read", async () => {
    const d = deps([HERE], { securitySummary: jest.fn(async () => THREE) });
    render(<ActiveSessionsModal onClose={() => {}} deps={d} />);
    const box = await summaryBox();
    expect(box).toHaveTextContent("3 wrong-password attempts since.");
    expect(box).toHaveTextContent("Not you? Change your password and sign out the other browsers.");
    expect(box).toHaveClass("border-warn");
    await waitFor(() => expect(d.markNoticeRead).toHaveBeenCalledWith(THREE.notice_id));
    expect(track).toHaveBeenCalledWith("security_summary_seen", { failed_since: "2_4" });
  });

  test("Scenario: first ever sign-in — no count is shown", async () => {
    render(<ActiveSessionsModal onClose={() => {}} deps={deps([HERE], { securitySummary: jest.fn(async () => FIRST) })} />);
    const box = await summaryBox();
    expect(box).toHaveTextContent("This is your first sign-in.");
    expect(box).not.toHaveTextContent("attempt");
    expect(track).toHaveBeenCalledWith("security_summary_seen", {});
  });

  test("a locked username says until when; a summary that cannot be read leaves the sessions working", async () => {
    const locked = { ...THREE, failed_since: 5, locked_until: "2026-11-16T10:15:00Z" };
    const { unmount } = render(<ActiveSessionsModal onClose={() => {}} deps={deps([HERE], { securitySummary: jest.fn(async () => locked) })} />);
    expect(await summaryBox()).toHaveTextContent(`Your username is locked until ${formatDateTime(locked.locked_until)} after too many wrong passwords.`);
    unmount();

    const failing = deps([HERE, PHONE], { securitySummary: jest.fn(async () => Promise.reject(new Error("503"))) });
    render(<ActiveSessionsModal onClose={() => {}} deps={failing} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not read your last sign-in: 503");
    expect(within(await screen.findByRole("list", { name: "Your sessions" })).getAllByRole("listitem")).toHaveLength(2);
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

describe("the sign-in summary's words (#803)", () => {
  test("one attempt is singular; a not-recorded previous sign-in says so", () => {
    expect(attemptsSentence(1)).toBe("1 wrong-password attempt since.");
    expect(summaryLine({ ...CLEAN, previous_sign_in: null })).toBe("Your sign-in before this one was not recorded. 0 wrong-password attempts since.");
    expect(lockedLine(CLEAN)).toBeNull();
  });

  test("the count travels as a bucket only", () => {
    expect([0, 1, 2, 4, 5, 40].map(failedBucket)).toEqual(["0", "1", "2_4", "2_4", "5_plus", "5_plus"]);
  });

  test("the bell's item: one Needs you item with a notice id, none without", () => {
    const item = securityItem({ ...THREE, failed_since: 1 }, Date.UTC(2026, 10, 16));
    expect(item).toEqual(expect.objectContaining({ id: THREE.notice_id, kind: "security", title: "1 wrong-password attempt on your account" }));
    expect(securityItem(CLEAN, Date.now())).toBeNull();
  });

  test("plain words: short sentences, no jargon", () => {
    const texts = [summaryLine(THREE), summaryLine(FIRST), lockedLine({ locked_until: YESTERDAY }), NOT_YOU, securityItem(THREE, Date.now()).text];
    for (const t of texts) {
      expect([t, longSentences(t)]).toEqual([t, []]);
      expect([t, undefinedJargon(t, "sign-in")]).toEqual([t, []]);
    }
  });
});
