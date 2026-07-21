import { createHash } from "node:crypto";
import { request as nodeRequest } from "node:http";
import { appendHTBResults, configureHTBActions, isInternalHTBCall, maxHTBInternalRounds } from "./htb-actions.js";
import { configureDevGitActions, isInternalDevGitCall } from "./dev-actions.js";

const PROTOCOL_VERSION = "mcp-devbox.model-turn.v1";
const DRIVER_PROTOCOL_VERSION = "mcp-devbox.model-turn-driver.v1";
const INLINE_REQUEST_BYTES = 64 << 10;
const MAX_REQUEST_BYTES = 4 << 20;
const MAX_RESPONSE_BYTES = 4 << 20;
const MAX_TEXT_BYTES = 1 << 20;
const MAX_TOOL_CALLS = 64;
const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const RUNTIME_PATTERN = /^mr_[a-f0-9]{32}$/;
const TURN_PATTERN = /^mt_[a-f0-9]{32}$/;
const REF_PATTERN = /^(?:mb|lr)_[a-f0-9]{32}$/;
const DIGEST_PATTERN = /^sha256:[a-f0-9]{64}$/;
const RETRYABLE_SOCKET_CODES = new Set(["ENOENT", "ECONNREFUSED", "ECONNRESET", "EPIPE"]);

export function createMCPDevboxModelBridge(options = {}) {
  const providerName = requireIdentifier(options.name ?? "mcp-devbox-model-bridge", "provider name");
  const socketPath = validateSocketPath(options.socketPath);
  const runtimeID = requirePattern(options.runtimeID, RUNTIME_PATTERN, "runtimeID");
  const ttlMs = validateDuration(options.ttlMs ?? 15 * 60 * 1000, "ttlMs", 1000, 60 * 60 * 1000);
  const timeoutMs = validateDuration(options.timeoutMs ?? ttlMs, "timeoutMs", 1000, ttlMs);
  const requestImpl = options.requestImpl ?? createUnixRequester(socketPath);
  if (typeof requestImpl !== "function") throw new Error("requestImpl must be a function");
  const htbRequesterFactory = options.htbRequestImpl === undefined ? createUnixRequester : (() => {
    if (typeof options.htbRequestImpl !== "function") throw new Error("htbRequestImpl must be a function");
    return options.htbRequestImpl;
  });
  const htb = configureHTBActions(options, htbRequesterFactory);
  const devGitRequesterFactory = options.devGitRequestImpl === undefined ? createUnixRequester : (() => {
    if (typeof options.devGitRequestImpl !== "function") throw new Error("devGitRequestImpl must be a function");
    return options.devGitRequestImpl;
  });
  const devGit = configureDevGitActions(options, devGitRequesterFactory);

  return Object.freeze({
    languageModel(modelID) {
      return new PullRendezvousLanguageModel({ providerName, modelID, runtimeID, ttlMs, timeoutMs, requestImpl, htb, devGit });
    },
  });
}

class PullRendezvousLanguageModel {
  specificationVersion = "v3";
  supportedUrls = {};

  constructor({ providerName, modelID, runtimeID, ttlMs, timeoutMs, requestImpl, htb, devGit }) {
    this.provider = providerName;
    this.modelId = requireIdentifier(modelID, "model id");
    this.runtimeID = runtimeID;
    this.ttlMs = ttlMs;
    this.timeoutMs = timeoutMs;
    this.requestImpl = requestImpl;
    this.htb = htb;
    this.devGit = devGit;
    this.queue = Promise.resolve();
  }

  async doGenerate(options) {
    return this.#serialize(async () => {
      const completed = await this.#runWithInternalTools(options);
      if (completed.response.finish_reason === "error") {
        throw new Error(completed.response.text || "external model returned an error");
      }
      return {
        content: responseContent(completed.response, completed.toolsByID),
        finishReason: finishReason(completed.response.finish_reason),
        usage: usage(completed.response.usage),
        providerMetadata: usageMetadata(completed.response.usage),
        warnings: completed.warnings,
        response: {
          id: completed.turn.turn_id,
          timestamp: new Date(completed.turn.created_at),
          modelId: this.modelId,
        },
      };
    });
  }

  async doStream(options) {
    const completed = await this.#serialize(() => this.#runWithInternalTools(options));
    const modelID = this.modelId;
    return {
      stream: new ReadableStream({
        start(controller) {
          controller.enqueue({ type: "stream-start", warnings: completed.warnings });
          controller.enqueue({
            type: "response-metadata",
            id: completed.turn.turn_id,
            timestamp: new Date(completed.turn.created_at),
            modelId: modelID,
          });
          if (completed.response.finish_reason === "error") {
            controller.enqueue({ type: "error", error: new Error(completed.response.text || "external model returned an error") });
            controller.enqueue({
              type: "finish",
              usage: usage(completed.response.usage),
              finishReason: finishReason("error"),
              providerMetadata: usageMetadata(completed.response.usage),
            });
            controller.close();
            return;
          }
          if (completed.response.text) {
            const textID = `${completed.turn.turn_id}.text`;
            controller.enqueue({ type: "text-start", id: textID });
            controller.enqueue({ type: "text-delta", id: textID, delta: completed.response.text });
            controller.enqueue({ type: "text-end", id: textID });
          }
          for (const call of completed.response.tool_calls) {
            const tool = completed.toolsByID.get(call.tool_id);
            controller.enqueue({
              type: "tool-call",
              toolCallId: call.call_id,
              toolName: tool.name,
              input: JSON.stringify(call.arguments),
              providerExecuted: false,
              dynamic: false,
            });
          }
          controller.enqueue({
            type: "finish",
            usage: usage(completed.response.usage),
            finishReason: finishReason(completed.response.finish_reason),
            providerMetadata: usageMetadata(completed.response.usage),
          });
          controller.close();
        },
      }),
      response: {},
    };
  }

  #serialize(operation) {
    const scheduled = this.queue.then(operation, operation);
    this.queue = scheduled.catch(() => undefined);
    return scheduled;
  }

  async #runWithInternalTools(options) {
    const handlers = [this.htb, this.devGit].filter(Boolean);
    if (handlers.length === 0) return this.#runTurn(options);
    if (!Array.isArray(options?.prompt)) throw new Error("prompt must be an array");
    let current = {
      ...options,
      prompt: [...options.prompt],
      tools: handlers.reduce((tools, handler) => handler.augmentTools(tools), options.tools ?? []),
    };
    for (let round = 0; round < maxHTBInternalRounds(); round += 1) {
      const completed = await this.#runTurn(current);
      const calls = completed.response.tool_calls.map((call) => ({ ...call, tool: completed.toolsByID.get(call.tool_id) }));
      const internal = calls.filter((call) => isInternalHTBCall(this.htb, call.tool) || isInternalDevGitCall(this.devGit, call.tool));
      if (internal.length === 0) return completed;
      const results = [];
      for (const call of internal) {
        const handler = isInternalHTBCall(this.htb, call.tool) ? this.htb : this.devGit;
        results.push(await handler.execute(call.tool.name, call.arguments, options.abortSignal));
      }
      current = {
        ...current,
        prompt: appendHTBResults(current.prompt, internal, results),
      };
    }
    throw new Error("internal tool loop exceeded the bounded round limit");
  }

  async #runTurn(options) {
    const externalSignal = options?.abortSignal;
    throwIfAborted(externalSignal);
    const controller = new AbortController();
    const deadline = Date.now() + this.timeoutMs;
    const timeout = setTimeout(() => controller.abort(timeoutError()), this.timeoutMs);
    const relayAbort = () => controller.abort(externalSignal?.reason ?? abortError());
    externalSignal?.addEventListener("abort", relayAbort, { once: true });
    let turn;
    let stage = "request_stage";
    try {
      const normalized = normalizeRequest(this.modelId, options);
      const payloadJSON = canonicalJSON(normalized.payload);
      const payloadBytes = Buffer.byteLength(payloadJSON, "utf8");
      if (payloadBytes > MAX_REQUEST_BYTES) throw new Error("canonical model request exceeds the bridge limit");
      const requestDigest = digest(payloadJSON);
      stage = "runtime_status";
      const status = await this.requestImpl({ method: "GET", path: `/v1/runtimes/${this.runtimeID}`, signal: controller.signal });
      validateRuntimeStatus(status, this.runtimeID);
      const sequence = status.last_sequence + 1;
      let createPayload;
      if (payloadBytes <= INLINE_REQUEST_BYTES) {
        createPayload = JSON.parse(payloadJSON);
      } else {
        stage = "request_stage";
        const staged = await this.requestImpl({
          method: "POST",
          path: "/v1/request-bodies",
          signal: controller.signal,
          rawBody: payloadJSON,
          headers: {
            "Content-Type": "application/json",
            "X-MCP-Request-Digest": requestDigest,
            "X-MCP-TTL-Ms": String(this.ttlMs),
          },
        });
        validateRequestReference(staged, requestDigest, payloadBytes);
        createPayload = undefined;
        normalized.requestRef = staged.request_ref;
      }
      const createBody = {
        runtime_id: this.runtimeID,
        sequence,
        request_digest: requestDigest,
        offered_tools: normalized.offeredTools,
        ttl_ms: this.ttlMs,
      };
      if (createPayload !== undefined) createBody.payload = createPayload;
      else createBody.request_ref = normalized.requestRef;
      stage = "turn_create";
      turn = await this.requestImpl({ method: "POST", path: "/v1/turns", signal: controller.signal, jsonBody: createBody });
      validateTurn(turn, this.runtimeID, sequence, requestDigest, normalized.toolsByID);
      stage = "response_wait";
      const envelope = await waitForResponse({
        requestImpl: this.requestImpl,
        turn,
        signal: controller.signal,
        deadline,
      });
      stage = "response_identity";
      validateResponseEnvelope(envelope, turn);
      const response = validateModelResponse(envelope.payload, normalized.toolsByID);
      return { turn, response, toolsByID: normalized.toolsByID, warnings: normalized.warnings };
    } catch (error) {
      if (turn && controller.signal.aborted) {
        await cancelQuietly(this.requestImpl, turn.turn_id);
      }
      if (controller.signal.aborted) throw abortReason(controller.signal.reason);
      throw closedStageError(stage, error);
    } finally {
      clearTimeout(timeout);
      externalSignal?.removeEventListener("abort", relayAbort);
    }
  }
}

async function waitForResponse({ requestImpl, turn, signal, deadline }) {
  let retries = 0;
  for (;;) {
    throwIfAborted(signal);
    try {
      return await requestImpl({ method: "GET", path: `/v1/turns/${turn.turn_id}/response`, signal });
    } catch (error) {
      if (!isRetryableSocketError(error) || Date.now() >= deadline) throw error;
      retries += 1;
      const delay = Math.min(250, 25 * 2 ** Math.min(retries, 4));
      await sleep(delay, signal);
    }
  }
}

function createUnixRequester(socketPath) {
  return ({ method, path, jsonBody, rawBody, headers = {}, signal }) =>
    new Promise((resolve, reject) => {
      if (jsonBody !== undefined && rawBody !== undefined) {
        reject(new Error("request cannot contain both jsonBody and rawBody"));
        return;
      }
      const body = jsonBody !== undefined ? JSON.stringify(jsonBody) : rawBody;
      const requestHeaders = { Accept: "application/json", ...headers };
      if (jsonBody !== undefined) requestHeaders["Content-Type"] = "application/json";
      if (body !== undefined) requestHeaders["Content-Length"] = String(Buffer.byteLength(body));
      const req = nodeRequest({ socketPath, host: "localhost", method, path, headers: requestHeaders });
      const abort = () => req.destroy(abortReason(signal?.reason));
      if (signal?.aborted) {
        abort();
        return;
      }
      signal?.addEventListener("abort", abort, { once: true });
      req.on("response", (response) => {
        const chunks = [];
        let total = 0;
        response.on("data", (chunk) => {
          total += chunk.length;
          if (total > MAX_RESPONSE_BYTES) {
            req.destroy(new Error("model turn driver response exceeds the limit"));
            return;
          }
          chunks.push(chunk);
        });
        response.on("end", () => {
          signal?.removeEventListener("abort", abort);
          const text = Buffer.concat(chunks).toString("utf8");
          let parsed;
          try {
            parsed = text ? JSON.parse(text) : {};
          } catch {
            reject(new Error(`model turn driver returned non-JSON status ${response.statusCode}`));
            return;
          }
          if (response.statusCode < 200 || response.statusCode >= 300) {
            const detail = typeof parsed?.error === "string" ? parsed.error : (typeof parsed?.failure_category === "string" ? parsed.failure_category : `model turn driver status ${response.statusCode}`);
            const error = new Error(detail);
            error.statusCode = response.statusCode;
            error.driverCode = parsed?.code;
            reject(error);
            return;
          }
          resolve(parsed);
        });
      });
      req.on("error", (error) => {
        signal?.removeEventListener("abort", abort);
        reject(error);
      });
      if (body !== undefined) req.end(body);
      else req.end();
    });
}

function normalizeRequest(modelID, options = {}) {
  if (!Array.isArray(options.prompt)) throw new Error("prompt must be an array");
  const prompt = options.prompt.map(normalizeMessage);
  const toolsByID = new Map();
  const names = new Set();
  const offeredTools = [];
  const payloadTools = [];
  for (const tool of options.tools ?? []) {
    if (!tool || tool.type !== "function") throw new Error("provider tools are not supported in the initial bridge");
    const name = requireIdentifier(tool.name, "tool name");
    if (names.has(name)) throw new Error(`duplicate tool name: ${name}`);
    names.add(name);
    const schema = jsonClone(tool.inputSchema, "tool input schema");
    const toolID = `tool_${digest(`${name}\n${canonicalJSON(schema)}`).slice(7, 31)}`;
    const definition = { id: toolID, name, schema };
    offeredTools.push(definition);
    const payloadTool = {
      id: toolID,
      name,
      description: typeof tool.description === "string" ? tool.description : "",
      input_schema: schema,
    };
    payloadTools.push(payloadTool);
    toolsByID.set(toolID, payloadTool);
  }
  const generation = {};
  copyFiniteNumber(generation, "max_output_tokens", options.maxOutputTokens, { integer: true, min: 1 });
  copyFiniteNumber(generation, "temperature", options.temperature);
  copyFiniteNumber(generation, "top_p", options.topP);
  copyFiniteNumber(generation, "top_k", options.topK);
  copyFiniteNumber(generation, "presence_penalty", options.presencePenalty);
  copyFiniteNumber(generation, "frequency_penalty", options.frequencyPenalty);
  copyFiniteNumber(generation, "seed", options.seed, { integer: true });
  if (options.stopSequences !== undefined) {
    if (!Array.isArray(options.stopSequences) || options.stopSequences.some((item) => typeof item !== "string")) throw new Error("stopSequences must be strings");
    generation.stop_sequences = [...options.stopSequences];
  }
  const warnings = [];
  if (options.headers && Object.keys(options.headers).length > 0) warnings.push({ type: "other", message: "HTTP headers are intentionally ignored" });
  if (options.providerOptions && Object.keys(options.providerOptions).length > 0) warnings.push({ type: "other", message: "provider options are intentionally omitted" });
  return {
    payload: {
      protocol_version: PROTOCOL_VERSION,
      model_id: modelID,
      prompt,
      tools: payloadTools,
      tool_choice: normalizeToolChoice(options.toolChoice),
      generation,
      response_format: normalizeResponseFormat(options.responseFormat),
    },
    offeredTools,
    toolsByID,
    warnings,
    requestRef: undefined,
  };
}

function normalizeMessage(message) {
  if (!message || typeof message !== "object") throw new Error("message must be an object");
  switch (message.role) {
    case "system":
      if (typeof message.content !== "string") throw new Error("system content must be text");
      return { role: "system", content: message.content };
    case "user":
      return { role: "user", content: normalizeParts(message.content, new Set(["text"])) };
    case "assistant":
      return { role: "assistant", content: normalizeParts(message.content, new Set(["text", "reasoning", "tool-call", "tool-result"])) };
    case "tool":
      return { role: "tool", content: normalizeParts(message.content, new Set(["tool-result"])) };
    default:
      throw new Error(`unsupported message role: ${String(message.role)}`);
  }
}

function normalizeParts(parts, allowed) {
  if (!Array.isArray(parts)) throw new Error("message content must be an array");
  return parts.map((part) => {
    if (!part || typeof part !== "object" || !allowed.has(part.type)) throw new Error(`unsupported message part: ${String(part?.type)}`);
    if (part.type === "text" || part.type === "reasoning") {
      if (typeof part.text !== "string") throw new Error(`${part.type} content must be text`);
      return { type: part.type, text: part.text };
    }
    if (part.type === "tool-call") {
      return {
        type: "tool-call",
        tool_call_id: requireIdentifier(part.toolCallId, "tool call id"),
        tool_name: requireIdentifier(part.toolName, "tool name"),
        input: jsonClone(part.input, "tool call input"),
      };
    }
    return {
      type: "tool-result",
      tool_call_id: requireIdentifier(part.toolCallId, "tool call id"),
      tool_name: requireIdentifier(part.toolName, "tool name"),
      output: jsonClone(part.output, "tool result output"),
    };
  });
}

function normalizeToolChoice(choice) {
  if (choice === undefined) return { type: "auto" };
  if (!choice || typeof choice !== "object") throw new Error("toolChoice must be an object");
  if (["auto", "none", "required"].includes(choice.type)) return { type: choice.type };
  if (choice.type === "tool") return { type: "tool", tool_name: requireIdentifier(choice.toolName, "tool choice name") };
  throw new Error(`unsupported tool choice: ${String(choice.type)}`);
}

function normalizeResponseFormat(format) {
  if (format === undefined || format?.type === "text") return { type: "text" };
  if (format?.type !== "json") throw new Error("unsupported response format");
  const normalized = { type: "json" };
  if (format.schema !== undefined) normalized.schema = jsonClone(format.schema, "response schema");
  if (format.name !== undefined) normalized.name = String(format.name);
  if (format.description !== undefined) normalized.description = String(format.description);
  return normalized;
}

function validateModelResponse(raw, toolsByID) {
  const value = typeof raw === "string" ? JSON.parse(raw) : raw;
  requirePlainObject(value, "model response");
  requireExactKeys(value, new Set(["text", "tool_calls", "finish_reason", "usage"]), "model response");
  if (!["stop", "tool_calls", "length", "cancelled", "error"].includes(value.finish_reason)) throw new Error("invalid model finish reason");
  const text = value.text ?? "";
  if (typeof text !== "string" || Buffer.byteLength(text, "utf8") > MAX_TEXT_BYTES) throw new Error("model text is invalid or too large");
  const calls = value.tool_calls ?? [];
  if (!Array.isArray(calls) || calls.length > MAX_TOOL_CALLS) throw new Error("model tool calls are invalid or too many");
  const callIDs = new Set();
  const toolCalls = calls.map((call) => {
    requirePlainObject(call, "model tool call");
    requireExactKeys(call, new Set(["call_id", "tool_id", "arguments"]), "model tool call");
    const callID = requireIdentifier(call.call_id, "response call id");
    const toolID = requireIdentifier(call.tool_id, "response tool id");
    if (callIDs.has(callID)) throw new Error(`duplicate response call id: ${callID}`);
    callIDs.add(callID);
    if (!toolsByID.has(toolID)) throw new Error(`response referenced an unoffered tool id: ${toolID}`);
    requirePlainObject(call.arguments, "tool arguments");
    return { call_id: callID, tool_id: toolID, arguments: jsonClone(call.arguments, "tool arguments") };
  });
  if (value.finish_reason === "tool_calls" && toolCalls.length === 0) throw new Error("tool_calls finish reason requires a tool call");
  return { text, tool_calls: toolCalls, finish_reason: value.finish_reason, usage: validateUsage(value.usage) };
}

function validateUsage(value) {
  if (value === undefined || value === null) return undefined;
  requirePlainObject(value, "usage");
  requireExactKeys(value, new Set(["input_tokens", "output_tokens", "total_tokens"]), "usage");
  const input = requireNonNegativeInteger(value.input_tokens, "input_tokens");
  const output = requireNonNegativeInteger(value.output_tokens, "output_tokens");
  const total = requireNonNegativeInteger(value.total_tokens, "total_tokens");
  if (total < input + output) throw new Error("total_tokens is inconsistent");
  return { input_tokens: input, output_tokens: output, total_tokens: total };
}

function validateRuntimeStatus(value, runtimeID) {
  requirePlainObject(value, "runtime status");
  if (value.runtime_id !== runtimeID) throw new Error("runtime status id mismatch");
  if (!Number.isSafeInteger(value.last_sequence) || value.last_sequence < 0) throw new Error("runtime sequence is invalid");
  if (["cancelled", "completed", "failed"].includes(value.status)) throw new Error(`runtime is terminal: ${value.status}`);
}

function validateRequestReference(value, requestDigest, contentBytes) {
  requirePlainObject(value, "request reference");
  requirePattern(value.request_ref, REF_PATTERN, "request reference");
  if (value.request_digest !== requestDigest || value.content_bytes !== contentBytes) throw new Error("staged request identity mismatch");
}

function validateTurn(turn, runtimeID, sequence, requestDigest, toolsByID) {
  requirePlainObject(turn, "turn");
  requirePattern(turn.turn_id, TURN_PATTERN, "turn id");
  if (turn.runtime_id !== runtimeID || turn.sequence !== sequence || turn.request_digest !== requestDigest) throw new Error("created turn identity mismatch");
  const offered = turn.offered_tool_ids ?? [];
  if (!Array.isArray(offered) || offered.length !== toolsByID.size || offered.some((id) => !toolsByID.has(id))) throw new Error("created turn offered-tool set mismatch");
}

function validateResponseEnvelope(envelope, turn) {
  requirePlainObject(envelope, "model response envelope");
  if (envelope.runtime_id !== turn.runtime_id || envelope.turn_id !== turn.turn_id || envelope.sequence !== turn.sequence || envelope.request_digest !== turn.request_digest) {
    throw new Error("model response identity mismatch");
  }
}

function responseContent(response, toolsByID) {
  const content = [];
  if (response.text) content.push({ type: "text", text: response.text });
  for (const call of response.tool_calls) {
    const tool = toolsByID.get(call.tool_id);
    content.push({ type: "tool-call", toolCallId: call.call_id, toolName: tool.name, input: JSON.stringify(call.arguments), providerExecuted: false, dynamic: false });
  }
  return content;
}

function finishReason(reason) {
  const unified = { stop: "stop", tool_calls: "tool-calls", length: "length", cancelled: "other", error: "error" }[reason];
  if (!unified) throw new Error(`unsupported finish reason: ${reason}`);
  return { unified, raw: reason };
}

function usage(value) {
  return {
    inputTokens: { total: value?.input_tokens, noCache: value?.input_tokens, cacheRead: undefined, cacheWrite: undefined },
    outputTokens: { total: value?.output_tokens, text: value?.output_tokens, reasoning: undefined },
    raw: value ? { total_tokens: value.total_tokens } : undefined,
  };
}

function usageMetadata(value) {
  return { "mcp-devbox": { usage_source: value ? "external-reported-unverified" : "unknown" } };
}

async function cancelQuietly(requestImpl, turnID) {
  try {
    await requestImpl({ method: "DELETE", path: `/v1/turns/${turnID}` });
  } catch {
    // Best effort only after the caller has already aborted.
  }
}

function validateSocketPath(value) {
  if (typeof value !== "string" || !value.startsWith("/") || value.includes("\0")) throw new Error("socketPath must be an absolute Unix socket path");
  return value;
}

function validateDuration(value, label, minimum, maximum) {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) throw new Error(`${label} is invalid`);
  return value;
}

function requirePattern(value, pattern, label) {
  if (typeof value !== "string" || !pattern.test(value)) throw new Error(`${label} is invalid`);
  return value;
}

function requireIdentifier(value, label) {
  return requirePattern(value, ID_PATTERN, label);
}

function requirePlainObject(value, label) {
  if (value === null || typeof value !== "object" || Array.isArray(value) || Object.getPrototypeOf(value) !== Object.prototype) throw new Error(`${label} must be a plain JSON object`);
  return value;
}

function requireExactKeys(value, allowed, label) {
  for (const key of Object.keys(value)) if (!allowed.has(key)) throw new Error(`${label} contains unknown field: ${key}`);
}

function requireNonNegativeInteger(value, label) {
  if (!Number.isSafeInteger(value) || value < 0) throw new Error(`${label} must be a non-negative integer`);
  return value;
}

function copyFiniteNumber(target, key, value, constraints = {}) {
  if (value === undefined) return;
  if (typeof value !== "number" || !Number.isFinite(value)) throw new Error(`${key} must be finite`);
  if (constraints.integer && !Number.isSafeInteger(value)) throw new Error(`${key} must be an integer`);
  if (constraints.min !== undefined && value < constraints.min) throw new Error(`${key} is too small`);
  target[key] = Object.is(value, -0) ? 0 : value;
}

function jsonClone(value, label) {
  try {
    return JSON.parse(canonicalJSON(value));
  } catch (error) {
    throw new Error(`${label} must be JSON-serializable: ${error.message}`);
  }
}

function canonicalJSON(value) {
  return JSON.stringify(sortJSON(value));
}

function sortJSON(value) {
  if (value === null || typeof value === "string" || typeof value === "boolean") return value;
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new Error("non-finite JSON number");
    return Object.is(value, -0) ? 0 : value;
  }
  if (Array.isArray(value)) return value.map(sortJSON);
  if (typeof value !== "object" || Object.getPrototypeOf(value) !== Object.prototype) throw new Error("non-JSON value");
  const sorted = {};
  for (const key of Object.keys(value).sort()) if (value[key] !== undefined) sorted[key] = sortJSON(value[key]);
  return sorted;
}

function digest(value) {
  return `sha256:${createHash("sha256").update(value, "utf8").digest("hex")}`;
}

function isRetryableSocketError(error) {
  return RETRYABLE_SOCKET_CODES.has(error?.code) || error?.message === "socket hang up";
}

function sleep(milliseconds, signal) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, milliseconds);
    const abort = () => {
      clearTimeout(timer);
      reject(abortReason(signal?.reason));
    };
    if (signal?.aborted) abort();
    else signal?.addEventListener("abort", abort, { once: true });
  });
}

function throwIfAborted(signal) {
  if (signal?.aborted) throw abortReason(signal.reason);
}

function abortError() {
  const error = new Error("model turn aborted");
  error.name = "AbortError";
  return error;
}

function timeoutError() {
  const error = new Error("model turn timed out");
  error.name = "TimeoutError";
  return error;
}

function abortReason(reason) {
  if (reason instanceof Error) return reason;
  return abortError();
}

function closedStageError(stage, cause) {
  const allowed = ["runtime_status", "request_stage", "turn_create", "response_wait", "response_identity"];
  const code = allowed.includes(stage) ? stage : "response_identity";
  const error = new Error(code);
  error.name = "MCPDevboxStageError";
  error.mcpStage = code;
  Object.defineProperty(error, "cause", { value: cause, enumerable: false });
  return error;
}

export const __test = Object.freeze({
  PROTOCOL_VERSION,
  DRIVER_PROTOCOL_VERSION,
  INLINE_REQUEST_BYTES,
  MAX_REQUEST_BYTES,
  canonicalJSON,
  digest,
  normalizeRequest,
  validateModelResponse,
  finishReason,
  usage,
  usageMetadata,
  createUnixRequester,
  waitForResponse,
});
