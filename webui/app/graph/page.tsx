'use client';

import { Suspense, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { apiGet } from '@/lib/api';
import { GraphCanvas, type GNode } from '@/components/graph-canvas';
import { PageTitle, OnionAddr, StatusPill } from '@/components/bits';
import { num, STATUS, STATUS_COLOR } from '@/lib/format';
import { NativeSelect as Select } from '@/components/native-select';

function GraphInner() {
  const router = useRouter();
  const params = useSearchParams();
  const site = Number(params.get('site') ?? 0);
  const hops = Number(params.get('hops') ?? 2);
  const max = Number(params.get('max') ?? (site ? 200 : 400));
  const [sel, setSel] = useState<GNode | null>(null);

  const key = site ? `graph?site=${site}&hops=${hops}&max=${max}` : `graph?max=${max}`;
  const { data } = useQuery({ queryKey: [key], queryFn: () => apiGet<{ nodes: GNode[]; edges: any[] }>(key) });

  const set = (patch: Record<string, string>) => {
    const p = new URLSearchParams(params.toString());
    for (const [k, v] of Object.entries(patch)) v ? p.set(k, v) : p.delete(k);
    router.push(`/graph?${p.toString()}`);
  };

  return (
    <div>
      <PageTitle
        title="Link graph"
        sub={data ? `${num(data.nodes.length)} services · ${num(data.edges.length)} links` : 'Loading…'}
        right={
          <div className="flex flex-wrap items-center gap-2">
            <Select value={site ? 'site' : 'top'} onChange={(e) => { if (e.target.value === 'top') router.push('/graph'); }}>
              <option value="top">best-connected</option>
              {site ? <option value="site">around #{site}</option> : null}
            </Select>
            {site ? <Select value={String(hops)} onChange={(e) => set({ hops: e.target.value })}><option value="1">1 hop</option><option value="2">2 hops</option></Select> : null}
            <Select value={String(max)} onChange={(e) => set({ max: e.target.value })}>
              {[100, 200, 400, 800, 1200].map((n) => <option key={n} value={n}>{n} nodes</option>)}
            </Select>
            <div className="flex flex-wrap gap-2 text-[11px] text-muted-foreground">
              {STATUS.map((s) => <span key={s} className="inline-flex items-center gap-1"><span className="size-2 rounded-full" style={{ background: STATUS_COLOR[s] }} />{s.replace('_', ' ')}</span>)}
            </div>
          </div>
        }
      />
      <div className="relative">
        {data && <GraphCanvas nodes={data.nodes} edges={data.edges} center={site || undefined} height={620} onSelect={setSel} />}
        {sel && (
          <div className="absolute right-3 top-3 w-72 rounded-lg border border-border bg-popover/95 p-4 backdrop-blur">
            <div className="font-medium text-foreground">{sel.title || <span className="text-muted-foreground/60">untitled</span>}</div>
            <div className="mt-1"><OnionAddr onion={sel.onion} /></div>
            <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
              <StatusPill status={sel.status} /> linked from {num(sel.indeg)} · out {num(sel.outdeg)}
            </div>
            <div className="mt-3 flex gap-3 text-sm">
              <Link href={`/site/${sel.id}`} className="text-primary hover:underline">Open service</Link>
              <Link href={`/graph?site=${sel.id}&hops=${hops}&max=${max}`} className="text-accent hover:underline">Center here</Link>
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
