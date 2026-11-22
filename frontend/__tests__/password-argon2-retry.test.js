/**
 * @jest-environment node
 */
// #866: an argon2 computation that throws (hash-wasm could not get its
// WebAssembly memory under a loaded run) is retried once, then thrown; it is
// never read as a wrong password, which answered a right password 401.
import { argon2id, argon2Verify } from "hash-wasm";
import { hashPassword, verifyPassword } from "@/lib/auth/password";

jest.mock("hash-wasm", () => {
  const actual = jest.requireActual("hash-wasm");
  return { ...actual, argon2id: jest.fn(actual.argon2id), argon2Verify: jest.fn(actual.argon2Verify) };
});

jest.setTimeout(60000);

const PASSWORD = "violet tractor sings";
const NO_MEMORY = new RangeError("WebAssembly.Memory(): could not allocate memory");

let logs;
let hash;
beforeAll(async () => {
  hash = await hashPassword(PASSWORD);
});
beforeEach(() => {
  logs = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  argon2Verify.mockClear();
  argon2id.mockClear();
});
afterEach(() => jest.restoreAllMocks());

describe("Scenario: argon2 fails once", () => {
  test("verifying the right password still says true, with a warning", async () => {
    argon2Verify.mockRejectedValueOnce(NO_MEMORY);
    expect(await verifyPassword(PASSWORD, hash)).toBe(true);
    expect(argon2Verify).toHaveBeenCalledTimes(2);
    expect(logs.join("\n")).toContain("argon2 failed: retrying once");
    expect(logs.join("\n")).not.toContain(PASSWORD);
  });

  test("a wrong password stays false after the retry", async () => {
    argon2Verify.mockRejectedValueOnce(NO_MEMORY);
    expect(await verifyPassword("violet tractor hums", hash)).toBe(false);
  });

  test("hashing still returns a hash that verifies", async () => {
    argon2id.mockRejectedValueOnce(NO_MEMORY);
    const again = await hashPassword(PASSWORD);
    expect(argon2id).toHaveBeenCalledTimes(2);
    expect(await verifyPassword(PASSWORD, again)).toBe(true);
  });
});

describe("Scenario: argon2 fails twice", () => {
  test("verifying throws instead of answering wrong password", async () => {
    argon2Verify.mockRejectedValueOnce(NO_MEMORY).mockRejectedValueOnce(NO_MEMORY);
    await expect(verifyPassword(PASSWORD, hash)).rejects.toThrow("could not allocate memory");
  });

  test("hashing throws", async () => {
    argon2id.mockRejectedValueOnce(NO_MEMORY).mockRejectedValueOnce(NO_MEMORY);
    await expect(hashPassword(PASSWORD)).rejects.toThrow("could not allocate memory");
  });
});

describe("Scenario: a malformed hash", () => {
  test.each([
    ["not a hash", "not a hash"],
    ["empty", ""],
    ["null", null],
    ["another scheme", "$2b$10$abcdefghijklmnopqrstuv"],
    ["cut short", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA"],
  ])("%s: false, argon2 not run", async (_, bad) => {
    expect(await verifyPassword(PASSWORD, bad)).toBe(false);
    expect(argon2Verify).not.toHaveBeenCalled();
  });
});
