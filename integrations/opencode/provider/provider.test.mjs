import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createMCPDevboxModelBridge, __test } from "./index.js";

const runtimeID = "mr_11111111111111111111111111111111";
const turnID = "mt_22222222222222222222222222222222";
const requestRef = "mb_33333333333333333333333333333333";

function modelOptions(text = "inspect the repository") {
  return {
    prompt: [{ role: "user", content: [{ type: "text", text }] }],
    tools: [
      { type: "function", name: "read_file", description: "Read one file", inputSchema: { type: "object", properties: { path: { type: "string" } }, required: ["path"] } },
    ],
  };
}

function fakeDriver(responseFactory, options = {}) {
  const calls = [];
  let created;
  let waitFailures = options.waitFailures ?? 0;
  const requestImpl = async (request) => {
    calls.push(request);
    if (request.method === "GET" && request.path === `/v1/runtimes/${runtimeID}`) {
      return { runtime_id: runtimeID, status: "ready", last_sequence: 0 };
    }
    if (request.method === "POST" && request.path === "/v1/request-bodies") {
      const bytes = Buffer.byteLength(request.rawBody);
      return { request_ref: requestRef, request_digest: request.headers["X-MCP-Request-Digest"], content_bytes: bytes, expires_at: new Date(Date.now() + 60_000).toISOString() };
    }
    if (request.method === "POST" && request.path === "/v1/turns") {
      created = request.jsonBody;
      return {
        runtime_id: runtimeID,
        turn_id: turnID,
        sequence: 1,
        request_digest: created.request_digest,
        offered_tool_ids: created.offered_tools.map((tool) => tool.id),
        created_at: "2026-07-15T12:00:00Z",
        expires_at: "2026-07-15T12:15:00Z",
      };
    }
    if (request.method === "GET" && request.path === `/v1/turns/${turnID}/response`) {
      if (waitFailures > 0) {
        waitFailures -= 1;
        const error = new Error("socket reset");
        error.code = "ECONNRESET";
        throw error;
      }
      const payload = await responseFactory(created, calls);
      return { runtime_id: runtimeID, turn_id: turnID, sequence: 1, request_digest: created.request_digest, payload };
    }
    if (request.method === "DELETE" && request.path === `/v1/turns/${turnID}`) return { turn_id: turnID, status: "cancelled" };
    throw new Error(`unexpected fake request ${request.method} ${request.path}`);
  };
  return { requestImpl, calls, get created() { return created; } };
}

function createModel(driver, extra = {}) {
  return createMCPDevboxModelBridge({
    socketPath: "/private/model-turn-driver.sock",
    runtimeID,
    requestImpl: driver.requestImpl,
    ttlMs: extra.ttlMs ?? 60_000,
    timeoutMs: extra.timeoutMs ?? 5_000,
  }).languageModel("external-model");
}

test("canonicalization and digest are deterministic", () => {
  const first = __test.canonicalJSON({ z: 1, a: { y: 2, x: [3, { b: true, a: false }] } });
  const second = __test.canonicalJSON({ a: { x: [3, { a: false, b: true }], y: 2 }, z: 1 });
  assert.equal(first, second);
  assert.match(__test.digest(first), /^sha256:[a-f0-9]{64}$/);
});

test("inline request and LanguageModelV3 stream event order are valid", async () => {
  const driver = fakeDriver((created) => ({ text: "done", tool_calls: [], finish_reason: "stop" }));
  const model = createModel(driver);
  assert.equal(model.specificationVersion, "v3");
  const result = await model.doStream(modelOptions());
  const events = [];
  for await (const event of result.stream) events.push(event);
  assert.deepEqual(events.map((event) => event.type), ["stream-start", "response-metadata", "text-start", "text-delta", "text-end", "finish"]);
  assert.equal(events.at(-1).finishReason.unified, "stop");
  assert.equal(events.at(-1).usage.inputTokens.total, undefined);
  assert.equal(events.at(-1).providerMetadata["mcp-devbox"].usage_source, "unknown");
  assert.ok(driver.created.payload);
  assert.equal(driver.created.request_ref, undefined);
});

test("large request uses immutable request_ref contract instead of payload_json", async () => {
  const driver = fakeDriver(() => ({ text: "large accepted", tool_calls: [], finish_reason: "stop" }));
  const model = createModel(driver);
  await model.doGenerate(modelOptions("x".repeat(__test.INLINE_REQUEST_BYTES + 4096)));
  const stage = driver.calls.find((call) => call.path === "/v1/request-bodies");
  assert.ok(stage);
  assert.equal(typeof stage.rawBody, "string");
  assert.equal(stage.jsonBody, undefined);
  assert.match(stage.headers["X-MCP-Request-Digest"], /^sha256:/);
  assert.equal(driver.created.request_ref, requestRef);
  assert.equal(driver.created.payload, undefined);
  assert.equal(JSON.stringify(driver.created).includes("payload_json"), false);
});

test("same offered tool may be called more than once with distinct call_id", async () => {
  const driver = fakeDriver((created) => {
    const toolID = created.offered_tools[0].id;
    return {
      finish_reason: "tool_calls",
      tool_calls: [
        { call_id: "call-1", tool_id: toolID, arguments: { path: "README.md" } },
        { call_id: "call-2", tool_id: toolID, arguments: { path: "go.mod" } },
      ],
    };
  });
  const result = await createModel(driver).doGenerate(modelOptions());
  assert.equal(result.content.length, 2);
  assert.equal(result.content[0].toolName, "read_file");
  assert.equal(result.content[1].toolName, "read_file");
  assert.notEqual(result.content[0].toolCallId, result.content[1].toolCallId);
});

test("duplicate call_id and unoffered tool IDs fail closed", async () => {
  const duplicate = fakeDriver((created) => {
    const toolID = created.offered_tools[0].id;
    return { finish_reason: "tool_calls", tool_calls: [{ call_id: "same", tool_id: toolID, arguments: {} }, { call_id: "same", tool_id: toolID, arguments: {} }] };
  });
  await assert.rejects(() => createModel(duplicate).doGenerate(modelOptions()), /duplicate response call id/);

  const invented = fakeDriver(() => ({ finish_reason: "tool_calls", tool_calls: [{ call_id: "call-1", tool_id: "invented", arguments: {} }] }));
  await assert.rejects(() => createModel(invented).doGenerate(modelOptions()), /unoffered tool id/);
});

test("wrong runtime, turn sequence, or digest in response is rejected", async () => {
  for (const mutate of [
    (value) => { value.runtime_id = "mr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; },
    (value) => { value.sequence += 1; },
    (value) => { value.request_digest = `sha256:${"0".repeat(64)}`; },
  ]) {
    const driver = fakeDriver(() => ({ finish_reason: "stop", tool_calls: [] }));
    const original = driver.requestImpl;
    driver.requestImpl = async (request) => {
      const value = await original(request);
      if (request.path.endsWith("/response")) mutate(value);
      return value;
    };
    await assert.rejects(() => createModel(driver).doGenerate(modelOptions()), /response identity mismatch/);
  }
});

test("AbortSignal cancels the exact created turn", async () => {
  let waitingReject;
  const driver = fakeDriver(() => new Promise((_, reject) => { waitingReject = reject; }));
  const original = driver.requestImpl;
  driver.requestImpl = async (request) => {
    if (request.method === "GET" && request.path.endsWith("/response")) {
      return new Promise((resolve, reject) => {
        const abort = () => reject(request.signal.reason);
        request.signal.addEventListener("abort", abort, { once: true });
      });
    }
    return original(request);
  };
  const controller = new AbortController();
  const operation = createModel(driver).doGenerate({ ...modelOptions(), abortSignal: controller.signal });
  while (!driver.created) await new Promise((resolve) => setTimeout(resolve, 1));
  controller.abort(new Error("caller cancelled"));
  await assert.rejects(() => operation, /caller cancelled/);
  assert.ok(driver.calls.some((call) => call.method === "DELETE" && call.path.endsWith(turnID)));
  void waitingReject;
});

test("timeout cancels without fabricating a fallback", async () => {
  const driver = fakeDriver(() => ({ finish_reason: "stop" }));
  const original = driver.requestImpl;
  driver.requestImpl = async (request) => {
    if (request.method === "GET" && request.path.endsWith("/response")) {
      return new Promise((resolve, reject) => request.signal.addEventListener("abort", () => reject(request.signal.reason), { once: true }));
    }
    return original(request);
  };
  await assert.rejects(() => createModel(driver, { ttlMs: 2_000, timeoutMs: 1_000 }).doGenerate(modelOptions()), /timed out/);
  assert.equal(driver.calls.filter((call) => call.method === "POST" && call.path === "/v1/turns").length, 1);
  assert.ok(driver.calls.some((call) => call.method === "DELETE"));
});

test("driver restart retries waiting for the same turn only", async () => {
  const driver = fakeDriver(() => ({ text: "resumed", tool_calls: [], finish_reason: "stop" }), { waitFailures: 2 });
  const result = await createModel(driver).doGenerate(modelOptions());
  assert.equal(result.content[0].text, "resumed");
  assert.equal(driver.calls.filter((call) => call.method === "POST" && call.path === "/v1/turns").length, 1);
  assert.equal(driver.calls.filter((call) => call.method === "GET" && call.path.endsWith("/response")).length, 3);
});

test("tool results are preserved in the following canonical request", () => {
  const normalized = __test.normalizeRequest("external-model", {
    prompt: [{ role: "tool", content: [{ type: "tool-result", toolCallId: "call-1", toolName: "read_file", output: { type: "text", value: "contents" } }] }],
    tools: [],
  });
  assert.deepEqual(normalized.payload.prompt[0].content[0], {
    type: "tool-result",
    tool_call_id: "call-1",
    tool_name: "read_file",
    output: { type: "text", value: "contents" },
  });
});

test("reported usage is explicitly unverified and missing usage stays unknown", () => {
  assert.equal(__test.usageMetadata(undefined)["mcp-devbox"].usage_source, "unknown");
  assert.equal(__test.usageMetadata({ input_tokens: 1, output_tokens: 2, total_tokens: 3 })["mcp-devbox"].usage_source, "external-reported-unverified");
  assert.equal(__test.usage(undefined).inputTokens.total, undefined);
});

test("provider source has no provider fallback, browser automation, API key, or TCP model client", async () => {
  const source = await readFile(new URL("./index.js", import.meta.url), "utf8");
  for (const forbidden of ["OPENAI_API_KEY", "ANTHROPIC_API_KEY", "openrouter", "api.openai.com", "api.anthropic.com", "playwright", "puppeteer", "responses.create", "fetch(", "https.request", "net.connect"]) {
    assert.equal(source.toLowerCase().includes(forbidden.toLowerCase()), false, `forbidden source marker: ${forbidden}`);
  }
  assert.match(source, /socketPath/);
  assert.match(source, /node:http/);
});
