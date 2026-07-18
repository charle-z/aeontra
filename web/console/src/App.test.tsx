import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const runtime = {
  status: "ok", version: "1.0.0", protocol_version: "2025-06-18",
  commit: "aa1c30da07751a1b1701aac289adb88ee5c7d38b", tool_count: 85,
  catalog_hash: "sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941",
  authenticated: true, surface: "presentation-only",
};

const windowData = { requests: 8, tool_calls: 4, input_bytes: 4096, output_bytes: 2048, estimated_payload_tokens: 1536, client_errors: 1, server_errors: 0, external_wait_ms: 9, updated_at: "2026-07-17T12:00:00Z" };
const data = {
  schema_version: 3,
  system: { available: true, cpu_count: 2, memory_total_bytes: 4294967296, memory_available_bytes: 2147483648, disk_total_bytes: 85899345920, disk_available_bytes: 42949672960, load_1: 0.1, load_5: 0.2, load_15: 0.3 },
  payload: { process_started_at: "2026-07-17T11:00:00Z", tool_call_count: 4, estimated_payload_tokens: 1536, warning: "estimate, not provider billing", request_count: 8, input_bytes: 4096, output_bytes: 2048, input_tokens_estimate: 1024, output_tokens_estimate: 512, formula: "bytes / 4 (estimate)" },
  durable_activity: { last_24_hours: windowData, last_7_days: windowData, last_30_days: windowData, last_90_days: windowData, lifetime: windowData },
  controllers: [{ kind: "http", state: "connected", last_seen_at: "2026-07-17T12:00:00.123Z", active_operations: 1, active_runtimes: 0 }],
  runtimes: [{ runtime_id: "mr_0123456789abcdef0123456789abcdef", state: "awaiting_model", controller: "http", edge_id: "edge_0123456789abcdef01234567", last_activity: "2026-07-17T12:00:00.789Z" }],
  projects: [{ id: "prj_0123456789abcdef01234567", label: "Configured project 1", current: true }],
  storage: { available: true, database_bytes: 1048576, wal_bytes: 65536, log_bytes: 32768, total_bytes: 1146880, limit_bytes: 268435456, state: "healthy" },
  brain: { available: true, ready: true, schema_version: 1, note_count: 2, source_bytes: 512, link_count: 1, broken_link_count: 0, indexed_at: "2026-07-14T20:00:00Z", graph_truncated: false, nodes: [{ id: "bn_release", title: "Release gates", summary: "Verified release controls.", trust: "curated", degree: 1 }, { id: "bn_working", title: "Console hypothesis", summary: "Working note awaiting review.", trust: "working", degree: 1 }], edges: [{ source: "bn_release", target: "bn_working" }] },
  observability: { enabled: true, failures: 0, routes: [{ route: "mcp", requests: 10, client_4xx: 1, server_5xx: 0, p95_ms: 12 }] },
  security: { oauth_enabled: true, bearer_recovery: true, query_auth: "rejected", free_shell: "absent", cookie: "Secure; HttpOnly; SameSite=Strict", console_authority: "presentation-only" },
  edge: { state: "paired", devices: [{ id: "edge_0123456789abcdef01234567", label: "Paired Edge 1", paired_at: "2026-07-17T10:00:00.456Z" }] },
};

const storage = { storage: "healthy", detail: "durable", record_count: 2, database_size_bytes: 4096, wal_size_bytes: 512 };
const task = {
  task_id: "0123456789abcdef0123456789abcdef", sequence: 1, controller: "http", operation: "sandbox_status",
  safe_summary: "MCP tool operation: sandbox_status", project_id: "prj_0123456789abcdef01234567", edge_id: "edge_0123456789abcdef01234567", state: "completed", derived_state: false,
  created_at: "2026-07-17T11:59:00.001Z", updated_at: "2026-07-17T12:00:00.002Z",
  heartbeat_at: "2026-07-17T12:00:00.003Z", terminal_at: "2026-07-17T12:00:00.004Z", version: 2,
};
const olderTask = { ...task, task_id: "1123456789abcdef0123456789abcdef", operation: "repo_status", safe_summary: "MCP tool operation: repo_status", updated_at: "2026-07-17T11:00:00.002Z" };
const tasks = { schema_version: 2, available: true, storage, tasks: [task], next_cursor: "task-cursor", has_more: true };
const olderTasks = { schema_version: 2, available: true, storage, tasks: [olderTask], next_cursor: "", has_more: false };
const journalEvent = { event_id: 1, task_id: task.task_id, task_version: 2, sequence: 1, occurred_at: "2026-07-17T12:00:00.005Z", event_type: "transition", state: "completed", operation: "sandbox_status", task };
const liveTask = { ...task, version: 3, state: "validating", updated_at: "2026-07-17T12:01:00.002Z", heartbeat_at: "2026-07-17T12:01:00.003Z", terminal_at: null };
const liveEvent = { ...journalEvent, event_id: 2, task_version: 3, occurred_at: "2026-07-17T12:01:00.005Z", state: "validating", task: liveTask };
const olderEvent = { ...journalEvent, event_id: 0, task_id: olderTask.task_id, task_version: 2, operation: "repo_status", task: olderTask, occurred_at: "2026-07-17T11:00:00.005Z" };
const eventLog = { schema_version: 1, available: true, storage, events: [journalEvent], next_cursor: "event-cursor", has_more: true };
const olderEventLog = { schema_version: 1, available: true, storage, events: [olderEvent], next_cursor: "", has_more: false };

class EventSourceStub {
  static instances: EventSourceStub[] = [];
  readonly url: string;
  onerror: ((event: Event) => void) | null = null;
  close = vi.fn();
  private listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>();
  constructor(url: string) { this.url = url; EventSourceStub.instances.push(this); }
  addEventListener(name: string, callback: EventListenerOrEventListenerObject) {
    const listener = typeof callback === "function" ? callback as (event: MessageEvent<string>) => void : (event: MessageEvent<string>) => callback.handleEvent(event);
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), listener]);
  }
  emit(name: string, value: unknown, lastEventId = "") {
    const event = new MessageEvent(name, { data: JSON.stringify(value), lastEventId });
    for (const listener of this.listeners.get(name) ?? []) listener(event);
  }
}

function response(value: unknown) { return { ok: true, status: 200, json: async () => value }; }

function fetchFixture(input: RequestInfo | URL) {
  const path = String(input);
  if (path.endsWith("/console/status")) return Promise.resolve(response(runtime));
  if (path.endsWith("/console/data")) return Promise.resolve(response(data));
  if (path.includes("/console/tasks?")) return Promise.resolve(response(path.includes("cursor=task-cursor") ? olderTasks : tasks));
  if (path.includes("/console/event-log?")) return Promise.resolve(response(path.includes("cursor=event-cursor") ? olderEventLog : eventLog));
  return Promise.reject(new Error("unexpected fetch: " + path));
}

describe("Neo-BIOS operations firmware", () => {
  beforeEach(() => {
    EventSourceStub.instances = [];
    vi.stubGlobal("EventSource", EventSourceStub);
    vi.stubGlobal("fetch", vi.fn(fetchFixture));
  });
  afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

  it("renders exact catalog, real selectors, VPS and combined storage", async () => {
    render(<App />);
    expect(screen.getByText("MCP DEVBOX OPERATIONS FIRMWARE")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("85 tools")).toBeInTheDocument());
    expect(screen.getByRole("combobox", { name: "Project" })).toHaveValue(data.projects[0].id);
    expect(screen.getByRole("combobox", { name: "Edge device" })).toHaveValue(data.edge.devices[0].id);
    expect(screen.getByRole("option", { name: "Configured project 1 [current]" })).toBeInTheDocument();
    expect(screen.getByText("Paired Edge 1")).toBeInTheDocument();
    expect(screen.getByText("2.0 GiB / 4.0 GiB")).toBeInTheDocument();
    expect(screen.getByText(runtime.catalog_hash)).toBeInTheDocument();
    expect(screen.getByText("1.1 MiB / 256.0 MiB")).toBeInTheDocument();
  });

  it("shows real controllers and paginates durable tasks with precise timestamps", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("85 tools")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("tab", { name: "Agents" }));
    expect(screen.getByText("connected")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Tasks" }));
    expect(screen.getByText("sandbox_status")).toBeInTheDocument();
    expect(screen.getByText(task.updated_at)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Load older tasks" }));
    await waitFor(() => expect(screen.getByText("repo_status")).toBeInTheDocument());
    await waitFor(() => expect(screen.getByRole("button", { name: "Load older tasks" })).toBeDisabled());
  });

  it("uses persistent events, reconnects from Last-Event-ID and cleans resources", async () => {
    const view = render(<App />);
    await waitFor(() => expect(EventSourceStub.instances).toHaveLength(1));
    EventSourceStub.instances[0].emit("event_snapshot", eventLog, "1");
    EventSourceStub.instances[0].emit("stream", { state: "live" });
    EventSourceStub.instances[0].emit("journal", liveEvent, "2");
    fireEvent.click(screen.getByRole("tab", { name: "Events" }));
    expect(await screen.findByText("SSE: live")).toBeInTheDocument();
    expect(screen.getByText("Last-Event-ID: 2")).toBeInTheDocument();
    expect(screen.getByText(liveEvent.occurred_at)).toBeInTheDocument();

    vi.useFakeTimers();
    EventSourceStub.instances[0].onerror?.(new Event("error"));
    expect(EventSourceStub.instances[0].close).toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1000);
    expect(EventSourceStub.instances).toHaveLength(2);
    expect(EventSourceStub.instances[1].url).toContain("last_event_id=2");
    view.unmount();
    expect(EventSourceStub.instances[1].close).toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("filters and paginates server events", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("85 tools")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("tab", { name: "Events" }));
    expect(await screen.findByText(journalEvent.occurred_at)).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox", { name: "Event state filter" }), { target: { value: "completed" } });
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([target]) => String(target).includes("state=completed"))).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Load older events" }));
    await waitFor(() => expect(screen.getByText(olderEvent.occurred_at)).toBeInTheDocument());
  });

  it("renders the safe Brain graph and keyboard help", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("85 tools")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("tab", { name: "Graph" }));
    const node = screen.getByRole("group", { name: /Release gates.*Verified release controls.*curated.*1 link/i });
    fireEvent.focus(node);
    expect(screen.getByRole("tooltip")).toHaveTextContent("Verified release controls.");
    fireEvent.keyDown(window, { key: "F1" });
    expect(screen.getByRole("dialog", { name: "Help" })).toBeInTheDocument();
  });
});
