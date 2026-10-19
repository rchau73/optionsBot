/** @type {import('next').NextConfig} */
const nextConfig = {
  // Self-contained server build, used by the Docker image only (it sets
  // NEXT_OUTPUT=standalone); `npm start` locally keeps the regular output.
  output: process.env.NEXT_OUTPUT === "standalone" ? "standalone" : undefined,
  reactStrictMode: true,
  // sql.js (the user database) loads its .wasm from node_modules at runtime.
  serverExternalPackages: ["sql.js", "hash-wasm"],
  outputFileTracingIncludes: { "/**": ["./node_modules/sql.js/dist/sql-wasm.wasm"] },
  // This app is its own project root (avoid picking up stray lockfiles above it).
  turbopack: { root: import.meta.dirname },
};

export default nextConfig;
