import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const runtime = {
  status: "ok", version: "0.9.0", protocol_version: "2025-06-18",
  commit: "4fbe1dda02351c632e67c0f10a5c5b314df745e2", tool_count: 67,
  catalog_hash: "sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed",
  authenticated: true, surface: "presentation-only",
};

const data = {
  schema_version: 1,
  system: { available: true, cpu_count: 2, memory_total_bytes: 4294967296, memory_available_bytes: 2147483648, disk_total_bytes: 85899345920, disk_available_bytes: 42949672960, load_1: 0.1, load_5: 0.2, load_15: 0.3 },
  payload: { request_count: 8, input_bytes: 4096, output_bytes: 2048, input_tokens_estimate: 1024, output_tokens_estimate: 512, formula: "bytes / 4 (estimate)" },
  brain: { available: true, ready: true, schema_version: 1, note_count: 2, source_bytes: 512, link_count: 1, broken_link_count: 0, indexed_at: "2026-07-14T20:00:00Z", graph_truncated: false, nodes: [{ id: "bn_release", title: "Release gates", summary: "Verified release controls.", trust: "curated", degree: 1 }, { id: "bn_working", title: "Console hypothesis", summary: "Working note awaiting review.", trust: "working", degree: 1 }], edges: [{ source: "bn_release", target: "bn_working" }] },
  observability: { enabled: true, failures: 0, routes: [{ route: "mcp", requests: 10, client_4xx: 1, server_5xx: 0, p95_ms: 12 }] },
  security: { oauth_enabled: true, bearer_recovery: true, query_auth: "rejected", free_shell: "absent", cookie: "Secure; HttpOnly; SameSite=Strict", console_authority: "presentation-only" },
  edge: { state: "not_paired" },
};

const tasks = {
  schema_version: 1, available: true,
  tasks: [{ task_id: "0123456789abcdef0123456789abcdef", operation: "sandbox_status", summary: "MCP tool operation: sandbox_status", state: "completed", heartbeat: "2026-07-14T20:00:00Z", controller: "http" }],
};

class EventSourceStub {
  onerror: ((event: Event) => void) | null = null;
  addEventListener() { return undefined; }
  close() { return undefined; }
}

function response(value: unknown) {
  return { ok: true, status: 200, json: async () => value };
}

describe("Neo-BIOS operations firmware", () => {
  beforeEach(() => {
    vi.stubGlobal("EventSource", EventSourceStub);
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/console/status")) return Promise.resolve(response(runtime));
      if (path.endsWith("/console/data")) return Promise.resolve(response(data));
      if (path.endsWith("/console/tasks")) return Promise.resolve(response(tasks));
      return Promise.reject(new Error("unexpected fetch"));
    }));
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("renders real runtime and VPS data across all specified tabs", async () => {
    render(<App />);
    expect(screen.getByText("MCP DEVBOX OPERATIONS FIRMWARE")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Project" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Edge device" })).toBeInTheDocument();
    for (const tab of ["System", "Agents", "Tasks", "Brain", "Graph", "Edge", "Observability", "Security", "Events"]) {
      expect(screen.getByRole("tab", { name: tab })).toBeInTheDocument();
    }
    await waitFor(() => expect(screen.getByText("67 tools")).toBeInTheDocument());
    expect(screen.getByText("2.0 GiB / 4.0 GiB")).toBeInTheDocument();
    expect(screen.getByText(runtime.catalog_hash)).toBeInTheDocument();
  });

  it("shows durable tasks, aggregate Brain state and honest unpaired Edge state", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("67 tools")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("tab", { name: "Tasks" }));
    expect(screen.getByText("sandbox_status")).toBeInTheDocument();
    expect(screen.getAllByText("completed").length).toBeGreaterThanOrEqual(2);
    fireEvent.click(screen.getByRole("tab", { name: "Brain" }));
    expect(screen.getByText("2 opaque nodes · 1 edges")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Edge" }));
    expect(screen.getByText("Not paired")).toBeInTheDocument();
    expect(screen.queryByText("charles-parrot")).not.toBeInTheDocument();
  });

  it("renders the real opaque graph with zoom and pan controls", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("67 tools")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("tab", { name: "Graph" }));
    expect(screen.getByRole("img", { name: "Opaque Brain link graph" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Zoom in" })).toBeInTheDocument();
    expect(screen.getByText("Release gates")).toBeInTheDocument();
    const node = screen.getByRole("group", { name: /Release gates.*Verified release controls.*curated.*1 link/i });
    fireEvent.focus(node);
    expect(screen.getByRole("tooltip")).toHaveTextContent("Verified release controls.");
  });

  it("supports keyboard screen navigation and firmware help", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("67 tools")).toBeInTheDocument());
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("tab", { name: "Agents" })).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(window, { key: "F1" });
    expect(screen.getByRole("dialog", { name: "Help" })).toBeInTheDocument();
  });
});
