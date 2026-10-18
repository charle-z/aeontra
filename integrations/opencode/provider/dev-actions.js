const WORKSPACE_PATTERN = /^ws_[a-f0-9]{32}$/;
const PLAN_PATTERN = /^dp_[a-f0-9]{32}$/;
const ENDPOINTS = Object.freeze({
  workspace_dev_git_clone: "/v1/clone",
  workspace_dev_publish_preview: "/v1/publish-preview",
  workspace_dev_publish: "/v1/publish",
});

export function configureDevGitActions(options, createRequester) {
  const values = [options.devGitSocketPath, options.devGitWorkspaceID, options.devGitTools];
  const configured = values.some((value) => value !== undefined);
  if (!configured) return undefined;
  if (values.some((value) => value === undefined)) throw new Error("development Git provider options must be configured together");
  if (typeof options.devGitSocketPath !== "string" || !options.devGitSocketPath.startsWith("/") || options.devGitSocketPath.includes("\0")) {
    throw new Error("devGitSocketPath must be an absolute Unix socket path");
  }
  if (typeof options.devGitWorkspaceID !== "string" || !WORKSPACE_PATTERN.test(options.devGitWorkspaceID)) throw new Error("devGitWorkspaceID is invalid");
  const definitions = validateDefinitions(options.devGitTools);
  const names = new Set(definitions.map((definition) => definition.name));
  const requestImpl = createRequester(options.devGitSocketPath);
  return Object.freeze({
    names,
    augmentTools(tools = []) {
      const existing = new Set(tools.filter((tool) => tool?.type === "function").map((tool) => tool.name));
      for (const name of names) if (existing.has(name)) throw new Error(`development Git tool name is already offered: ${name}`);
      return [...tools, ...definitions.map((definition) => ({ type: "function", name: definition.name, description: definition.description, inputSchema: definition.input_schema }))];
    },
    async execute(name, args, signal) {
      if (!names.has(name)) throw new Error("unrecognized development Git action");
      validateArguments(name, args, options.devGitWorkspaceID);
      return requestImpl({ method: "POST", path: ENDPOINTS[name], jsonBody: args, signal });
    },
  });
}

export function isInternalDevGitCall(devGit, tool) {
  return Boolean(devGit && tool && devGit.names.has(tool.name));
}

function validateDefinitions(value) {
  if (!Array.isArray(value) || value.length !== Object.keys(ENDPOINTS).length) throw new Error("development Git tool definitions are invalid");
  const names = new Set();
  const output = value.map((definition) => {
    requirePlainObject(definition, "development Git tool definition");
    requireExactKeys(definition, new Set(["name", "description", "input_schema"]), "development Git tool definition");
    if (typeof definition.name !== "string" || ENDPOINTS[definition.name] === undefined || names.has(definition.name)) throw new Error("development Git tool name is invalid");
    if (typeof definition.description !== "string" || !/Git|repository|publication/.test(definition.description)) throw new Error("development Git tool description must be transparent");
    requireClosedSchema(definition.input_schema, definition.name);
    names.add(definition.name);
    return JSON.parse(JSON.stringify(definition));
  });
  for (const name of Object.keys(ENDPOINTS)) if (!names.has(name)) throw new Error(`development Git tool definition is missing: ${name}`);
  return output;
}

function requireClosedSchema(schema, name) {
  requirePlainObject(schema, `${name} input schema`);
  if (schema.type !== "object" || schema.additionalProperties !== false) throw new Error(`${name} input schema must be closed`);
  requirePlainObject(schema.properties, `${name} properties`);
  if (!Array.isArray(schema.required) || !schema.required.includes("workspace_id")) throw new Error(`${name} must require workspace_id`);
  for (const forbidden of ["token", "password", "credential", "url", "remote", "refspec", "force", "command"]) {
    if (Object.hasOwn(schema.properties, forbidden)) throw new Error(`${name} exposes forbidden field: ${forbidden}`);
  }
}

function validateArguments(name, args, workspaceID) {
  requirePlainObject(args, `${name} arguments`);
  if (args.workspace_id !== workspaceID) throw new Error("development Git action workspace does not match the runtime");
  for (const forbidden of ["token", "password", "credential", "url", "remote", "refspec", "force", "command"]) {
    if (Object.hasOwn(args, forbidden)) throw new Error(`development Git action contains forbidden field: ${forbidden}`);
  }
  if (name === "workspace_dev_publish" && (typeof args.plan_id !== "string" || !PLAN_PATTERN.test(args.plan_id))) throw new Error("development Git publication plan is invalid");
}

function requirePlainObject(value, label) {
  if (value === null || typeof value !== "object" || Array.isArray(value) || Object.getPrototypeOf(value) !== Object.prototype) throw new Error(`${label} must be a plain JSON object`);
}

function requireExactKeys(value, allowed, label) {
  for (const key of Object.keys(value)) if (!allowed.has(key)) throw new Error(`${label} contains unknown field: ${key}`);
  if (Object.keys(value).length !== allowed.size) throw new Error(`${label} is incomplete`);
}
