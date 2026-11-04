/**
 * @jest-environment node
 */
import fs from "node:fs";
import path from "node:path";
import { GET as videosRoute } from "@/app/api/help/videos/route";
import { allVideos, findVideo, videosForTip } from "@/lib/videos";
import { signedIn, useTestAuth } from "@/test/auth";

// #611 (epic E11): the video manifest served read-only to any signed-in role.

const get = (cookie) => videosRoute(new Request("http://monitor/api/help/videos", { headers: cookie ? { cookie } : {} }));

beforeEach(() => useTestAuth());

describe("Scenario: the monitor serves the manifest", () => {
  test("a signed-in session receives every video", async () => {
    const { cookie } = await signedIn("viewer", "user");
    const res = await get(cookie);
    expect(res.status).toBe(200);
    const { videos } = await res.json();
    const source = JSON.parse(fs.readFileSync(path.join(__dirname, "../content/videos.json"), "utf8"));
    expect(videos.map((v) => v.id)).toEqual(Object.keys(source).sort());
    expect(videos[0]).toMatchObject({ id: expect.any(String), title: expect.any(String), task: expect.any(String), status: "storyboard" });
  });

  test("a request without a session is refused", async () => {
    expect((await get(null)).status).toBe(401);
  });

  test("the lookup helper returns the videos for a tip key", () => {
    expect(videosForTip("rollout_dte").map((v) => v.id)).toEqual(["video.change_setting"]);
    expect(videosForTip("max_mm_pct").map((v) => v.id)).toEqual(["video.margin_bar"]);
    expect(videosForTip("strategy_id")).toEqual([]);
    expect(videosForTip("constructor")).toEqual([]);
  });

  test("a video by id; an unknown id is null", () => {
    expect(findVideo("video.freeze").duration_sec).toBe(45);
    expect(findVideo("video.nope")).toBeNull();
    expect(findVideo("constructor")).toBeNull();
    expect(allVideos().length).toBeGreaterThan(0);
  });

  test("every tip that names a video is found through videosForTip", () => {
    const tips = JSON.parse(fs.readFileSync(path.join(__dirname, "../content/tips.json"), "utf8"));
    for (const [key, tip] of Object.entries(tips)) {
      if (tip.video) expect(videosForTip(key).map((v) => v.id)).toContain(tip.video);
    }
  });
});

describe("Scenario: a stale copy is caught", () => {
  test("content/videos.json equals docs/product/education/videos.json (cp docs/product/education/videos.json frontend/content/videos.json)", () => {
    const read = (rel) => fs.readFileSync(path.join(__dirname, rel), "utf8");
    expect(read("../content/videos.json")).toBe(read("../../docs/product/education/videos.json"));
  });
});
