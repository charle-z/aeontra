import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const runtime = {
  status: "ok",
  version: "0.9.0",
  protocol_version: "2025-06-18",
  commit: "4fbe1dda02351c632e67c0f10a5c5b314df745e2",
  tool_count: 67,
  catalog_hash: "sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed",
  authenticated: true,
  surface: "presentation-only",
};

describe("Neo-BIOS console shell", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => runtime,
    }));
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("renders the real post-P9 runtime identity and all specified tabs", async () => {
    render(<App />);
    expect(screen.getByText("MCP DEVBOX OPERATIONS FIRMWARE")).toBeInTheDocument();
    for (const tab of ["System", "Agents", "Tasks", "Brain", "Graph", "Edge", "Observability", "Security", "Events"]) {
      expect(screen.getByRole("tab", { name: tab })).toBeInTheDocument();
    }
    await waitFor(() => expect(screen.getByText("67 tools")).toBeInTheDocument());
    expect(screen.getByText(runtime.commit)).toBeInTheDocument();
    expect(screen.getByText(runtime.catalog_hash)).toBeInTheDocument();
  });

  it("does not invent edge devices, task state, brain metrics, or VPS resources", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("67 tools")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("tab", { name: "Edge" }));
    expect(screen.getByText("Not paired")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Tasks" }));
    expect(screen.getByText("Unavailable — safe endpoint not implemented")).toBeInTheDocument();
    expect(screen.getByText("Idle / unknown")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Brain" }));
    expect(screen.queryByText(/notes/i)).not.toBeInTheDocument();
  });

  it("supports keyboard screen navigation and the help firmware key", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByText("67 tools")).toBeInTheDocument());
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("tab", { name: "Agents" })).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(window, { key: "F1" });
    expect(screen.getByRole("dialog", { name: "Help" })).toBeInTheDocument();
  });
});
