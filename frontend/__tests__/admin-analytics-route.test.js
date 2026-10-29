/**
 * @jest-environment node
 */
// #445: GET /api/admin/analytics serves the weekly usage reports cmd/usage
// wrote, as written, to admins only.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET as analyticsRoute } from "@/app/api/admin/analytics/route";
import { isoWeekOf, parseAnalyticsQuery } from "@/lib/analytics/usageView";
import { signedIn, useTestAuth } from "@/test/auth";
import { usageReport } from "@/test/usageReport";

jest.setTimeout(30000);

const ORIGIN = "http://localhost:3000";
const get = (query, cookie) => analyticsRoute(new Request(`${ORIGIN}/api/admin/analytics${query ? `?${query}` : ""}`, { headers: { cookie } }));

let dir;
beforeEach(() => {
  useTestAuth();
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "usage-reports-"));
  process.env.USAGE_REPORTS_DIR = dir;
  process.env.ANALYTICS_SALT = "s".repeat(20); // a test value, not a secret
  process.env.MONITOR_DEPLOYMENT = "stable";
});
afterEach(() => {
  jest.restoreAllMocks();
  for (const k of ["USAGE_REPORTS_DIR", "ANALYTICS_SALT", "MONITOR_DEPLOYMENT"]) delete process.env[k];
  fs.rmSync(dir, { recursive: true, force: true });
});

function writeReport(env, week, report = usageReport(week)) {
  fs.mkdirSync(path.join(dir, env), { recursive: true });
  fs.writeFileSync(path.join(dir, env, `${week}.json`), JSON.stringify(report));
}

describe("Scenario: admin sees the latest week", () => {
  test("reports for W43 and W44 → W44, as written, with its drops and the week list", async () => {
    writeReport("stable", "2026-W43");
    writeReport("stable", "2026-W44");
    fs.writeFileSync(path.join(dir, "stable", "2026-W44.md"), "# not a report file");
    const { cookie } = await signedIn("root", "admin");
    const res = await get("", cookie);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.off).toBe(false);
    expect(body.env).toBe("stable");
    expect(body.weeks).toEqual(["2026-W43", "2026-W44"]);
    expect(body.week).toBe("2026-W44");
    expect(body.file).toBe("2026-W44.json");
    expect(Date.parse(body.written_at)).not.toBeNaN();
    expect(body.report.week).toBe("2026-W44");
    expect(body.report.drops.drops[0].metric).toBe("active_users");
    // The host's folders are cut to the file name.
    expect(body.report.source.file).toBe("events.jsonl");
    // This monitor's own environment: consent counts of its users (the admin, never asked).
    expect(body.consent).toEqual({ yes: 0, no: 0, unasked: expect.any(Number) });
    expect(body.current_week).toBe(isoWeekOf(new Date()));
  });

  test("week= picks that week; env=mvp reads mvp's folder, without this monitor's consent counts", async () => {
    writeReport("stable", "2026-W43");
    writeReport("stable", "2026-W44");
    writeReport("mvp", "2026-W42");
    const { cookie } = await signedIn("root", "admin");
    expect((await (await get("week=2026-W43", cookie)).json()).report.week).toBe("2026-W43");
    const mvp = await (await get("env=mvp", cookie)).json();
    expect(mvp.weeks).toEqual(["2026-W42"]);
    expect(mvp.week).toBe("2026-W42");
    expect(mvp.consent).toBeNull();
  });

  test("no report at all → week and report null, 200", async () => {
    const { cookie } = await signedIn("root", "admin");
    const body = await (await get("", cookie)).json();
    expect(body).toMatchObject({ off: false, weeks: [], week: null, report: null });
  });
});

describe("Scenario: not an admin", () => {
  test("a manager or a user gets 403 and no figure; no session 401", async () => {
    writeReport("stable", "2026-W44");
    for (const role of ["manager", "user"]) {
      const { cookie } = await signedIn(`p-${role}`, role);
      const res = await get("", cookie);
      expect(res.status).toBe(403);
      expect((await res.json()).report).toBeUndefined();
    }
    expect((await get("", "")).status).toBe(401);
  });
});

describe("Scenario: analytics off", () => {
  test("ANALYTICS_SALT unset → off: true and no figure, even with reports on disk", async () => {
    delete process.env.ANALYTICS_SALT;
    writeReport("stable", "2026-W44");
    const { cookie } = await signedIn("root", "admin");
    const body = await (await get("", cookie)).json();
    expect(body.off).toBe(true);
    expect(body.report).toBeUndefined();
    expect(body.weeks).toBeUndefined();
  });
});

describe("missing and bad weeks", () => {
  test("a week without a report → 404 with the weeks there are, report null", async () => {
    writeReport("stable", "2026-W44");
    const { cookie } = await signedIn("root", "admin");
    const res = await get("week=2026-W40", cookie);
    expect(res.status).toBe(404);
    expect(await res.json()).toMatchObject({ week: "2026-W40", weeks: ["2026-W44"], report: null, error: "no report for 2026-W40" });
  });

  test.each(["week=2026-44", "week=2026-W54", "week=../../etc/passwd", "env=live", "env=..%2Fstable"])("%s → 400", async (query) => {
    const { cookie } = await signedIn("root", "admin");
    const res = await get(query, cookie);
    expect(res.status).toBe(400);
    expect((await res.json()).report).toBeUndefined();
  });

  test("a damaged report file → 500 with no figure", async () => {
    fs.mkdirSync(path.join(dir, "stable"), { recursive: true });
    fs.writeFileSync(path.join(dir, "stable", "2026-W44.json"), "{ not json");
    const { cookie } = await signedIn("root", "admin");
    const res = await get("", cookie);
    expect(res.status).toBe(500);
  });
});

describe("parseAnalyticsQuery and isoWeekOf", () => {
  test("env defaults to this monitor's deployment, else stable", () => {
    expect(parseAnalyticsQuery(new URLSearchParams(""), "mvp")).toEqual({ ok: true, topic: "usage", env: "mvp", week: null });
    expect(parseAnalyticsQuery({}, "dev")).toEqual({ ok: true, topic: "usage", env: "stable", week: null });
    expect(parseAnalyticsQuery({ week: "2026-W01" }).week).toBe("2026-W01");
  });

  // #473 Scenario: an admin opens Delivery with no env → stable (production), even on the mvp monitor.
  test("Delivery's env defaults to stable; an unknown topic is refused", () => {
    expect(parseAnalyticsQuery(new URLSearchParams("topic=delivery"), "mvp")).toEqual({ ok: true, topic: "delivery", env: "stable", week: null });
    expect(parseAnalyticsQuery({ topic: "delivery", env: "mvp" }, "stable").env).toBe("mvp");
    expect(parseAnalyticsQuery({ topic: "sales" })).toEqual({ ok: false, error: "topic: one of usage, delivery" });
  });

  test.each([
    ["2026-10-26T00:00:00Z", "2026-W44"],
    ["2026-11-01T23:59:59Z", "2026-W44"],
    ["2027-01-01T12:00:00Z", "2026-W53"],
    ["2025-12-29T00:00:00Z", "2026-W01"],
  ])("%s is %s", (iso, want) => {
    expect(isoWeekOf(new Date(iso))).toBe(want);
  });
});
