// Runs once when the monitor's server starts (Node.js only): see instrumentation-node.js.
export async function register() {
  if (process.env.NEXT_RUNTIME === "nodejs") {
    await import("./instrumentation-node");
  }
}
