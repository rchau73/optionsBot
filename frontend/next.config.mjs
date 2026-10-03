/** @type {import('next').NextConfig} */
const nextConfig = {
  // Self-contained server build for the Docker image.
  output: "standalone",
  reactStrictMode: true,
  // This app is its own project root (avoid picking up stray lockfiles above it).
  turbopack: { root: import.meta.dirname },
};

export default nextConfig;
