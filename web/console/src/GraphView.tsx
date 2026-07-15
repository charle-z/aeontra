import { useMemo, useRef, useState } from "react";
import type { BrainData, BrainNode } from "./dataTypes";

type View = { x: number; y: number; scale: number };
type Point = { x: number; y: number };

type Props = { brain: BrainData | null };

export default function GraphView({ brain }: Props) {
  const svgRef = useRef<SVGSVGElement>(null);
  const dragRef = useRef<{ pointerX: number; pointerY: number; viewX: number; viewY: number } | null>(null);
  const [view, setView] = useState<View>({ x: 0, y: 0, scale: 1 });
  const [hovered, setHovered] = useState<BrainNode | null>(null);
  const points = useMemo(() => {
    const nodes = brain?.nodes ?? [];
    const centerX = 480;
    const centerY = 210;
    const radius = Math.min(165, 55 + nodes.length * 2);
    const result = new Map<string, Point>();
    nodes.forEach((node, index) => {
      const angle = (Math.PI * 2 * index) / Math.max(nodes.length, 1) - Math.PI / 2;
      result.set(node.id, { x: centerX + Math.cos(angle) * radius, y: centerY + Math.sin(angle) * radius });
    });
    return result;
  }, [brain]);

  if (!brain?.available) {
    return <div className="graph-empty"><p>Brain unavailable</p><small>No graph data was fabricated.</small></div>;
  }
  if (!brain.nodes.length) {
    return <div className="graph-empty"><p>Brain graph is empty</p><small>The index is real but currently contains no visible opaque nodes.</small></div>;
  }

  const zoom = (factor: number) => setView((current) => ({ ...current, scale: Math.max(0.5, Math.min(4, current.scale * factor)) }));
  const reset = () => setView({ x: 0, y: 0, scale: 1 });

  return (
    <div className="graph-wrap">
      <div className="graph-controls" aria-label="Graph controls">
        <button type="button" onClick={() => zoom(1.25)} aria-label="Zoom in">+</button>
        <button type="button" onClick={() => zoom(0.8)} aria-label="Zoom out">−</button>
        <button type="button" onClick={reset} aria-label="Reset graph">↻</button>
      </div>
      <svg
        ref={svgRef}
        className="brain-graph"
        viewBox="0 0 960 420"
        role="img"
        aria-label="Opaque Brain link graph"
        onWheel={(event) => {
          event.preventDefault();
          zoom(event.deltaY < 0 ? 1.12 : 0.89);
        }}
        onPointerDown={(event) => {
          dragRef.current = { pointerX: event.clientX, pointerY: event.clientY, viewX: view.x, viewY: view.y };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          const drag = dragRef.current;
          if (!drag) return;
          setView((current) => ({ ...current, x: drag.viewX + event.clientX - drag.pointerX, y: drag.viewY + event.clientY - drag.pointerY }));
        }}
        onPointerUp={(event) => {
          dragRef.current = null;
          event.currentTarget.releasePointerCapture(event.pointerId);
        }}
        onPointerCancel={() => { dragRef.current = null; }}
      >
        <g transform={"translate(" + view.x + " " + view.y + ") scale(" + view.scale + ")"}>
          {brain.edges.map((edge, index) => {
            const source = points.get(edge.source);
            const target = points.get(edge.target);
            if (!source || !target) return null;
            return <line key={edge.source + "-" + edge.target + "-" + index} x1={source.x} y1={source.y} x2={target.x} y2={target.y} />;
          })}
          {brain.nodes.map((node) => {
            const point = points.get(node.id);
            if (!point) return null;
            return (
              <g key={node.id} className="graph-node" data-trust={node.trust} transform={"translate(" + point.x + " " + point.y + ")"} onPointerEnter={() => setHovered(node)} onPointerLeave={() => setHovered(null)}>
                <circle r={Math.min(18, 6 + node.degree * 1.4)} />
                <text y="28" textAnchor="middle">{node.id}</text>
              </g>
            );
          })}
        </g>
      </svg>
      {hovered && <div className="graph-tooltip"><b>{hovered.id}</b><span>trust: {hovered.trust}</span><span>links: {hovered.degree}</span></div>}
      <p className="graph-note">Opaque IDs only · yellow curated · white working · wheel zoom · drag pan{brain.graph_truncated ? " · graph bounded" : ""}</p>
    </div>
  );
}
