import nextJest from "next/jest.js";

const createJestConfig = nextJest({ dir: "./" });

export default createJestConfig({
  testEnvironment: "jsdom",
  setupFilesAfterEnv: ["<rootDir>/jest.setup.js"],
  moduleNameMapper: { "^@/(.*)$": "<rootDir>/$1" },
  testPathIgnorePatterns: ["/node_modules/", "/.next/"],
  testMatch: ["<rootDir>/__tests__/**/*.test.{js,jsx}"],
  // No watchman (#452): one-off runs in a fresh worktree per agent each added a
  // watch root that stayed for days; Jest's own crawler is fast enough here.
  watchman: false,
});
