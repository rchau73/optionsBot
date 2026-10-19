import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { apiFetch, fetchBotState, navigation } from "@/lib/api";
import { loginNext } from "@/lib/session";
import LoginForm from "@/components/LoginForm";

const expired = () => ({ ok: false, status: 401, headers: new Headers({ "x-monitor-session": "required" }), json: async () => ({}) });
const ok = (body = {}) => ({ ok: true, status: 200, headers: new Headers(), json: async () => body });

describe("Scenario: silent refresh and logout (browser)", () => {
  const realFetch = global.fetch;
  const realAssign = navigation.assign;
  beforeEach(() => {
    navigation.assign = jest.fn();
  });
  afterEach(() => {
    global.fetch = realFetch;
    navigation.assign = realAssign;
  });

  test("an expired access token refreshes once for every poll and keeps working", async () => {
    let session = "expired";
    global.fetch = jest.fn(async (url) => {
      if (url === "/api/auth/refresh") {
        await new Promise((r) => setTimeout(r, 10));
        session = "fresh";
        return ok();
      }
      if (session === "expired") return expired();
      return ok(url.endsWith("/account") ? null : { url });
    });

    const state = await fetchBotState("btc"); // four requests at once
    expect(state.status).toEqual({ url: "/api/bots/btc/status" });
    expect(global.fetch.mock.calls.filter(([u]) => u === "/api/auth/refresh")).toHaveLength(1);
    expect(navigation.assign).not.toHaveBeenCalled();
  });

  test("when the refresh fails, the page goes to /login and comes back afterwards", async () => {
    global.fetch = jest.fn(async (url) => (url === "/api/auth/refresh" ? { ok: false, status: 401 } : expired()));
    const res = await apiFetch("/api/bots/btc/status");
    expect(res.status).toBe(401);
    expect(navigation.assign).toHaveBeenCalledWith(`/login?next=${encodeURIComponent("/")}`);
  });

  test("a bot's own 401 is an error, not a sign-out", async () => {
    global.fetch = jest.fn(async () => ({ ok: false, status: 401, headers: new Headers(), json: async () => ({ error: "bad token" }) }));
    const res = await apiFetch("/api/bots/btc/close", { method: "POST" });
    expect(res.status).toBe(401);
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(navigation.assign).not.toHaveBeenCalled();
  });
});

describe("sign-in page", () => {
  const realFetch = global.fetch;
  const realAssign = navigation.assign;
  afterEach(() => {
    global.fetch = realFetch;
    navigation.assign = realAssign;
  });

  test("shows the server's answer and clears the password", async () => {
    global.fetch = jest.fn(async (url) =>
      url === "/api/auth/refresh" ? { ok: false, status: 401 } : { ok: false, status: 401, json: async () => ({ error: "invalid username or password" }) },
    );
    render(<LoginForm />);
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "alice" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "wrong password" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("invalid username or password");
    expect(screen.getByLabelText("Password")).toHaveValue("");
    const [, init] = global.fetch.mock.calls.find(([u]) => u === "/api/auth/login");
    expect(JSON.parse(init.body)).toEqual({ username: "alice", password: "wrong password" });
    await waitFor(() => expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled());
  });

  test("goes back only to a page of this monitor", () => {
    expect(loginNext("?next=%2F%3Fbot%3Dbtc")).toBe("/?bot=btc");
    expect(loginNext("")).toBe("/");
    expect(loginNext("?next=https://evil.example")).toBe("/");
    expect(loginNext("?next=//evil.example")).toBe("/");
    expect(loginNext("?next=/login")).toBe("/");
    expect(loginNext("?next=/api/bots")).toBe("/");
  });
});
