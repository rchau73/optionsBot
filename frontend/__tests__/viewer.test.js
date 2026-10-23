/**
 * @jest-environment node
 */
import { signAccess, authKey, ACCESS_COOKIE } from "@/lib/auth/tokens";

// #287: the page reads the signed-in person's role for the details default.
let cookieValue;
jest.mock("next/headers", () => ({
  cookies: async () => ({ get: (name) => (name === ACCESS_COOKIE && cookieValue ? { value: cookieValue } : undefined) }),
}));

const { viewerRole } = require("@/lib/auth/viewer");
const SECRET = "x".repeat(40);

describe("viewerRole", () => {
  const saved = process.env.AUTH_SECRET;
  beforeEach(() => {
    process.env.AUTH_SECRET = SECRET;
    cookieValue = undefined;
  });
  afterAll(() => {
    process.env.AUTH_SECRET = saved;
  });

  test.each(["user", "manager", "admin"])("the access token's role: %s", async (role) => {
    cookieValue = await signAccess({ id: 1, username: "ana", role }, authKey(SECRET));
    expect(await viewerRole()).toBe(role);
  });

  test("no cookie, a forged token or no AUTH_SECRET → null", async () => {
    expect(await viewerRole()).toBeNull();
    cookieValue = await signAccess({ id: 1, username: "ana", role: "admin" }, authKey("y".repeat(40)));
    expect(await viewerRole()).toBeNull();
    delete process.env.AUTH_SECRET;
    expect(await viewerRole()).toBeNull();
  });
});
