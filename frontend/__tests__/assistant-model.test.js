/**
 * @jest-environment node
 */
import { DEFAULT_MODEL, SYSTEM_PROMPT, anthropicClient, buildRequest, parseReply } from "@/lib/assistant/model";
import { DONT_KNOW, REFUSAL } from "@/lib/assistant/phrases";

// The model client (#191): the request it sends and how it reads the reply,
// with a fake fetch — no test calls the API.

const passages = [{ id: "glossary:delta/1", source: "glossary:delta", title: "delta", text: "How much an option moves." }];

test("off without a key", () => {
  expect(anthropicClient({ apiKey: "" })).toBeNull();
});

test("the system prompt carries the contract's replies and rules", () => {
  expect(SYSTEM_PROMPT).toContain(DONT_KNOW);
  expect(SYSTEM_PROMPT).toContain(REFUSAL);
  expect(SYSTEM_PROMPT).toMatch(/never forecast/i);
});

test("the request: sources with their ids, the question, a JSON answer", () => {
  const req = buildRequest({ question: "what is delta?", passages });
  expect(req.model).toBe(DEFAULT_MODEL);
  expect(req.messages[0].content).toContain("[glossary:delta/1] delta\nHow much an option moves.");
  expect(req.messages[0].content).toContain("<question>\nwhat is delta?\n</question>");
  expect(req.output_config.format.schema.required).toEqual(["answer", "citations"]);
});

test("ask sends the key in a header and reads the JSON answer", async () => {
  const fetchFn = jest.fn(async () =>
    Response.json({ stop_reason: "end_turn", content: [{ type: "text", text: JSON.stringify({ answer: "Delta is…", citations: ["glossary:delta"] }) }] }),
  );
  const client = anthropicClient({ apiKey: "test-key", fetchFn });
  expect(await client.ask({ question: "q", passages })).toEqual({ text: "Delta is…", citations: ["glossary:delta"], stop: "end_turn" });
  const [, init] = fetchFn.mock.calls[0];
  expect(init.headers["x-api-key"]).toBe("test-key");
  expect(JSON.parse(init.body).messages).toHaveLength(1);
});

test("an API error is thrown, naming the status only", async () => {
  const client = anthropicClient({ apiKey: "test-key", fetchFn: async () => new Response("overloaded", { status: 529 }) });
  await expect(client.ask({ question: "q", passages })).rejects.toThrow("model API answered 529");
});

test.each([
  ["a refusal stop", { stop_reason: "refusal", content: [] }, { text: "", citations: [], stop: "refusal" }],
  ["text that is not JSON", { stop_reason: "end_turn", content: [{ type: "text", text: "plain" }] }, { text: "plain", citations: [], stop: "end_turn" }],
  ["citations that are not strings", { content: [{ type: "text", text: '{"answer":"a","citations":[1,"x"]}' }] }, { text: "a", citations: ["x"], stop: "" }],
])("parseReply: %s has no usable citation", (_name, body, want) => {
  expect(parseReply(body)).toEqual(want);
});
