import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createMCPDevboxModelBridge, __test } from "./index.js";

const runtimeID = "mr_11111111111111111111111111111111";
const turnID = "mt_22222222222222222222222222222222";
const requestRef = "mb_33333333333333333333333333333333";
const localRequestRef = "lr_44444444444444444444444444444444";

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
  let lastSequence = 0;
  const createdTurns = [];
  let waitFailures = options.waitFailures ?? 0;
  const requestImpl = async (request) => {
    calls.push(request);
    if (request.method === "GET" && request.path === `/v1/runtimes/${runtimeID}`) {
      return { runtime_id: runtimeID, status: "ready", last_sequence: lastSequence };
    }
    if (request.method === "POST" && request.path === "/v1/request-bodies") {
      const bytes = Buffer.byteLength(request.rawBody);
      return { request_ref: options.requestRef ?? requestRef, request_digest: request.headers["X-MCP-Request-Digest"], content_bytes: bytes, expires_at: new Date(Date.now() + 60_000).toISOString() };
    }
    if (request.method === "POST" && request.path === "/v1/turns") {
      created = request.jsonBody;
      createdTurns.push(created);
      lastSequence = created.sequence;
      return {
        runtime_id: runtimeID,
        turn_id: turnID,
        sequence: created.sequence,
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
      return { runtime_id: runtimeID, turn_id: turnID, sequence: created.sequence, request_digest: created.request_digest, payload };
    }
    if (request.method === "DELETE" && request.path === `/v1/turns/${turnID}`) return { turn_id: turnID, status: "cancelled" };
    throw new Error(`unexpected fake request ${request.method} ${request.path}`);
  };
  return { requestImpl, calls, get created() { return created; }, get createdTurns() { return createdTurns; } };
}

function createModel(driver, extra = {}) {
  return createMCPDevboxModelBridge({
    socketPath: "/private/model-turn-driver.sock",
    runtimeID,
    requestImpl: driver.requestImpl,
    ttlMs: extra.ttlMs ?? 60_000,
    timeoutMs: extra.timeoutMs ?? 5_000,
    htbSocketPath: extra.htbSocketPath,
    htbWorkspaceID: extra.htbWorkspaceID,
    htbTools: extra.htbTools,
    htbRequestImpl: extra.htbRequestImpl,
    devGitSocketPath: extra.devGitSocketPath,
    devGitWorkspaceID: extra.devGitWorkspaceID,
    devGitTools: extra.devGitTools,
    devGitRequestImpl: extra.devGitRequestImpl,
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

test("large request accepts only authoritative or private local request_ref prefixes", async () => {
  const local = fakeDriver(() => ({ text: "local accepted", tool_calls: [], finish_reason: "stop" }), { requestRef: localRequestRef });
  await createModel(local).doGenerate(modelOptions("x".repeat(__test.INLINE_REQUEST_BYTES + 4096)));
  assert.equal(local.created.request_ref, localRequestRef);

  const invalid = fakeDriver(() => ({ text: "unreachable", tool_calls: [], finish_reason: "stop" }), { requestRef: "xx_55555555555555555555555555555555" });
  await assert.rejects(() => createModel(invalid).doGenerate(modelOptions("x".repeat(__test.INLINE_REQUEST_BYTES + 4096))), /request_stage/);
  assert.equal(invalid.calls.some((call) => call.path === "/v1/turns"), false);
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
  await assert.rejects(() => createModel(duplicate).doGenerate(modelOptions()), /response_identity/);

  const invented = fakeDriver(() => ({ finish_reason: "tool_calls", tool_calls: [{ call_id: "call-1", tool_id: "invented", arguments: {} }] }));
  await assert.rejects(() => createModel(invented).doGenerate(modelOptions()), /response_identity/);
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
    await assert.rejects(() => createModel(driver).doGenerate(modelOptions()), /response_identity/);
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
  const source = (await readFile(new URL("./index.js", import.meta.url), "utf8")) + (await readFile(new URL("./htb-actions.js", import.meta.url), "utf8")) + (await readFile(new URL("./dev-actions.js", import.meta.url), "utf8"));
  for (const forbidden of ["OPENAI_API_KEY", "ANTHROPIC_API_KEY", "openrouter", "api.openai.com", "api.anthropic.com", "playwright", "puppeteer", "responses.create", "fetch(", "https.request", "net.connect"]) {
    assert.equal(source.toLowerCase().includes(forbidden.toLowerCase()), false, `forbidden source marker: ${forbidden}`);
  }
  assert.match(source, /socketPath/);
  assert.match(source, /node:http/);
});


function htbDefinitions() {
  const workspace = { type: "string", pattern: "^ws_[a-f0-9]{32}$" };
  const session = { type: "string", pattern: "^hs_[a-f0-9]{32}$" };
  const closed = (properties, required) => ({ type: "object", properties, required, additionalProperties: false });
  return [
    { name: "workspace_htb_status", description: "Safe Hack The Box authorized CTF status", input_schema: closed({ workspace_id: workspace }, ["workspace_id"]) },
    { name: "workspace_htb_auth_validate", description: "Validate local credentials for an authorized Hack The Box CTF", input_schema: closed({ workspace_id: workspace, username: { type: "string" }, credential: closed({ source: { type: "string" }, extract_after: { type: "string" } }, ["source", "extract_after"]), timeout_seconds: { type: "integer" } }, ["workspace_id", "username", "credential", "timeout_seconds"]) },
    { name: "workspace_htb_command", description: "Run a target-locked command in an authorized HTB CTF", input_schema: closed({ workspace_id: workspace, session_id: session, command: { type: "string" }, timeout_seconds: { type: "integer" } }, ["workspace_id", "session_id", "command", "timeout_seconds"]) },
    { name: "workspace_htb_command_save", description: "Save local output from an authorized HTB CTF", input_schema: closed({ workspace_id: workspace, session_id: session, command: { type: "string" }, save_output: { type: "string" }, timeout_seconds: { type: "integer" } }, ["workspace_id", "session_id", "command", "save_output", "timeout_seconds"]) },
    { name: "workspace_htb_command_with_credential_stdin", description: "Use local credential stdin in an authorized HTB CTF", input_schema: closed({ workspace_id: workspace, session_id: session, command: { type: "string" }, timeout_seconds: { type: "integer" } }, ["workspace_id", "session_id", "command", "timeout_seconds"]) },
    { name: "workspace_htb_session_close", description: "Close an authorized Hack The Box CTF session", input_schema: closed({ workspace_id: workspace, session_id: session }, ["workspace_id", "session_id"]) },
  ];
}

test("HTB actions are injected and executed internally without returning a Bash or OpenCode tool call", async () => {
  const workspaceID = "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  const htbCalls = [];
  let responseIndex = 0;
  const driver = fakeDriver((created) => {
    if (responseIndex++ === 0) {
      const tool = created.offered_tools.find((item) => item.name === "workspace_htb_status");
      assert.ok(tool);
      return { finish_reason: "tool_calls", tool_calls: [{ call_id: "htb-status-1", tool_id: tool.id, arguments: { workspace_id: workspaceID } }] };
    }
    return { text: "authorized lab status received", tool_calls: [], finish_reason: "stop" };
  });
  const result = await createModel(driver, {
    htbSocketPath: "/runtime/htb-lab-broker.sock",
    htbWorkspaceID: workspaceID,
    htbTools: htbDefinitions(),
    htbRequestImpl: async (request) => {
      htbCalls.push(request);
      return { status: "ok", workspace_id: workspaceID, mode: "htb-linux", authorized: true };
    },
  }).doGenerate(modelOptions());
  assert.equal(result.content.length, 1);
  assert.equal(result.content[0].text, "authorized lab status received");
  assert.equal(htbCalls.length, 1);
  assert.equal(htbCalls[0].method, "POST");
  assert.equal(htbCalls[0].path, "/v1/status");
  assert.deepEqual(htbCalls[0].jsonBody, { workspace_id: workspaceID });
  assert.equal(driver.createdTurns.length, 2);
  const secondPrompt = driver.createdTurns[1].payload.prompt;
  assert.equal(secondPrompt.at(-2).role, "assistant");
  assert.equal(secondPrompt.at(-1).role, "tool");
  assert.equal(JSON.stringify(secondPrompt).includes("mcp-edge lab ssh-exec"), false);
});

test("mixed HTB and ordinary calls execute HTB safely and defer ordinary work instead of killing the runtime", async () => {
  const workspaceID = "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  const htbCalls = [];
  let responseIndex = 0;
  const driver = fakeDriver((created) => {
    const htb = created.offered_tools.find((item) => item.name === "workspace_htb_status");
    const read = created.offered_tools.find((item) => item.name === "read_file");
    if (responseIndex++ === 0) {
      return { finish_reason: "tool_calls", tool_calls: [
        { call_id: "htb-status-mixed", tool_id: htb.id, arguments: { workspace_id: workspaceID } },
        { call_id: "read-deferred", tool_id: read.id, arguments: { path: "current-state.md" } },
      ] };
    }
    return { finish_reason: "tool_calls", tool_calls: [{ call_id: "read-reissued", tool_id: read.id, arguments: { path: "current-state.md" } }] };
  });
  const result = await createModel(driver, {
    htbSocketPath: "/runtime/htb-lab-broker.sock",
    htbWorkspaceID: workspaceID,
    htbTools: htbDefinitions(),
    htbRequestImpl: async (request) => {
      htbCalls.push(request);
      return { status: "ok", workspace_id: workspaceID, authorized: true };
    },
  }).doGenerate(modelOptions());
  assert.equal(htbCalls.length, 1);
  assert.equal(result.content.length, 1);
  assert.equal(result.content[0].toolName, "read_file");
  assert.equal(result.content[0].toolCallId, "read-reissued");
  assert.equal(driver.createdTurns.length, 2);
});

test("dev runtimes do not offer HTB tools", async () => {
  const driver = fakeDriver(() => ({ text: "dev", tool_calls: [], finish_reason: "stop" }));
  await createModel(driver).doGenerate(modelOptions());
  assert.equal(driver.created.offered_tools.some((tool) => tool.name.startsWith("workspace_htb_")), false);
});

test("HTB action workspace mismatch fails before the broker requester", async () => {
  const workspaceID = "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  let brokerCalled = false;
  const driver = fakeDriver((created) => {
    const tool = created.offered_tools.find((item) => item.name === "workspace_htb_status");
    return { finish_reason: "tool_calls", tool_calls: [{ call_id: "bad-workspace", tool_id: tool.id, arguments: { workspace_id: "ws_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" } }] };
  });
  await assert.rejects(() => createModel(driver, {
    htbSocketPath: "/runtime/htb-lab-broker.sock",
    htbWorkspaceID: workspaceID,
    htbTools: htbDefinitions(),
    htbRequestImpl: async () => { brokerCalled = true; return {}; },
  }).doGenerate(modelOptions()), /workspace does not match/);
  assert.equal(brokerCalled, false);
});

function devGitDefinitions() {
  const workspace = { type: "string", pattern: "^ws_[a-f0-9]{32}$" };
  const simple = { type: "string" };
  const closed = (properties, required) => ({ type: "object", properties, required, additionalProperties: false });
  return [
    { name: "workspace_dev_git_clone", description: "Clone a Git repository with Edge-only authentication", input_schema: closed({ workspace_id: workspace, repository: simple, branch: simple, directory: simple }, ["workspace_id", "repository", "branch", "directory"]) },
    { name: "workspace_dev_publish_preview", description: "Preview a Git publication plan", input_schema: closed({ workspace_id: workspace, directory: simple, branch: simple }, ["workspace_id", "directory", "branch"]) },
    { name: "workspace_dev_publish", description: "Execute a Git publication plan", input_schema: closed({ workspace_id: workspace, plan_id: simple }, ["workspace_id", "plan_id"]) },
  ];
}

test("development Git actions clone and publish through the private broker without exposing credentials", async () => {
  const workspaceID = "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  const planID = "dp_44444444444444444444444444444444";
  const brokerCalls = [];
  let responseIndex = 0;
  const driver = fakeDriver((created) => {
    const name = responseIndex === 0 ? "workspace_dev_git_clone" : responseIndex === 1 ? "workspace_dev_publish_preview" : responseIndex === 2 ? "workspace_dev_publish" : undefined;
    responseIndex += 1;
    if (!name) return { text: "private repository published", tool_calls: [], finish_reason: "stop" };
    const tool = created.offered_tools.find((item) => item.name === name);
    const argumentsByName = {
      workspace_dev_git_clone: { workspace_id: workspaceID, repository: "ekoparty-trip-agent", branch: "mvp-flight-first-telegram-agent", directory: "ekoparty-trip-agent" },
      workspace_dev_publish_preview: { workspace_id: workspaceID, directory: "ekoparty-trip-agent", branch: "mvp-flight-first-telegram-agent" },
      workspace_dev_publish: { workspace_id: workspaceID, plan_id: planID },
    };
    return { finish_reason: "tool_calls", tool_calls: [{ call_id: `dev-${responseIndex}`, tool_id: tool.id, arguments: argumentsByName[name] }] };
  });
  const result = await createModel(driver, {
    devGitSocketPath: "/runtime/dev-git-broker.sock",
    devGitWorkspaceID: workspaceID,
    devGitTools: devGitDefinitions(),
    devGitRequestImpl: async (request) => {
      brokerCalls.push(request);
      if (request.path === "/v1/publish-preview") return { status: "ok", plan_id: planID };
      return { status: "ok", published: request.path === "/v1/publish" };
    },
  }).doGenerate(modelOptions());
  assert.equal(result.content[0].text, "private repository published");
  assert.deepEqual(brokerCalls.map((call) => call.path), ["/v1/clone", "/v1/publish-preview", "/v1/publish"]);
  assert.equal(JSON.stringify(brokerCalls).includes("token"), false);
  assert.equal(driver.createdTurns.length, 4);
});

test("development Git action rejects injected authority fields before reaching the broker", async () => {
  const workspaceID = "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  let brokerCalled = false;
  const driver = fakeDriver((created) => {
    const tool = created.offered_tools.find((item) => item.name === "workspace_dev_git_clone");
    return { finish_reason: "tool_calls", tool_calls: [{ call_id: "bad-dev", tool_id: tool.id, arguments: { workspace_id: workspaceID, repository: "repo", branch: "main", directory: "repo", token: "forbidden" } }] };
  });
  await assert.rejects(() => createModel(driver, {
    devGitSocketPath: "/runtime/dev-git-broker.sock", devGitWorkspaceID: workspaceID, devGitTools: devGitDefinitions(),
    devGitRequestImpl: async () => { brokerCalled = true; return {}; },
  }).doGenerate(modelOptions()), /forbidden field/);
  assert.equal(brokerCalled, false);
});
