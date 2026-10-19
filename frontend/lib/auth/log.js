// One JSON line per auth event on stdout (docker logs), like the bot's slog
// lines. Never pass a password, hash, token or AUTH_SECRET here.
export function authLog(level, msg, fields = {}) {
  const line = JSON.stringify({ time: new Date().toISOString(), level, msg, component: "auth", ...fields });
  if (level === "ERROR" || level === "WARN") console.error(line);
  else console.log(line);
}
