import fs from "node:fs";
import path from "node:path";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import DeleteAccountModal from "@/components/DeleteAccount";
import UserMenu from "@/components/UserMenu";
import { navigation } from "@/lib/api/http";
import { DELETE_COPY, EXIT_CODES, EXIT_COPY, exitAnswerBody } from "@/lib/analytics/exit";
import { NPS_WHAT_WORKS } from "@/lib/analytics/surveys";

// #543: "Delete my account" with the optional exit question (surveys.md §3):
// it never blocks leaving, shows the exit codes only, and its words are the
// survey doc's, in EN and PT-BR.

const SESSION = "0123456789abcdef0123456789abcdef";
const SURVEYS_MD = fs.readFileSync(path.join(__dirname, "..", "..", "docs", "product", "analytics", "surveys.md"), "utf8");

function fakeDeps(over = {}) {
  return {
    sendAnswer: jest.fn(async () => {}),
    deleteAccount: jest.fn(async () => ({ events_removed: 0, survey_answers_removed: 0 })),
    track: jest.fn(),
    sessionId: () => SESSION,
    wait: () => new Promise(() => {}),
    ...over,
  };
}

function openFinal(deps = fakeDeps(), onClose = jest.fn()) {
  render(<DeleteAccountModal onClose={onClose} deps={deps} />);
  fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.continue }));
  return { deps, onClose };
}

beforeEach(() => {
  jest.spyOn(navigation, "assign").mockImplementation(() => {});
});
afterEach(() => jest.restoreAllMocks());

describe("the first screen", () => {
  test("opens from the user menu and says what is deleted and what is not", () => {
    render(<UserMenu role="user" username="ana" />);
    fireEvent.click(screen.getByRole("button", { name: "ana ▾" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete my account" }));
    expect(screen.getByRole("dialog", { name: "Delete my account" })).toBeInTheDocument();
    expect(screen.getByText(/analytics events and your survey answers/)).toBeInTheDocument();
    expect(screen.getByText(/exchange accounts and funds are not touched/)).toBeInTheDocument();
    expect(screen.queryByText(EXIT_COPY.en.question)).not.toBeInTheDocument();
  });

  test("Cancel and Escape close it and delete nothing", () => {
    const deps = fakeDeps();
    const onClose = jest.fn();
    render(<DeleteAccountModal onClose={onClose} deps={deps} />);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(deps.deleteAccount).not.toHaveBeenCalled();
    expect(deps.track).not.toHaveBeenCalled();
  });
});

describe("Scenario: Leaving is never blocked", () => {
  test.each([
    ["Skip", () => fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.skip }))],
    ["close (×)", () => fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.close }))],
    ["Delete with no choice", () => fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.confirm }))],
  ])("%s deletes at once and sends no reason", async (_name, act) => {
    const { deps } = openFinal();
    expect(deps.track).toHaveBeenCalledWith("survey_shown", { survey_id: "exit" });
    act();
    await waitFor(() => expect(navigation.assign).toHaveBeenCalledWith("/login"));
    expect(deps.deleteAccount).toHaveBeenCalledTimes(1);
    expect(deps.sendAnswer).not.toHaveBeenCalled();
    expect(deps.track).toHaveBeenCalledWith("survey_dismissed", { survey_id: "exit" });
  });

  test("the question sits below the delete button, and the button works unanswered", () => {
    openFinal();
    const confirm = screen.getByRole("button", { name: DELETE_COPY.confirm });
    const question = screen.getByRole("group", { name: EXIT_COPY.en.question });
    expect(confirm.compareDocumentPosition(question) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(confirm).toBeEnabled();
  });

  test("a failed answer is never shown and the deletion goes ahead", async () => {
    const deps = fakeDeps({ sendAnswer: jest.fn(async () => Promise.reject(new Error("analytics is off for this user: no consent"))) });
    openFinal(deps);
    fireEvent.click(screen.getByRole("radio", { name: "Price" }));
    fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.confirm }));
    await waitFor(() => expect(navigation.assign).toHaveBeenCalledWith("/login"));
    expect(deps.deleteAccount).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  test("an answer that never returns waits only EXIT_ANSWER_WAIT_MS", async () => {
    const deps = fakeDeps({ sendAnswer: () => new Promise(() => {}), wait: () => Promise.resolve() });
    openFinal(deps);
    fireEvent.click(screen.getByRole("radio", { name: "Price" }));
    fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.confirm }));
    await waitFor(() => expect(deps.deleteAccount).toHaveBeenCalledTimes(1));
  });

  test("Escape on the final screen steps back and deletes nothing", () => {
    const { deps, onClose } = openFinal();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.getByRole("button", { name: DELETE_COPY.continue })).toBeInTheDocument();
    expect(deps.deleteAccount).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("Scenario: A consenting user gives a reason", () => {
  test("price is sent, then the account is deleted", async () => {
    const order = [];
    const deps = fakeDeps({
      sendAnswer: jest.fn(async () => order.push("answer")),
      deleteAccount: jest.fn(async () => order.push("delete")),
    });
    openFinal(deps);
    fireEvent.click(screen.getByRole("radio", { name: "Price" }));
    fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.confirm }));
    await waitFor(() => expect(navigation.assign).toHaveBeenCalled());
    expect(deps.sendAnswer).toHaveBeenCalledWith({ survey_id: "exit", session_id: SESSION, codes: { reason_code: "price" } });
    expect(order).toEqual(["answer", "delete"]);
    expect(deps.track).not.toHaveBeenCalledWith("survey_dismissed", expect.anything());
  });

  test("the text box opens only for other, and its text goes with it", async () => {
    const { deps } = openFinal();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Other" }));
    fireEvent.change(screen.getByRole("textbox", { name: EXIT_COPY.en.other }), { target: { value: "moving abroad" } });
    fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.confirm }));
    await waitFor(() => expect(deps.sendAnswer).toHaveBeenCalled());
    expect(deps.sendAnswer.mock.calls[0][0]).toEqual({ survey_id: "exit", session_id: SESSION, codes: { reason_code: "other" }, text: "moving abroad" });
  });

  test("text typed under other is not sent once another reason is chosen", () => {
    expect(exitAnswerBody("price", "moving abroad", SESSION)).toEqual({ survey_id: "exit", session_id: SESSION, codes: { reason_code: "price" } });
    expect(exitAnswerBody("other", "   ", SESSION)).toEqual({ survey_id: "exit", session_id: SESSION, codes: { reason_code: "other" } });
    expect(exitAnswerBody(null, "x", SESSION)).toBeNull();
    expect(exitAnswerBody("saves_time", "", SESSION)).toBeNull();
  });
});

describe("Scenario: Last admin", () => {
  test("the server's reason is shown and the person stays signed in", async () => {
    const deps = fakeDeps({ deleteAccount: jest.fn(async () => Promise.reject(new Error("you are the only admin: make another person an admin on the Users page first, then delete your account"))) });
    openFinal(deps);
    fireEvent.click(screen.getByRole("button", { name: DELETE_COPY.skip }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Not deleted: you are the only admin");
    expect(navigation.assign).not.toHaveBeenCalled();
  });
});

describe("the exit question's words", () => {
  test("shows the exit codes only, never the promoter codes", () => {
    openFinal();
    const radios = screen.getAllByRole("radio");
    expect(radios.map((r) => r.value)).toEqual(EXIT_CODES);
    for (const code of NPS_WHAT_WORKS.filter((c) => c !== "other")) expect(EXIT_CODES).not.toContain(code);
  });

  test.each(["en", "pt"])("the %s question, text box and code labels are surveys.md's", (lang) => {
    const copy = EXIT_COPY[lang];
    expect(SURVEYS_MD).toContain(`"${copy.question}"`);
    expect(SURVEYS_MD).toContain(`"${copy.other}"`);
    for (const code of EXIT_CODES) {
      const row = SURVEYS_MD.split("\n").find((l) => l.startsWith(`| \`${code}\` |`));
      const [pt, en] = row.split("|").slice(2, 4).map((c) => c.trim());
      expect(copy.labels[code]).toBe(lang === "en" ? en : pt);
    }
  });

  test("every sentence of the screen is short and plain (≤ 25 words, ends with a stop)", () => {
    for (const text of [DELETE_COPY.removes, DELETE_COPY.untouched]) {
      for (const sentence of text.split(/(?<=[.!?])\s+/)) {
        expect(sentence.split(/\s+/).length).toBeLessThanOrEqual(25);
        expect(sentence).toMatch(/[.!?]$/);
      }
    }
  });
});
