// Server-side only: the delivery report cmd/dora writes (#470, dora.sh daily),
// read for the admin Analytics page's Delivery (DORA) topic (#473). The
// environments' shared <envs>/history/delivery is mounted read-only at
// DORA_DIR (docker-compose.yml); the file is read as written, never
// recomputed.
import fs from "node:fs/promises";
import path from "node:path";

/** DORA_DIR, default data/delivery under the monitor's working directory. */
export function doraDir(raw = process.env.DORA_DIR) {
  return raw || path.join(process.cwd(), "data", "delivery");
}

/** dir/dora.json → { report, writtenAt } (the file's time, ISO), or null when there is none. Throws on a damaged file. */
export async function readDora(dir) {
  const file = path.join(dir, "dora.json");
  let raw, stat;
  try {
    [raw, stat] = await Promise.all([fs.readFile(file, "utf8"), fs.stat(file)]);
  } catch (err) {
    if (err.code === "ENOENT") return null;
    throw err;
  }
  return { report: JSON.parse(raw), writtenAt: stat.mtime.toISOString() };
}
