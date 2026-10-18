import { describe, expect, it } from "vitest";
import type { BrainEdge, BrainNode } from "./dataTypes";
import {
  computeNodePoints,
  maximumVisibleLabels,
  placeGraphLabels,
  rectsOverlap,
  resolveConsoleLabel,
  screenPoint,
  visualNodeRadius,
  zoomLevel,
  type GraphViewState,
  type GraphViewport,
} from "./graphLayout";

function node(index: number, overrides: Partial<BrainNode> = {}): BrainNode {
  return {
    id: `bn_${index.toString().padStart(4, "0")}`,
    console_label: `Node ${index}`,
    title: `Complete safe node title ${index}`,
    summary: `Curated safe summary ${index}.`,
    trust: index % 2 ? "working" : "curated",
    degree: (index % 7) + 1,
    ...overrides,
  };
}

function graph(count: number): { nodes: BrainNode[]; edges: BrainEdge[] } {
  const nodes = Array.from({ length: count }, (_, index) => node(index));
  const edges = nodes.slice(1).map((entry, index) => ({ source: nodes[index].id, target: entry.id }));
  return { nodes, edges };
}

function centeredView(scale: number, viewport: GraphViewport): GraphViewState {
  return { scale, x: viewport.width * (1 - scale) / 2, y: viewport.height * (1 - scale) / 2 };
}

function placements(count: number, viewport: GraphViewport, scale = 1, selectedId: string | null = null) {
  const fixture = graph(count);
  const points = computeNodePoints(fixture.nodes, viewport);
  const view = centeredView(scale, viewport);
  return {
    ...fixture,
    points,
    view,
    labels: placeGraphLabels({
      ...fixture,
      points,
      viewport,
      view,
      selectedId,
      activeId: null,
      controls: { x: viewport.width - 124, y: 8, width: 116, height: 36 },
      measure: (label) => Array.from(label).length * 7.2,
    }),
  };
}

function expectCollisionFree(result: ReturnType<typeof placements>, viewport: GraphViewport): void {
  result.labels.forEach((label, index) => {
    expect(label.x).toBeGreaterThanOrEqual(8);
    expect(label.y).toBeGreaterThanOrEqual(8);
    expect(label.x + label.width).toBeLessThanOrEqual(viewport.width - 8);
    expect(label.y + label.height).toBeLessThanOrEqual(viewport.height - 8);
    for (const other of result.labels.slice(index + 1)) {
      expect(rectsOverlap(label, other, 3)).toBe(false);
    }
    for (const item of result.nodes) {
      if (item.id === label.nodeId) continue;
      const point = screenPoint(result.points.get(item.id)!, result.view);
      const radius = visualNodeRadius(item.degree) * result.view.scale + 6;
      expect(rectsOverlap(label, { x: point.x - radius, y: point.y - radius, width: radius * 2, height: radius * 2 }, 3)).toBe(false);
    }
  });
}

describe("Brain graph deterministic layout", () => {
  it("keeps one node centered and selectable without a collapsed layout", () => {
    const viewport = { width: 360, height: 330 };
    const result = placements(1, viewport, 1, "bn_0000");
    expect(result.points.get("bn_0000")).toEqual({ x: 180, y: 165 });
    expect(result.labels.map((label) => label.nodeId)).toContain("bn_0000");
    expectCollisionFree(result, viewport);
  });

  it("separates four nearby nodes and places short labels without collisions", () => {
    const viewport = { width: 412, height: 380 };
    const result = placements(4, viewport, 1, "bn_0001");
    expect(new Set([...result.points.values()].map((point) => `${point.x}:${point.y}`))).toHaveLength(4);
    expectCollisionFree(result, viewport);
  });

  it.each([20, 100])("bounds visible labels for %i nodes by viewport area", (count) => {
    const viewport = { width: 768, height: 520 };
    const result = placements(count, viewport);
    expect(result.labels.length).toBeLessThanOrEqual(maximumVisibleLabels(viewport, 1));
    expect(result.labels.length).toBeGreaterThan(0);
    expectCollisionFree(result, viewport);
  });

  it("uses a whole-word fallback without ellipsis for a long title", () => {
    expect(resolveConsoleLabel(node(1, {
      console_label: "",
      title: "P11.2 Remote OpenCode relay production closure",
    }))).toBe("P11.2 Remote OpenCode relay");
    expect(resolveConsoleLabel(node(2, {
      console_label: "",
      title: "ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMN",
    }))).toBe("ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEF");
  });

  it("handles equal labels as distinct collision rectangles", () => {
    const viewport = { width: 500, height: 400 };
    const fixture = graph(20);
    fixture.nodes = fixture.nodes.map((entry) => ({ ...entry, console_label: "Same label" }));
    const points = computeNodePoints(fixture.nodes, viewport);
    const view = centeredView(1, viewport);
    const labels = placeGraphLabels({
      ...fixture,
      points,
      viewport,
      view,
      selectedId: fixture.nodes[0].id,
      activeId: fixture.nodes[1].id,
      controls: { x: 376, y: 8, width: 116, height: 36 },
      measure: (label) => label.length * 7.2,
    });
    const result = { ...fixture, points, view, labels };
    expect(new Set(labels.map((label) => label.nodeId)).size).toBe(labels.length);
    expectCollisionFree(result, viewport);
  });

  it("keeps the selected label clear of direct neighbors", () => {
    const viewport = { width: 360, height: 360 };
    const result = placements(20, viewport, 1, "bn_0007");
    const selected = result.labels.find((label) => label.nodeId === "bn_0007");
    expect(selected).toBeDefined();
    expectCollisionFree(result, viewport);
  });

  it("returns identical points and labels for the same inputs", () => {
    const viewport = { width: 1366, height: 540 };
    const first = placements(100, viewport, 1.25, "bn_0010");
    const second = placements(100, viewport, 1.25, "bn_0010");
    expect([...first.points]).toEqual([...second.points]);
    expect(first.labels).toEqual(second.labels);
  });

  it("changes label density across far, medium and near zoom", () => {
    const viewport = { width: 1366, height: 540 };
    const far = placements(100, viewport, 0.65, "bn_0000");
    const medium = placements(100, viewport, 1, "bn_0000");
    const near = placements(100, viewport, 1.8, "bn_0000");
    expect(zoomLevel(far.view.scale)).toBe("far");
    expect(zoomLevel(medium.view.scale)).toBe("medium");
    expect(zoomLevel(near.view.scale)).toBe("near");
    expect(far.labels.length).toBeLessThanOrEqual(medium.labels.length);
    expect(medium.labels.length).toBeLessThanOrEqual(near.labels.length);
    expectCollisionFree(far, viewport);
    expectCollisionFree(medium, viewport);
    expectCollisionFree(near, viewport);
  });
});
