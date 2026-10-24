// No AUTH_SECRET (≥ 32 bytes) or an unreadable user database → the server
// exits instead of serving pages nobody can sign in to. Also creates the first
// admin from ADMIN_USERNAME / ADMIN_PASSWORD when there are no users, and
// starts the analytics retention job (prune now, then daily; #296).
import { authDbPath, getAuth } from "./lib/auth/server";
import { authLog } from "./lib/auth/log";
import { auditPath, fileAudit } from "./lib/auth/audit";
import { eventsPath } from "./lib/analytics/config.js";
import { startRetentionJob } from "./lib/analytics/retention.js";

try {
  await getAuth();
} catch (err) {
  authLog("ERROR", "monitor cannot start", { error: err.message });
  process.exit(1);
}

const db = authDbPath();
startRetentionJob({ file: eventsPath(db), audit: fileAudit(auditPath(db)) });
