'use client';

import { Suspense, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import dynamic from 'next/dynamic';
import { useRouter, useSearchParams } from 'next/navigation';
import { ArrowLeft } from 'lucide-react';
import { apiGet } from '@/lib/api';
import { type GNode, type GEdge } from '@/components/graph-canvas';
import type { Community } from '@/components/meta-graph';
import { PageTitle, OnionAddr, StatusPill, Panel } from '@/components/bits';
import { num } from '@/lib/format';
import { NativeSelect as Select } from '@/components/native-select';
import { Button } from '@/components/ui/button';

const GraphCanvas = dynamic(() => import('@/components/graph-canvas').then((m) => m.GraphCanvas), { ssr: false });
const MetaGraph = dynamic(() => import('@/components/meta-graph').then((m) => m.MetaGraph), { ssr: false });

function GraphInner() {
  const router = useRouter();
  const params = useSearchParams();
  const site = Number(params.get('site') ?? 0);
  const hops = Number(params.get('hops') ?? 2);
  const view = params.get('view') ?? 'communities'; // communities | services
  const max = Number(params.get('max') ?? (site ? 200 : 700));
  const [sel, setSel] = useState<GNode | null>(null);
  const [drill, setDrill] = useState<Community | null>(null);

  const key = site ? `graph?site=${site}&hops=${hops}&max=${max}` : `graph?max=${max}`;
  const { data, isLoading } = useQuery({ queryKey: [key], queryFn: () => apiGet<{ nodes: GNode[]; edges: GEdge[] }>(key) });

  const set = (patch: Record<string, string>) => {
    const p = new URLSearchParams(params.toString());
    for (const [k, v] of Object.entries(patch)) v ? p.set(k, v) : p.delete(k);
    setDrill(null); setSel(null);
    router.push(`/graph?${p.toString()}`);
  };

  const isEgo = !!site;
  const showMeta = !isEgo && view === 'communities' && !drill;

  let sub = '';
  if (drill) sub = `${num(drill.size)} services in this community`;
  else if (data) sub = showMeta ? `${num(data.nodes.length)} services grouped into communities` : `${num(data.nodes.length)} services · ${num(data.edges.length)} links`;
  else sub = 'Loading…';

  return (
    <div>
      <PageTitle
        title="Link graph"
        sub={sub}
        right={
          <div className="flex flex-wrap items-center gap-2">
            {!isEgo && (
              <Select value={view} onChange={(e) => set({ view: e.target.value })}>
                <option value="communities">communities</option>
                <option value="services">all services</option>
              </Select>
            )}
            {isEgo && <Select value={String(hops)} onChange={(e) => set({ hops: e.target.value })}><option value="1">1 hop</option><option value="2">2 hops</option></Select>}
            <Select value={String(max)} onChange={(e) => set({ max: e.target.value })}>
              {(isEgo ? [100, 200, 400] : [400, 700, 1000, 1500]).map((n) => <option key={n} value={n}>{n} nodes</option>)}
            </Select>
            {isEgo && <Button variant="ghost" size="sm" onClick={() => router.push('/graph')}>whole graph</Button>}
            <span className="text-[11px] text-muted-foreground">
              {showMeta ? 'bubble = community · size = services · click to open' : 'colour = community · size = links in · hover to focus'}
            </span>
          </div>
        }
      />

      {drill && (
        <div className="mb-3 flex items-center gap-3">
          <Button variant="outline" size="sm" onClick={() => { setDrill(null); setSel(null); }}>
            <ArrowLeft className="size-3.5" /> communities
          </Button>
          <span className="font-display text-sm font-semibold text-foreground">{drill.label}</span>
        </div>
      )}

      <div className="relative">
        {isLoading && <div className="grid h-[660px] place-items-center rounded-lg border border-border/60 text-sm text-muted-foreground">Loading graph…</div>}

        {data && showMeta && <MetaGraph nodes={data.nodes} edges={data.edges} onPick={setDrill} />}

        {data && drill && <GraphCanvas nodes={drill.members} edges={drill.subEdges} colorBy="status" height={660} onSelect={setSel} />}

        {data && !showMeta && !drill && (
          <GraphCanvas nodes={data.nodes} edges={data.edges} center={site || undefined} colorBy={isEgo ? 'status' : 'community'} height={660} onSelect={setSel} />
        )}

        {sel && (
          <div className="absolute right-3 top-3 w-72 rounded-lg border border-border bg-popover/95 p-4 backdrop-blur">
            <div className="font-medium text-foreground">{sel.title || <span className="text-muted-foreground/60">untitled</span>}</div>
            <div className="mt-1"><OnionAddr onion={sel.onion} /></div>
            <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
              <StatusPill status={sel.status} /> in {num(sel.indeg)} · out {num(sel.outdeg)}
            </div>
            <div className="mt-3 flex gap-3 text-sm">
              <Link href={`/site/${sel.id}`} className="text-primary hover:underline">Open service</Link>
              <Link href={`/graph?site=${sel.id}&hops=${hops}`} className="text-accent hover:underline">Neighbourhood</Link>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default function Graph() {
  return <Suspense fallback={null}><GraphInner /></Suspense>;
}
