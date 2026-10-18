import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import GraphView from "./GraphView";
import type { BrainData, BrainNode } from "./dataTypes";

function graphNode(index: number, overrides: Partial<BrainNode> = {}): BrainNode {
  return {
    id: `bn_${index}`,
    console_label: `Node ${index}`,
    title: `Complete safe node title ${index}`,
    summary: `Curated safe summary ${index}.`,
    trust: index % 2 ? "working" : "curated",
    degree: index + 1,
    ...overrides,
  };
}

function brain(nodes: BrainNode[] = [graphNode(0), graphNode(1)]): BrainData {
  return {
    available: true,
    ready: true,
    schema_version: 1,
    note_count: nodes.length,
    source_bytes: 100,
    link_count: Math.max(0, nodes.length - 1),
    broken_link_count: 0,
    indexed_at: "2026-07-17T19:32:10Z",
    graph_truncated: false,
    nodes,
    edges: nodes.slice(1).map((node, index) => ({ source: nodes[index].id, target: node.id })),
  };
}

let graphRect = { width: 960, height: 460 };
let resizeCallback: ResizeObserverCallback | null = null;

class ResizeObserverStub {
  constructor(callback: ResizeObserverCallback) { resizeCallback = callback; }
  observe() {}
  unobserve() {}
  disconnect() {}
}

describe("Brain GraphView", () => {
  beforeEach(() => {
    graphRect = { width: 960, height: 460 };
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if ((this as HTMLElement).classList?.contains("graph-canvas")) {
        return {
          x: 0, y: 0, top: 0, left: 0, right: graphRect.width, bottom: graphRect.height,
          width: graphRect.width, height: graphRect.height, toJSON: () => ({}),
        };
      }
      return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, toJSON: () => ({}) };
    });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    resizeCallback = null;
  });

  it("selects with a small node halo and keeps full content in the detail panel", async () => {
    const selected = graphNode(0, {
      console_label: "P11.2 Relay",
      title: "P11.2 Remote OpenCode relay production closure",
      summary: "Production closure validated without exposing private note content.",
      degree: 4,
    });
    render(<GraphView brain={brain([selected, graphNode(1), graphNode(2), graphNode(3)])} />);
    const node = screen.getByRole("button", { name: /P11\.2 Remote OpenCode relay production closure/ });
    const transformBefore = node.getAttribute("transform");
    fireEvent.click(node);
    expect(node).toHaveAttribute("aria-pressed", "true");
    expect(node.querySelector("circle.graph-node-halo")).toBeInTheDocument();
    expect(node.querySelector("rect")).not.toBeInTheDocument();
    expect(node.getAttribute("transform")).toBe(transformBefore);

    const detail = screen.getByRole("status");
    expect(within(detail).getByText("P11.2 Relay")).toBeInTheDocument();
    expect(within(detail).getByText(selected.title)).toBeInTheDocument();
    expect(within(detail).getByText(selected.summary)).toBeInTheDocument();
    expect(within(detail).getByText("selected")).toBeInTheDocument();
    expect([...document.querySelectorAll(".graph-label text")].map((label) => label.textContent)).not.toContain(selected.title);

    fireEvent.click(node);
    await waitFor(() => expect(detail).toHaveFocus());
  });

  it("supports keyboard selection, Space and Escape", () => {
    render(<GraphView brain={brain()} />);
    const node = screen.getByRole("button", { name: /Complete safe node title 0/ });
    node.focus();
    fireEvent.keyDown(node, { key: "Enter" });
    expect(node).toHaveAttribute("aria-pressed", "true");
    fireEvent.keyDown(node, { key: "Escape" });
    expect(node).toHaveAttribute("aria-pressed", "false");
    fireEvent.keyDown(node, { key: " " });
    expect(node).toHaveAttribute("aria-pressed", "true");
  });

  it("uses hover and focus as temporary preview without selecting", () => {
    render(<GraphView brain={brain()} />);
    const node = screen.getByRole("button", { name: /Complete safe node title 1/ });
    fireEvent.pointerEnter(node);
    expect(within(screen.getByRole("status")).getByText("preview")).toBeInTheDocument();
    expect(node).toHaveAttribute("aria-pressed", "false");
    fireEvent.pointerLeave(node);
    fireEvent.focus(node);
    expect(within(screen.getByRole("status")).getByText("Complete safe node title 1")).toBeInTheDocument();
  });

  it("updates density on zoom without resetting selection", () => {
    const nodes = Array.from({ length: 20 }, (_, index) => graphNode(index));
    render(<GraphView brain={brain(nodes)} />);
    const node = screen.getByRole("button", { name: /Complete safe node title 0/ });
    fireEvent.click(node);
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(document.querySelector(".graph-canvas")).toHaveAttribute("data-zoom-level", "far");
    const farLabels = document.querySelectorAll('[data-graph-label="true"]').length;
    fireEvent.click(screen.getByRole("button", { name: "Reset graph" }));
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(node).toHaveAttribute("aria-pressed", "true");
    expect(document.querySelector(".graph-canvas")).toHaveAttribute("data-zoom-level", "near");
    expect(document.querySelectorAll('[data-graph-label="true"]').length).toBeGreaterThanOrEqual(farLabels);
  });

  it("responds deterministically to resize and orientation changes", async () => {
    render(<GraphView brain={brain(Array.from({ length: 20 }, (_, index) => graphNode(index)))} />);
    const first = [...document.querySelectorAll('[data-graph-node="true"]')].map((node) => node.getAttribute("transform"));
    graphRect = { width: 412, height: 520 };
    resizeCallback?.([], {} as ResizeObserver);
    await waitFor(() => expect(document.querySelector(".brain-graph")?.getAttribute("viewBox")).toBe("0 0 412 520"));
    const mobile = [...document.querySelectorAll('[data-graph-node="true"]')].map((node) => node.getAttribute("transform"));
    expect(mobile).not.toEqual(first);
    resizeCallback?.([], {} as ResizeObserver);
    expect([...document.querySelectorAll('[data-graph-node="true"]')].map((node) => node.getAttribute("transform"))).toEqual(mobile);
  });

  it("supports tap selection and exposes a 40px-equivalent hit target", () => {
    render(<GraphView brain={brain()} />);
    const node = screen.getByRole("button", { name: /Complete safe node title 0/ });
    const hit = node.querySelector("circle.graph-node-hit");
    expect(Number(hit?.getAttribute("r"))).toBeGreaterThanOrEqual(20);
    fireEvent.click(node, { pointerType: "touch" });
    expect(node).toHaveAttribute("aria-pressed", "true");
  });

  it("renders empty and truncated states without fabricating data", () => {
    const { rerender } = render(<GraphView brain={{ ...brain([]), nodes: [], edges: [] }} />);
    expect(screen.getByText("Brain graph is empty")).toBeInTheDocument();
    rerender(<GraphView brain={{ ...brain(), graph_truncated: true }} />);
    expect(screen.getByText(/graph bounded/)).toBeInTheDocument();
  });

  it("never renders slug, body, path or provenance fields supplied outside the contract", () => {
    const privateNode = {
      ...graphNode(0),
      slug: "private-slug",
      body: "private body must stay hidden",
      path: "/private/path",
      provenance: "private provenance",
    } as BrainNode;
    render(<GraphView brain={brain([privateNode])} />);
    expect(document.body).not.toHaveTextContent("private-slug");
    expect(document.body).not.toHaveTextContent("private body must stay hidden");
    expect(document.body).not.toHaveTextContent("/private/path");
    expect(document.body).not.toHaveTextContent("private provenance");
  });
});
