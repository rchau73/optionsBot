import { act, fireEvent, render, screen, within } from "@testing-library/react";
import AssistantProvider from "@/components/AssistantProvider";
import { MenuNav } from "@/components/MenuBar";
import PrivacySettings from "@/components/PrivacySettings";
import { ConfirmCloseModal } from "@/components/ManualClose";
import { currentSessionId, track } from "@/lib/track";
import { NPS_TO_IMPROVE, NPS_WHAT_WORKS } from "@/lib/analytics/surveys";
import { NPS_ASK_LABEL, NPS_THANKS, REASON_LABELS } from "@/lib/analytics/nps";

// #446: the monthly NPS card in the assistant panel — the Ask badge while it
// is due, the card when the panel opens, one list by score, and its exits.

jest.mock("@/lib/track", () => ({ track: jest.fn(), currentSessionId: jest.fn(() => "0123456789abcdef0123456789abcdef") }));

const SESSION = "0123456789abcdef0123456789abcdef";
const json = (status, body) => ({ ok: status >= 200 && status < 300, status, headers: { get: () => null }, json: async () => body });

/** fetch of the monitor's routes: the NPS view, one bot and its last 7 days. */
function fakeFetch({ due = true, net = 0.01, optedOut = false } = {}) {
  global.fetch = jest.fn(async (url, init = {}) => {
    const method = init.method ?? "GET";
    if (url === "/api/assistant") return json(200, { enabled: true, remaining: 30 });
    if (url === "/api/survey/nps" && method === "GET") return json(200, { due, reason: due ? null : "shown_recently", opted_out: optedOut });
    if (url === "/api/survey/nps") {
      const { action } = JSON.parse(init.body);
      return json(200, { due: false, reason: action === "opt_in" ? null : "shown_recently", opted_out: action === "opt_out" });
    }
    if (url === "/api/survey") return json(204, null);
    if (url === "/api/bots") return json(200, { bots: [{ name: "btc" }], pollMs: 1000 });
    if (url.startsWith("/api/bots/btc/performance")) return json(200, { net_pnl: net, net_pnl_known: true });
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
const card = () => screen.queryByRole("region", { name: "Optional question from the team" });
const openAsk = async (name = NPS_ASK_LABEL) => {
  await act(async () => fireEvent.click(screen.getByRole("button", { name })));
};

beforeEach(() => {
  track.mockClear();
  currentSessionId.mockClear();
});

describe("Scenario: badge while due", () => {
  test("the Ask button shows the static badge; opening it shows the card and sends survey_shown{nps} once", async () => {
    fakeFetch();
    await renderMenu();
    const ask = screen.getByRole("button", { name: NPS_ASK_LABEL });
    expect(within(ask).getByTestId("nps-badge")).toHaveTextContent("1");
    expect(within(ask).getByTestId("nps-badge").className).not.toMatch(/animate/);
    expect(card()).toBeNull(); // the badge alone is not "shown"
    expect(tracked("survey_shown")).toEqual([]);

    await openAsk();
    expect(card()).not.toBeNull();
    expect(tracked("survey_shown")).toEqual([{ survey_id: "nps" }]);
    expect(posts("/api/survey/nps")).toEqual([{ action: "shown" }]);

    // Closing the panel and opening it again: not shown again, no badge.
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Close the assistant" })));
    expect(screen.queryByTestId("nps-badge")).toBeNull();
    await openAsk("Ask");
    expect(card()).toBeNull();
    expect(tracked("survey_shown")).toHaveLength(1);
    expect(posts("/api/survey/nps")).toHaveLength(1);
  });

  test("not due on the server: no badge, no card", async () => {
    fakeFetch({ due: false });
    await renderMenu();
    expect(screen.queryByTestId("nps-badge")).toBeNull();
    await openAsk("Ask");
    expect(card()).toBeNull();
    expect(tracked("survey_shown")).toEqual([]);
  });

  test("right after a losing week: postponed — no badge, no card", async () => {
    fakeFetch({ net: -0.002 });
    await renderMenu();
    expect(screen.queryByTestId("nps-badge")).toBeNull();
    await openAsk("Ask");
    expect(card()).toBeNull();
  });

  test("never while a manual close dialog is open", async () => {
    fakeFetch();
    const row = { bot: "btc", instrument: "BTC-27NOV26-60000-P", qty: 1, positionIds: [1], manualClose: {} };
    await renderMenu(<ConfirmCloseModal row={row} status={null} onCancel={() => {}} onDone={() => {}} />);
    await openAsk();
    expect(card()).toBeNull();
    expect(tracked("survey_shown")).toEqual([]);
    expect(posts("/api/survey/nps")).toEqual([]);
  });
});

describe("Scenario: promoter question", () => {
  test("tapping 9 asks what we do best, with only the what-works codes", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    fireEvent.click(within(card()).getByRole("button", { name: "9" }));
    expect(within(card()).getByText("What do we do best?")).toBeInTheDocument();
    const chips = within(within(card()).getByRole("group", { name: /What do we do best/ })).getAllByRole("button").map((b) => b.textContent);
    expect(chips).toEqual(NPS_WHAT_WORKS.map((c) => REASON_LABELS[c]));
    expect(chips).toContain("Something else");
  });

  test("tapping 5 asks what to improve first, with only the to-improve codes", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    fireEvent.click(within(card()).getByRole("button", { name: "5" }));
    const chips = within(within(card()).getByRole("group", { name: /What should we improve first/ })).getAllByRole("button").map((b) => b.textContent);
    expect(chips).toEqual(NPS_TO_IMPROVE.map((c) => REASON_LABELS[c]));
  });

  test("Send posts the score, the code and the private comment to /api/survey, then thanks", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    fireEvent.click(within(card()).getByRole("button", { name: "10" }));
    fireEvent.click(within(card()).getByRole("button", { name: REASON_LABELS.explains_trades }));
    fireEvent.change(within(card()).getByLabelText(/Anything you want to add/), { target: { value: "Clear" } });
    await act(async () => fireEvent.click(within(card()).getByRole("button", { name: "Send" })));
    expect(posts("/api/survey")).toEqual([{ survey_id: "nps", session_id: SESSION, codes: { score: "10", reason_code: "explains_trades" }, text: "Clear" }]);
    expect(within(card()).getByRole("status")).toHaveTextContent(NPS_THANKS);
    // The answer's event is written by the server with the codes only; the page sends no text in any event.
    expect(JSON.stringify(track.mock.calls)).not.toContain("Clear");
  });

  test("Send the score only sends the score alone", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    fireEvent.click(within(card()).getByRole("button", { name: "3" }));
    fireEvent.click(within(card()).getByRole("button", { name: REASON_LABELS.price }));
    await act(async () => fireEvent.click(within(card()).getByRole("button", { name: "Send the score only" })));
    expect(posts("/api/survey")).toEqual([{ survey_id: "nps", session_id: SESSION, codes: { score: "3" } }]);
  });
});

describe("dismissing", () => {
  test.each([["Not now"], ["Close the question"]])("%s counts as shown and sends survey_dismissed", async (name) => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    fireEvent.click(within(card()).getByRole("button", { name }));
    expect(card()).toBeNull();
    expect(tracked("survey_dismissed")).toEqual([{ survey_id: "nps" }]);
    expect(posts("/api/survey")).toEqual([]);
    // The assistant works the same without the card.
    expect(screen.getByLabelText("Your question")).toBeEnabled();
  });

  test("Scenario: opted out — Don't ask me again turns NPS off on the server", async () => {
    fakeFetch();
    await renderMenu();
    await openAsk();
    await act(async () => fireEvent.click(within(card()).getByRole("button", { name: "Don't ask me again" })));
    expect(posts("/api/survey/nps")).toEqual([{ action: "shown" }, { action: "opt_out" }]);
    expect(within(card()).getByRole("status")).toHaveTextContent("Account › Privacy");
  });
});

describe("Account › Privacy", () => {
  test("the switch turns the monthly question back on", async () => {
    fakeFetch({ due: false, optedOut: true });
    render(<PrivacySettings />);
    await act(async () => {});
    const sw = screen.getByRole("switch", { name: /Monthly question in the assistant/ });
    expect(sw).toHaveAttribute("aria-checked", "false");
    await act(async () => fireEvent.click(sw));
    expect(posts("/api/survey/nps")).toEqual([{ action: "opt_in" }]);
    expect(sw).toHaveAttribute("aria-checked", "true");
  });
});
