/**
 * @jest-environment node
 */
// #473: GET /api/admin/analytics/delivery serves dora.json as cmd/dora wrote
// it, to admins only (403 for anyone else, as the Usage topic).
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET as deliveryRoute } from "@/app/api/admin/analytics/delivery/route";
import { signedIn, useTestAuth } from "@/test/auth";
import { doraReport } from "@/test/doraReport";

jest.setTimeout(30000);

const get = (cookie) => deliveryRoute(new Request("http://localhost:3000/api/admin/analytics/delivery", { headers: { cookie } }));

let dir;
beforeEach(() => {
  useTestAuth();
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "dora-"));
  process.env.DORA_DIR = dir;
});
afterEach(() => {
  jest.restoreAllMocks();
  delete process.env.DORA_DIR;
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Scenario: dora.json with 4 weeks", () => {
  test("an admin gets the report as written, with its file and time", async () => {
    fs.writeFileSync(path.join(dir, "dora.json"), JSON.stringify(doraReport()));
    const { cookie } = await signedIn("root", "admin");
    const res = await get(cookie);
    expect(res.status).toBe(200);
    expect(res.headers.get("Cache-Control")).toBe("no-store");
    const body = await res.json();
    expect(body.report).toEqual(doraReport());
    expect(body.file).toBe("dora.json");
    expect(Date.parse(body.written_at)).not.toBeNaN();
  });
});

describe("Scenario: no dora.json", () => {
  test("report null with 200: the page shows the empty state", async () => {
    const { cookie } = await signedIn("root", "admin");
    const res = await get(cookie);
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ report: null, file: null, written_at: null });
  });

  test("a damaged file → 500 with no figure", async () => {
    fs.writeFileSync(path.join(dir, "dora.json"), "{ not json");
    const { cookie } = await signedIn("root", "admin");
    const res = await get(cookie);
    expect(res.status).toBe(500);
    expect((await res.json()).report).toBeUndefined();
  });
});

describe("Scenario: a non-admin", () => {
  test("a manager or a user gets 403 and no figure; no session 401", async () => {
    fs.writeFileSync(path.join(dir, "dora.json"), JSON.stringify(doraReport()));
    for (const role of ["manager", "user"]) {
      const { cookie } = await signedIn(`p-${role}`, role);
      const res = await get(cookie);
      expect(res.status).toBe(403);
      expect((await res.json()).report).toBeUndefined();
    }
    expect((await get("")).status).toBe(401);
  });
});
