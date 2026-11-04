import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import AuditPage from "@/app/admin/audit/page";
import AuditChainStatus from "@/components/AuditChainStatus";
import { viewer } from "@/lib/auth/viewer";

// #619: the Audit page says in words whether the audit trail's hash chain is intact.

jest.mock("@/lib/auth/viewer", () => ({ viewer: jest.fn() }));

const reply = (status, body = {}) => ({ ok: status < 300, status, headers: new Headers(), json: async () => body });
const realFetch = global.fetch;
afterEach(() => {
  global.fetch = realFetch;
});

const INTACT = { off: false, ok: true, checked: 1250, legacy: 0, legacy_before: null, first_break: null };
const BROKEN = {
  off: false,
  ok: false,
  checked: 19,
  legacy: 0,
  legacy_before: null,
  first_break: { file: "audit.jsonl", archive: false, line: 20, time: "2026-10-21T09:00:00.000Z", reason: "edited" },
};
const ENTRIES = [
  { time: "2026-10-21T09:00:00.000Z", action: "manual_close", username: "alice", role: "manager", bot: "btc", prev: "a".repeat(64), line_hash: "b".repeat(64) },
  { time: "2026-10-20T10:00:00.000Z", action: "role_changed", username: "root", role: "admin", target: "bob", prev: "", line_hash: "c".repeat(64) },
];

/** fetch answering the verify route with `chain` (a body, or a function of the call number) and the audit route with ENTRIES. */
function routes(chain) {
  let n = 0;
  return jest.fn(async (url) => {
    if (url === "/api/admin/audit/verify") return typeof chain === "function" ? chain(n++) : reply(200, chain);
    return reply(200, { off: false, entries: ENTRIES, skipped: 0, next_before: null });
  });
}

describe("Scenario: the page says it in words", () => {
  test("a broken chain: the page says where it breaks and still lists every line", async () => {
    viewer.mockResolvedValue({ username: "root", role: "admin" });
    global.fetch = routes(BROKEN);
    render(await AuditPage());
    const chain = await screen.findByText(/Chain broken at line 20/);
    expect(chain).toHaveTextContent("Chain broken at line 20 (2026-10-21 06:00:00 BRT): this line was edited. 19 lines before it checked intact.");
    expect(chain.closest("[role=alert]")).not.toBeNull();
    expect(await screen.findAllByTestId("audit-row")).toHaveLength(2);
    // The chain's own fields are not shown as detail.
    expect(screen.queryByText(/line_hash|prev=/)).toBeNull();
  });

  test("an intact chain says how many lines were checked; legacy lines are named with their time", async () => {
    global.fetch = routes({ ...INTACT, legacy: 3, legacy_before: "2026-10-20T10:00:00.000Z" });
    render(<AuditChainStatus />);
    expect(await screen.findByRole("status")).toHaveTextContent("Chain intact — 1,250 lines checked. 3 legacy lines before 2026-10-20 07:00:00 BRT are not covered.");
  });

  test("checking, then Check again asks the route once more", async () => {
    global.fetch = routes((n) => reply(200, n === 0 ? INTACT : BROKEN));
    render(<AuditChainStatus />);
    expect(screen.getByText("Checking the audit chain…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Check again" })).toBeDisabled();
    expect(await screen.findByRole("status")).toHaveTextContent("Chain intact");
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Chain broken at line 20");
    expect(global.fetch).toHaveBeenCalledTimes(2);
  });

  test("an error is said in words and Check again stays available", async () => {
    global.fetch = routes(() => reply(500, { error: "the audit trail could not be checked: see the monitor's logs" }));
    render(<AuditChainStatus />);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("The chain could not be checked: the audit trail could not be checked");
    expect(screen.getByRole("button", { name: "Check again" })).toBeEnabled();
  });

  test("an in-memory trail (off) shows no chain line: the trail itself says it is off", async () => {
    global.fetch = routes({ off: true, ok: null, checked: 0, legacy: 0, legacy_before: null, first_break: null });
    const { container } = render(<AuditChainStatus />);
    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await waitFor(() => expect(within(container).queryByTestId("audit-chain")).toBeNull());
  });

  test("a manager sees no chain line and nothing is asked", async () => {
    viewer.mockResolvedValue({ username: "mia", role: "manager" });
    global.fetch = routes(INTACT);
    render(await AuditPage());
    expect(screen.queryByTestId("audit-chain")).toBeNull();
    expect(global.fetch).not.toHaveBeenCalled();
  });
});
