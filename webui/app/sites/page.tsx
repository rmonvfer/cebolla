'use client';

import { Suspense, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useRouter, useSearchParams } from 'next/navigation';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, type Row } from '@/lib/api';
import { DataTable } from '@/components/data-table';
import { Panel, PageTitle, SiteName, StatusPill } from '@/components/bits';
import { day, num, ago, STATUS } from '@/lib/format';
import { Input } from '@/components/ui/input';
import { NativeSelect as Select } from '@/components/native-select';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

const SORTS = [
  ['rank', 'PageRank'], ['indeg', 'most linked-to'], ['outdeg', 'most links out'],
  ['pages', 'most pages'], ['recent', 'newest'], ['seen', 'recently online'],
] as const;

function SitesInner() {
  const router = useRouter();
  const params = useSearchParams();
  const q0 = params.get('q') ?? '';
  const status = params.get('status') ?? '';
  const sort = params.get('sort') ?? 'rank';
  const component = params.get('component');
  const operator = params.get('operator');
  const offset = Number(params.get('offset') ?? 0);
  const [q, setQ] = useState(q0);

  const extra = (component ? `&component=${component}` : '') + (operator ? `&operator=${operator}` : '');
  const key = `sites?q=${encodeURIComponent(q0)}&status=${status}&sort=${sort}&offset=${offset}${extra}`;
  const { data, isLoading } = useQuery({ queryKey: [key], queryFn: () => apiGet<Row[]>(key) });

  const setParam = (patch: Record<string, string | null>) => {
    const p = new URLSearchParams(params.toString());
    for (const [k, v] of Object.entries(patch)) v == null ? p.delete(k) : p.set(k, v);
    if (!('offset' in patch)) p.delete('offset');
    router.push(`/sites?${p.toString()}`);
  };

  const cols: ColumnDef<Row, any>[] = [
    { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
    { header: 'State', cell: (c) => <StatusPill status={c.row.original.status} />, meta: { align: 'right' } },
    { header: 'Linked from', cell: (c) => <span className="mono tnum">{num(c.row.original.indeg)}</span>, meta: { align: 'right' } },
    { header: 'Links out', cell: (c) => <span className="mono tnum">{num(c.row.original.outdeg)}</span>, meta: { align: 'right' } },
    { header: 'PageRank', cell: (c) => <span className="mono tnum text-primary">{c.row.original.pagerank != null ? (c.row.original.pagerank * 1e6).toFixed(1) : '—'}</span>, meta: { align: 'right' } },
    { header: 'Pages', cell: (c) => <span className="mono tnum">{num(c.row.original.pages_fetched)}</span>, meta: { align: 'right' } },
    { header: 'First seen', cell: (c) => <span className="text-muted-foreground">{day(c.row.original.first_seen)}</span>, meta: { align: 'right' } },
    { header: 'Last online', cell: (c) => <span className="text-muted-foreground">{ago(c.row.original.last_ok)}</span>, meta: { align: 'right' } },
  ];

  const banner = component
    ? `Component #${component}`
    : operator
      ? `Shared-identifier cluster #${operator} — services sharing a PGP key or contact email`
      : undefined;

  return (
    <div>
      <PageTitle title="Services" sub={banner ?? 'Every crawled hidden service.'} />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <form onSubmit={(e) => { e.preventDefault(); setParam({ q: q || null }); }}>
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Title or address prefix" className="w-64" />
        </form>
        <Select value={status} onChange={(e) => setParam({ status: e.target.value || null })}>
          <option value="">any state</option>
          {STATUS.map((s) => <option key={s} value={s}>{s.replace('_', ' ')}</option>)}
        </Select>
        <Select value={sort} onChange={(e) => setParam({ sort: e.target.value })}>
          {SORTS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
        </Select>
        {(component || operator) && <Button variant="ghost" size="sm" onClick={() => router.push('/sites')}>clear filter</Button>}
      </div>

      <Panel className="p-0">
        {isLoading ? <div className="p-8 text-sm text-muted-foreground">Loading…</div> : <DataTable columns={cols} data={data ?? []} empty="No services match." />}
      </Panel>

      <div className="mt-4 flex items-center gap-4 text-sm text-muted-foreground">
        {offset > 0 && <button className="hover:text-foreground" onClick={() => setParam({ offset: String(Math.max(0, offset - 100)) })}>← Previous</button>}
        <span className="mono tnum">{num(offset + 1)}–{num(offset + (data?.length ?? 0))}</span>
        {(data?.length ?? 0) >= 100 && <button className="hover:text-foreground" onClick={() => setParam({ offset: String(offset + 100) })}>Next →</button>}
      </div>
    </div>
  );
}

export default function Sites() {
  return (
    <Suspense fallback={<div className="text-sm text-muted-foreground">Loading…</div>}>
      <SitesInner />
    </Suspense>
  );
}
