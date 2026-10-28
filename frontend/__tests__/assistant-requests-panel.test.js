import { act, fireEvent, render, screen, within } from "@testing-library/react";
import AssistantProvider from "@/components/AssistantProvider";
import { MenuNav } from "@/components/MenuBar";
import { CHECKLIST, DRAFT_HEADING, DRAFT_NOTICE, DRAFT_TEXT, NEEDS_INFO_TEXT, sentText } from "@/lib/assistantRequests";

// The panel's request pieces (#409): the checklist under a report, the draft
// card (Send for review · Edit · Discard) and the My requests tab — against a
// fake fetch of the monitor's own routes. Nothing is sent without the click.

jest.mock("@/lib/track", () => ({ track: jest.fn() }));

const json = (status, body) => ({ ok: status >= 200 && status < 300, status, headers: { get: () => null }, json: async () => body });

const CHECK = (unticked) => CHECKLIST.map((c) => ({ ...c, ok: !unticked.includes(c.key) }));
const DRAFT_ANSWER = {
  id: "a9",
  answer: DRAFT_TEXT,
  outcome: "draft",
  citations: [
    { id: "pnl:btc/2026-10-27T14:00:00Z", source: "pnl:btc", title: "btc P&L history at 2026-10-27T14:00:00Z" },
    { id: "journal:btc", source: "journal:btc", title: "btc journal" },
  ],
  checklist: CHECK([]),
  draft: {
    kind: "bug",
    title: "P&L spike at 14:00 UTC",
    what: "The total P&L dropped for one hour",
    where: "Overview · btc · 2026-10-27 14:00 UTC",
    expected: "No drop without a journal entry",
    evidence: [
      { text: "Total P&L -0.048 at 14:00 UTC", citation: "pnl:btc/2026-10-27T14:00:00Z" },
      { text: "No journal entry between 09:12 and 18:40", citation: "journal:btc" },
    ],
    steps: [],
  },
  remaining: 29,
};
const MINE = [
  { id: "R-0013", ts: "2026-10-28T10:00:00Z", kind: "enhancement", title: "CSV export", status: "in_review", note: null, issue_url: null },
  { id: "R-0012", ts: "2026-10-27T10:00:00Z", kind: "bug", title: "P&L spike", status: "filed", note: "Amounts removed", issue_url: "https://github.com/rchau73/optionsBot/issues/500" },
  { id: "R-0011", ts: "2026-10-26T10:00:00Z", kind: "bug", title: "Chart", status: "not_a_bug", note: "The journal explains it", issue_url: null },
];

function fakeFetch({ ask = () => json(200, DRAFT_ANSWER), sendReq = () => json(201, { id: "R-0012", status: "in_review" }), mine = () => json(200, { requests: MINE }) } = {}) {
  global.fetch = jest.fn(async (url, init = {}) => {
    const method = init.method ?? "GET";
    if (url === "/api/assistant" && method === "GET") return json(200, { enabled: true, remaining: 30 });
    if (url === "/api/assistant") return ask(JSON.parse(init.body));
    if (url === "/api/assistant/requests" && method === "POST") return sendReq(JSON.parse(init.body));
    if (url === "/api/assistant/requests") return mine();
    throw new Error(`unexpected ${url}`);
  });
}
const posts = (url) => global.fetch.mock.calls.filter(([u, i]) => u === url && i?.method === "POST").map(([, i]) => JSON.parse(i.body));

async function openAndAsk(text = "Bug: the P&L chart dropped at 14:00") {
  window.history.replaceState(null, "", "/?bot=btc");
  render(
    <AssistantProvider>
      <MenuNav pathname="/" bot="btc" />
    </AssistantProvider>,
  );
  await act(async () => {});
  fireEvent.click(screen.getByRole("button", { name: "Ask" }));
  const p = screen.getByRole("complementary", { name: "Assistant" });
  fireEvent.change(within(p).getByLabelText("Your question"), { target: { value: text } });
  fireEvent.keyDown(within(p).getByLabelText("Your question"), { key: "Enter" });
  await within(p).findByTestId("assistant-answer");
  return p;
}

const realFetch = global.fetch;
beforeEach(() => fakeFetch());
afterEach(() => {
  global.fetch = realFetch;
});

test("needs_info: the checklist ticks what the report has and crosses what is missing, no card", async () => {
  fakeFetch({ ask: () => json(200, { id: "a1", answer: NEEDS_INFO_TEXT, outcome: "needs_info", citations: [], checklist: CHECK(["what", "where", "evidence"]) }) });
  const p = await openAndAsk("the chart is wrong");
  const list = within(p).getByRole("list", { name: "Request check" });
  const items = within(list).getAllByRole("listitem");
  expect(items).toHaveLength(4);
  expect(items.map((li) => li.textContent.startsWith("✓"))).toEqual([false, false, false, true]);
  expect(within(items[0]).getByText("Missing:")).toBeInTheDocument();
  expect(within(p).queryByTestId("request-draft")).toBeNull();
});

test("the draft card: kind, heading, fields, evidence with its source, the notice and the three buttons; nothing sent yet", async () => {
  const p = await openAndAsk();
  const card = within(p).getByRole("region", { name: "Draft request" });
  expect(within(card).getByText("Bug")).toBeInTheDocument();
  expect(within(card).getByRole("heading", { name: DRAFT_HEADING })).toBeInTheDocument();
  expect(within(card).getByText("P&L spike at 14:00 UTC")).toBeInTheDocument();
  expect(within(card).getByText("Overview · btc · 2026-10-27 14:00 UTC")).toBeInTheDocument();
  expect(within(card).getByText("(btc P&L history at 2026-10-27T14:00:00Z)")).toBeInTheDocument();
  expect(within(card).getByText(DRAFT_NOTICE)).toBeInTheDocument();
  for (const name of ["Send for review", "Edit", "Discard"]) expect(within(card).getByRole("button", { name })).toBeInTheDocument();
  expect(posts("/api/assistant/requests")).toEqual([]);
});

test("Send for review sends the draft once, then says its id and opens My requests on demand", async () => {
  const p = await openAndAsk();
  await act(async () => fireEvent.click(within(p).getByRole("button", { name: "Send for review" })));
  expect(posts("/api/assistant/requests")).toEqual([
    {
      answer_id: "a9",
      kind: "bug",
      fields: {
        title: "P&L spike at 14:00 UTC",
        what: "The total P&L dropped for one hour",
        where: "Overview · btc · 2026-10-27 14:00 UTC",
        expected: "No drop without a journal entry",
        evidence: DRAFT_ANSWER.draft.evidence,
        steps: [],
      },
    },
  ]);
  expect(within(p).getByText(sentText("R-0012"))).toBeInTheDocument();
  expect(within(p).queryByRole("button", { name: "Send for review" })).toBeNull();

  fireEvent.click(within(p).getByRole("button", { name: "Open My requests" }));
  expect(within(p).getByRole("tab", { name: "My requests" })).toHaveAttribute("aria-selected", "true");
  const list = await within(p).findByRole("list", { name: "My requests" });
  const rows = within(list).getAllByRole("listitem");
  expect(rows.map((r) => within(r).getByText(/^R-/).textContent)).toEqual(["R-0013", "R-0012", "R-0011"]);
  expect(within(rows[0]).getByText("In review")).toBeInTheDocument();
  expect(within(rows[1]).getByText("Filed")).toBeInTheDocument();
  expect(within(rows[1]).getByRole("link", { name: "The public issue" })).toHaveAttribute("href", MINE[1].issue_url);
  expect(within(rows[2]).getByText("Not a bug")).toBeInTheDocument();
  expect(within(rows[2]).getByText("Reviewer: The journal explains it")).toBeInTheDocument();
});

test("Edit makes the fields editable and Send carries the edits", async () => {
  const p = await openAndAsk();
  fireEvent.click(within(p).getByRole("button", { name: "Edit" }));
  fireEvent.change(within(p).getByLabelText("Title"), { target: { value: "My title" } });
  fireEvent.change(within(p).getByLabelText("Evidence 1"), { target: { value: "My words" } });
  await act(async () => fireEvent.click(within(p).getByRole("button", { name: "Send for review" })));
  const [sent] = posts("/api/assistant/requests");
  expect(sent.fields.title).toBe("My title");
  expect(sent.fields.evidence[0]).toEqual({ text: "My words", citation: "pnl:btc/2026-10-27T14:00:00Z" });
});

test("Discard drops the draft: nothing is sent", async () => {
  const p = await openAndAsk();
  fireEvent.click(within(p).getByRole("button", { name: "Discard" }));
  expect(within(p).getByText("Draft discarded. Nothing was sent.")).toBeInTheDocument();
  expect(within(p).queryByTestId("request-draft")).toBeNull();
  expect(posts("/api/assistant/requests")).toEqual([]);
});

test("a refused send says why and keeps the card", async () => {
  fakeFetch({ sendReq: () => json(429, { error: "10 requests a day: try again tomorrow (UTC)" }) });
  const p = await openAndAsk();
  await act(async () => fireEvent.click(within(p).getByRole("button", { name: "Send for review" })));
  expect(within(p).getByRole("alert")).toHaveTextContent("Not sent: 10 requests a day");
  expect(within(p).getByRole("button", { name: "Send for review" })).toBeEnabled();
});

test("My requests: empty and error states", async () => {
  fakeFetch({ mine: () => json(200, { requests: [] }) });
  const p = await openAndAsk();
  fireEvent.click(within(p).getByRole("tab", { name: "My requests" }));
  expect(await within(p).findByText(/No requests yet/)).toBeInTheDocument();
  // The question box belongs to Ask only.
  expect(within(p).queryByLabelText("Your question")).toBeNull();

  fakeFetch({ mine: () => json(503, { error: "requests are off" }) });
  fireEvent.click(within(p).getByRole("tab", { name: "Ask" }));
  fireEvent.click(within(p).getByRole("tab", { name: "My requests" }));
  expect(await within(p).findByRole("alert")).toHaveTextContent("Your requests could not be read: requests are off.");
});
