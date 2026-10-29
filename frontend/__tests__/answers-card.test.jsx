import { act, fireEvent, render, screen, within } from "@testing-library/react";
import AssistantProvider from "@/components/AssistantProvider";
import { MenuNav } from "@/components/MenuBar";
import { ConfirmCloseModal } from "@/components/ManualClose";
import { currentSessionId, track } from "@/lib/track";
import { ANSWERS_ASK_LABEL, ANSWERS_CODES, ANSWERS_LABELS, ANSWERS_THANKS } from "@/lib/analytics/answers";
import { NPS_ASK_LABEL } from "@/lib/analytics/nps";

// #498: the four answers card in the assistant panel — the Ask badge while it
// is due, the card when the panel opens (once the server has recorded it),
// two optional questions, and its exits.

jest.mock("@/lib/track", () => ({ track: jest.fn(), currentSessionId: jest.fn(() => "0123456789abcdef0123456789abcdef") }));

const SESSION = "0123456789abcdef0123456789abcdef";
const json = (status, body) => ({ ok: status >= 200 && status < 300, status, headers: { get: () => null }, json: async () => body });

/** fetch of the monitor's routes: the two survey views (shownStatus: what POST shown answers), one bot and its last 7 days. */
function fakeFetch({ due = true, npsDue = false, shownStatus = 200 } = {}) {
  global.fetch = jest.fn(async (url, init = {}) => {
    const method = init.method ?? "GET";
    if (url === "/api/assistant") return json(200, { enabled: true, remaining: 30 });
    if (url === "/api/survey/nps" && method === "GET") return json(200, { due: npsDue, reason: npsDue ? null : "shown_recently", opted_out: false });
    if (url === "/api/survey/nps") return json(200, { due: false, reason: "shown_recently", opted_out: false });
    if (url === "/api/survey/answers" && method === "GET") return json(200, { due, reason: due ? null : "shown_recently" });
    if (url === "/api/survey/answers") return json(shownStatus, { due: false, reason: "shown_recently" });
    if (url === "/api/survey") return json(204, null);
    if (url === "/api/bots") return json(200, { bots: [{ name: "btc" }], pollMs: 1000 });
    if (url.startsWith("/api/bots/btc/performance")) return json(200, { net_pnl: 0.01, net_pnl_known: true });
    throw new Error(`unexpected ${url}`);
  });
}
const posts = (url) => global.fetch.mock.calls.filter(([u, i]) => u === url && i?.method === "POST").map(([, i]) => JSON.parse(i.body));
const tracked = (event) => track.mock.calls.filter(([e]) => e === event).map(([, p]) => p);

async function renderMenu(extra = null) {
  window.history.replaceState(null, "", "/?bot=btc");
  render(
    <AssistantProvider>
      <MenuNav pathname="/" bot="btc" />
      {extra}
    </AssistantProvider>,
  );
  await act(async () => {});
}
const card = () => screen.queryByRole("region", { name: "Optional questions from the team" });
const npsCard = () => screen.queryByRole("region", { name: "Optional question from the team" });
const openAsk = async (name = ANSWERS_ASK_LABEL) => {
  await act(async () => fireEvent.click(screen.getByRole("button", { name })));
};
const group = (name) => within(card()).getByRole("group", { name: new RegExp(name) });

beforeEach(() => {
  track.mockClear();
  currentSessionId.mockClear();
});

describe("Scenario: Card is due", () => {
  test("the badge shows; opening Ask records shown and shows both optional questions with a close button", async () => {
    fakeFetch();
    await renderMenu();
    expect(within(screen.getByRole("button", { name: ANSWERS_ASK_LABEL })).getByTestId("answers-badge")).toHaveTextContent("1");
    expect(card()).toBeNull();

    await openAsk();
    expect(posts("/api/survey/answers")).toEqual([{ action: "shown" }]);
    expect(tracked("survey_shown")).toEqual([{ survey_id: "answers" }]);
    for (const q of ["What did you come to check today?", "What was hardest to find?"]) {
      expect(within(group(q)).getAllByRole("button").map((b) => b.textContent)).toEqual(ANSWERS_CODES.map((c) => ANSWERS_LABELS[c]));
    }
    expect(within(card()).getAllByText("(optional)")).toHaveLength(2);
    expect(within(card()).getByRole("button", { name: "Close the questions" })).toBeInTheDocument();
    expect(within(card()).getByRole("button", { name: "Send" })).toBeDisabled();

    // Closing and reopening the panel: not shown again.
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Close the assistant" })));
    await openAsk("Ask");
    expect(card()).toBeNull();
    expect(posts("/api/survey/answers")).toHaveLength(1);
  });

  test("not in the same session as NPS: NPS goes first and the answers card never follows", async () => {
    fakeFetch({ npsDue: true });
    await renderMenu();
    await openAsk(NPS_ASK_LABEL);
    expect(npsCard()).not.toBeNull();
    expect(card()).toBeNull();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Close the assistant" })));
    await openAsk("Ask");
    expect(card()).toBeNull();
    expect(posts("/api/survey/answers")).toEqual([]);
    expect(tracked("survey_shown")).toEqual([{ survey_id: "nps" }]);
  });

  test("never while a manual close dialog is open", async () => {
    fakeFetch();
    const row = { bot: "btc", instrument: "BTC-27NOV26-60000-P", qty: 1, positionIds: [1], manualClose: {} };
    await renderMenu(<ConfirmCloseModal row={row} status={null} onCancel={() => {}} onDone={() => {}} />);
    await openAsk();
    expect(card()).toBeNull();
    expect(posts("/api/survey/answers")).toEqual([]);
  });
});

describe("Scenario: Answer one question", () => {
  test("only hardest to find: Send posts that code alone, then thanks; the page sends no answer event itself", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    fireEvent.click(within(group("What was hardest to find")).getByRole("button", { name: ANSWERS_LABELS.worst_case }));
    await act(async () => fireEvent.click(within(card()).getByRole("button", { name: "Send" })));
    expect(posts("/api/survey")).toEqual([{ survey_id: "answers", session_id: SESSION, codes: { hardest_to_find: "worst_case" } }]);
    expect(within(card()).getByRole("status")).toHaveTextContent(ANSWERS_THANKS);
    expect(tracked("survey_answered")).toEqual([]); // written by the server, codes only
  });

  test("both answered, and a second tap clears a choice", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    const came = group("What did you come to check");
    fireEvent.click(within(came).getByRole("button", { name: ANSWERS_LABELS.holding }));
    fireEvent.click(within(came).getByRole("button", { name: ANSWERS_LABELS.income }));
    expect(within(came).getByRole("button", { name: ANSWERS_LABELS.income })).toHaveAttribute("aria-pressed", "true");
    expect(within(came).getByRole("button", { name: ANSWERS_LABELS.holding })).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(within(group("What was hardest to find")).getByRole("button", { name: ANSWERS_LABELS.none }));
    fireEvent.click(within(group("What was hardest to find")).getByRole("button", { name: ANSWERS_LABELS.none }));
    await act(async () => fireEvent.click(within(card()).getByRole("button", { name: "Send" })));
    expect(posts("/api/survey")).toEqual([{ survey_id: "answers", session_id: SESSION, codes: { came_for: "income" } }]);
  });

  test("a refused answer says why and keeps the card", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    global.fetch.mockImplementationOnce(async () => json(403, { error: "analytics is off for this user: no consent" }));
    fireEvent.click(within(group("What did you come to check")).getByRole("button", { name: ANSWERS_LABELS.last_action }));
    await act(async () => fireEvent.click(within(card()).getByRole("button", { name: "Send" })));
    expect(within(card()).getByRole("alert")).toHaveTextContent("Not sent");
  });
});

describe("Scenario: Dismiss", () => {
  test.each([["Not now"], ["Close the questions"]])("%s sends survey_dismissed{answers} and nothing else", async (name) => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    fireEvent.click(within(card()).getByRole("button", { name }));
    expect(card()).toBeNull();
    expect(tracked("survey_dismissed")).toEqual([{ survey_id: "answers" }]);
    expect(posts("/api/survey")).toEqual([]);
    expect(screen.getByLabelText("Your question")).toBeEnabled();
  });

  test("closing the panel with the card unanswered dismisses it", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Close the assistant" })));
    expect(tracked("survey_dismissed")).toEqual([{ survey_id: "answers" }]);
  });
});

describe("Scenario: No consent / not due", () => {
  test("not due on the server: no badge, no card, nothing sent", async () => {
    fakeFetch({ due: false });
    await renderMenu();
    expect(screen.queryByTestId("answers-badge")).toBeNull();
    await openAsk("Ask");
    expect(card()).toBeNull();
    expect(posts("/api/survey/answers")).toEqual([]);
    expect(track.mock.calls.filter(([e]) => e.startsWith("survey_"))).toEqual([]);
  });
});

describe("Scenario: Second tab or device", () => {
  test("another tab recorded it first (409): this tab shows no card and sends no event", async () => {
    fakeFetch({ shownStatus: 409 });
    await renderMenu();
    await openAsk();
    expect(posts("/api/survey/answers")).toEqual([{ action: "shown" }]);
    expect(card()).toBeNull();
    expect(tracked("survey_shown")).toEqual([]);
  });
});
