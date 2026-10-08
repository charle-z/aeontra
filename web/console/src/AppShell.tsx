import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchRuntimeStatus } from "./api";
import { fetchConsoleData, fetchEventLog, fetchPreferences, fetchTasks, updatePreferences } from "./dataApi";
import {
  parseEventLogResponse,
  parseJournalEvent,
  parseTasksResponse,
  type ConsoleData,
  type Controller,
  type EventFilters,
  type EventLogResponse,
  type EventType,
  type JournalEvent,
  type TaskEntry,
  type TaskFilters,
  type TasksResponse,
  type TaskState,
} from "./dataTypes";
import type { RuntimeStatus } from "./types";
import GraphView from "./GraphView";
import { TimeDisplayProvider, Timestamp } from "./TimeDisplay";
import { DEFAULT_TIMEZONE } from "./time-format";

const tabs = ["System", "Agents", "Tasks", "Brain", "Graph", "Edge", "Observability", "Security", "Events"] as const;
const consoleHelp = "Choose a section from the navigation. Use the filters to narrow real records, and Refresh to reload safe endpoints. This console cannot execute tools or approve actions.";
const tabDescriptions: Record<(typeof tabs)[number], string> = {
  System: "Runtime health and capacity at a glance.",
  Agents: "Controllers, model runtimes and measured activity.",
  Tasks: "Durable operations and their current state.",
  Brain: "Browse the safe index metadata available to this console.",
  Graph: "Explore relationships between safe Brain notes.",
  Edge: "Paired devices and their reported state.",
  Observability: "Bounded route counters and response times.",
  Security: "Authentication and console authority boundaries.",
  Events: "Persistent journal events and live updates.",
};
const taskStates: TaskState[] = ["requested", "planned", "awaiting_approval", "executing", "observing", "validating", "completed", "failed", "cancelled", "disconnected"];
const controllers: Controller[] = ["http", "stdio", "internal"];
const eventTypes: EventType[] = ["started", "heartbeat", "transition"];
type Tab = (typeof tabs)[number];
type DialogState = { title: string; body: string } | null;
type Tone = "normal" | "ok" | "warn" | "dim";
type StreamState = "connecting" | "live" | "reconnecting" | "offline";
type TaskFilterState = { controller: Controller | ""; state: TaskState | ""; operation: string };

function bytes(value: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let next = value;
  let index = 0;
  while (next >= 1024 && index < units.length - 1) { next /= 1024; index += 1; }
  return next.toFixed(index === 0 ? 0 : 1) + " " + units[index];
}

function stateTone(state: string): Tone {
  if (state === "ok" || state === "ready" || state === "completed" || state === "healthy" || state === "live" || state === "connected") return "ok";
  if (["failed", "cancelled", "disconnected", "degraded", "offline", "unavailable"].includes(state)) return "warn";
  if (["planned", "awaiting_approval", "nearing_limit", "reconnecting", "connecting"].includes(state)) return "dim";
  return "normal";
}

function Row({ label, value, tone = "normal", help }: { label: string; value: React.ReactNode; tone?: Tone; help: string }) {
  return <button className="detail-row" type="button" data-help={help}><span>{label}</span><strong data-tone={tone}>{value}</strong></button>;
}
function Section({ title }: { title: string }) { return <h2 className="section-title">{title}</h2>; }
function Panel({ children }: { children: React.ReactNode }) {
  const [help, setHelp] = useState("Select a detail to see where its value comes from.");
  return (
    <div className="detail-layout">
      <section className="detail-grid" onFocus={(event) => { const next = (event.target as HTMLElement).dataset.help; if (next) setHelp(next); }} onClick={(event) => { const target = (event.target as HTMLElement).closest<HTMLElement>("[data-help]"); if (target?.dataset.help) setHelp(target.dataset.help); }}>
        {children}
      </section>
      <aside className="item-help"><h2>About this detail</h2><p>{help}</p></aside>
    </div>
  );
}

function SystemTab({ status, data, error }: { status: RuntimeStatus | null; data: ConsoleData | null; error: string | null }) {
  const system = data?.system;
  const memoryUsed = system ? Math.max(0, system.memory_total_bytes - system.memory_available_bytes) : 0;
  const diskUsed = system ? Math.max(0, system.disk_total_bytes - system.disk_available_bytes) : 0;
  const storage = data?.storage;
  return (
    <Panel>
      <Section title="Runtime Identity" />
      <Row label="Status" value={error ? "[Unavailable]" : status ? "[" + status.status + "]" : "[Loading]"} tone={error ? "warn" : status ? "ok" : "dim"} help="Live status from the exact allowlisted /console/status response." />
      <Row label="Version / Commit" value={status ? status.version + " / " + status.commit : "—"} help="Exact process version and commit." />
      <Row label="Protocol" value={status?.protocol_version ?? "—"} help="MCP protocol version reported by the runtime." />
      <Row label="Tool Catalog" value={status ? status.tool_count + " tools" : "—"} help="Exact public catalog count reported by the running server." />
      <Row label="Catalog Hash" value={status?.catalog_hash ?? "—"} help="Deterministic catalog identity used to detect drift." />
      <Section title="VPS Resources" />
      <Row label="Resource probe" value={system?.available ? "[Available]" : "[Unavailable]"} tone={system?.available ? "ok" : "warn"} help="The server marks this unavailable when any bounded host probe fails." />
      <Row label="CPU" value={system ? system.cpu_count + " logical CPUs" : "—"} help="Real logical CPU count visible to the container." />
      <Row label="RAM" value={system?.available ? bytes(memoryUsed) + " / " + bytes(system.memory_total_bytes) : "Unavailable"} help="Used and total memory derived from MemAvailable; no process list is exposed." />
      <Row label="Disk" value={system?.available ? bytes(diskUsed) + " / " + bytes(system.disk_total_bytes) : "Unavailable"} help="Used and total bytes for the container root filesystem." />
      <Row label="Load average" value={system?.available ? system.load_1.toFixed(2) + " · " + system.load_5.toFixed(2) + " · " + system.load_15.toFixed(2) : "Unavailable"} help="Real one, five and fifteen minute load averages." />
      <Section title="State Storage Budget" />
      <Row label="Combined state" value={storage?.available ? "[" + storage.state + "]" : "[Unavailable]"} tone={stateTone(storage?.available ? storage.state : "unavailable")} help="Combined bounded total for SQLite databases, WAL/SHM files and safe logs under the private state root." />
      <Row label="DB / WAL / logs" value={storage?.available ? bytes(storage.database_bytes) + " / " + bytes(storage.wal_bytes) + " / " + bytes(storage.log_bytes) : "—"} help="Only aggregate file sizes are returned; paths and filenames stay private." />
      <Row label="Total / limit" value={storage?.available ? bytes(storage.total_bytes) + " / " + bytes(storage.limit_bytes) : "—"} help="Pruning begins before the hard cap. The console reports nearing_limit and degraded explicitly." />
    </Panel>
  );
}

function AgentsTab({ data, projectSelection, edgeSelection }: { data: ConsoleData | null; projectSelection: string; edgeSelection: string }) {
  const payload = data?.payload;
  const durable = data?.durable_activity;
  const activityWindows = durable ? [
    ["24 hours", durable.last_24_hours],
    ["7 days", durable.last_7_days],
    ["30 days", durable.last_30_days],
    ["90 days", durable.last_90_days],
    ["Lifetime", durable.lifetime],
  ] as const : [];
  const projectMatches = !projectSelection || Boolean(data?.projects.some((project) => project.id === projectSelection && project.current));
  const runtimes = projectMatches ? (data?.runtimes ?? []).filter((runtime) => !edgeSelection || runtime.edge_id === edgeSelection) : [];
  const runtimeControllers = new Set(runtimes.map((runtime) => runtime.controller));
  const visibleControllers = projectMatches ? (edgeSelection ? (data?.controllers ?? []).filter((controller) => runtimeControllers.has(controller.kind)) : (data?.controllers ?? [])) : [];
  return (
    <div className="table-panel">
      <Section title="Current process" />
      <div className="table-scroll"><table><thead><tr><th>Started at</th><th>Requests</th><th>Tool calls</th><th>Input bytes</th><th>Output bytes</th><th>Estimated payload tokens</th></tr></thead><tbody><tr><td><Timestamp value={payload?.process_started_at ?? ""} /></td><td>{payload?.request_count ?? 0}</td><td>{payload?.tool_call_count ?? 0}</td><td>{payload ? bytes(payload.input_bytes) : "—"}</td><td>{payload ? bytes(payload.output_bytes) : "—"}</td><td>{payload?.estimated_payload_tokens ?? 0}</td></tr></tbody></table></div>
      <p className="panel-note">Current process counters may reset when the process restarts. Estimated payload tokens is the explicit bytes/4 estimate, not provider-reported token usage.</p>
      <Section title="Durable activity" />
      <div className="table-scroll"><table><thead><tr><th>Window</th><th>Requests</th><th>Tool calls</th><th>Input bytes</th><th>Output bytes</th><th>Estimated payload tokens</th><th>Updated</th></tr></thead><tbody>{activityWindows.map(([label, window]) => <tr key={label}><td>{label}</td><td>{window.requests}</td><td>{window.tool_calls}</td><td>{bytes(window.input_bytes)}</td><td>{bytes(window.output_bytes)}</td><td>{window.estimated_payload_tokens}</td><td><Timestamp value={window.updated_at} /></td></tr>)}</tbody></table></div>
      <p className="panel-note">Durable windows are persisted independently from the current process and survive deployments.</p>
      <Section title="Controllers — real transport state" />
      {!visibleControllers.length ? <p className="empty-note">No controller activity matches the selected real scope.</p> : <div className="table-scroll"><table><thead><tr><th>Kind</th><th>State</th><th>Operations</th><th>Runtimes</th><th>Last seen</th></tr></thead><tbody>{visibleControllers.map((controller) => <tr key={controller.kind}><td>{controller.kind}</td><td data-tone={stateTone(controller.state)}>{controller.state}</td><td>{controller.active_operations}</td><td>{controller.active_runtimes}</td><td><Timestamp value={controller.last_seen_at} /></td></tr>)}</tbody></table></div>}
      <Section title="Model Runtimes — distinct from tool calls" />
      {!runtimes.length ? <p className="empty-note">No durable model runtime matches the selected real scope.</p> : <div className="table-scroll"><table><thead><tr><th>Runtime</th><th>Controller</th><th>State</th><th>Last activity</th></tr></thead><tbody>{runtimes.map((runtime) => <tr key={runtime.runtime_id}><td>{runtime.runtime_id.slice(0, 11)}</td><td>{runtime.controller}</td><td data-tone={stateTone(runtime.state)}>{runtime.state}</td><td><Timestamp value={runtime.last_activity} /></td></tr>)}</tbody></table></div>}
      <p className="panel-note">A tool call is an operation, not an agent. Controllers, runtimes and operations remain separate entities.</p>
    </div>
  );
}

function TasksTab({ tasks, filters, setFilters, loadMore, loading }: { tasks: TasksResponse | null; filters: TaskFilterState; setFilters: (next: TaskFilterState) => void; loadMore: () => void; loading: boolean }) {
  const visible = tasks?.tasks ?? [];
  return (
    <div className="table-panel">
      <Section title="Operation Journal — durable server state" />
      <div className="filter-bar" aria-label="Task filters">
        <label>Controller<select aria-label="Task controller filter" value={filters.controller} onChange={(event) => setFilters({ ...filters, controller: event.target.value as Controller | "" })}><option value="">All</option>{controllers.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
        <label>State<select aria-label="Task state filter" value={filters.state} onChange={(event) => setFilters({ ...filters, state: event.target.value as TaskState | "" })}><option value="">All</option>{taskStates.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
        <label>Operation<input aria-label="Task operation filter" value={filters.operation} onChange={(event) => setFilters({ ...filters, operation: event.target.value })} placeholder="exact operation" /></label>
      </div>
      <div className="state-flow" aria-label="Task lifecycle">{taskStates.map((state) => <span key={state}>{state}</span>)}</div>
      {!tasks?.available && <p className="empty-note">Operation journal unavailable.</p>}
      {tasks?.available && !visible.length && <p className="empty-note">No durable operation matches the selected server-side filters.</p>}
      {!!visible.length && <div className="table-scroll"><table><thead><tr><th>Task ID</th><th>Controller</th><th>Operation</th><th>State</th><th>Version</th><th>Created</th><th>Last activity / Updated</th><th>Heartbeat</th><th>Terminal time</th></tr></thead><tbody>{visible.map((task) => <tr key={task.task_id}><td>{task.task_id.slice(0, 8)}</td><td>{task.controller}</td><td>{task.operation}</td><td data-tone={stateTone(task.state)}>{task.state}{task.derived_state ? " (derived)" : ""}</td><td>{task.version}</td><td><Timestamp value={task.created_at} /></td><td><Timestamp value={task.updated_at} /></td><td>{["completed", "failed", "cancelled"].includes(task.state) ? "—" : <Timestamp value={task.heartbeat_at} />}</td><td>{task.terminal_at ? <Timestamp value={task.terminal_at} /> : "—"}</td></tr>)}</tbody></table></div>}
      <div className="paging-bar"><span>storage: {tasks?.storage.storage ?? "unavailable"} · {tasks?.storage.record_count ?? 0} records</span><button type="button" disabled={!tasks?.has_more || loading} onClick={loadMore}>{loading ? "Loading…" : "Load older tasks"}</button></div>
      <p className="panel-note">Filtering and cursor pagination are server-side. Prompts, parameters, results, paths and identities are never journaled.</p>
    </div>
  );
}

function BrainTab({ data, onOpenGraph }: { data: ConsoleData | null; onOpenGraph: () => void }) {
  const [query, setQuery] = useState("");
  const [trust, setTrust] = useState("");
  const [visibleCount, setVisibleCount] = useState(12);
  const brain = data?.brain;
  const nodes = brain?.nodes ?? [];
  const matching = nodes.filter((node) => {
    const text = query.trim().toLocaleLowerCase();
    return (!trust || node.trust === trust)
      && (!text || (node.title + " " + node.summary + " " + node.console_label).toLocaleLowerCase().includes(text));
  });
  return (
    <div className="brain-page">
      <div className="brain-overview" aria-label="Brain index summary">
        <div className="metric-card"><span>Index</span><strong data-tone={brain?.ready ? "ok" : "warn"}>{brain?.ready ? "Ready" : "Unavailable"}</strong><small>Schema {brain?.available ? brain.schema_version : "—"}</small></div>
        <div className="metric-card"><span>Notes</span><strong>{brain?.available ? brain.note_count : "—"}</strong><small>{brain?.available ? bytes(brain.source_bytes) : "Unavailable"} indexed source</small></div>
        <div className="metric-card"><span>Links</span><strong>{brain?.available ? brain.link_count : "—"}</strong><small>{brain?.available ? brain.broken_link_count : "—"} broken</small></div>
        <div className="metric-card"><span>Graph</span><strong>{brain?.available ? nodes.length : "—"}</strong><small>safe nodes · {brain?.available ? brain.edges.length : "—"} edges</small></div>
      </div>
      <div className="brain-explorer">
        <div className="brain-explorer-heading"><div><Section title="Explore notes" /><p>Titles and summaries are explicitly prepared for this console. Source text and paths stay private.</p></div><button type="button" onClick={onOpenGraph}>Open relationship map <span aria-hidden="true">↗</span></button></div>
        <div className="filter-bar" aria-label="Brain filters"><label>Find a note<input type="search" aria-label="Find a note" value={query} onChange={(event) => { setQuery(event.target.value); setVisibleCount(12); }} placeholder="Search safe titles and summaries" /></label><label>Trust<select aria-label="Brain trust filter" value={trust} onChange={(event) => { setTrust(event.target.value); setVisibleCount(12); }}><option value="">All notes</option><option value="curated">Curated</option><option value="working">Working</option></select></label></div>
        {!brain?.available ? <p className="empty-note">Brain is unavailable.</p> : matching.length ? <ul className="brain-note-list">{matching.slice(0, visibleCount).map((node) => <li key={node.id}><div><span className="trust-label" data-trust={node.trust}>{node.trust}</span><span className="link-count">{node.degree} {node.degree === 1 ? "link" : "links"}</span></div><h3>{node.title}</h3><p>{node.summary.trim() || "No curated summary available."}</p></li>)}</ul> : <p className="empty-note">No safe notes match these filters.</p>}
        {brain?.available && matching.length > visibleCount && <div className="paging-bar"><span>Showing {visibleCount} of {matching.length} safe nodes</span><button type="button" onClick={() => setVisibleCount((count) => count + 12)}>Show more notes</button></div>}
        <p className="panel-note">Indexed at <Timestamp value={brain?.indexed_at || ""} />{brain?.graph_truncated ? " · The relationship map is bounded; this list may be incomplete." : ""}</p>
      </div>
    </div>
  );
}

function EdgeTab({ data }: { data: ConsoleData | null }) {
  const paired = data?.edge.state === "paired";
  return (
    <Panel>
      <Section title="Edge Devices" />
      <Row label="Pairing" value={paired ? "Paired" : data?.edge.state === "not_paired" ? "Not paired" : "Unavailable"} tone={paired ? "ok" : data?.edge.state === "unavailable" ? "warn" : "dim"} help="Derived from the real active-device registry." />
      <Row label="Active devices" value={data ? String(data.edge.devices.length) : "—"} help="Only opaque selector IDs, generic labels and pairing timestamps are exposed." />
      {data?.edge.devices.map((device) => <Row key={device.id} label={device.label} value={device.paired_at ? <Timestamp value={device.paired_at} /> : "paired"} tone="ok" help="Real active Edge entry. Device name, device_id, keys and network details remain private." />)}
      {!data?.edge.devices.length && <Row label="Workcell" value="Unavailable until pairing" tone="dim" help="No Edge identity is fabricated when none is active." />}
    </Panel>
  );
}

function ObservabilityTab({ data, projectSelection, edgeSelection }: { data: ConsoleData | null; projectSelection: string; edgeSelection: string }) {
  const projectMatches = !projectSelection || Boolean(data?.projects.some((project) => project.id === projectSelection && project.current));
  const scopedAvailable = projectMatches && !edgeSelection;
  const observability = scopedAvailable ? data?.observability : null;
  return (
    <div className="table-panel">
      <Section title="Structured Route Summary" />
      <p className="panel-note">Window: persisted aggregate · last durable update follows activity windows · storage: {data?.storage.state ?? "unavailable"}</p>
      {!projectMatches && <p className="empty-note">No observability aggregate belongs to the selected non-current project.</p>}
      {projectMatches && edgeSelection && <p className="empty-note">No device-scoped observability aggregate is recorded; global metrics are not relabeled as Edge data.</p>}
      {scopedAvailable && <p className="panel-note">Enabled: {String(observability?.enabled ?? false)} · sink failures: {observability?.failures ?? 0}</p>}
      {scopedAvailable && <p className="panel-note">Last durable update: <Timestamp value={data?.durable_activity.lifetime.updated_at ?? ""} /></p>}
      {scopedAvailable && !observability?.routes.length && <p className="empty-note">No route events are available in the selected persisted scope.</p>}
      {scopedAvailable && !!observability?.routes.length && <div className="table-scroll"><table><thead><tr><th>Route</th><th>Requests</th><th>4xx</th><th>5xx</th><th>P95 ms</th></tr></thead><tbody>{observability.routes.map((route) => <tr key={route.route}><td>{route.route}</td><td>{route.requests}</td><td>{route.client_4xx}</td><td>{route.server_5xx}</td><td>{route.p95_ms}</td></tr>)}</tbody></table></div>}
      <p className="panel-note">Counters and bounded durations only. Raw request content remains outside this surface.</p>
    </div>
  );
}

function SecurityTab({ data, status }: { data: ConsoleData | null; status: RuntimeStatus | null }) {
  const security = data?.security;
  return (
    <Panel>
      <Section title="Authentication" />
      <Row label="OAuth" value={security?.oauth_enabled ? "[Enabled]" : "[Unavailable]"} tone={security?.oauth_enabled ? "ok" : "warn"} help="OAuth provider status from the running server configuration." />
      <Row label="Bearer recovery" value={security ? (security.bearer_recovery ? "[Enabled]" : "[Disabled]") : "—"} help="Header-only and console-form recovery posture." />
      <Row label="Query auth" value={security?.query_auth ?? "—"} tone={security?.query_auth === "rejected" ? "ok" : "warn"} help="Legacy query credentials are rejected." />
      <Row label="Cookie" value={security?.cookie ?? "—"} help="Opaque durable browser session cookie posture." />
      <Section title="Authority Boundary" />
      <Row label="Console" value={security?.console_authority ?? status?.surface ?? "—"} help="The console observes allowlisted state and does not execute tools." />
      <Row label="Free shell" value={security?.free_shell ?? "—"} tone={security?.free_shell === "absent" ? "ok" : "warn"} help="No terminal or arbitrary command interface exists in the browser." />
    </Panel>
  );
}

function hasTaskFilters(filters: TaskFilters): boolean {
  return Boolean(filters.controller || filters.state || filters.operation?.trim() || filters.project_id || filters.edge_id);
}
function matchesTaskScope(task: TaskEntry, filters: TaskFilters): boolean {
  return (!filters.controller || task.controller === filters.controller)
    && (!filters.state || task.state === filters.state)
    && (!filters.operation?.trim() || task.operation === filters.operation.trim())
    && (!filters.project_id || task.project_id === filters.project_id)
    && (!filters.edge_id || task.edge_id === filters.edge_id);
}
function hasEventFilters(filters: EventFilters): boolean {
  return hasTaskFilters(filters) || Boolean(filters.event_type);
}
function matchesEvent(event: JournalEvent, filters: EventFilters): boolean {
  return matchesTaskScope(event.task, filters) && (!filters.event_type || event.event_type === filters.event_type);
}

function eventKey(event: JournalEvent): string {
  return event.event_id + ":" + event.task_version;
}

function mergeLiveEvents(page: EventLogResponse, live: JournalEvent[], filters: EventFilters): EventLogResponse {
  const seen = new Set<string>();
  const events = [...live.filter((event) => matchesEvent(event, filters)), ...page.events].filter((event) => {
    const key = eventKey(event);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  }).slice(0, 200);
  return { ...page, events };
}

function EventsTab({ log, filters, setFilters, streamState, lastEventID, loadMore, loading }: { log: EventLogResponse | null; filters: EventFilters; setFilters: (next: EventFilters) => void; streamState: StreamState; lastEventID: string; loadMore: () => void; loading: boolean }) {
  return (
    <div className="events-panel">
      <Section title="Persistent Server Event Log" />
      <div className="stream-bar"><span data-tone={stateTone(streamState)}>SSE: {streamState}</span><span>Last-Event-ID: {lastEventID || "none"}</span><span>retention: 30 days / 20,000 events</span></div>
      <div className="filter-bar" aria-label="Event filters">
        <label>Controller<select aria-label="Event controller filter" value={filters.controller ?? ""} onChange={(event) => setFilters({ ...filters, controller: event.target.value as Controller | "" })}><option value="">All</option>{controllers.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
        <label>State<select aria-label="Event state filter" value={filters.state ?? ""} onChange={(event) => setFilters({ ...filters, state: event.target.value as TaskState | "" })}><option value="">All</option>{taskStates.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
        <label>Type<select aria-label="Event type filter" value={filters.event_type ?? ""} onChange={(event) => setFilters({ ...filters, event_type: event.target.value as EventType | "" })}><option value="">All</option>{eventTypes.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
        <label>Operation<input aria-label="Event operation filter" value={filters.operation ?? ""} onChange={(event) => setFilters({ ...filters, operation: event.target.value })} placeholder="exact operation" /></label>
      </div>
      {!log?.available && <p className="empty-note">Persistent event log unavailable.</p>}
      {log?.available && !log.events.length && <p className="empty-note">No durable event matches the selected server-side filters.</p>}
      {!!log?.events.length && <div className="table-scroll"><table><thead><tr><th>ID</th><th>Occurred at</th><th>Controller</th><th>Operation</th><th>Type</th><th>State</th><th>Version</th></tr></thead><tbody>{log.events.map((event) => <tr key={eventKey(event)}><td>{event.event_id}</td><td><Timestamp value={event.occurred_at} /></td><td>{event.task.controller}</td><td>{event.operation}</td><td>{event.event_type}</td><td data-tone={stateTone(event.state)}>{event.state}</td><td>{event.task_version}</td></tr>)}</tbody></table></div>}
      <div className="paging-bar"><span>storage: {log?.storage.storage ?? "unavailable"} · {log?.storage.record_count ?? 0} tasks</span><button type="button" disabled={!log?.has_more || loading} onClick={loadMore}>{loading ? "Loading…" : "Load older events"}</button></div>
    </div>
  );
}

function mergeTaskPage(current: TasksResponse, page: TasksResponse): TasksResponse {
  const seen = new Set(current.tasks.map((task) => task.task_id));
  return { ...page, tasks: [...current.tasks, ...page.tasks.filter((task) => !seen.has(task.task_id))] };
}
function mergeEventPage(current: EventLogResponse, page: EventLogResponse): EventLogResponse {
  const seen = new Set(current.events.map(eventKey));
  return { ...page, events: [...current.events, ...page.events.filter((event) => !seen.has(eventKey(event)))] };
}

export default function AppShell() {
  const [active, setActive] = useState<Tab>("System");
  const [status, setStatus] = useState<RuntimeStatus | null>(null);
  const [data, setData] = useState<ConsoleData | null>(null);
  const [tasks, setTasks] = useState<TasksResponse | null>(null);
  const [eventLog, setEventLog] = useState<EventLogResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<DialogState>(null);
  const [projectSelection, setProjectSelection] = useState("");
  const [edgeSelection, setEdgeSelection] = useState("");
  const [taskFilters, setTaskFilters] = useState<TaskFilterState>({ controller: "", state: "", operation: "" });
  const [eventFilters, setEventFilters] = useState<EventFilters>({ controller: "", state: "", event_type: "", operation: "" });
  const [taskPaging, setTaskPaging] = useState(false);
  const [taskRefreshing, setTaskRefreshing] = useState(false);
  const [eventPaging, setEventPaging] = useState(false);
  const [streamState, setStreamState] = useState<StreamState>("connecting");
  const [lastEventID, setLastEventID] = useState("");
  const [timezone, setTimezone] = useState(DEFAULT_TIMEZONE);
  const [timezoneDraft, setTimezoneDraft] = useState(DEFAULT_TIMEZONE);
  const [timezoneSaving, setTimezoneSaving] = useState(false);
  const [relativeNow, setRelativeNow] = useState(() => Date.now());
  const lastEventIDRef = useRef("");
  const projectSelectionInitializedRef = useRef(false);
  const edgeSelectionInitializedRef = useRef(false);
  const streamStateRef = useRef<StreamState>("connecting");
  const taskFiltersRef = useRef<TaskFilters>({});
  const eventFiltersRef = useRef<EventFilters>({});
  const pendingEventsRef = useRef<JournalEvent[]>([]);

  const scopedTaskFilters = useMemo<TaskFilters>(() => ({ ...taskFilters, project_id: projectSelection, edge_id: edgeSelection }), [taskFilters, projectSelection, edgeSelection]);
  const scopedEventFilters = useMemo<EventFilters>(() => ({ ...eventFilters, project_id: projectSelection, edge_id: edgeSelection }), [eventFilters, projectSelection, edgeSelection]);

  useEffect(() => { streamStateRef.current = streamState; }, [streamState]);
  useEffect(() => { taskFiltersRef.current = scopedTaskFilters; }, [scopedTaskFilters]);
  useEffect(() => { eventFiltersRef.current = scopedEventFilters; }, [scopedEventFilters]);
  useEffect(() => {
    const projects = data?.projects ?? [];
    const ids = projects.map((project) => project.id);
    if (projectSelection && !ids.includes(projectSelection)) {
      setProjectSelection(projects.find((project) => project.current)?.id ?? "");
    }
  }, [data?.projects, projectSelection]);
  useEffect(() => {
    const devices = data?.edge.devices ?? [];
    const ids = devices.map((device) => device.id);
    if (edgeSelection && !ids.includes(edgeSelection)) setEdgeSelection(devices[0]?.id ?? "");
  }, [data?.edge.devices, edgeSelection]);

  const refresh = useCallback(async () => {
    const results = await Promise.allSettled([fetchRuntimeStatus(), fetchConsoleData(), fetchPreferences()]);
    if (results[0].status === "fulfilled") setStatus(results[0].value);
    if (results[1].status === "fulfilled") {
      const nextData = results[1].value;
      setData(nextData);
      if (!projectSelectionInitializedRef.current) {
        projectSelectionInitializedRef.current = true;
        setProjectSelection(nextData.projects.find((project) => project.current)?.id ?? "");
      }
      if (!edgeSelectionInitializedRef.current) {
        edgeSelectionInitializedRef.current = true;
        setEdgeSelection(nextData.edge.devices[0]?.id ?? "");
      }
    }
    if (results[2].status === "fulfilled") {
      setTimezone(results[2].value.timezone);
      setTimezoneDraft(results[2].value.timezone);
    }
    let failed = results.filter((result) => result.status === "rejected").length;
    if (streamStateRef.current !== "live") {
      try { setTasks(await fetchTasks(taskFiltersRef.current)); }
      catch { failed += 1; }
    }
    setError(failed ? failed + " console endpoint(s) unavailable" : null);
  }, []);

  useEffect(() => {
    void refresh();
    setRelativeNow(Date.now());
    const timer = window.setInterval(() => { setRelativeNow(Date.now()); void refresh(); }, 30_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  useEffect(() => {
    const controller = new AbortController();
    setTaskRefreshing(true);
    const timer = window.setTimeout(() => {
      void fetchTasks(scopedTaskFilters, "", controller.signal)
        .then(setTasks)
        .catch(() => { if (!controller.signal.aborted) setError("task journal unavailable"); })
        .finally(() => { if (!controller.signal.aborted) setTaskRefreshing(false); });
    }, 250);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [scopedTaskFilters]);

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void fetchEventLog(scopedEventFilters, "", controller.signal).then((page) => setEventLog(mergeLiveEvents(page, pendingEventsRef.current, scopedEventFilters))).catch(() => { if (!controller.signal.aborted) setError("event log unavailable"); });
    }, 250);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [scopedEventFilters]);

  useEffect(() => {
    if (typeof EventSource === "undefined") { setStreamState("offline"); return; }
    let stopped = false;
    let source: EventSource | null = null;
    let reconnectTimer: number | null = null;
    let attempt = 0;

    const connect = () => {
      if (stopped) return;
      setStreamState(attempt === 0 ? "connecting" : "reconnecting");
      const query = new URLSearchParams();
      if (lastEventIDRef.current) query.set("last_event_id", lastEventIDRef.current);
      const current = new EventSource("/console/events" + (query.size ? "?" + query.toString() : ""));
      source = current;
      current.addEventListener("snapshot", (message) => {
        try {
          const snapshot = parseTasksResponse(JSON.parse((message as MessageEvent<string>).data));
          const filters = taskFiltersRef.current;
          if (hasTaskFilters(filters)) void fetchTasks(filters).then(setTasks).catch(() => setStreamState("offline"));
          else setTasks(snapshot);
        } catch { setStreamState("offline"); }
      });
      current.addEventListener("event_snapshot", (message) => {
        try {
          const raw = message as MessageEvent<string>;
          const snapshot = parseEventLogResponse(JSON.parse(raw.data));
          lastEventIDRef.current = raw.lastEventId;
          setLastEventID(raw.lastEventId);
          const filters = eventFiltersRef.current;
          if (hasEventFilters(filters)) void fetchEventLog(filters).then((page) => setEventLog(mergeLiveEvents(page, pendingEventsRef.current, filters))).catch(() => setStreamState("offline"));
          else setEventLog(mergeLiveEvents(snapshot, pendingEventsRef.current, filters));
        } catch { setStreamState("offline"); }
      });
      current.addEventListener("journal", (message) => {
        try {
          const raw = message as MessageEvent<string>;
          const event = parseJournalEvent(JSON.parse(raw.data));
          const nextID = raw.lastEventId || String(event.event_id);
          lastEventIDRef.current = nextID;
          setLastEventID(nextID);
          pendingEventsRef.current = [event, ...pendingEventsRef.current.filter((item) => eventKey(item) !== eventKey(event))].slice(0, 200);
          if (matchesTaskScope(event.task, taskFiltersRef.current)) {
            setTasks((current) => current ? { ...current, tasks: [event.task, ...current.tasks.filter((task) => task.task_id !== event.task.task_id)].slice(0, 200) } : current);
          }
          if (matchesEvent(event, eventFiltersRef.current)) {
            setEventLog((current) => current
              ? mergeLiveEvents(current, [event], eventFiltersRef.current)
              : { schema_version: 1, available: true, storage: { storage: "degraded", detail: "awaiting durable snapshot", record_count: 0, database_size_bytes: 0, wal_size_bytes: 0 }, events: [event], next_cursor: "", has_more: false });
          }
        } catch { setStreamState("offline"); }
      });
      current.addEventListener("stream", () => { attempt = 0; setStreamState("live"); });
      current.onerror = () => {
        current.close();
        if (stopped || reconnectTimer !== null) return;
        setStreamState("reconnecting");
        const delay = Math.min(30_000, 1_000 * (2 ** Math.min(attempt, 5)));
        attempt += 1;
        reconnectTimer = window.setTimeout(() => { reconnectTimer = null; connect(); }, delay);
      };
    };

    connect();
    return () => {
      stopped = true;
      source?.close();
      if (reconnectTimer !== null) window.clearTimeout(reconnectTimer);
    };
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "F1") {
        event.preventDefault(); setDialog({ title: "Help", body: consoleHelp });
      } else if (event.key === "Escape") setDialog(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const loadMoreTasks = useCallback(async () => {
    if (!tasks?.has_more || taskPaging) return;
    setTaskPaging(true);
    try { const page = await fetchTasks(taskFiltersRef.current, tasks.next_cursor); setTasks((current) => current ? mergeTaskPage(current, page) : page); }
    finally { setTaskPaging(false); }
  }, [taskPaging, tasks]);

  const loadMoreEvents = useCallback(async () => {
    if (!eventLog?.has_more || eventPaging) return;
    setEventPaging(true);
    try { const page = await fetchEventLog(eventFiltersRef.current, eventLog.next_cursor); setEventLog((current) => current ? mergeEventPage(current, page) : page); }
    finally { setEventPaging(false); }
  }, [eventLog, eventPaging]);

  const saveTimezone = useCallback(async () => {
    setTimezoneSaving(true);
    try {
      const preference = await updatePreferences(timezoneDraft.trim());
      setTimezone(preference.timezone);
      setTimezoneDraft(preference.timezone);
      setRelativeNow(Date.now());
      setError(null);
    } catch {
      setError("timezone preference rejected");
    } finally {
      setTimezoneSaving(false);
    }
  }, [timezoneDraft]);

  const content = (() => {
    switch (active) {
      case "System": return <SystemTab status={status} data={data} error={error} />;
      case "Agents": return <AgentsTab data={data} projectSelection={projectSelection} edgeSelection={edgeSelection} />;
      case "Tasks": return <TasksTab tasks={tasks} filters={taskFilters} setFilters={setTaskFilters} loadMore={() => void loadMoreTasks()} loading={taskPaging || taskRefreshing} />;
      case "Brain": return <BrainTab data={data} onOpenGraph={() => setActive("Graph")} />;
      case "Graph": return <div className="graph-panel"><Section title="Brain Link Graph" /><GraphView brain={data?.brain ?? null} /></div>;
      case "Edge": return <EdgeTab data={data} />;
      case "Observability": return <ObservabilityTab data={data} projectSelection={projectSelection} edgeSelection={edgeSelection} />;
      case "Security": return <SecurityTab data={data} status={status} />;
      case "Events": return <EventsTab log={eventLog} filters={eventFilters} setFilters={setEventFilters} streamState={streamState} lastEventID={lastEventID} loadMore={() => void loadMoreEvents()} loading={eventPaging} />;
    }
  })();

  return (
    <TimeDisplayProvider value={{ timezone, now: relativeNow }}>
      <main className="console-layout">
      <aside className="console-sidebar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true" />
          <div><strong>Aeontra</strong><small>Operator console</small></div>
        </div>
        <p className="sidebar-label">Workspace</p>
        <nav aria-label="Console sections">
          <div className="tabs" role="tablist" aria-label="Console screens">
            {tabs.map((tab) => <button
              key={tab}
              id={`tab-${tab.toLowerCase()}`}
              type="button"
              role="tab"
              aria-controls="console-panel"
              aria-selected={active === tab}
              tabIndex={active === tab ? 0 : -1}
              onClick={() => setActive(tab)}
              onKeyDown={(event) => {
                const delta = event.key === "ArrowRight" || event.key === "ArrowDown" ? 1 : event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1 : 0;
                if (!delta) return;
                event.preventDefault();
                const next = tabs[(tabs.indexOf(tab) + delta + tabs.length) % tabs.length];
                setActive(next);
                document.getElementById(`tab-${next.toLowerCase()}`)?.focus();
              }}
            >{tab}<span aria-hidden="true">↗</span></button>)}
          </div>
        </nav>
        <div className="sidebar-bottom">
          <div className="sidebar-status"><span className="status-dot" data-tone={stateTone(streamState)} /><span>Event stream <strong>{streamState}</strong></span></div>
          <form method="post" action="/console/logout"><button type="submit">Sign out <span aria-hidden="true">↗</span></button></form>
        </div>
      </aside>
      <div className="console-main">
        <header className="console-header">
          <div><p className="eyebrow">Control plane / {active}</p><h1>{active}</h1><p className="page-description">{tabDescriptions[active]}</p></div>
          <div className="header-actions">
            <span className="runtime-badge" data-tone={error ? "warn" : status ? stateTone(status.status) : "dim"}>
              <span className="status-dot" />{error ? "Partial data" : status ? status.status : "Loading"}
            </span>
            <button type="button" onClick={() => void refresh()}>Refresh</button>
            <button type="button" onClick={() => setDialog({ title: "Help", body: consoleHelp })}>Help</button>
          </div>
        </header>
        <div className="console-toolbar">
          <div className="runtime-selectors">
            <label>Project
              <select aria-label="Project" value={projectSelection} onChange={(event) => { projectSelectionInitializedRef.current = true; setProjectSelection(event.target.value); }}>
                <option value="">All projects</option>
                {data?.projects.map((project) => <option key={project.id} value={project.id}>{project.label}{project.current ? " [current]" : ""}</option>)}
              </select>
            </label>
            <label>Edge device
              <select aria-label="Edge device" value={edgeSelection} onChange={(event) => { edgeSelectionInitializedRef.current = true; setEdgeSelection(event.target.value); }}>
                <option value="">All Edge devices</option>
                {data?.edge.devices.map((device) => <option key={device.id} value={device.id}>{device.label}</option>)}
              </select>
            </label>
          </div>
          <form className="timezone-control" onSubmit={(event) => { event.preventDefault(); void saveTimezone(); }}>
            <label>Timezone<input aria-label="Timezone" list="console-timezones" maxLength={64} value={timezoneDraft} onChange={(event) => setTimezoneDraft(event.target.value)} /></label>
            <datalist id="console-timezones"><option value="America/Bogota" /><option value="America/Argentina/Buenos_Aires" /><option value="Europe/Moscow" /><option value="UTC" /></datalist>
            <button type="submit" disabled={timezoneSaving}>{timezoneSaving ? "Saving…" : "Apply"}</button>
            <span aria-live="polite">Timezone: {timezone}</span>
          </form>
        </div>
        {error && <p className="load-error" role="alert">Some console data is unavailable: {error}.</p>}
        <section id="console-panel" className="screen" role="tabpanel" aria-labelledby={`tab-${active.toLowerCase()}`}>{content}</section>
        <footer className="console-footer">
          <span>Read-only view · consequential actions remain in MCP</span>
          <span>{status?.tool_count ?? "—"} tools · Rev {status?.commit ? status.commit.slice(0, 8) : "unknown"}</span>
        </footer>
      </div>
      {dialog && <div className="dialog-backdrop" role="presentation" onClick={() => setDialog(null)}>
        <section className="console-dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title" onClick={(event) => event.stopPropagation()}>
          <h2 id="dialog-title">{dialog.title}</h2>
          <p>{dialog.body}</p>
          <button type="button" onClick={() => setDialog(null)}>Close</button>
        </section>
      </div>}
      </main>
    </TimeDisplayProvider>
  );
}
