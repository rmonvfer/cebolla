'use client';

import { use } from 'react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, type Row } from '@/lib/api';
import { DataTable } from '@/components/data-table';
import { Copyable, OnionAddr, Panel, PageTitle, SectionLabel } from '@/components/bits';
import { datetime, num } from '@/lib/format';

export default function PageView({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const sp = useSearchParams();
  const v = sp.get('v');
  const { data: d } = useQuery({ queryKey: ['page', id, v], queryFn: () => apiGet<Row>(`page/${id}${v ? `?v=${v}` : ''}`) });
  if (!d) return <div className="text-sm text-muted-foreground">Loading…</div>;
  const p = d.page;
  const cur = d.version;
  const path = String(p.url).replace(/^https?:\/\/[^/]+/, '') || '/';

  return (
    <div>
      <PageTitle
        title={cur?.title || 'untitled page'}
        sub={
          <div className="flex flex-wrap items-center gap-2">
            <Link href={`/site/${p.site_id}`} className="hover:text-foreground">{p.site_title || 'service'}</Link>
            <Copyable value={p.url}><span className="mono text-[12px] text-accent">{path}</span></Copyable>
            <span className="text-xs text-muted-foreground">HTTP {num(p.last_status)} · depth {num(p.depth)}</span>
          </div>
        }
      />
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]">
        <div>
          <SectionLabel hint={cur ? `version of ${datetime(cur.fetched_at)}` : undefined}>Extracted text</SectionLabel>
          {cur ? (
            <pre className="mono max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-border/60 bg-background/60 p-4 text-[13px] leading-relaxed text-foreground/90">{cur.text}</pre>
          ) : (
            <p className="text-sm text-muted-foreground">No stored version.</p>
          )}
          {cur?.truncated && <p className="mt-2 text-xs text-muted-foreground">Truncated at 300,000 characters.</p>}
        </div>
        <div className="grid content-start gap-6">
          <div>
            <SectionLabel hint={`${num((d.versions ?? []).length)}`}>Versions</SectionLabel>
            <Panel className="max-h-64 overflow-auto p-0">
              <DataTable
                columns={[
                  { header: 'Fetched', cell: (c) => <Link href={`/page/${p.id}?v=${c.row.original.id}`} className={c.row.original.id === cur?.id ? 'font-medium text-foreground' : 'text-accent hover:underline'}>{datetime(c.row.original.fetched_at)}</Link> },
                  { header: 'Chars', cell: (c) => <span className="mono tnum">{num(c.row.original.chars)}</span>, meta: { align: 'right' } },
                  { header: 'SimHash', cell: (c) => <span className="mono text-[11px] text-muted-foreground">{c.row.original.simhash}</span> },
                ]}
                data={d.versions ?? []}
              />
            </Panel>
          </div>
          <div>
            <SectionLabel>Entities</SectionLabel>
            <Panel className="max-h-64 overflow-auto p-0">
              <DataTable
                columns={[
                  { header: 'Kind', cell: (c) => <span className="mono text-[12px] text-muted-foreground">{c.row.original.kind}</span> },
                  { header: 'Value', cell: (c) => <Link href={`/entity?kind=${c.row.original.kind}&value=${encodeURIComponent(c.row.original.value)}`} className="mono block max-w-[16rem] truncate text-[12px] text-accent hover:underline">{c.row.original.value}</Link> },
                ]}
                data={d.entities ?? []}
                empty="None."
              />
            </Panel>
          </div>
        </div>
      </div>
      <div className="mt-6">
        <SectionLabel hint={`${num((d.links ?? []).length)}`}>Links</SectionLabel>
        <Panel className="max-h-[420px] overflow-auto p-0">
          <DataTable
            columns={[
              { header: 'Anchor', cell: (c) => <span className="block max-w-[16rem] truncate">{c.row.original.anchor}</span> },
              { header: 'Target', cell: (c) => (c.row.original.site_id ? <Link href={`/site/${c.row.original.site_id}`} className="text-accent hover:underline">{c.row.original.site_title || 'service'}</Link> : <Copyable value={c.row.original.url}><span className="mono block max-w-[24rem] truncate text-[12px] text-muted-foreground">{c.row.original.url}</span></Copyable>) },
              { header: '', cell: (c) => <span className="text-[11px] text-muted-foreground">{c.row.original.is_onion ? 'onion' : 'clearnet'}</span>, meta: { align: 'right' } },
            ]}
            data={d.links ?? []}
            empty="No links."
          />
        </Panel>
      </div>
    </div>
  );
}
