'use client';

import { useEffect, useRef, useState } from 'react';
import Graph from 'graphology';
import Sigma from 'sigma';
import forceAtlas2 from 'graphology-layout-forceatlas2';
import louvain from 'graphology-communities-louvain';
import { STATUS_COLOR, shortOnion } from '@/lib/format';

export interface GNode { id: number; onion: string; title: string; status: string; indeg: number; outdeg: number; component?: number | null; }
export interface GEdge { s: number; t: number; w: number; }

// Community palette (rose is reserved for the focused node).
const COMMUNITY = ['#82aaff', '#5fd38d', '#e5c07b', '#56b6c2', '#c792ea', '#f0a35e', '#7fdbca', '#b39ddb', '#6cb6ff', '#d9b45b', '#79d7a8', '#e0879b'];
const MUTED = '#4a4152';

export function GraphCanvas({
  nodes, edges, center, height = 560, onSelect, colorBy = 'community',
}: {
  nodes: GNode[]; edges: GEdge[]; center?: number; height?: number;
  onSelect?: (n: GNode) => void; colorBy?: 'community' | 'status';
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [busy, setBusy] = useState(true);

  useEffect(() => {
    if (!ref.current) return;
    setBusy(true);

    const present = new Set(nodes.map((n) => n.id));
    const good = edges.filter((e) => present.has(e.s) && present.has(e.t) && e.s !== e.t);
    const linked = new Set<number>();
    good.forEach((e) => { linked.add(e.s); linked.add(e.t); });
    const shown = nodes.filter((n) => linked.has(n.id) || n.id === center);
    const maxIn = Math.max(1, ...shown.map((n) => n.indeg));

    const g = new Graph({ multi: false, type: 'directed' });
    shown.forEach((n) => {
      const isCenter = n.id === center;
      const r = Math.sqrt(Math.random());
      const a = Math.random() * 2 * Math.PI;
      g.addNode(String(n.id), {
        x: r * Math.cos(a),
        y: r * Math.sin(a),
        size: isCenter ? 12 : 2.5 + Math.sqrt(n.indeg / maxIn) * (center ? 9 : 16),
        label: n.title || shortOnion(n.onion),
        color: MUTED,
        n,
      });
    });
    good.forEach((e) => {
      const s = String(e.s), t = String(e.t);
      if (g.hasNode(s) && g.hasNode(t) && !g.hasEdge(s, t)) g.addEdge(s, t, { size: 0.4, color: '#241d2c' });
    });

    // Detect communities (modularity) on the shown subgraph, then colour the
    // biggest ones. Weakly-connected components collapse the whole graph into
    // one blob; Louvain finds the real sub-structure inside it.
    if (colorBy === 'community' && g.order > 2) {
      try { louvain.assign(g, { resolution: 1 }); } catch {}
      const size = new Map<number, number>();
      g.forEachNode((_, at: any) => { if (at.community != null) size.set(at.community, (size.get(at.community) ?? 0) + 1); });
      const top = new Map<number, string>();
      [...size.entries()].sort((a, b) => b[1] - a[1]).slice(0, COMMUNITY.length).forEach(([c], i) => top.set(c, COMMUNITY[i]));
      g.forEachNode((id, at: any) => g.setNodeAttribute(id, 'color', top.get(at.community) ?? MUTED));
    } else {
      shown.forEach((n) => g.setNodeAttribute(String(n.id), 'color', STATUS_COLOR[n.status] ?? MUTED));
    }
    if (center && g.hasNode(String(center))) { g.setNodeAttribute(String(center), 'color', '#d76b96'); g.setNodeAttribute(String(center), 'highlighted', true); }

    const N = g.order;
    forceAtlas2.assign(g, {
      iterations: N > 700 ? 320 : 550,
      settings: {
        linLogMode: true,               // pulls each community into its own region
        outboundAttractionDistribution: false,
        adjustSizes: true,
        barnesHutOptimize: N > 300,
        scalingRatio: 6,
        gravity: 1.1,                   // fills the centre instead of a hollow ring
        slowDown: 4,
        edgeWeightInfluence: 0.5,
      },
    });

    const renderer = new Sigma(g, ref.current, {
      renderLabels: true,
      labelColor: { color: '#c3b8ca' },
      labelSize: 11,
      labelFont: 'var(--font-plex), monospace',
      labelRenderedSizeThreshold: center ? 4 : 10,
      defaultEdgeColor: '#241d2c',
      minCameraRatio: 0.04,
      maxCameraRatio: 10,
      zIndex: true,
      allowInvalidContainer: true,
      hideEdgesOnMove: N > 600,
    });

    let hovered: string | null = null;
    let neigh = new Set<string>();
    renderer.setSetting('nodeReducer', (node, data) => {
      if (!hovered) return data;
      if (node === hovered) return { ...data, zIndex: 2, forceLabel: true };
      if (neigh.has(node)) return { ...data, zIndex: 1, forceLabel: true };
      return { ...data, color: '#221b29', label: '', zIndex: 0 };
    });
    renderer.setSetting('edgeReducer', (edge, data) => {
      if (!hovered) return data;
      const [s, t] = g.extremities(edge);
      if (s === hovered || t === hovered) return { ...data, color: '#d76b96', size: 1, zIndex: 1 };
      return { ...data, hidden: true };
    });
    renderer.on('enterNode', ({ node }) => { hovered = node; neigh = new Set(g.neighbors(node)); if (ref.current) ref.current.style.cursor = 'pointer'; renderer.refresh(); });
    renderer.on('leaveNode', () => { hovered = null; neigh = new Set(); if (ref.current) ref.current.style.cursor = 'default'; renderer.refresh(); });
    renderer.on('clickNode', ({ node }) => onSelect?.(g.getNodeAttribute(node, 'n') as GNode));

    setBusy(false);
    return () => renderer.kill();
  }, [nodes, edges, center, onSelect, colorBy]);

  return (
    <div className="relative">
      <div ref={ref} style={{ height }} className="w-full rounded-lg border border-border/60 bg-[#0a080d]" />
      {busy && <div className="pointer-events-none absolute inset-0 grid place-items-center text-xs text-muted-foreground">laying out…</div>}
    </div>
  );
}
