import type { BrainEdge, BrainNode } from "./dataTypes";

export type GraphPoint = { x: number; y: number };
export type GraphViewport = { width: number; height: number };
export type GraphViewState = { x: number; y: number; scale: number };
export type GraphRect = { x: number; y: number; width: number; height: number };
export type GraphZoomLevel = "far" | "medium" | "near";
export type LabelPlacement = GraphRect & {
  nodeId: string;
  label: string;
  textX: number;
  textY: number;
  priority: number;
};

type LabelOptions = {
  nodes: BrainNode[];
  edges: BrainEdge[];
  points: Map<string, GraphPoint>;
  viewport: GraphViewport;
  view: GraphViewState;
  selectedId: string | null;
  activeId: string | null;
  controls: GraphRect;
  measure: (label: string) => number;
};

const goldenAngle = Math.PI * (3 - Math.sqrt(5));
const labelHeight = 20;
const viewportMargin = 8;
const collisionGap = 4;

export function resolveConsoleLabel(node: Pick<BrainNode, "console_label" | "title">): string {
  const explicit = node.console_label.trim();
  if (explicit) return Array.from(explicit).slice(0, 32).join("");
  const words = node.title.trim().split(/\s+/u).filter(Boolean);
  if (!words.length) return "Brain note";
  const selected: string[] = [];
  for (const word of words) {
    const candidate = [...selected, word].join(" ");
    if (Array.from(candidate).length > 32) break;
    selected.push(word);
  }
  if (selected.length) return selected.join(" ");
  return Array.from(words[0]).slice(0, 32).join("");
}

export function visualNodeRadius(degree: number): number {
  return Math.max(7, Math.min(14, 7 + Math.sqrt(Math.max(0, degree)) * 1.8));
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.max(minimum, Math.min(maximum, value));
}

function stableDirection(left: string, right: string): GraphPoint {
  let value = 2166136261;
  for (const point of left + ":" + right) {
    value ^= point.codePointAt(0) ?? 0;
    value = Math.imul(value, 16777619);
  }
  const angle = ((value >>> 0) / 0xffffffff) * Math.PI * 2;
  return { x: Math.cos(angle), y: Math.sin(angle) };
}

export function computeNodePoints(nodes: BrainNode[], viewport: GraphViewport): Map<string, GraphPoint> {
  const sorted = [...nodes].sort((left, right) => left.id.localeCompare(right.id));
  const width = Math.max(280, viewport.width);
  const height = Math.max(260, viewport.height);
  const center = { x: width / 2, y: height / 2 };
  const marginX = Math.min(70, Math.max(42, width * 0.1));
  const marginY = Math.min(64, Math.max(42, height * 0.12));
  const result = new Map<string, GraphPoint>();

  if (sorted.length === 1) {
    result.set(sorted[0].id, center);
    return result;
  }
  if (sorted.length === 2) {
    result.set(sorted[0].id, { x: width * 0.34, y: center.y });
    result.set(sorted[1].id, { x: width * 0.66, y: center.y });
    return result;
  }
  if (sorted.length === 3) {
    result.set(sorted[0].id, { x: center.x, y: height * 0.27 });
    result.set(sorted[1].id, { x: width * 0.31, y: height * 0.69 });
    result.set(sorted[2].id, { x: width * 0.69, y: height * 0.69 });
    return result;
  }
  if (sorted.length === 4) {
    const positions = [
      { x: width * 0.31, y: height * 0.31 },
      { x: width * 0.69, y: height * 0.31 },
      { x: width * 0.31, y: height * 0.69 },
      { x: width * 0.69, y: height * 0.69 },
    ];
    sorted.forEach((node, index) => result.set(node.id, positions[index]));
    return result;
  }

  const radiusX = Math.max(70, width / 2 - marginX);
  const radiusY = Math.max(70, height / 2 - marginY);
  sorted.forEach((node, index) => {
    const normalized = Math.sqrt((index + 0.65) / sorted.length);
    const angle = index * goldenAngle - Math.PI / 2;
    result.set(node.id, {
      x: center.x + Math.cos(angle) * radiusX * normalized,
      y: center.y + Math.sin(angle) * radiusY * normalized,
    });
  });

  const areaPerNode = (width * height) / sorted.length;
  const minimumDistance = clamp(Math.sqrt(areaPerNode) * 0.42, 18, 54);
  for (let iteration = 0; iteration < 8; iteration += 1) {
    for (let leftIndex = 0; leftIndex < sorted.length; leftIndex += 1) {
      for (let rightIndex = leftIndex + 1; rightIndex < sorted.length; rightIndex += 1) {
        const left = result.get(sorted[leftIndex].id)!;
        const right = result.get(sorted[rightIndex].id)!;
        const deltaX = right.x - left.x;
        const deltaY = right.y - left.y;
        const distance = Math.hypot(deltaX, deltaY);
        if (distance >= minimumDistance) continue;
        const direction = distance > 0.001
          ? { x: deltaX / distance, y: deltaY / distance }
          : stableDirection(sorted[leftIndex].id, sorted[rightIndex].id);
        const push = (minimumDistance - distance) / 2;
        left.x -= direction.x * push;
        left.y -= direction.y * push;
        right.x += direction.x * push;
        right.y += direction.y * push;
      }
    }
    for (const point of result.values()) {
      point.x = clamp(point.x, marginX, width - marginX);
      point.y = clamp(point.y, marginY, height - marginY);
    }
  }
  return result;
}

export function screenPoint(point: GraphPoint, view: GraphViewState): GraphPoint {
  return { x: view.x + point.x * view.scale, y: view.y + point.y * view.scale };
}

export function zoomLevel(scale: number): GraphZoomLevel {
  if (scale < 0.85) return "far";
  if (scale < 1.6) return "medium";
  return "near";
}

export function maximumVisibleLabels(viewport: GraphViewport, scale: number): number {
  const areaSlots = Math.max(1, Math.floor((viewport.width * viewport.height) / 22_000));
  switch (zoomLevel(scale)) {
    case "far": return clamp(areaSlots, 3, 8);
    case "medium": return clamp(areaSlots, 6, 24);
    case "near": return clamp(areaSlots * 2, 10, 48);
  }
}

export function rectsOverlap(left: GraphRect, right: GraphRect, padding = 0): boolean {
  return left.x < right.x + right.width + padding
    && left.x + left.width + padding > right.x
    && left.y < right.y + right.height + padding
    && left.y + left.height + padding > right.y;
}

function insideViewport(rect: GraphRect, viewport: GraphViewport): boolean {
  return rect.x >= viewportMargin
    && rect.y >= viewportMargin
    && rect.x + rect.width <= viewport.width - viewportMargin
    && rect.y + rect.height <= viewport.height - viewportMargin;
}

function candidateRects(point: GraphPoint, radius: number, width: number, expanded: boolean): GraphRect[] {
  const gap = expanded ? 16 : 10;
  const distance = radius + gap;
  const diagonal = radius + (expanded ? 22 : 15);
  return [
    { x: point.x - width / 2, y: point.y - distance - labelHeight, width, height: labelHeight },
    { x: point.x - width / 2, y: point.y + distance, width, height: labelHeight },
    { x: point.x - distance - width, y: point.y - labelHeight / 2, width, height: labelHeight },
    { x: point.x + distance, y: point.y - labelHeight / 2, width, height: labelHeight },
    { x: point.x + diagonal, y: point.y - diagonal - labelHeight, width, height: labelHeight },
    { x: point.x - diagonal - width, y: point.y - diagonal - labelHeight, width, height: labelHeight },
    { x: point.x + diagonal, y: point.y + diagonal, width, height: labelHeight },
    { x: point.x - diagonal - width, y: point.y + diagonal, width, height: labelHeight },
  ];
}

export function placeGraphLabels(options: LabelOptions): LabelPlacement[] {
  const neighbors = new Set<string>();
  if (options.selectedId) {
    for (const edge of options.edges) {
      if (edge.source === options.selectedId) neighbors.add(edge.target);
      if (edge.target === options.selectedId) neighbors.add(edge.source);
    }
  }
  const priority = (node: BrainNode): number => {
    if (node.id === options.selectedId) return 0;
    if (node.id === options.activeId) return 1;
    if (neighbors.has(node.id)) return 2;
    return 3;
  };
  const sorted = [...options.nodes].sort((left, right) => {
    const priorityDelta = priority(left) - priority(right);
    if (priorityDelta !== 0) return priorityDelta;
    const degreeDelta = right.degree - left.degree;
    return degreeDelta !== 0 ? degreeDelta : left.id.localeCompare(right.id);
  });
  const nodeRects = new Map<string, GraphRect>();
  for (const node of options.nodes) {
    const point = options.points.get(node.id);
    if (!point) continue;
    const screen = screenPoint(point, options.view);
    const radius = visualNodeRadius(node.degree) * options.view.scale + 6;
    nodeRects.set(node.id, { x: screen.x - radius, y: screen.y - radius, width: radius * 2, height: radius * 2 });
  }

  const placements: LabelPlacement[] = [];
  const occupied: GraphRect[] = [];
  const maximum = Math.min(options.nodes.length, maximumVisibleLabels(options.viewport, options.view.scale));
  for (const node of sorted) {
    if (placements.length >= maximum && node.id !== options.selectedId && node.id !== options.activeId) break;
    const basePoint = options.points.get(node.id);
    const ownRect = nodeRects.get(node.id);
    if (!basePoint || !ownRect) continue;
    const point = screenPoint(basePoint, options.view);
    const label = resolveConsoleLabel(node);
    const width = clamp(Math.ceil(options.measure(label)) + 12, 48, 250);
    const radius = ownRect.width / 2;
    const candidates = candidateRects(point, radius, width, node.id === options.selectedId || node.id === options.activeId);
    const valid = candidates.find((candidate) => {
      if (!insideViewport(candidate, options.viewport)) return false;
      if (rectsOverlap(candidate, options.controls, collisionGap)) return false;
      if (occupied.some((rect) => rectsOverlap(candidate, rect, collisionGap))) return false;
      for (const [otherID, rect] of nodeRects) {
        if (otherID !== node.id && rectsOverlap(candidate, rect, collisionGap)) return false;
      }
      return true;
    });
    if (!valid) continue;
    const placement: LabelPlacement = {
      ...valid,
      nodeId: node.id,
      label,
      textX: valid.x + 6,
      textY: valid.y + 14,
      priority: priority(node),
    };
    placements.push(placement);
    occupied.push(valid);
  }
  return placements;
}
