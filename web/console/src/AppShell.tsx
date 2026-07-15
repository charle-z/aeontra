import { useCallback, useEffect, useMemo, useState } from "react";
import { fetchRuntimeStatus } from "./api";
import { fetchConsoleData, fetchTasks } from "./dataApi";
import { parseTaskEntry, parseTasksResponse, type ConsoleData, type TaskEntry, type TasksResponse } from "./dataTypes";
import type { RuntimeStatus } from "./types";
import GraphView from "./GraphView";

const tabs = ["System", "Agents", "Tasks", "Brain", "Graph", "Edge", "Observability", "Security", "Events"] as const;
const taskStates = ["requested", "planned", "awaiting_approval", "executing", "observing", "validating", "completed", "failed", "cancelled", "disconnected"] as const;
type Tab = (typeof tabs)[number];
type DialogState = { title: string; body: string } | null;
type EventEntry = { at: string; level: "INFO" | "WARN"; message: string };
type Tone = "normal" | "ok" | "warn" | "dim";

function nowLabel(): string {
  return new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function bytes(value: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let next = value;
  let index = 0;
  while (next >= 1024 && index < units.length - 1) { next /= 1024; index += 1; }
  return next.toFixed(index === 0 ? 0 : 1) + " " + units[index];
}

function age(value: string): string {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) return "unknown";
  const seconds = Math.max(0, Math.floor((Date.now() - parsed) / 1000));
  if (seconds < 60) return seconds + "s";
  if (seconds < 3600) return Math.floor(seconds / 60) + "m";
  return Math.floor(seconds / 3600) + "h";
}

function stateTone(state: string): Tone {
  if (state === "completed") return "ok";
  if (state === "failed" || state === "cancelled" || state === "disconnected") return "warn";
  if (state === "planned" || state === "awaiting_approval") return "dim";
  return "normal";
}

function Row({ label, value, tone = "normal", help }: { label: string; value: string; tone?: Tone; help: string }) {
  return <button className="firmware-row" type="button" data-help={help}><span>{label}</span><strong data-tone={tone}>{value}</strong></button>;
}

function Section({ title }: { title: string }) { return <h2 className="section-title">{title}</h2>; }

function Panel({ children }: { children: React.ReactNode }) {
  const [help, setHelp] = useState("Select an item to inspect its verified meaning.");
  return (
    <div className="firmware-body">
      <section className="firmware-list" onFocus={(event) => { const next = (event.target as HTMLElement).dataset.help; if (next) setHelp(next); }} onClick={(event) => { const target = (event.target as HTMLElement).closest<HTMLElement>("[data-help]"); if (target?.dataset.help) setHelp(target.dataset.help); }}>
        {children}
      </section>
      <aside className="item-help"><h2>Item Specific Help</h2><p>{help}</p><small>↑↓ item · ←→ screen · F1 help · F5 refresh</small></aside>
    </div>
  );
}

function SystemTab({ status, data, error }: { status: RuntimeStatus | null; data: ConsoleData | null; error: string | null }) {
  const system = data?.system;
  const memoryUsed = system ? Math.max(0, system.memory_total_bytes - system.memory_available_bytes) : 0;
  const diskUsed = system ? Math.max(0, system.disk_total_bytes - system.disk_available_bytes) : 0;
  return (
    <Panel>
      <Section title="Runtime Identity" />
      <Row label="Status" value={error ? "[Unavailable]" : status ? "[" + status.status + "]" : "[Loading]"} tone={error ? "warn" : status ? "ok" : "dim"} help="Live status from the exact allowlisted /console/status response." />
      <Row label="Version / Commit" value={status ? status.version + " / " + status.commit : "—"} help="Exact process version and commit." />
      <Row label="Protocol" value={status?.protocol_version ?? "—"} help="MCP protocol version reported by the runtime." />
      <Row label="Tool Catalog" value={status ? status.tool_count + " tools" : "—"} help="The public post-P9 catalog remains exactly 67 tools." />
      <Row label="Catalog Hash" value={status?.catalog_hash ?? "—"} help="Deterministic catalog identity used to detect drift." />
      <Section title="VPS Resources" />
      <Row label="Resource probe" value={system?.available ? "[Available]" : "[Unavailable]"} tone={system?.available ? "ok" : "warn"} help="The server marks this unavailable when any bounded host probe fails." />
      <Row label="CPU" value={system ? system.cpu_count + " logical CPUs" : "—"} help="Real logical CPU count visible to the container." />
      <Row label="RAM" value={system?.available ? bytes(memoryUsed) + " / " + bytes(system.memory_total_bytes) : "Unavailable"} help="Used and total memory derived from MemAvailable; no process list is exposed." />
      <Row label="Disk" value={system?.available ? bytes(diskUsed) + " / " + bytes(system.disk_total_bytes) : "Unavailable"} help="Used and total bytes for the container root filesystem." />
      <Row label="Load average" value={system?.available ? system.load_1.toFixed(2) + " · " + system.load_5.toFixed(2) + " · " + system.load_15.toFixed(2) : "Unavailable"} help="Real one, five and fifteen minute load averages." />
    </Panel>
  );
}

function AgentsTab({ status, data, tasks }: { status: RuntimeStatus | null; data: ConsoleData | null; tasks: TasksResponse | null }) {
  const latestByController = new Map<string, TaskEntry>();
  for (const task of tasks?.tasks ?? []) if (!latestByController.has(task.controller)) latestByController.set(task.controller, task);
  return (
    <Panel>
      <Section title="Session Ledger" />
      <Row label="Console session" value={status?.authenticated ? "Authenticated" : "Unavailable"} tone={status?.authenticated ? "ok" : "warn"} help="Derived only from the authenticated status response." />
      {[...latestByController.entries()].map(([controller, task]) => <Row key={controller} label={controller} value={task.operation + " · " + task.state} tone={stateTone(task.state)} help="Latest durable externalized operation for this generic transport controller. No identity or repository is exposed." />)}
      {!latestByController.size && <Row label="Controllers" value={tasks?.available ? "Idle" : "Unavailable"} tone="dim" help="No durable operation is currently recorded; the console does not invent active agents." />}
      <Section title="Payload I/O Estimate" />
      <Row label="Requests" value={data ? String(data.payload.request_count) : "—"} help="Authenticated MCP POST requests observed by this process since startup." />
      <Row label="Input" value={data ? bytes(data.payload.input_bytes) + " · ~" + data.payload.input_tokens_estimate + " tokens" : "—"} help="MCP request bytes divided by four. This is an explicit estimate, not provider billing." />
      <Row label="Output" value={data ? bytes(data.payload.output_bytes) + " · ~" + data.payload.output_tokens_estimate + " tokens" : "—"} help="MCP response bytes divided by four. The server never sees model-provider token accounting." />
      <Row label="Formula" value={data?.payload.formula ?? "—"} tone="dim" help="Declared estimation formula." />
    </Panel>
  );
}

function TasksTab({ tasks }: { tasks: TasksResponse | null }) {
  return (
    <div className="table-panel">
      <Section title="Task Journal — durable states via SSE" />
      <div className="state-flow" aria-label="Task lifecycle">{taskStates.map((state) => <span key={state}>{state}</span>)}</div>
      {!tasks?.available && <p className="empty-note">Task journal unavailable.</p>}
      {tasks?.available && !tasks.tasks.length && <p className="empty-note">Idle — no externalized operation is recorded.</p>}
      {!!tasks?.tasks.length && <div className="table-scroll"><table><thead><tr><th>Task ID</th><th>Controller</th><th>Operation</th><th>State</th><th>Heartbeat</th></tr></thead><tbody>{tasks.tasks.map((task) => <tr key={task.task_id}><td>{task.task_id.slice(0, 8)}</td><td>{task.controller}</td><td>{task.operation}</td><td data-tone={stateTone(task.state)}>{task.state}</td><td>{age(task.heartbeat)}</td></tr>)}</tbody></table></div>}
      <p className="panel-note">Only server-generated actions and verifiable state appear here. Model reasoning, prompts, parameters, results, paths and identities are never journaled.</p>
    </div>
  );
}

function BrainTab({ data }: { data: ConsoleData | null }) {
  const brain = data?.brain;
  return (
    <Panel>
      <Section title="Brain Index" />
      <Row label="Available" value={brain ? String(brain.available) : "—"} tone={brain?.available ? "ok" : "warn"} help="Whether the isolated Brain store is attached to this runtime." />
      <Row label="Ready" value={brain?.available ? String(brain.ready) : "Unavailable"} tone={brain?.ready ? "ok" : "warn"} help="Disposable SQLite index readiness." />
      <Row label="Schema" value={brain?.available ? String(brain.schema_version) : "—"} help="Real index schema version." />
      <Row label="Notes" value={brain?.available ? String(brain.note_count) : "—"} help="Aggregate note count only; titles and bodies remain private." />
      <Row label="Source bytes" value={brain?.available ? bytes(brain.source_bytes) : "—"} help="Aggregate Markdown source size." />
      <Row label="Links" value={brain?.available ? brain.link_count + " total · " + brain.broken_link_count + " broken" : "—"} help="Aggregate link counts from the Brain index." />
      <Row label="Indexed at" value={brain?.indexed_at || "—"} help="Last real index timestamp." />
      <Row label="Graph" value={brain?.available ? brain.nodes.length + " opaque nodes · " + brain.edges.length + " edges" : "Unavailable"} help="Bounded real graph with request-local opaque IDs; no slugs or titles." />
    </Panel>
  );
}

function ObservabilityTab({ data }: { data: ConsoleData | null }) {
  const observability = data?.observability;
  return (
    <div className="table-panel">
      <Section title="Structured Route Summary" />
      <p className="panel-note">Enabled: {String(observability?.enabled ?? false)} · sink failures: {observability?.failures ?? 0}</p>
      {!observability?.routes.length ? <p className="empty-note">No route events observed in this process.</p> : <div className="table-scroll"><table><thead><tr><th>Route</th><th>Requests</th><th>4xx</th><th>5xx</th><th>P95 ms</th></tr></thead><tbody>{observability.routes.map((route) => <tr key={route.route}><td>{route.route}</td><td>{route.requests}</td><td>{route.client_4xx}</td><td>{route.server_5xx}</td><td>{route.p95_ms}</td></tr>)}</tbody></table></div>}
      <p className="panel-note">Counters and bounded durations only. Raw events and request content remain outside this surface.</p>
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
      <Row label="Query auth" value={security?.query_auth ?? "—"} tone={security?.query_auth === "rejected" ? "ok" : "warn"} help="Every ?key= credential returns 401, including a correct legacy token." />
      <Row label="Cookie" value={security?.cookie ?? "—"} help="Opaque browser session cookie posture." />
      <Section title="Authority Boundary" />
      <Row label="Console" value={security?.console_authority ?? status?.surface ?? "—"} help="The console observes allowlisted state and does not execute tools." />
      <Row label="Free shell" value={security?.free_shell ?? "—"} tone={security?.free_shell === "absent" ? "ok" : "warn"} help="No terminal or arbitrary command interface exists in the browser." />
    </Panel>
  );
}

export default function AppShell() {
  const [active, setActive] = useState<Tab>("System");
  const [status, setStatus] = useState<RuntimeStatus | null>(null);
  const [data, setData] = useState<ConsoleData | null>(null);
  const [tasks, setTasks] = useState<TasksResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<DialogState>(null);
  const [attract, setAttract] = useState(false);
  const [events, setEvents] = useState<EventEntry[]>([]);
  const reducedMotion = useMemo(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches, []);

  const log = useCallback((level: EventEntry["level"], message: string) => setEvents((current) => [{ at: nowLabel(), level, message }, ...current].slice(0, 60)), []);

  const refresh = useCallback(async () => {
    const results = await Promise.allSettled([fetchRuntimeStatus(), fetchConsoleData(), fetchTasks()]);
    if (results[0].status === "fulfilled") setStatus(results[0].value);
    if (results[1].status === "fulfilled") setData(results[1].value);
    if (results[2].status === "fulfilled") setTasks(results[2].value);
    const failed = results.filter((result) => result.status === "rejected").length;
    setError(failed ? failed + " console endpoint(s) unavailable" : null);
    log(failed ? "WARN" : "INFO", failed ? "Refresh completed with unavailable safe endpoint data." : "Runtime, operations and task state refreshed.");
  }, [log]);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 30_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const source = new EventSource("/console/events");
    source.addEventListener("snapshot", (event) => {
      try { setTasks(parseTasksResponse(JSON.parse((event as MessageEvent<string>).data))); }
      catch { log("WARN", "Rejected an invalid task snapshot."); }
    });
    source.addEventListener("task", (event) => {
      try {
        const task = parseTaskEntry(JSON.parse((event as MessageEvent<string>).data));
        setTasks((current) => ({ schema_version: 1, available: true, tasks: [task, ...(current?.tasks ?? []).filter((item) => item.task_id !== task.task_id)].slice(0, 100) }));
        log("INFO", task.operation + " → " + task.state);
      } catch { log("WARN", "Rejected an invalid task event."); }
    });
    source.onerror = () => log("WARN", "Task event stream disconnected; durable snapshot polling remains active.");
    return () => source.close();
  }, [log]);

  useEffect(() => {
    if (!attract || reducedMotion) return;
    const timer = window.setInterval(() => setActive((current) => tabs[(tabs.indexOf(current) + 1) % tabs.length]), 2600);
    return () => window.clearInterval(timer);
  }, [attract, reducedMotion]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const index = tabs.indexOf(active);
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        const delta = event.key === "ArrowLeft" ? -1 : 1;
        setActive(tabs[(index + delta + tabs.length) % tabs.length]);
      } else if (event.key === "ArrowUp" || event.key === "ArrowDown") {
        const rows = Array.from(document.querySelectorAll<HTMLButtonElement>(".firmware-row"));
        if (!rows.length) return;
        event.preventDefault();
        const current = rows.indexOf(document.activeElement as HTMLButtonElement);
        const delta = event.key === "ArrowUp" ? -1 : 1;
        rows[(Math.max(current, 0) + delta + rows.length) % rows.length].focus();
      } else if (event.key === "F1") {
        event.preventDefault(); setDialog({ title: "Help", body: "Arrow keys and touch navigate verified screens. F5 refreshes safe endpoints. The console never displays private model reasoning." });
      } else if (event.key === "F5") {
        event.preventDefault(); void refresh();
      } else if (event.key === "F8") {
        event.preventDefault(); if (reducedMotion) setDialog({ title: "Attract", body: "Automatic rotation is disabled by reduced-motion preference." }); else setAttract((value) => !value);
      } else if (event.key === "F9" || event.key === "F10") {
        event.preventDefault(); setDialog({ title: event.key === "F9" ? "Cancel" : "Approve", body: "No consequential action is exposed by P8.1. MCP single-use plans remain the only authority path." });
      } else if (event.key === "Escape") setDialog(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, reducedMotion, refresh]);

  const content = (() => {
    switch (active) {
      case "System": return <SystemTab status={status} data={data} error={error} />;
      case "Agents": return <AgentsTab status={status} data={data} tasks={tasks} />;
      case "Tasks": return <TasksTab tasks={tasks} />;
      case "Brain": return <BrainTab data={data} />;
      case "Graph": return <div className="graph-panel"><Section title="Brain Link Graph" /><GraphView brain={data?.brain ?? null} /></div>;
      case "Edge": return <Panel><Section title="Edge Devices" /><Row label="Pairing" value={data?.edge.state === "not_paired" ? "Not paired" : data?.edge.state ?? "Unavailable"} tone="dim" help="P11 has not paired a device. No host or workcell identity is fabricated." /><Row label="Workcell" value="Unavailable — P12" tone="dim" help="Parrot Workcell is outside P8.1." /></Panel>;
      case "Observability": return <ObservabilityTab data={data} />;
      case "Security": return <SecurityTab data={data} status={status} />;
      case "Events": return <div className="events-panel"><Section title="Safe Console Events" />{events.length ? events.map((entry, index) => <p key={entry.at + "-" + index}><time>{entry.at}</time> <strong>{entry.level}</strong> {entry.message}</p>) : <p>No browser event recorded.</p>}</div>;
    }
  })();

  return (
    <main className="firmware-shell">
      <header className="firmware-header"><span>MCP DEVBOX OPERATIONS FIRMWARE</span><span>Rev {status?.commit ? status.commit.slice(0, 8) : "unknown"}</span></header>
      <nav className="tabs" role="tablist" aria-label="Console screens">{tabs.map((tab) => <button key={tab} type="button" role="tab" aria-selected={active === tab} onClick={() => { setActive(tab); setAttract(false); }}>{tab}</button>)}</nav>
      <section className="screen" aria-live="polite">{content}</section>
      <footer className="keybar"><span><b>F1</b> Help</span><span><b>↑↓</b> Item</span><span><b>←→</b> Screen</span><span><b>F5</b> Refresh</span><span><b>F8</b> Attract</span><span><b>F9</b> Cancel</span><span><b>F10</b> Approve</span><form method="post" action="/console/logout"><button type="submit">ESC Sign out</button></form></footer>
      {dialog && <div className="dialog-backdrop" role="presentation" onClick={() => setDialog(null)}><section className="firmware-dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title" onClick={(event) => event.stopPropagation()}><h2 id="dialog-title">{dialog.title}</h2><p>{dialog.body}</p><button type="button" onClick={() => setDialog(null)}>[ Ok ]</button></section></div>}
    </main>
  );
}
