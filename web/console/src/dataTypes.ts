export type SystemData = {
  available: boolean;
  cpu_count: number;
  memory_total_bytes: number;
  memory_available_bytes: number;
  disk_total_bytes: number;
  disk_available_bytes: number;
  load_1: number;
  load_5: number;
  load_15: number;
};

export type PayloadData = {
  request_count: number;
  input_bytes: number;
  output_bytes: number;
  input_tokens_estimate: number;
  output_tokens_estimate: number;
  formula: string;
};

export type BrainNode = { id: string; title: string; summary: string; trust: string; degree: number };
export type BrainEdge = { source: string; target: string };
export type BrainData = {
  available: boolean;
  ready: boolean;
  schema_version: number;
  note_count: number;
  source_bytes: number;
  link_count: number;
  broken_link_count: number;
  indexed_at: string;
  graph_truncated: boolean;
  nodes: BrainNode[];
  edges: BrainEdge[];
};

export type ObservabilityRoute = {
  route: string;
  requests: number;
  client_4xx: number;
  server_5xx: number;
  p95_ms: number;
};

export type ConsoleData = {
  schema_version: number;
  system: SystemData;
  payload: PayloadData;
  brain: BrainData;
  observability: { enabled: boolean; failures: number; routes: ObservabilityRoute[] };
  security: {
    oauth_enabled: boolean;
    bearer_recovery: boolean;
    query_auth: string;
    free_shell: string;
    cookie: string;
    console_authority: string;
  };
  edge: { state: string };
};

export type TaskState =
  | "requested" | "planned" | "awaiting_approval" | "executing"
  | "observing" | "validating" | "completed" | "failed"
  | "cancelled" | "disconnected";

export type TaskEntry = {
  task_id: string;
  operation: string;
  summary: string;
  state: TaskState;
  heartbeat: string;
  controller: "http" | "stdio" | "internal";
};

export type TasksResponse = { schema_version: number; available: boolean; tasks: TaskEntry[] };

function record(value: unknown, label: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("invalid " + label);
  return value as Record<string, unknown>;
}

function exact(value: unknown, keys: readonly string[], label: string): Record<string, unknown> {
  const item = record(value, label);
  if (Object.keys(item).sort().join("\n") !== [...keys].sort().join("\n")) throw new Error("unexpected " + label + " schema");
  return item;
}

function text(item: Record<string, unknown>, key: string, label: string): string {
  if (typeof item[key] !== "string") throw new Error("invalid " + label + "." + key);
  return item[key];
}

function numberValue(item: Record<string, unknown>, key: string, label: string, integer = false): number {
  const value = item[key];
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0 || (integer && !Number.isInteger(value))) {
    throw new Error("invalid " + label + "." + key);
  }
  return value;
}

function flag(item: Record<string, unknown>, key: string, label: string): boolean {
  if (typeof item[key] !== "boolean") throw new Error("invalid " + label + "." + key);
  return item[key];
}

function parseNode(value: unknown): BrainNode {
  const item = exact(value, ["degree", "id", "summary", "title", "trust"], "brain node");
  return {
    id: text(item, "id", "node"), title: text(item, "title", "node"), summary: text(item, "summary", "node"),
    trust: text(item, "trust", "node"), degree: numberValue(item, "degree", "node", true),
  };
}

function parseEdge(value: unknown): BrainEdge {
  const item = exact(value, ["source", "target"], "brain edge");
  return { source: text(item, "source", "edge"), target: text(item, "target", "edge") };
}

function parseRoute(value: unknown): ObservabilityRoute {
  const item = exact(value, ["client_4xx", "p95_ms", "requests", "route", "server_5xx"], "route");
  return {
    route: text(item, "route", "route"),
    requests: numberValue(item, "requests", "route", true),
    client_4xx: numberValue(item, "client_4xx", "route", true),
    server_5xx: numberValue(item, "server_5xx", "route", true),
    p95_ms: numberValue(item, "p95_ms", "route", true),
  };
}

export function parseConsoleData(value: unknown): ConsoleData {
  const root = exact(value, ["brain", "edge", "observability", "payload", "schema_version", "security", "system"], "console data");
  const system = exact(root.system, ["available", "cpu_count", "disk_available_bytes", "disk_total_bytes", "load_1", "load_15", "load_5", "memory_available_bytes", "memory_total_bytes"], "system");
  const payload = exact(root.payload, ["formula", "input_bytes", "input_tokens_estimate", "output_bytes", "output_tokens_estimate", "request_count"], "payload");
  const brain = exact(root.brain, ["available", "broken_link_count", "edges", "graph_truncated", "indexed_at", "link_count", "nodes", "note_count", "ready", "schema_version", "source_bytes"], "brain");
  const observability = exact(root.observability, ["enabled", "failures", "routes"], "observability");
  const security = exact(root.security, ["bearer_recovery", "console_authority", "cookie", "free_shell", "oauth_enabled", "query_auth"], "security");
  const edge = exact(root.edge, ["state"], "edge");
  if (!Array.isArray(brain.nodes) || !Array.isArray(brain.edges) || !Array.isArray(observability.routes)) throw new Error("invalid console arrays");
  return {
    schema_version: numberValue(root, "schema_version", "console data", true),
    system: {
      available: flag(system, "available", "system"), cpu_count: numberValue(system, "cpu_count", "system", true),
      memory_total_bytes: numberValue(system, "memory_total_bytes", "system", true), memory_available_bytes: numberValue(system, "memory_available_bytes", "system", true),
      disk_total_bytes: numberValue(system, "disk_total_bytes", "system", true), disk_available_bytes: numberValue(system, "disk_available_bytes", "system", true),
      load_1: numberValue(system, "load_1", "system"), load_5: numberValue(system, "load_5", "system"), load_15: numberValue(system, "load_15", "system"),
    },
    payload: {
      request_count: numberValue(payload, "request_count", "payload", true), input_bytes: numberValue(payload, "input_bytes", "payload", true),
      output_bytes: numberValue(payload, "output_bytes", "payload", true), input_tokens_estimate: numberValue(payload, "input_tokens_estimate", "payload", true),
      output_tokens_estimate: numberValue(payload, "output_tokens_estimate", "payload", true), formula: text(payload, "formula", "payload"),
    },
    brain: {
      available: flag(brain, "available", "brain"), ready: flag(brain, "ready", "brain"), schema_version: numberValue(brain, "schema_version", "brain", true),
      note_count: numberValue(brain, "note_count", "brain", true), source_bytes: numberValue(brain, "source_bytes", "brain", true), link_count: numberValue(brain, "link_count", "brain", true),
      broken_link_count: numberValue(brain, "broken_link_count", "brain", true), indexed_at: text(brain, "indexed_at", "brain"), graph_truncated: flag(brain, "graph_truncated", "brain"),
      nodes: brain.nodes.map(parseNode), edges: brain.edges.map(parseEdge),
    },
    observability: { enabled: flag(observability, "enabled", "observability"), failures: numberValue(observability, "failures", "observability", true), routes: observability.routes.map(parseRoute) },
    security: {
      oauth_enabled: flag(security, "oauth_enabled", "security"), bearer_recovery: flag(security, "bearer_recovery", "security"), query_auth: text(security, "query_auth", "security"),
      free_shell: text(security, "free_shell", "security"), cookie: text(security, "cookie", "security"), console_authority: text(security, "console_authority", "security"),
    },
    edge: { state: text(edge, "state", "edge") },
  };
}

const taskStates = new Set<TaskState>(["requested", "planned", "awaiting_approval", "executing", "observing", "validating", "completed", "failed", "cancelled", "disconnected"]);
const controllers = new Set(["http", "stdio", "internal"]);

export function parseTaskEntry(value: unknown): TaskEntry {
  const item = exact(value, ["controller", "heartbeat", "operation", "state", "summary", "task_id"], "task");
  const state = text(item, "state", "task") as TaskState;
  const controller = text(item, "controller", "task");
  if (!taskStates.has(state) || !controllers.has(controller)) throw new Error("invalid task state or controller");
  return { task_id: text(item, "task_id", "task"), operation: text(item, "operation", "task"), summary: text(item, "summary", "task"), state, heartbeat: text(item, "heartbeat", "task"), controller: controller as TaskEntry["controller"] };
}

export function parseTasksResponse(value: unknown): TasksResponse {
  const root = exact(value, ["available", "schema_version", "tasks"], "tasks response");
  if (!Array.isArray(root.tasks)) throw new Error("invalid tasks response.tasks");
  return { schema_version: numberValue(root, "schema_version", "tasks response", true), available: flag(root, "available", "tasks response"), tasks: root.tasks.map(parseTaskEntry) };
}
