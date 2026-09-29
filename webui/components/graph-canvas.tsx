'use client';

import { useEffect, useRef } from 'react';
import cytoscape, { type Core, type ElementDefinition } from 'cytoscape';
import { STATUS_COLOR, shortOnion } from '@/lib/format';

export interface GNode { id: number; onion: string; title: string; status: string; indeg: number; outdeg: number; }
export interface GEdge { s: number; t: number; w: number; }

export function GraphCanvas({
  nodes, edges, center, height = 560, onSelect, labelTop = 40,
}: {
  nodes: GNode[]; edges: GEdge[]; center?: number; height?: number;
  onSelect?: (n: GNode) => void; labelTop?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const cyRef = useRef<Core | null>(null);

  useEffect(() => {
    if (!ref.current) return;
    const present = new Set(nodes.map((n) => n.id));
    const good = edges.filter((e) => present.has(e.s) && present.has(e.t));
    const linked = new Set<number>();
    good.forEach((e) => { linked.add(e.s); linked.add(e.t); });
    const shown = nodes.filter((n) => linked.has(n.id) || n.id === center);
    const maxIn = Math.max(1, ...shown.map((n) => n.indeg));
    const label = new Set([...shown].sort((a, b) => b.indeg - a.indeg).slice(0, labelTop).map((n) => n.id));
    if (center) label.add(center);

    const elements: ElementDefinition[] = [
      ...shown.map((n) => ({
        data: {
          id: String(n.id), n,
          color: STATUS_COLOR[n.status] ?? '#8a7f92',
          size: 8 + (n.indeg / maxIn) * (center ? 22 : 46),
          label: label.has(n.id) ? (n.title || shortOnion(n.onion)).slice(0, 26) : '',
          center: n.id === center ? 1 : 0,
        },
      })),
      ...good.map((e) => ({ data: { id: `${e.s}-${e.t}`, source: String(e.s), target: String(e.t), w: e.w } })),
    ];

    const cy = cytoscape({
      container: ref.current,
      elements,
      wheelSensitivity: 0.3,
      minZoom: 0.05,
      maxZoom: 4,
      style: [
        { selector: 'node', style: {
          'background-color': 'data(color)', width: 'data(size)', height: 'data(size)',
          label: 'data(label)', color: '#c9bfce', 'font-size': 9, 'font-family': 'var(--font-plex, monospace)',
          'text-valign': 'bottom', 'text-margin-y': 3, 'text-outline-color': '#0d0a0f', 'text-outline-width': 2,
          'min-zoomed-font-size': 7, 'border-width': 0,
        } },
        { selector: 'node[center = 1]', style: { 'border-width': 3, 'border-color': '#d76b96' } },
        { selector: 'edge', style: {
          width: 'mapData(w, 1, 50, 0.5, 2.5)', 'line-color': '#3a2f42', opacity: 0.5, 'curve-style': 'straight',
          'target-arrow-shape': 'triangle', 'target-arrow-color': '#3a2f42', 'arrow-scale': 0.55,
        } },
        { selector: '.dim', style: { opacity: 0.1 } },
        { selector: 'node.hl', style: { 'border-width': 2, 'border-color': '#ece6ea' } },
        { selector: 'edge.hl', style: { 'line-color': '#d76b96', 'target-arrow-color': '#d76b96', opacity: 0.9 } },
      ],
      layout: { name: 'cose', animate: false, randomize: true, nodeRepulsion: () => 9000, idealEdgeLength: () => 70, numIter: 1000, componentSpacing: 60 } as any,
    });
    cyRef.current = cy;
    cy.on('mouseover', 'node', (e) => { cy.elements().addClass('dim'); e.target.closedNeighborhood().removeClass('dim').addClass('hl'); });
    cy.on('mouseout', 'node', () => cy.elements().removeClass('dim hl'));
    cy.on('tap', 'node', (e) => onSelect?.(e.target.data('n')));
    return () => cy.destroy();
  }, [nodes, edges, center, labelTop, onSelect]);

  return <div ref={ref} style={{ height }} className="w-full rounded-lg border border-border/60" />;
}
