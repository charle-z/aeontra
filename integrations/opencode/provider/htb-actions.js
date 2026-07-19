const WORKSPACE_PATTERN = /^ws_[a-f0-9]{32}$/;
const SESSION_PATTERN = /^hs_[a-f0-9]{32}$/;
const MAX_INTERNAL_ROUNDS = 32;

const ENDPOINTS = Object.freeze({
  workspace_htb_status: "/v1/status",
  workspace_htb_auth_validate: "/v1/auth-validate",
  workspace_htb_command: "/v1/command",
  workspace_htb_command_save: "/v1/command-save",
  workspace_htb_command_with_credential_stdin: "/v1/command-credential-stdin",
  workspace_htb_session_close: "/v1/session-close",
});

export function configureHTBActions(options, createRequester) {
  const values = [options.htbSocketPath, options.htbWorkspaceID, options.htbTools];
  const configured = values.some((value) => value !== undefined);
  if (!configured) return undefined;
  if (values.some((value) => value === undefined)) throw new Error("HTB provider options must be configured together");
  if (typeof options.htbSocketPath !== "string" || !options.htbSocketPath.startsWith("/") || options.htbSocketPath.includes("\0")) {
    throw new Error("htbSocketPath must be an absolute Unix socket path");
  }
  if (typeof options.htbWorkspaceID !== "string" || !WORKSPACE_PATTERN.test(options.htbWorkspaceID)) {
    throw new Error("htbWorkspaceID is invalid");
  }
  const definitions = validateDefinitions(options.htbTools);
  const names = new Set(definitions.map((definition) => definition.name));
  const requestImpl = createRequester(options.htbSocketPath);
  return Object.freeze({
    workspaceID: options.htbWorkspaceID,
    definitions,
    names,
    requestImpl,
    augmentTools(tools = []) {
      const existing = new Set();
      for (const tool of tools) {
        if (tool?.type === "function" && typeof tool.name === "string") existing.add(tool.name);
      }
      for (const name of names) if (existing.has(name)) throw new Error(`HTB tool name is already offered: ${name}`);
      return [
        ...tools,
        ...definitions.map((definition) => ({
          type: "function",
          name: definition.name,
          description: definition.description,
          inputSchema: definition.input_schema,
        })),
      ];
    },
    async execute(name, args, signal) {
      if (!names.has(name)) throw new Error("unrecognized HTB action");
      validateArguments(name, args, options.htbWorkspaceID);
      return requestImpl({ method: "POST", path: ENDPOINTS[name], jsonBody: args, signal });
    },
  });
}

export function isInternalHTBCall(htb, tool) {
  return Boolean(htb && tool && htb.names.has(tool.name));
}

export function appendHTBResults(prompt, calls, results) {
  const assistant = [];
  const tool = [];
  for (let index = 0; index < calls.length; index += 1) {
    const call = calls[index];
    assistant.push({
      type: "tool-call",
      toolCallId: call.call_id,
      toolName: call.tool.name,
      input: call.arguments,
    });
    tool.push({
      type: "tool-result",
      toolCallId: call.call_id,
      toolName: call.tool.name,
      output: results[index],
    });
  }
  return [...prompt, { role: "assistant", content: assistant }, { role: "tool", content: tool }];
}

export function maxHTBInternalRounds() {
  return MAX_INTERNAL_ROUNDS;
}

function validateDefinitions(value) {
  if (!Array.isArray(value) || value.length !== Object.keys(ENDPOINTS).length) throw new Error("HTB tool definitions are invalid");
  const names = new Set();
  const output = value.map((definition) => {
    requirePlainObject(definition, "HTB tool definition");
    requireExactKeys(definition, new Set(["name", "description", "input_schema"]), "HTB tool definition");
    if (typeof definition.name !== "string" || ENDPOINTS[definition.name] === undefined || names.has(definition.name)) throw new Error("HTB tool name is invalid");
    if (typeof definition.description !== "string" || !/Hack The Box|HTB|CTF/.test(definition.description)) throw new Error("HTB tool description must be transparent");
    requireClosedSchema(definition.input_schema, definition.name);
    names.add(definition.name);
    return JSON.parse(JSON.stringify(definition));
  });
  for (const name of Object.keys(ENDPOINTS)) if (!names.has(name)) throw new Error(`HTB tool definition is missing: ${name}`);
  return output;
}

function requireClosedSchema(schema, name) {
  requirePlainObject(schema, `${name} input schema`);
  if (schema.type !== "object" || schema.additionalProperties !== false) throw new Error(`${name} input schema must be closed`);
  requirePlainObject(schema.properties, `${name} properties`);
  if (!Array.isArray(schema.required) || !schema.required.includes("workspace_id")) throw new Error(`${name} must require workspace_id`);
  for (const forbidden of ["target", "host", "ip", "password", "private_key", "key_path"]) {
    if (Object.hasOwn(schema.properties, forbidden)) throw new Error(`${name} exposes forbidden field: ${forbidden}`);
  }
}

function validateArguments(name, args, workspaceID) {
  requirePlainObject(args, `${name} arguments`);
  if (args.workspace_id !== workspaceID) throw new Error("HTB action workspace does not match the runtime");
  for (const forbidden of ["target", "host", "ip", "password", "private_key", "key_path", "port", "pty"]) {
    if (Object.hasOwn(args, forbidden)) throw new Error(`HTB action contains forbidden field: ${forbidden}`);
  }
  if (name !== "workspace_htb_status" && name !== "workspace_htb_auth_validate") {
    if (typeof args.session_id !== "string" || !SESSION_PATTERN.test(args.session_id)) throw new Error("HTB session id is invalid");
  }
}

function requirePlainObject(value, label) {
  if (value === null || typeof value !== "object" || Array.isArray(value) || Object.getPrototypeOf(value) !== Object.prototype) throw new Error(`${label} must be a plain JSON object`);
}

function requireExactKeys(value, allowed, label) {
  for (const key of Object.keys(value)) if (!allowed.has(key)) throw new Error(`${label} contains unknown field: ${key}`);
  if (Object.keys(value).length !== allowed.size) throw new Error(`${label} is incomplete`);
}
