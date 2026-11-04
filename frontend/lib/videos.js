// The how-to video manifest (#611, epic E11). The source of truth is
// docs/product/education/videos.json; content/videos.json is its copy, because
// the monitor image is built from frontend/ alone (a test fails when it is stale).
// No screen uses it yet. A video with status "storyboard" is not recorded: it has no file URL.
import videos from "@/content/videos.json";

/** A video by id (`video.margin_bar`), or null. */
export function findVideo(id, entries = videos) {
  return Object.hasOwn(entries, id) ? entries[id] : null;
}

/** The videos that link a tip key (a setting), in id order; [] when none. */
export function videosForTip(tipKey, entries = videos) {
  return allVideos(entries).filter((v) => v.tips.includes(tipKey));
}

/** Every video, in id order. */
export function allVideos(entries = videos) {
  return Object.keys(entries).sort().map((id) => entries[id]);
}
