import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import AuditPage from "@/app/admin/audit/page";
import AuditTrail from "@/components/AuditTrail";
import { viewer } from "@/lib/auth/viewer";

// #702: Export CSV on the Audit page downloads the lines of the filters applied.

jest.mock("@/lib/auth/viewer", () => ({ viewer: jest.fn() }));

const LIST = { off: false, entries: [{ time: "2026-10-21T09:00:00.000Z", action: "signed_in", username: "bob", role: "user" }], skipped: 0, next_before: null };

const csv = ({ lines = 1, cut = false, skipped = 0 } = {}) => ({
  ok: true,
  status: 200,
  headers: new Headers({
    "content-disposition": 'attachment; filename="audit-trail-2026-11-07.csv"',
    "x-audit-lines": String(lines),
    "x-audit-cut": String(cut),
    "x-audit-skipped": String(skipped),
  }),
  blob: async () => new Blob(["time\r\n"]),
});

/** fetch answering the list with `list` and the export with `exported`. */
function routes(list, exported) {
  return jest.fn(async (url) => {
    if (url.startsWith("/api/admin/audit/export")) return exported();
    return { ok: true, status: 200, headers: new Headers(), json: async () => list };
  });
}

const exportCalls = () => global.fetch.mock.calls.map(([url]) => url).filter((u) => u.startsWith("/api/admin/audit/export"));

const realFetch = global.fetch;
let saved;
beforeAll(() => {
  // jsdom has neither; the page revokes the address a moment after the click.
  URL.createObjectURL = jest.fn(() => "blob:audit");
  URL.revokeObjectURL = jest.fn();
});
beforeEach(() => {
  saved = [];
  jest.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function click() {
    saved.push({ href: this.href, download: this.download });
  });
});
afterEach(() => {
  global.fetch = realFetch;
  jest.restoreAllMocks();
});

const button = () => screen.findByRole("button", { name: "Export CSV" });

describe("Scenario: admin exports a filtered period", () => {
  test("Export CSV asks for the filters applied and saves the file under the server's name", async () => {
    global.fetch = routes(LIST, () => csv({ lines: 2 }));
    render(<AuditTrail />);
    await screen.findByTestId("audit-row");
    fireEvent.change(screen.getByLabelText("Person"), { target: { value: "bob" } });
    fireEvent.change(screen.getByLabelText("From (UTC day)"), { target: { value: "2026-10-19" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await screen.findByTestId("audit-row");

    fireEvent.click(await button());
    expect(await screen.findByRole("status")).toHaveTextContent("Exported 2 lines.");
    expect(exportCalls()).toEqual(["/api/admin/audit/export?user=bob&from=2026-10-19"]);
    expect(saved).toEqual([{ href: "blob:audit", download: "audit-trail-2026-11-07.csv" }]);
  });

  test("a filter typed but not applied is not exported: the file is what the table shows", async () => {
    global.fetch = routes(LIST, () => csv());
    render(<AuditTrail />);
    await screen.findByTestId("audit-row");
    fireEvent.change(screen.getByLabelText("Person"), { target: { value: "bob" } });
    fireEvent.click(await button());
    await screen.findByRole("status");
    expect(exportCalls()).toEqual(["/api/admin/audit/export"]);
  });

  test("while it runs the button says so and is disabled; a new filter clears the last result", async () => {
    let finish;
    global.fetch = routes(LIST, () => new Promise((resolve) => (finish = resolve)));
    render(<AuditTrail />);
    await screen.findByTestId("audit-row");
    fireEvent.click(await button());
    expect(await screen.findByRole("button", { name: "Exporting…" })).toBeDisabled();
    finish(csv());
    await screen.findByRole("status");

    fireEvent.change(screen.getByLabelText("Person"), { target: { value: "bob" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await screen.findByTestId("audit-row");
    expect(screen.queryByRole("status")).toBeNull();
  });

  test("the button waits for the list", async () => {
    global.fetch = routes(LIST, () => csv());
    render(<AuditTrail />);
    expect(screen.getByRole("button", { name: "Export CSV" })).toBeDisabled();
    await waitFor(() => expect(screen.getByRole("button", { name: "Export CSV" })).toBeEnabled());
  });

  test("a refused export is said in words, nothing is saved and the button stays", async () => {
    global.fetch = routes(LIST, () => ({ ok: false, status: 500, headers: new Headers(), json: async () => ({ error: "the audit trail could not be read: see the monitor's logs" }) }));
    render(<AuditTrail />);
    await screen.findByTestId("audit-row");
    fireEvent.click(await button());
    expect(await screen.findByRole("alert")).toHaveTextContent("The export failed: the audit trail could not be read");
    expect(saved).toEqual([]);
    expect(await button()).toBeEnabled();
  });
});

describe("Scenario: a very long trail is capped", () => {
  test("the page says the file was cut and how to get the rest", async () => {
    global.fetch = routes(LIST, () => csv({ lines: 50000, cut: true }));
    render(<AuditTrail />);
    await screen.findByTestId("audit-row");
    fireEvent.click(await button());
    expect(await screen.findByRole("status")).toHaveTextContent("The export was cut: the file holds the newest 50,000 lines. Narrow the filters to export the older ones.");
  });
});

describe("Scenario: in-memory trail", () => {
  test("the button is disabled with the trail is off", async () => {
    global.fetch = routes({ off: true, entries: [], skipped: 0, next_before: null }, () => csv());
    render(<AuditTrail />);
    await screen.findByText(/The audit trail is off/);
    const exportButton = await button();
    expect(exportButton).toBeDisabled();
    expect(exportButton).toHaveAccessibleDescription("Export is not available: the trail is off.");
    fireEvent.click(exportButton);
    expect(exportCalls()).toEqual([]);
  });
});

describe("Scenario: not an admin", () => {
  test("a manager's page has no button and asks nothing", async () => {
    viewer.mockResolvedValue({ username: "mia", role: "manager" });
    global.fetch = routes(LIST, () => csv());
    render(await AuditPage());
    expect(screen.queryByRole("button", { name: "Export CSV" })).toBeNull();
    expect(global.fetch).not.toHaveBeenCalled();
  });
});
