export type SystemData = {
  available: boolean; cpu_count: number; memory_total_bytes: number; memory_available_bytes: number;
  disk_total_bytes: number; disk_available_bytes: number; load_1: number; load_5: number; load_15: number;
};

export type PayloadData = {
  process_started_at: string; tool_call_count: number; estimated_payload_tokens: number; warning: string;
  request_count: number; input_bytes: number; output_bytes: number; input_tokens_estimate: number;
  output_tokens_estimate: number; formula: string;
};

export type ActivityWindow = {
  requests: number; tool_calls: number; input_bytes: number; output_bytes: number; estimated_payload_tokens: number;
  client_errors: number; server_errors: number; external_wait_ms: number; updated_at: string;
};
export type DurableActivity = {
  last_24_hours: ActivityWindow; last_7_days: ActivityWindow; last_30_days: ActivityWindow;
  last_90_days: ActivityWindow; lifetime: ActivityWindow;
};

export type ControllerData = { kind: string; state: string; last_seen_at: string; active_operations: number; active_runtimes: number };
export type RuntimeData = { runtime_id: string; state: string; controller: string; edge_id: string; last_activity: string };
export type ProjectData = { id: string; label: string; current: boolean };
export type EdgeDeviceData = { id: string; label: string; paired_at: string };
export type StorageData = {
  available: boolean; database_bytes: number; wal_bytes: number; log_bytes: number;
  total_bytes: number; limit_bytes: number; state: string;
};

export type BrainNode = { id: string; title: string; summary: string; trust: string; degree: number };
export type BrainEdge = { source: string; target: string };
export type BrainData = {
  available: boolean; ready: boolean; schema_version: number; note_count: number; source_bytes: number;
  link_count: number; broken_link_count: number; indexed_at: string; graph_truncated: boolean;
  nodes: BrainNode[]; edges: BrainEdge[];
};

export type ObservabilityRoute = { route: string; requests: number; client_4xx: number; server_5xx: number; p95_ms: number };

export type ConsoleData = {
  schema_version: number; system: SystemData; payload: PayloadData; durable_activity: DurableActivity;
  controllers: ControllerData[]; runtimes: RuntimeData[]; projects: ProjectData[]; storage: StorageData;
  brain: BrainData; observability: { enabled: boolean; failures: number; routes: ObservabilityRoute[] };
  security: { oauth_enabled: boolean; bearer_recovery: boolean; query_auth: string; free_shell: string; cookie: string; console_authority: string };
  edge: { state: string; devices: EdgeDeviceData[] };
};

export type TaskState =
  | "requested" | "planned" | "awaiting_approval" | "executing" | "observing" | "validating"
  | "completed" | "failed" | "cancelled" | "disconnected";
export type Controller = "http" | "stdio" | "internal";
export type TaskEntry = {
  task_id: string; sequence: number; controller: Controller; operation: string; safe_summary: string;
  project_id: string; edge_id: string; state: TaskState; derived_state: boolean; created_at: string; updated_at: string; heartbeat_at: string;
  terminal_at: string | null; version: number;
};
export type JournalStorage = { storage: "healthy" | "nearing_limit" | "degraded"; detail: string; record_count: number; database_size_bytes: number; wal_size_bytes: number };
export type TasksResponse = { schema_version: number; available: boolean; storage: JournalStorage; tasks: TaskEntry[]; next_cursor: string; has_more: boolean };

export type EventType = "started" | "heartbeat" | "transition";
export type JournalEvent = {
  event_id: number; task_id: string; task_version: number; sequence: number; occurred_at: string;
  event_type: EventType; state: TaskState; operation: string; task: TaskEntry;
};
export type EventLogResponse = { schema_version: number; available: boolean; storage: JournalStorage; events: JournalEvent[]; next_cursor: string; has_more: boolean };
export type TaskFilters = { controller?: Controller | ""; state?: TaskState | ""; operation?: string; project_id?: string; edge_id?: string };
export type EventFilters = TaskFilters & { event_type?: EventType | "" };

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
function nullableText(item: Record<string, unknown>, key: string, label: string): string | null {
  const value = item[key];
  if (value !== null && typeof value !== "string") throw new Error("invalid " + label + "." + key);
  return value as string | null;
}
function numberValue(item: Record<string, unknown>, key: string, label: string, integer = false): number {
  const value = item[key];
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0 || (integer && !Number.isInteger(value))) throw new Error("invalid " + label + "." + key);
  return value;
}
function flag(item: Record<string, unknown>, key: string, label: string): boolean {
  if (typeof item[key] !== "boolean") throw new Error("invalid " + label + "." + key);
  return item[key];
}
function timestamp(item: Record<string, unknown>, key: string, label: string, empty = false): string {
  const value = text(item, key, label);
  if ((!empty || value !== "") && !Number.isFinite(Date.parse(value))) throw new Error("invalid " + label + "." + key);
  return value;
}
function array(value: unknown, label: string): unknown[] {
  if (!Array.isArray(value)) throw new Error("invalid " + label);
  return value;
}

const taskStates = new Set<TaskState>(["requested", "planned", "awaiting_approval", "executing", "observing", "validating", "completed", "failed", "cancelled", "disconnected"]);
const controllers = new Set<Controller>(["http", "stdio", "internal"]);
const eventTypes = new Set<EventType>(["started", "heartbeat", "transition"]);
const storageStates = new Set(["healthy", "nearing_limit", "degraded"]);

function parseNode(value: unknown): BrainNode {
  const item = exact(value, ["degree", "id", "summary", "title", "trust"], "brain node");
  return { id: text(item, "id", "node"), title: text(item, "title", "node"), summary: text(item, "summary", "node"), trust: text(item, "trust", "node"), degree: numberValue(item, "degree", "node", true) };
}
function parseEdge(value: unknown): BrainEdge {
  const item = exact(value, ["source", "target"], "brain edge");
  return { source: text(item, "source", "edge"), target: text(item, "target", "edge") };
}
function parseRoute(value: unknown): ObservabilityRoute {
  const item = exact(value, ["client_4xx", "p95_ms", "requests", "route", "server_5xx"], "route");
  return { route: text(item, "route", "route"), requests: numberValue(item, "requests", "route", true), client_4xx: numberValue(item, "client_4xx", "route", true), server_5xx: numberValue(item, "server_5xx", "route", true), p95_ms: numberValue(item, "p95_ms", "route", true) };
}
function parseActivity(value: unknown): ActivityWindow {
  const item = exact(value, ["client_errors", "estimated_payload_tokens", "external_wait_ms", "input_bytes", "output_bytes", "requests", "server_errors", "tool_calls", "updated_at"], "activity window");
  return { requests: numberValue(item, "requests", "activity", true), tool_calls: numberValue(item, "tool_calls", "activity", true), input_bytes: numberValue(item, "input_bytes", "activity", true), output_bytes: numberValue(item, "output_bytes", "activity", true), estimated_payload_tokens: numberValue(item, "estimated_payload_tokens", "activity", true), client_errors: numberValue(item, "client_errors", "activity", true), server_errors: numberValue(item, "server_errors", "activity", true), external_wait_ms: numberValue(item, "external_wait_ms", "activity", true), updated_at: timestamp(item, "updated_at", "activity", true) };
}
function parseController(value: unknown): ControllerData {
  const item = exact(value, ["active_operations", "active_runtimes", "kind", "last_seen_at", "state"], "controller");
  return { kind: text(item, "kind", "controller"), state: text(item, "state", "controller"), last_seen_at: timestamp(item, "last_seen_at", "controller", true), active_operations: numberValue(item, "active_operations", "controller", true), active_runtimes: numberValue(item, "active_runtimes", "controller", true) };
}
function parseRuntime(value: unknown): RuntimeData {
  const item = exact(value, ["controller", "edge_id", "last_activity", "runtime_id", "state"], "runtime");
  return { runtime_id: text(item, "runtime_id", "runtime"), state: text(item, "state", "runtime"), controller: text(item, "controller", "runtime"), edge_id: text(item, "edge_id", "runtime"), last_activity: timestamp(item, "last_activity", "runtime", true) };
}
function parseProject(value: unknown): ProjectData {
  const item = exact(value, ["current", "id", "label"], "project");
  return { id: text(item, "id", "project"), label: text(item, "label", "project"), current: flag(item, "current", "project") };
}
function parseEdgeDevice(value: unknown): EdgeDeviceData {
  const item = exact(value, ["id", "label", "paired_at"], "edge device");
  return { id: text(item, "id", "edge device"), label: text(item, "label", "edge device"), paired_at: timestamp(item, "paired_at", "edge device", true) };
}
function parseStorage(value: unknown): StorageData {
  const item = exact(value, ["available", "database_bytes", "limit_bytes", "log_bytes", "state", "total_bytes", "wal_bytes"], "storage");
  return { available: flag(item, "available", "storage"), database_bytes: numberValue(item, "database_bytes", "storage", true), wal_bytes: numberValue(item, "wal_bytes", "storage", true), log_bytes: numberValue(item, "log_bytes", "storage", true), total_bytes: numberValue(item, "total_bytes", "storage", true), limit_bytes: numberValue(item, "limit_bytes", "storage", true), state: text(item, "state", "storage") };
}

export function parseConsoleData(value: unknown): ConsoleData {
  const root = exact(value, ["brain", "controllers", "durable_activity", "edge", "observability", "payload", "projects", "runtimes", "schema_version", "security", "storage", "system"], "console data");
  const system = exact(root.system, ["available", "cpu_count", "disk_available_bytes", "disk_total_bytes", "load_1", "load_15", "load_5", "memory_available_bytes", "memory_total_bytes"], "system");
  const payload = exact(root.payload, ["estimated_payload_tokens", "formula", "input_bytes", "input_tokens_estimate", "output_bytes", "output_tokens_estimate", "process_started_at", "request_count", "tool_call_count", "warning"], "payload");
  const activity = exact(root.durable_activity, ["last_24_hours", "last_30_days", "last_7_days", "last_90_days", "lifetime"], "durable activity");
  const brain = exact(root.brain, ["available", "broken_link_count", "edges", "graph_truncated", "indexed_at", "link_count", "nodes", "note_count", "ready", "schema_version", "source_bytes"], "brain");
  const observability = exact(root.observability, ["enabled", "failures", "routes"], "observability");
  const security = exact(root.security, ["bearer_recovery", "console_authority", "cookie", "free_shell", "oauth_enabled", "query_auth"], "security");
  const edge = exact(root.edge, ["devices", "state"], "edge");
  return {
    schema_version: numberValue(root, "schema_version", "console data", true),
    system: { available: flag(system, "available", "system"), cpu_count: numberValue(system, "cpu_count", "system", true), memory_total_bytes: numberValue(system, "memory_total_bytes", "system", true), memory_available_bytes: numberValue(system, "memory_available_bytes", "system", true), disk_total_bytes: numberValue(system, "disk_total_bytes", "system", true), disk_available_bytes: numberValue(system, "disk_available_bytes", "system", true), load_1: numberValue(system, "load_1", "system"), load_5: numberValue(system, "load_5", "system"), load_15: numberValue(system, "load_15", "system") },
    payload: { process_started_at: timestamp(payload, "process_started_at", "payload"), tool_call_count: numberValue(payload, "tool_call_count", "payload", true), estimated_payload_tokens: numberValue(payload, "estimated_payload_tokens", "payload", true), warning: text(payload, "warning", "payload"), request_count: numberValue(payload, "request_count", "payload", true), input_bytes: numberValue(payload, "input_bytes", "payload", true), output_bytes: numberValue(payload, "output_bytes", "payload", true), input_tokens_estimate: numberValue(payload, "input_tokens_estimate", "payload", true), output_tokens_estimate: numberValue(payload, "output_tokens_estimate", "payload", true), formula: text(payload, "formula", "payload") },
    durable_activity: { last_24_hours: parseActivity(activity.last_24_hours), last_7_days: parseActivity(activity.last_7_days), last_30_days: parseActivity(activity.last_30_days), last_90_days: parseActivity(activity.last_90_days), lifetime: parseActivity(activity.lifetime) },
    controllers: array(root.controllers, "controllers").map(parseController), runtimes: array(root.runtimes, "runtimes").map(parseRuntime), projects: array(root.projects, "projects").map(parseProject), storage: parseStorage(root.storage),
    brain: { available: flag(brain, "available", "brain"), ready: flag(brain, "ready", "brain"), schema_version: numberValue(brain, "schema_version", "brain", true), note_count: numberValue(brain, "note_count", "brain", true), source_bytes: numberValue(brain, "source_bytes", "brain", true), link_count: numberValue(brain, "link_count", "brain", true), broken_link_count: numberValue(brain, "broken_link_count", "brain", true), indexed_at: timestamp(brain, "indexed_at", "brain", true), graph_truncated: flag(brain, "graph_truncated", "brain"), nodes: array(brain.nodes, "brain nodes").map(parseNode), edges: array(brain.edges, "brain edges").map(parseEdge) },
    observability: { enabled: flag(observability, "enabled", "observability"), failures: numberValue(observability, "failures", "observability", true), routes: array(observability.routes, "routes").map(parseRoute) },
    security: { oauth_enabled: flag(security, "oauth_enabled", "security"), bearer_recovery: flag(security, "bearer_recovery", "security"), query_auth: text(security, "query_auth", "security"), free_shell: text(security, "free_shell", "security"), cookie: text(security, "cookie", "security"), console_authority: text(security, "console_authority", "security") },
    edge: { state: text(edge, "state", "edge"), devices: array(edge.devices, "edge devices").map(parseEdgeDevice) },
  };
}

function parseJournalStorage(value: unknown): JournalStorage {
  const item = exact(value, ["database_size_bytes", "detail", "record_count", "storage", "wal_size_bytes"], "journal storage");
  const state = text(item, "storage", "journal storage");
  if (!storageStates.has(state)) throw new Error("invalid journal storage state");
  return { storage: state as JournalStorage["storage"], detail: text(item, "detail", "journal storage"), record_count: numberValue(item, "record_count", "journal storage", true), database_size_bytes: numberValue(item, "database_size_bytes", "journal storage", true), wal_size_bytes: numberValue(item, "wal_size_bytes", "journal storage", true) };
}

export function parseTaskEntry(value: unknown): TaskEntry {
  const item = exact(value, ["controller", "created_at", "derived_state", "edge_id", "heartbeat_at", "operation", "project_id", "safe_summary", "sequence", "state", "task_id", "terminal_at", "updated_at", "version"], "task");
  const state = text(item, "state", "task") as TaskState;
  const controller = text(item, "controller", "task") as Controller;
  if (!taskStates.has(state) || !controllers.has(controller)) throw new Error("invalid task state or controller");
  const terminalAt = nullableText(item, "terminal_at", "task");
  if (terminalAt !== null && !Number.isFinite(Date.parse(terminalAt))) throw new Error("invalid task.terminal_at");
  return { task_id: text(item, "task_id", "task"), sequence: numberValue(item, "sequence", "task", true), controller, operation: text(item, "operation", "task"), safe_summary: text(item, "safe_summary", "task"), project_id: text(item, "project_id", "task"), edge_id: text(item, "edge_id", "task"), state, derived_state: flag(item, "derived_state", "task"), created_at: timestamp(item, "created_at", "task"), updated_at: timestamp(item, "updated_at", "task"), heartbeat_at: timestamp(item, "heartbeat_at", "task"), terminal_at: terminalAt, version: numberValue(item, "version", "task", true) };
}

export function parseTasksResponse(value: unknown): TasksResponse {
  const root = exact(value, ["available", "has_more", "next_cursor", "schema_version", "storage", "tasks"], "tasks response");
  return { schema_version: numberValue(root, "schema_version", "tasks response", true), available: flag(root, "available", "tasks response"), storage: parseJournalStorage(root.storage), tasks: array(root.tasks, "tasks").map(parseTaskEntry), next_cursor: text(root, "next_cursor", "tasks response"), has_more: flag(root, "has_more", "tasks response") };
}

export function parseJournalEvent(value: unknown): JournalEvent {
  const item = exact(value, ["event_id", "event_type", "occurred_at", "operation", "sequence", "state", "task", "task_id", "task_version"], "journal event");
  const state = text(item, "state", "journal event") as TaskState;
  const eventType = text(item, "event_type", "journal event") as EventType;
  if (!taskStates.has(state) || !eventTypes.has(eventType)) throw new Error("invalid journal event state or type");
  return { event_id: numberValue(item, "event_id", "journal event", true), task_id: text(item, "task_id", "journal event"), task_version: numberValue(item, "task_version", "journal event", true), sequence: numberValue(item, "sequence", "journal event", true), occurred_at: timestamp(item, "occurred_at", "journal event"), event_type: eventType, state, operation: text(item, "operation", "journal event"), task: parseTaskEntry(item.task) };
}

export function parseEventLogResponse(value: unknown): EventLogResponse {
  const root = exact(value, ["available", "events", "has_more", "next_cursor", "schema_version", "storage"], "event log response");
  return { schema_version: numberValue(root, "schema_version", "event log response", true), available: flag(root, "available", "event log response"), storage: parseJournalStorage(root.storage), events: array(root.events, "events").map(parseJournalEvent), next_cursor: text(root, "next_cursor", "event log response"), has_more: flag(root, "has_more", "event log response") };
}
