'use client';

import { Suspense, useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { apiGet, type Row } from '@/lib/api';
import { PageTitle, Panel, OnionAddr } from '@/components/bits';
import { num } from '@/lib/format';
import { Input } from '@/components/ui/input';

// Highlight markers the API emits (U+0001 / U+0002) -> <mark>.
function Snippet({ text }: { text: string }) {
  const parts = text.split(/([\u0001\u0002])/);
  const out: React.ReactNode[] = [];
  let on = false;
  parts.forEach((p, i) => {
    if (p === '\u0001') on = true;
    else if (p === '\u0002') on = false;
    else if (p) out.push(on ? <mark key={i} className="rounded-sm bg-primary/20 px-0.5 text-primary">{p}</mark> : <span key={i}>{p}</span>);
  });
  return <>{out}</>;
}

function SearchInner() {
  const router = useRouter();
  const params = useSearchParams();
  const q = params.get('q') ?? '';
  const from = Number(params.get('from') ?? 0);
  const [input, setInput] = useState(q);
  useEffect(() => setInput(q), [q]);

  const { data, isLoading } = useQuery({
    queryKey: ['search', q, from],
    queryFn: () => apiGet<Row>(`search?q=${encodeURIComponent(q)}&from=${from}`),
    enabled: q.length > 0,
  });

  return (
    <div className="mx-auto max-w-3xl">
      <PageTitle title="Search" sub="Full-text over every indexed page." />
      <form onSubmit={(e) => { e.preventDefault(); router.push(`/search?q=${encodeURIComponent(input.trim())}`); }} className="mb-6">
        <Input autoFocus value={input} onChange={(e) => setInput(e.target.value)} placeholder="e.g. escrow, forum, market" className="h-11 w-full text-base" />
      </form>

      {!q && <p className="text-sm text-muted-foreground">Type a query to search titles and page text across the index.</p>}
      {q && isLoading && <p className="text-sm text-muted-foreground">Searching…</p>}
      {data && (
        <>
          <div className="mb-4 text-sm text-muted-foreground"><span className="mono tnum text-foreground">{num(data.total)}</span> pages</div>
          <div className="flex flex-col gap-1">
            {(data.hits ?? []).map((h: Row, i: number) => (
              <Panel key={i} className="border-transparent bg-transparent px-1 py-3 hover:bg-transparent">
                <Link href={`/page/${h.page_id}`} className="text-[15px] font-medium text-foreground hover:text-primary">
                  {h.title || <span className="text-muted-foreground/60">untitled page</span>}
                </Link>
                <div className="mt-0.5 flex items-center gap-3">
                  <OnionAddr onion={h.onion} />
                  <Link href={`/site/${h.site_id}`} className="text-xs text-muted-foreground hover:text-foreground">service</Link>
                </div>
                <p className="mt-1 text-[13px] leading-relaxed text-muted-foreground">
                  {(h.snippet ?? []).map((s: string, j: number) => <span key={j}>…<Snippet text={s} /> </span>)}
                </p>
              </Panel>
            ))}
          </div>
          <div className="mt-6 flex gap-4 text-sm text-muted-foreground">
            {from > 0 && <Link href={`/search?q=${encodeURIComponent(q)}&from=${Math.max(0, from - 25)}`} className="hover:text-foreground">← Previous</Link>}
            {from + 25 < data.total && <Link href={`/search?q=${encodeURIComponent(q)}&from=${from + 25}`} className="hover:text-foreground">Next →</Link>}
          </div>
        </>
      )}
    </div>
  );
}

export default function Search() {
  return <Suspense fallback={null}><SearchInner /></Suspense>;
}
