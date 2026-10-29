import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import NotificationBell from "@/components/NotificationBell";
import NotificationsPage from "@/components/NotificationsPage";
import { fetchNotifications, navigation, sendNotificationAction } from "@/lib/api";
import { track } from "@/lib/track";
import { badgeText, bellLabel, botEventItem, buildNotifications, requestItem, surveyItem } from "@/lib/notifications";

// #447: the notification bell in the menu bar and the /notifications page.

jest.mock("@/lib/api", () => ({ fetchNotifications: jest.fn(), sendNotificationAction: jest.fn(), navigation: { assign: jest.fn() } }));
jest.mock("@/lib/track", () => ({ track: jest.fn() }));
const assistant = { openPanel: jest.fn(), askWhy: jest.fn() };
jest.mock("@/components/AssistantProvider", () => ({ useAssistant: () => assistant }));

const NOW = Date.UTC(2026, 9, 28, 12);
const at = (h) => new Date(NOW - h * 3600 * 1000).toISOString();

const survey = { ...surveyItem(null, NOW), group: "needs_you", read: false };
const needsMore = { ...requestItem({ id: "R-0001", status: "needs_info", status_ts: at(2), title: "Chart gap", note: "Which bot?" }), group: "needs_you", read: false };
const stop = {
  ...botEventItem("btc", { seq: 42, event: "closed", at: at(3), data: { close_reason: "stop_loss", instrument: "BTC-27NOV26-60000-P" }, plain: "Bought back the put at the stop-loss." }),
  group: "updates",
  read: false,
};
const oldNote = { id: "release:v0.3.0", kind: "whats_new", ts: at(30), title: "What's new in v0.3.0: The bell", text: "A bell.", group: "updates", read: true };
const view = (items, extra = {}) => ({ items, needs_you: items.filter((i) => i.group === "needs_you" && !i.read).length, settings: { bot_event: true, request_status: true, whats_new: true }, errors: {}, ...extra });

const openBell = async (name) => {
  const bell = await screen.findByRole("button", { name });
  fireEvent.click(bell);
  return { bell, dialog: screen.getByRole("dialog", { name: "Notifications" }) };
};

beforeEach(() => {
  jest.clearAllMocks();
  delete window.matchMedia;
  fetchNotifications.mockResolvedValue(view([survey, needsMore, stop, oldNote]));
  sendNotificationAction.mockResolvedValue(view([survey, needsMore, stop, oldNote]));
});

describe("Scenario: badge counts only what needs the person", () => {
  test("a survey is due and bot updates are unread → the badge counts the Needs you items only", async () => {
    fetchNotifications.mockResolvedValue(view([survey, { ...stop, id: "a" }, { ...stop, id: "b" }, { ...stop, id: "c" }]));
    render(<NotificationBell bot="btc" />);
    const bell = await screen.findByRole("button", { name: "Notifications, 1 needs you" });
    expect(bell.tagName).toBe("BUTTON");
    expect(bell).toHaveTextContent("1");
    expect(fetchNotifications).toHaveBeenCalledWith("btc", expect.anything());
  });

  test("named as the design says: Notifications, 2 need you", async () => {
    render(<NotificationBell bot="btc" />);
    expect(await screen.findByRole("button", { name: "Notifications, 2 need you" })).toHaveClass("min-h-11", "min-w-11");
  });

  test("nothing needs the person → no badge, just Notifications", async () => {
    fetchNotifications.mockResolvedValue(view([stop]));
    render(<NotificationBell bot="btc" />);
    const bell = await screen.findByRole("button", { name: "Notifications" });
    expect(bell).toHaveTextContent("");
  });
});

describe("Scenario: a stop-loss appears with its reason", () => {
  test("Updates lists it with the event's plain sentence and a Why? that asks the assistant", async () => {
    render(<NotificationBell bot="btc" />);
    const { dialog } = await openBell("Notifications, 2 need you");
    const updates = within(dialog).getByRole("region", { name: "Updates" });
    expect(within(updates).getByText("Bought back the put at the stop-loss.")).toBeInTheDocument();
    await act(async () => fireEvent.click(within(updates).getByRole("button", { name: "Why? BTC · Stop-loss" })));
    expect(assistant.askWhy).toHaveBeenCalledWith(expect.objectContaining({ bot: "btc", seq: 42, kind: "close", instrument: "BTC-27NOV26-60000-P" }), expect.anything());
    expect(track).toHaveBeenCalledWith("notification_followed", { kind: "bot_event" });
    expect(sendNotificationAction).toHaveBeenCalledWith("btc", { action: "read", ids: ["bot:btc:42"] });
    // Following closes the dropdown: the assistant panel opens instead.
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("Scenario: read on one device, read on all", () => {
  test("Mark all read sends it to the server and shows the list it returns", async () => {
    sendNotificationAction.mockResolvedValue(view([survey, needsMore, stop, oldNote].map((i) => ({ ...i, read: true }))));
    render(<NotificationBell bot="btc" />);
    const { dialog } = await openBell("Notifications, 2 need you");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Mark all read" })));
    expect(sendNotificationAction).toHaveBeenCalledWith("btc", { action: "read_all" });
    expect(screen.getByRole("button", { name: "Notifications" })).toBeInTheDocument();
    expect(within(dialog).queryByText(/Unread:/)).toBeNull();
    expect(within(dialog).getByRole("button", { name: "Mark all read" })).toBeDisabled();
  });

  test("a failed save says so and keeps the list", async () => {
    sendNotificationAction.mockRejectedValue(new Error("sign-in is unavailable"));
    render(<NotificationBell bot="btc" />);
    const { dialog } = await openBell("Notifications, 2 need you");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Mark all read" })));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Not saved: sign-in is unavailable.");
    expect(screen.getByRole("button", { name: "Notifications, 2 need you" })).toBeInTheDocument();
  });
});

describe("the dropdown", () => {
  test("a dialog that takes focus, keeps it inside and closes on Escape back to the bell", async () => {
    render(<NotificationBell bot="btc" />);
    const { bell, dialog } = await openBell("Notifications, 2 need you");
    expect(track).toHaveBeenCalledWith("notifications_opened", {});
    expect(bell).toHaveAttribute("aria-expanded", "true");
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog.contains(document.activeElement)).toBe(true);
    const focusable = dialog.querySelectorAll("a[href], button:not([disabled])");
    focusable[focusable.length - 1].focus();
    fireEvent.keyDown(window, { key: "Tab" });
    expect(document.activeElement).toBe(focusable[0]);
    fireEvent.keyDown(window, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(focusable[focusable.length - 1]);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(bell);
  });

  test("a click outside closes it", async () => {
    render(<NotificationBell bot="btc" />);
    await openBell("Notifications, 2 need you");
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  test("unread is a dot and bold text with a spoken 'Unread'; read is neither", async () => {
    render(<NotificationBell bot="btc" />);
    const { dialog } = await openBell("Notifications, 2 need you");
    const unread = within(dialog).getByText("A short question from the team is waiting");
    expect(unread).toHaveClass("font-semibold");
    expect(unread).toHaveTextContent("Unread: A short question from the team is waiting");
    expect(unread.closest("li").querySelector("span[aria-hidden]")).toHaveClass("bg-testnet");
    const read = within(dialog).getByText("What's new in v0.3.0: The bell");
    expect(read).toHaveClass("font-normal");
    expect(read.closest("li").querySelector("span[aria-hidden]")).toHaveClass("bg-transparent");
  });

  test("Needs you first, counted; the survey opens the question and 'needs more' My requests", async () => {
    render(<NotificationBell bot="btc" />);
    const { dialog } = await openBell("Notifications, 2 need you");
    const needs = within(dialog).getByRole("region", { name: "Needs you (2)" });
    expect(within(needs).getAllByRole("listitem")).toHaveLength(2);
    await act(async () => fireEvent.click(within(needs).getByRole("button", { name: "Open the question" })));
    expect(assistant.openPanel).toHaveBeenCalledWith({ from: expect.anything() });
    expect(track).toHaveBeenCalledWith("notification_followed", { kind: "survey" });
    await openBell("Notifications, 2 need you");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open My requests" })));
    expect(assistant.openPanel).toHaveBeenLastCalledWith({ from: expect.anything(), tab: "requests" });
  });

  test("no 'act now' copy anywhere", async () => {
    render(<NotificationBell bot="btc" />);
    const { dialog } = await openBell("Notifications, 2 need you");
    expect(dialog.textContent).not.toMatch(/act now|hurry|don't miss|urgent/i);
  });

  test("links to the page with the bot kept", async () => {
    render(<NotificationBell bot="btc" />);
    const { dialog } = await openBell("Notifications, 2 need you");
    expect(within(dialog).getByRole("link", { name: "All notifications and settings" })).toHaveAttribute("href", "/notifications?bot=btc");
  });

  test("on a phone the bell opens the /notifications page", async () => {
    window.matchMedia = jest.fn(() => ({ matches: true }));
    render(<NotificationBell bot="eth" />);
    fireEvent.click(await screen.findByRole("button", { name: "Notifications, 2 need you" }));
    expect(navigation.assign).toHaveBeenCalledWith("/notifications?bot=eth");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("states", () => {
  test("loading, then empty groups in plain words", async () => {
    let resolve;
    fetchNotifications.mockReturnValue(new Promise((r) => (resolve = r)));
    render(<NotificationsPage bot="btc" />);
    expect(screen.getByText("Loading your notifications…")).toBeInTheDocument();
    await act(async () => resolve(view([])));
    expect(screen.getByText("Nothing needs you.")).toBeInTheDocument();
    expect(screen.getByText("No updates in the last 90 days.")).toBeInTheDocument();
  });

  test("an error with nothing read yet is an alert", async () => {
    fetchNotifications.mockRejectedValue(new Error("sign-in is unavailable"));
    render(<NotificationsPage bot="btc" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Your notifications could not be read: sign-in is unavailable.");
  });

  test("a bot that does not answer: named, the rest still listed", async () => {
    fetchNotifications.mockResolvedValue(view([oldNote], { errors: { bot_events: "bot btc is unreachable" } }));
    render(<NotificationsPage bot="btc" />);
    expect(await screen.findByText(/The bot's events could not be read \(bot btc is unreachable\)/)).toBeInTheDocument();
    expect(screen.getByText("What's new in v0.3.0: The bell")).toBeInTheDocument();
  });
});

describe("settings per kind (the page)", () => {
  test("each Updates kind can be turned off; Needs you is not offered", async () => {
    sendNotificationAction.mockResolvedValue(view([survey], { settings: { bot_event: false, request_status: true, whats_new: true } }));
    render(<NotificationsPage bot="btc" />);
    const settings = await screen.findByRole("region", { name: "What the bell lists" });
    const boxes = within(settings).getAllByRole("checkbox");
    expect(boxes.map((b) => b.closest("label").textContent)).toEqual(["What the bot did to your book", "The status of your requests", "What's new in a release"]);
    expect(within(settings).queryByRole("checkbox", { name: /survey|needs more/i })).toBeNull();
    expect(settings).toHaveTextContent(/Don't ask me again/);
    await act(async () => fireEvent.click(within(settings).getByRole("checkbox", { name: "What the bot did to your book" })));
    expect(sendNotificationAction).toHaveBeenCalledWith("btc", { action: "setting", kind: "bot_event", enabled: false });
    await waitFor(() => expect(within(settings).getByRole("checkbox", { name: "What the bot did to your book" })).not.toBeChecked());
  });
});

describe("lib/notifications", () => {
  test("buildNotifications: kinds turned off, 90 days, newest first, only unread Needs you counted", () => {
    const items = [
      { id: "bot:btc:1", kind: "bot_event", ts: at(1) },
      { id: "bot:btc:2", kind: "bot_event", ts: at(91 * 24) },
      { id: "release:v1.0.0", kind: "whats_new", ts: at(5) },
      { id: "survey:nps:never", kind: "survey", ts: at(0) },
      { id: "request:R-1:needs_info:1", kind: "request_needs_info", ts: at(2) },
      { id: "x:1", kind: "unknown", ts: at(1) },
      null,
    ];
    const out = buildNotifications(items, { readIds: new Set(["request:R-1:needs_info:1"]), settings: { whats_new: false, survey: false } }, NOW);
    expect(out.items.map((i) => [i.id, i.group, i.read])).toEqual([
      ["survey:nps:never", "needs_you", false],
      ["bot:btc:1", "updates", false],
      ["request:R-1:needs_info:1", "needs_you", true],
    ]);
    expect(out.needs_you).toBe(1);
  });

  test("a request in review is no item; each due survey period is a new item", () => {
    expect(requestItem({ id: "R-1", status: "in_review", status_ts: at(1) })).toBeNull();
    expect(requestItem({ id: "R-1", status: "filed", status_ts: null })).toBeNull();
    expect(surveyItem(null, NOW).id).not.toBe(surveyItem(1_700_000_000, NOW).id);
  });

  test("bot events without a sequence number or of other kinds are no item", () => {
    expect(botEventItem("btc", { event: "closed", at: at(1), data: { close_reason: "stop_loss" } })).toBeNull();
    expect(botEventItem("btc", { seq: 1, event: "filled", at: at(1), data: {} })).toBeNull();
    expect(botEventItem("btc", { seq: 1, event: "closed", at: at(1), data: { close_reason: "stop_loss" }, plain: " " }).text).toBe("Stop-loss");
  });

  test("bellLabel and badgeText", () => {
    expect(bellLabel(0)).toBe("Notifications");
    expect(bellLabel(1)).toBe("Notifications, 1 needs you");
    expect(bellLabel(2)).toBe("Notifications, 2 need you");
    expect(badgeText(0)).toBeNull();
    expect(badgeText(3)).toBe("3");
    expect(badgeText(12)).toBe("9+");
  });
});
