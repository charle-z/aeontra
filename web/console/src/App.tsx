import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchRuntimeStatus } from "./api";
import type { RuntimeStatus } from "./types";

const tabs = ["System", "Agents", "Tasks", "Brain", "Graph", "Edge", "Observability", "Security", "Events"] as const;
type Tab = (typeof tabs)[number];
type DialogState = { title: string; body: string } | null;
type EventEntry = { at: string; level: "INFO" | "WARN"; message: string };

const unavailable = "Unavailable — safe endpoint not implemented";

function nowLabel(): string {
  return new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function Row({ label, value, tone = "normal", help }: { label: string; value: string; tone?: "normal" | "ok" | "warn" | "dim"; help: string }) {
  return (
    <button className="firmware-row" type="button" data-help={help}>
      <span>{label}</span>
      <strong data-tone={tone}>{value}</strong>
    </button>
  );
}

function Panel({ children }: { children: React.ReactNode }) {
  const [help, setHelp] = useState("Select an item to inspect its verified meaning.");
  return (
    <div className="firmware-body">
      <section
        className="firmware-list"
        onFocus={(event) => {
          const target = event.target as HTMLElement;
          const next = target.dataset.help;
          if (next) setHelp(next);
        }}
        onClick={(event) => {
          const target = (event.target as HTMLElement).closest<HTMLElement>("[data-help]");
          if (target?.dataset.help) setHelp(target.dataset.help);
        }}
      >
        {children}
      </section>
      <aside className="item-help">
        <h2>Item Specific Help</h2>
        <p>{help}</p>
        <small>↑↓ item · ←→ screen · F1 help · F5 refresh</small>
      </aside>
    </div>
  );
}

function Section({ title }: { title: string }) {
  return <h2 className="section-title">{title}</h2>;
}

function SystemTab({ status, error }: { status: RuntimeStatus | null; error: string | null }) {
  return (
    <Panel>
      <Section title="Runtime Identity" />
      <Row label="Status" value={error ? "[Unavailable]" : status ? `[${status.status}]` : "[Loading]"} tone={error ? "warn" : status ? "ok" : "dim"} help="Live status from the exact allowlisted /console/status response." />
      <Row label="Version" value={status?.version ?? "—"} help="Runtime version reported by the Go process." />
      <Row label="Commit" value={status?.commit ?? "—"} help="Exact running commit, also exposed by the hardened runtime headers." />
      <Row label="Protocol" value={status?.protocol_version ?? "—"} help="MCP protocol version reported by the runtime." />
      <Row label="Tool Catalog" value={status ? `${status.tool_count} tools` : "—"} help="Current public catalog count. P8.1 preserves the post-P9 67-tool contract." />
      <Row label="Catalog Hash" value={status?.catalog_hash ?? "—"} help="Deterministic public catalog identity used to detect drift." />
      <Section title="Console Boundary" />
      <Row label="Authenticated" value={status ? String(status.authenticated) : "—"} tone={status?.authenticated ? "ok" : "dim"} help="Whether this response was served through an authenticated console session." />
      <Row label="Surface" value={status?.surface ?? "—"} help="The server-declared authority boundary for this browser surface." />
      <Row label="Resources" value={unavailable} tone="dim" help="No VPS resource values are shown until a dedicated allowlisted endpoint exists." />
    </Panel>
  );
}

function HonestUnavailable({ title, rows }: { title: string; rows: Array<[string, string, string]> }) {
  return (
    <Panel>
      <Section title={title} />
      {rows.map(([label, value, help]) => <Row key={label} label={label} value={value} tone="dim" help={help} />)}
    </Panel>
  );
}

function GraphTab() {
  const surfaceRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const surface = surfaceRef.current;
    if (!surface) return;
    let dragging = false;
    const begin = () => { dragging = true; surface.dataset.dragging = "true"; };
    const end = () => { dragging = false; delete surface.dataset.dragging; };
    const move = () => { if (dragging) surface.dataset.moved = "true"; };
    surface.addEventListener("pointerdown", begin);
    surface.addEventListener("pointermove", move);
    surface.addEventListener("pointerup", end);
    surface.addEventListener("pointercancel", end);
    return () => {
      surface.removeEventListener("pointerdown", begin);
      surface.removeEventListener("pointermove", move);
      surface.removeEventListener("pointerup", end);
      surface.removeEventListener("pointercancel", end);
    };
  }, []);
  return (
    <div className="graph-panel">
      <h2 className="section-title">Brain Link Graph</h2>
      <div ref={surfaceRef} className="graph-surface" role="img" aria-label="Brain graph unavailable">
        <p>{unavailable}</p>
        <small>No nodes or links are fabricated. Zoom and pan activate when the safe graph endpoint exists.</small>
      </div>
    </div>
  );
}

export default function App() {
  const [active, setActive] = useState<Tab>("System");
  const [status, setStatus] = useState<RuntimeStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<DialogState>(null);
  const [attract, setAttract] = useState(false);
  const [events, setEvents] = useState<EventEntry[]>([]);
  const reducedMotion = useMemo(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches, []);

  const log = useCallback((level: EventEntry["level"], message: string) => {
    setEvents((current) => [{ at: nowLabel(), level, message }, ...current].slice(0, 40));
  }, []);

  const refresh = useCallback(async () => {
    const controller = new AbortController();
    try {
      const next = await fetchRuntimeStatus(controller.signal);
      setStatus(next);
      setError(null);
      log("INFO", `Runtime status refreshed: ${next.status}, ${next.tool_count} tools.`);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "runtime status unavailable";
      setError(message);
      log("WARN", "Runtime status refresh failed without exposing response content.");
    }
    return () => controller.abort();
  }, [log]);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 30_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  useEffect(() => {
    if (!attract || reducedMotion) return;
    const timer = window.setInterval(() => {
      setActive((current) => tabs[(tabs.indexOf(current) + 1) % tabs.length]);
    }, 2_600);
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
        event.preventDefault();
        setDialog({ title: "Help", body: "Use arrow keys or touch to move through screens. This console only renders allowlisted server data and observable browser events." });
      } else if (event.key === "F5") {
        event.preventDefault();
        void refresh();
      } else if (event.key === "F8") {
        event.preventDefault();
        if (reducedMotion) setDialog({ title: "Attract", body: "Automatic screen rotation is disabled because reduced motion is enabled." });
        else setAttract((value) => !value);
      } else if (event.key === "F9" || event.key === "F10") {
        event.preventDefault();
        setDialog({ title: event.key === "F9" ? "Cancel" : "Approve", body: "No actionable plan is exposed here. Consequential operations remain bound to MCP single-use plans and explicit authority." });
      } else if (event.key === "Escape") {
        setDialog(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, reducedMotion, refresh]);

  const content = (() => {
    switch (active) {
      case "System": return <SystemTab status={status} error={error} />;
      case "Agents": return <HonestUnavailable title="Session Ledger" rows={[["Console session", status?.authenticated ? "Authenticated" : "Unavailable", "Derived only from the authenticated flag in /console/status."], ["Agent ledger", unavailable, "No agent identities or repository scopes are exposed until a safe ledger endpoint exists."], ["Payload estimate", unavailable, "Payload accounting will be bytes divided by four and explicitly labeled as an estimate."]]} />;
      case "Tasks": return <HonestUnavailable title="Task Journal" rows={[["Journal", unavailable, "Durable task state will come from /state/tasks through a strict safe endpoint."], ["Controller heartbeat", "Idle / unknown", "No heartbeat means idle or disconnected; the UI never claims autonomous work."]]} />;
      case "Brain": return <HonestUnavailable title="Brain Status" rows={[["Availability", unavailable, "Brain status is not inferred from the presence of tools."], ["Index", unavailable, "Index readiness and schema require a dedicated bounded endpoint."]]} />;
      case "Graph": return <GraphTab />;
      case "Edge": return <HonestUnavailable title="Edge Devices" rows={[["Pairing", "Not paired", "P11 has not paired an edge device. No device identity is invented."], ["Workcell", "Unavailable", "Parrot workcells are explicitly outside P8.1."]]} />;
      case "Observability": return <HonestUnavailable title="Observability Summary" rows={[["Route summary", unavailable, "Only aggregate content-free counters may be exposed by a future allowlisted endpoint."], ["Raw logs", "Never exposed", "Prompts, params, paths, targets, identities and raw errors remain outside the console."]]} />;
      case "Security": return <HonestUnavailable title="Security Posture" rows={[["Console authority", status?.surface ?? "presentation-only", "Server-declared console authority boundary."], ["Query-key auth", "Removal pending in P8.1", "The branch must force ?key= to return 401 before closure."], ["Free shell", "Absent", "The browser cannot invoke MCP tools or arbitrary commands."]]} />;
      case "Events": return <div className="events-panel"><h2 className="section-title">Console Events</h2>{events.length ? events.map((entry, index) => <p key={`${entry.at}-${index}`}><time>{entry.at}</time> <strong>{entry.level}</strong> {entry.message}</p>) : <p>No browser event has been recorded.</p>}</div>;
    }
  })();

  return (
    <main className="firmware-shell">
      <header className="firmware-header">
        <span>MCP DEVBOX OPERATIONS FIRMWARE</span>
        <span>Rev {status?.commit ? status.commit.slice(0, 8) : "unknown"}</span>
      </header>
      <nav className="tabs" role="tablist" aria-label="Console screens">
        {tabs.map((tab) => <button key={tab} type="button" role="tab" aria-selected={active === tab} onClick={() => { setActive(tab); setAttract(false); }}>{tab}</button>)}
      </nav>
      <section className="screen" aria-live="polite">{content}</section>
      <footer className="keybar">
        <span><b>F1</b> Help</span><span><b>↑↓</b> Item</span><span><b>←→</b> Screen</span><span><b>F5</b> Refresh</span><span><b>F8</b> Attract</span><span><b>F9</b> Cancel</span><span><b>F10</b> Approve</span>
        <form method="post" action="/console/logout"><button type="submit">ESC Sign out</button></form>
      </footer>
      {dialog && <div className="dialog-backdrop" role="presentation" onClick={() => setDialog(null)}><section className="firmware-dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title" onClick={(event) => event.stopPropagation()}><h2 id="dialog-title">{dialog.title}</h2><p>{dialog.body}</p><button type="button" onClick={() => setDialog(null)}>[ Ok ]</button></section></div>}
    </main>
  );
}
