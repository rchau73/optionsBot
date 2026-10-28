/**
 * @jest-environment node
 */
import { DEFAULT_MODEL, REQUEST_SYSTEM_PROMPT, SYSTEM_PROMPT, anthropicClient, buildRequest, parseReply, parseRequestReply } from "@/lib/assistant/model";
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
  expect(req.output_config.format.schema.required).toEqual(["answer", "citations", "report"]);
});

test("the request check (#409): its own prompt and the verdict schema", () => {
  const req = buildRequest({ question: "the chart is wrong", passages, requestCheck: true });
  expect(req.system).toBe(REQUEST_SYSTEM_PROMPT);
  expect(req.system).toMatch(/never forecast/i);
  const schema = req.output_config.format.schema;
  expect(schema.required).toEqual(expect.arrayContaining(["kind", "what", "where", "evidence", "explained_by", "steps"]));
  expect(schema.properties.kind.enum).toEqual(["bug", "enhancement", "none"]);
  expect(req.messages[0].content).toContain("<question>\nthe chart is wrong\n</question>");
});

test("checkRequest sends the request check and reads the verdict", async () => {
  const verdict = {
    kind: "bug",
    answer: "",
    citations: [],
    what: " A chart is off ",
    where: { page: "Overview", bot: "btc", time: "" },
    evidence: [{ text: "total 0.5", citation: "[pnl:btc/2026-10-27T14:00:00Z]" }, { text: "", citation: "x" }],
    steps: ["open Overview", ""],
    explained_by: [],
    title: "t",
    expected: "e",
    who: "",
    why: "",
    example: "",
  };
  const fetchFn = jest.fn(async () => Response.json({ stop_reason: "end_turn", content: [{ type: "text", text: JSON.stringify(verdict) }] }));
  const got = await anthropicClient({ apiKey: "test-key", fetchFn }).checkRequest({ question: "q", passages });
  expect(got).toMatchObject({
    kind: "bug",
    what: "A chart is off",
    where: { page: "Overview", bot: "btc", time: "" },
    evidence: [{ text: "total 0.5", citation: "pnl:btc/2026-10-27T14:00:00Z" }],
    steps: ["open Overview"],
  });
  expect(JSON.parse(fetchFn.mock.calls[0][1].body).system).toBe(REQUEST_SYSTEM_PROMPT);
});

test.each([
  ["a refusal stop", { stop_reason: "refusal", content: [] }],
  ["text that is not JSON", { content: [{ type: "text", text: "plain" }] }],
  ["an unknown kind", { content: [{ type: "text", text: '{"kind":"complaint","what":"x"}' }] }],
])("parseRequestReply: %s is kind none", (_name, body) => {
  const v = parseRequestReply(body);
  expect(v.kind).toBe("none");
  expect(v.evidence).toEqual([]);
});

test("ask sends the key in a header and reads the JSON answer", async () => {
  const fetchFn = jest.fn(async () =>
    Response.json({ stop_reason: "end_turn", content: [{ type: "text", text: JSON.stringify({ answer: "Delta is…", citations: ["glossary:delta"] }) }] }),
  );
  const client = anthropicClient({ apiKey: "test-key", fetchFn });
  expect(await client.ask({ question: "q", passages })).toEqual({ text: "Delta is…", citations: ["glossary:delta"], report: false, stop: "end_turn" });
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
  ["text that is not JSON", { stop_reason: "end_turn", content: [{ type: "text", text: "plain" }] }, { text: "plain", citations: [], report: false, stop: "end_turn" }],
  ["citations that are not strings", { content: [{ type: "text", text: '{"answer":"a","citations":[1,"x"]}' }] }, { text: "a", citations: ["x"], report: false, stop: "" }],
])("parseReply: %s has no usable citation", (_name, body, want) => {
  expect(parseReply(body)).toEqual(want);
});

test("parseReply keeps only the id a model copies from the sources list (#412)", () => {
  const body = {
    stop_reason: "end_turn",
    content: [{ type: "text", text: '{"answer":"a","citations":["[glossary:delta/1]"," glossary:theta ","[glossary:dvol/1] dvol","[x"]}' }],
  };
  expect(parseReply(body).citations).toEqual(["glossary:delta/1", "glossary:theta", "glossary:dvol/1", "[x"]);
});
