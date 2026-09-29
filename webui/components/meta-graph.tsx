'use client';

import { useEffect, useRef } from 'react';
import Graph from 'graphology';
import Sigma from 'sigma';
import forceAtlas2 from 'graphology-layout-forceatlas2';
import noverlap from 'graphology-layout-noverlap';
import louvain from 'graphology-communities-louvain';
import { shortOnion } from '@/lib/format';

const trunc = (t: string, n = 34) => (t.length > n ? t.slice(0, n - 1) + '…' : t);
import type { GNode, GEdge } from './graph-canvas';

const PALETTE = ['#82aaff', '#5fd38d', '#e5c07b', '#56b6c2', '#c792ea', '#f0a35e', '#7fdbca', '#b39ddb', '#6cb6ff', '#d9b45b', '#79d7a8', '#e0879b', '#a0e0c0', '#f2b56b', '#9bc0ff', '#cb9be0'];

export interface Community {
  id: number;
  label: string;
  members: GNode[];
  subEdges: GEdge[];
  size: number;
  color: string;
}

// Group the loaded subgraph into modularity communities and render them as a
// small, legible meta-graph: one bubble per community (sized by membership),
// edges weighted by how much two communities cross-link. Clicking a bubble
// drills into that community's services.
export function MetaGraph({
  nodes, edges, height = 660, onPick,
}: {
  nodes: GNode[]; edges: GEdge[]; height?: number; onPick?: (c: Community) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!ref.current) return;

    const base = new Graph({ type: 'undirected' });
    nodes.forEach((n) => { if (!base.hasNode(String(n.id))) base.addNode(String(n.id)); });
    edges.forEach((e) => {
      const s = String(e.s), t = String(e.t);
      if (s !== t && base.hasNode(s) && base.hasNode(t) && !base.hasEdge(s, t)) base.addEdge(s, t);
    });
    if (base.order < 2) return;
    louvain.assign(base, { resolution: 1.1 });

    const commOf = new Map<number, number>();
    nodes.forEach((n) => { if (base.hasNode(String(n.id))) commOf.set(n.id, base.getNodeAttribute(String(n.id), 'community') as number); });

    const groups = new Map<number, GNode[]>();
    nodes.forEach((n) => { const c = commOf.get(n.id); if (c == null) return; (groups.get(c) ?? groups.set(c, []).get(c)!).push(n); });

    const ranked = [...groups.entries()].filter(([, m]) => m.length >= 3).sort((a, b) => b[1].length - a[1].length).slice(0, 24);
    const rankOf = new Map<number, number>();
    ranked.forEach(([cid], i) => rankOf.set(cid, i));

    const communities: Community[] = ranked.map(([cid, members], i) => {
      const top = [...members].sort((a, b) => b.indeg - a.indeg)[0];
      const memberSet = new Set(members.map((m) => m.id));
      const subEdges = edges.filter((e) => memberSet.has(e.s) && memberSet.has(e.t));
      return { id: cid, label: trunc(top.title || shortOnion(top.onion)), members, subEdges, size: members.length, color: PALETTE[i % PALETTE.length] };
    });

    const metaW = new Map<string, number>();
    edges.forEach((e) => {
      const cu = commOf.get(e.s), cv = commOf.get(e.t);
      if (cu == null || cv == null || cu === cv || !rankOf.has(cu) || !rankOf.has(cv)) return;
      const k = cu < cv ? `${cu}|${cv}` : `${cv}|${cu}`;
      metaW.set(k, (metaW.get(k) ?? 0) + 1);
    });

    const g = new Graph({ type: 'undirected' });
    const maxSize = Math.max(1, ...communities.map((c) => c.size));
    communities.forEach((c, i) => {
      const a = (i / communities.length) * 2 * Math.PI;
      g.addNode(String(c.id), {
        x: Math.cos(a), y: Math.sin(a),
        size: 11 + Math.sqrt(c.size / maxSize) * 30,
        label: `${c.label}  ·  ${c.size}`,
        color: c.color, c,
      });
    });
    const maxW = Math.max(1, ...metaW.values());
    metaW.forEach((w, k) => {
      const [a, b] = k.split('|');
      if (g.hasNode(a) && g.hasNode(b)) g.addEdge(a, b, { size: 0.6 + Math.sqrt(w / maxW) * 6, color: '#33293c', w });
    });

    forceAtlas2.assign(g, { iterations: 300, settings: { adjustSizes: true, scalingRatio: 45, gravity: 0.5, slowDown: 6, barnesHutOptimize: false } });
    noverlap.assign(g, { maxIterations: 250, settings: { margin: 10, ratio: 1.3, expansion: 1.5, gridSize: 20, speed: 5 } });

    const renderer = new Sigma(g, ref.current, {
      renderLabels: true,
      labelColor: { color: '#ece6ea' },
      labelSize: 12,
      labelWeight: '600',
      labelFont: 'var(--font-hanken), sans-serif',
      labelRenderedSizeThreshold: 0,
      defaultEdgeColor: '#33293c',
      minCameraRatio: 0.2,
      maxCameraRatio: 4,
      zIndex: true,
      allowInvalidContainer: true,
    });

    let hov: string | null = null;
    let neigh = new Set<string>();
    renderer.setSetting('nodeReducer', (node, data) => {
      if (!hov) return data;
      if (node === hov || neigh.has(node)) return { ...data, zIndex: 2 };
      return { ...data, color: '#2a222f', label: '', zIndex: 0 };
    });
    renderer.setSetting('edgeReducer', (edge, data) => {
      if (!hov) return data;
      const [s, t] = g.extremities(edge);
      if (s === hov || t === hov) return { ...data, color: '#d76b96', zIndex: 1 };
      return { ...data, hidden: true };
    });
    renderer.on('enterNode', ({ node }) => { hov = node; neigh = new Set(g.neighbors(node)); if (ref.current) ref.current.style.cursor = 'pointer'; renderer.refresh(); });
    renderer.on('leaveNode', () => { hov = null; neigh = new Set(); if (ref.current) ref.current.style.cursor = 'default'; renderer.refresh(); });
    renderer.on('clickNode', ({ node }) => onPick?.(g.getNodeAttribute(node, 'c') as Community));

    return () => renderer.kill();
  }, [nodes, edges, onPick]);

  return <div ref={ref} style={{ height }} className="w-full rounded-lg border border-border/60 bg-[#0a080d]" />;
}
