import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { BrainData, BrainNode } from "./dataTypes";
import {
  computeNodePoints,
  placeGraphLabels,
  resolveConsoleLabel,
  visualNodeRadius,
  zoomLevel,
  type GraphRect,
  type GraphViewState,
  type GraphViewport,
} from "./graphLayout";

type Props = { brain: BrainData | null };

type DragState = {
  pointerId: number;
  pointerX: number;
  pointerY: number;
  viewX: number;
  viewY: number;
};

const defaultViewport: GraphViewport = { width: 960, height: 460 };
const defaultView: GraphViewState = { x: 0, y: 0, scale: 1 };

function accessibleNodeLabel(node: BrainNode, selected: boolean): string {
  const links = node.degree === 1 ? "1 connection" : node.degree + " connections";
  return node.title + ". Trust " + node.trust + ". " + links + ". " + (selected ? "Selected." : "Not selected.");
}

function edgeKey(source: string, target: string): string {
  return source < target ? source + ":" + target : target + ":" + source;
}

export default function GraphView({ brain }: Props) {
  const canvasRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const detailRef = useRef<HTMLElement>(null);
  const dragRef = useRef<DragState | null>(null);
  const [viewport, setViewport] = useState<GraphViewport>(defaultViewport);
  const [view, setView] = useState<GraphViewState>(defaultView);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const [focusedId, setFocusedId] = useState<string | null>(null);

  const nodes = brain?.nodes ?? [];
  const edges = brain?.edges ?? [];
  const nodeByID = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);
  const points = useMemo(() => computeNodePoints(nodes, viewport), [nodes, viewport]);
  const activeId = focusedId ?? hoveredId;
  const selectedNode = selectedId ? nodeByID.get(selectedId) ?? null : null;
  const activeNode = activeId ? nodeByID.get(activeId) ?? null : null;
  const detailNode = selectedNode ?? activeNode;

  useEffect(() => {
    if (selectedId && !nodeByID.has(selectedId)) setSelectedId(null);
    if (hoveredId && !nodeByID.has(hoveredId)) setHoveredId(null);
    if (focusedId && !nodeByID.has(focusedId)) setFocusedId(null);
  }, [focusedId, hoveredId, nodeByID, selectedId]);

  useLayoutEffect(() => {
    const target = canvasRef.current;
    if (!target) return;
    const update = () => {
      const rect = target.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0) {
        setViewport({ width: Math.round(rect.width), height: Math.round(rect.height) });
      }
    };
    update();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(update);
    observer.observe(target);
    return () => observer.disconnect();
  }, []);

  const measure = useMemo(() => {
    let context: CanvasRenderingContext2D | null = null;
    return (label: string): number => {
      if (typeof navigator !== "undefined" && navigator.userAgent.toLowerCase().includes("jsdom")) {
        return Array.from(label).length * 7.2;
      }
      if (!context && typeof document !== "undefined") {
        try {
          const canvas = document.createElement("canvas");
          context = canvas.getContext("2d");
          if (context) context.font = "12px Cascadia Mono, IBM Plex Mono, Consolas, Courier New, monospace";
        } catch {
          context = null;
        }
      }
      return context?.measureText(label).width ?? Array.from(label).length * 7.2;
    };
  }, []);

  const controls: GraphRect = useMemo(() => ({
    x: Math.max(8, viewport.width - 124),
    y: 8,
    width: 116,
    height: 36,
  }), [viewport.width]);

  const labels = useMemo(() => placeGraphLabels({
    nodes,
    edges,
    points,
    viewport,
    view,
    selectedId,
    activeId,
    controls,
    measure,
  }), [activeId, controls, edges, measure, nodes, points, selectedId, view, viewport]);

  const neighbors = useMemo(() => {
    const result = new Set<string>();
    if (!selectedId) return result;
    for (const edge of edges) {
      if (edge.source === selectedId) result.add(edge.target);
      if (edge.target === selectedId) result.add(edge.source);
    }
    return result;
  }, [edges, selectedId]);

  const highlightedEdges = useMemo(() => {
    const result = new Set<string>();
    if (!selectedId) return result;
    for (const edge of edges) {
      if (edge.source === selectedId || edge.target === selectedId) result.add(edgeKey(edge.source, edge.target));
    }
    return result;
  }, [edges, selectedId]);

  const zoom = useCallback((factor: number) => {
    setView((current) => {
      const scale = Math.max(0.5, Math.min(3.5, current.scale * factor));
      const ratio = scale / current.scale;
      const centerX = viewport.width / 2;
      const centerY = viewport.height / 2;
      return {
        scale,
        x: centerX - (centerX - current.x) * ratio,
        y: centerY - (centerY - current.y) * ratio,
      };
    });
  }, [viewport]);

  const reset = useCallback(() => setView(defaultView), []);

  const selectNode = useCallback((nodeID: string) => {
    setSelectedId((current) => {
      if (current === nodeID) {
        window.setTimeout(() => detailRef.current?.focus(), 0);
        return current;
      }
      return nodeID;
    });
  }, []);

  if (!brain?.available) return <div className="graph-empty"><p>Brain unavailable</p><small>No graph data was fabricated.</small></div>;
  if (!nodes.length) return <div className="graph-empty"><p>Brain graph is empty</p><small>The index is real but currently contains no visible safe nodes.</small></div>;

  return (
    <div className="graph-wrap" onKeyDown={(event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setSelectedId(null);
      }
    }}>
      <div className="graph-canvas" ref={canvasRef} data-zoom-level={zoomLevel(view.scale)}>
        <div className="graph-controls" role="group" aria-label="Graph controls">
          <button type="button" onClick={() => zoom(1.25)} aria-label="Zoom in">+</button>
          <button type="button" onClick={() => zoom(0.8)} aria-label="Zoom out">−</button>
          <button type="button" onClick={reset} aria-label="Reset graph">↻</button>
        </div>
        <svg
          ref={svgRef}
          className="brain-graph"
          viewBox={`0 0 ${viewport.width} ${viewport.height}`}
          preserveAspectRatio="none"
          role="img"
          aria-label="Opaque Brain relationship graph"
          onWheel={(event) => { event.preventDefault(); zoom(event.deltaY < 0 ? 1.12 : 0.89); }}
          onPointerDown={(event) => {
            if ((event.target as Element).closest('[data-graph-node="true"]')) return;
            dragRef.current = {
              pointerId: event.pointerId,
              pointerX: event.clientX,
              pointerY: event.clientY,
              viewX: view.x,
              viewY: view.y,
            };
            event.currentTarget.setPointerCapture(event.pointerId);
          }}
          onPointerMove={(event) => {
            const drag = dragRef.current;
            if (!drag || drag.pointerId !== event.pointerId) return;
            setView((current) => ({
              ...current,
              x: drag.viewX + event.clientX - drag.pointerX,
              y: drag.viewY + event.clientY - drag.pointerY,
            }));
          }}
          onPointerUp={(event) => {
            if (dragRef.current?.pointerId === event.pointerId) dragRef.current = null;
            if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
          }}
          onPointerCancel={() => { dragRef.current = null; }}
        >
          <rect className="graph-pan-surface" x="0" y="0" width={viewport.width} height={viewport.height} />
          <g className="graph-world" transform={`translate(${view.x} ${view.y}) scale(${view.scale})`}>
            {edges.map((edge, index) => {
              const source = points.get(edge.source);
              const target = points.get(edge.target);
              if (!source || !target) return null;
              const related = highlightedEdges.has(edgeKey(edge.source, edge.target));
              return <line
                key={edge.source + "-" + edge.target + "-" + index}
                className={selectedId ? (related ? "graph-edge-related" : "graph-edge-muted") : ""}
                x1={source.x}
                y1={source.y}
                x2={target.x}
                y2={target.y}
                data-related={related ? "true" : "false"}
              />;
            })}
            {nodes.map((node) => {
              const point = points.get(node.id);
              if (!point) return null;
              const selected = node.id === selectedId;
              const active = node.id === activeId;
              const related = !selectedId || selected || neighbors.has(node.id);
              const radius = visualNodeRadius(node.degree);
              return (
                <g
                  key={node.id}
                  className="graph-node"
                  data-graph-node="true"
                  data-node-id={node.id}
                  data-trust={node.trust}
                  data-selected={selected ? "true" : "false"}
                  data-active={active ? "true" : "false"}
                  data-related={related ? "true" : "false"}
                  transform={`translate(${point.x} ${point.y})`}
                  role="button"
                  tabIndex={0}
                  aria-label={accessibleNodeLabel(node, selected)}
                  aria-pressed={selected}
                  onPointerEnter={() => setHoveredId(node.id)}
                  onPointerLeave={() => setHoveredId((current) => current === node.id ? null : current)}
                  onFocus={() => setFocusedId(node.id)}
                  onBlur={() => setFocusedId((current) => current === node.id ? null : current)}
                  onClick={() => selectNode(node.id)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      selectNode(node.id);
                    } else if (event.key === "Escape") {
                      event.preventDefault();
                      setSelectedId(null);
                    }
                  }}
                >
                  <title>{accessibleNodeLabel(node, selected)}</title>
                  <circle className="graph-node-hit" r={20 / view.scale} />
                  {selected && <circle className="graph-node-halo" r={radius + 5 / view.scale} />}
                  <circle className="graph-node-focus" r={radius + 8 / view.scale} />
                  <circle className="graph-node-visual" r={radius} />
                </g>
              );
            })}
          </g>
          <g className="graph-label-layer" aria-hidden="true">
            {labels.map((label) => <g
              key={label.nodeId}
              className="graph-label"
              data-graph-label="true"
              data-node-id={label.nodeId}
              data-priority={label.priority}
            >
              <rect x={label.x} y={label.y} width={label.width} height={label.height} />
              <text x={label.textX} y={label.textY}>{label.label}</text>
            </g>)}
          </g>
        </svg>
      </div>
      <section
        ref={detailRef}
        id="graph-detail-panel"
        className="graph-detail"
        role="status"
        aria-live="polite"
        tabIndex={-1}
      >
        <h3>Node detail</h3>
        {detailNode ? <dl>
          <div><dt>Console label</dt><dd>{resolveConsoleLabel(detailNode)}</dd></div>
          <div><dt>Full title</dt><dd>{detailNode.title}</dd></div>
          <div><dt>Summary</dt><dd>{detailNode.summary.trim() || "No curated summary available."}</dd></div>
          <div><dt>Trust</dt><dd>{detailNode.trust}</dd></div>
          <div><dt>Links</dt><dd>{detailNode.degree}</dd></div>
          <div><dt>Selection</dt><dd>{selectedNode?.id === detailNode.id ? "selected" : "preview"}</dd></div>
        </dl> : <p>Select a node to inspect its complete safe metadata.</p>}
      </section>
      <p className="graph-note">Stable opaque IDs · short collision-aware labels · yellow curated · white working · wheel zoom · drag pan{brain.graph_truncated ? " · graph bounded" : ""}</p>
    </div>
  );
}
