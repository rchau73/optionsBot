// No AUTH_SECRET (≥ 32 bytes) or an unreadable user database → the server
// exits instead of serving pages nobody can sign in to. Also creates the first
// admin from ADMIN_USERNAME / ADMIN_PASSWORD when there are no users.
import { getAuth } from "./lib/auth/server";
import { authLog } from "./lib/auth/log";

try {
  await getAuth();
} catch (err) {
  authLog("ERROR", "monitor cannot start", { error: err.message });
  process.exit(1);
}
