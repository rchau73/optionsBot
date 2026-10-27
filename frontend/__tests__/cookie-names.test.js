import { cookieNames } from "@/lib/auth/tokens";

// #368: a browser keeps cookies per host, not per port, so the stable (:3000)
// and mvp (:3100) monitors on 127.0.0.1 must not share cookie names.
describe("session cookie names per environment", () => {
  test("stable and mvp get their own names, so one sign-in never replaces the other's", () => {
    const stable = cookieNames("stable");
    const mvp = cookieNames("mvp");
    expect(stable).toEqual({ access: "access_token_stable", refresh: "refresh_token_stable" });
    expect(mvp).toEqual({ access: "access_token_mvp", refresh: "refresh_token_mvp" });
    expect(stable.access).not.toBe(mvp.access);
  });

  test("without an environment (local runs, tests) the plain names", () => {
    expect(cookieNames("")).toEqual({ access: "access_token", refresh: "refresh_token" });
    expect(cookieNames(undefined)).toEqual({ access: "access_token", refresh: "refresh_token" });
  });

  test("only cookie-safe characters reach the name", () => {
    expect(cookieNames("Phone-Check").access).toBe("access_token_phone_check");
    expect(cookieNames("a;b=c d").access).toBe("access_token_a_b_c_d");
  });
});
