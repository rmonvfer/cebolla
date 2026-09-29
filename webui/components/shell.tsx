'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';
import { Activity, Boxes, Fingerprint, LayoutGrid, Network, Search, Share2, Layers } from 'lucide-react';
import { cn } from '@/lib/utils';
import { prom } from '@/lib/api';
import { compact } from '@/lib/format';

const NAV = [
  { href: '/', label: 'Overview', icon: LayoutGrid },
  { href: '/search', label: 'Search', icon: Search },
  { href: '/sites', label: 'Sites', icon: Boxes },
  { href: '/graph', label: 'Graph', icon: Share2 },
  { href: '/analytics', label: 'Analytics', icon: Activity },
  { href: '/entities', label: 'Entities', icon: Fingerprint },
  { href: '/clusters', label: 'Clusters', icon: Layers },
];

function useLive() {
  const rate = useQuery({
    queryKey: ['live', 'fetch_rate'],
    queryFn: () => prom('fetch_rate', 1),
    refetchInterval: 15_000,
  });
  const workers = useQuery({
    queryKey: ['live', 'workers'],
    queryFn: () => prom('workers', 1),
    refetchInterval: 15_000,
  });
  // Sum fetch-rate series per timestamp into a single sparkline.
  const series = rate.data?.series ?? [];
  const byT = new Map<number, number>();
  for (const s of series) for (const [t, v] of s.points) byT.set(t, (byT.get(t) ?? 0) + v);
  const spark = [...byT.entries()].sort((a, b) => a[0] - b[0]).map(([, v]) => v);
  const now = spark.length ? spark[spark.length - 1] : 0;
  const w = workers.data?.series?.[0]?.points ?? [];
  const busy = w.length ? w[w.length - 1][1] : 0;
  return { spark, now, busy };
}

function Sparkline({ data }: { data: number[] }) {
  if (data.length < 2) return <div className="h-5 w-24" />;
  const max = Math.max(...data, 0.0001);
  const w = 96, h = 20;
  const pts = data.map((v, i) => `${(i / (data.length - 1)) * w},${h - (v / max) * h}`).join(' ');
  return (
    <svg width={w} height={h} className="draw-in overflow-visible">
      <polyline points={pts} fill="none" stroke="var(--primary)" strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" opacity="0.9" />
    </svg>
  );
}

function Signal() {
  const { spark, now, busy } = useLive();
  const live = now > 0.05;
  return (
    <div className="flex items-center gap-4 text-xs">
      <div className="flex items-center gap-2">
        <span className={cn('h-2 w-2 rounded-full', live ? 'bg-primary signal-live' : 'bg-muted-foreground/40')} />
        <span className="text-muted-foreground">{live ? 'crawling' : 'idle'}</span>
      </div>
      <div className="hidden items-end gap-2 sm:flex">
        <Sparkline data={spark} />
        <div className="leading-none">
          <span className="mono text-sm text-foreground">{compact(now)}</span>
          <span className="text-muted-foreground">/s</span>
        </div>
      </div>
      <div className="hidden leading-none md:block">
        <span className="mono text-sm text-foreground">{compact(busy)}</span>
        <span className="text-muted-foreground"> workers</span>
      </div>
    </div>
  );
}

function TopSearch() {
  const router = useRouter();
  const [q, setQ] = useState('');
  return (
    <form
      onSubmit={(e) => { e.preventDefault(); if (q.trim()) router.push(`/search?q=${encodeURIComponent(q.trim())}`); }}
      className="relative"
    >
      <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search page text"
        spellCheck={false}
        className="h-8 w-52 rounded-md border border-border bg-card/60 pl-8 pr-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus:border-ring lg:w-72"
      />
    </form>
  );
}

export function Shell({ children }: { children: ReactNode }) {
  const path = usePathname();
  const active = (href: string) => (href === '/' ? path === '/' : path.startsWith(href));
  return (
    <div className="relative z-[1] flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-[13.5rem] shrink-0 flex-col border-r border-sidebar-border bg-sidebar">
        <div className="flex items-center gap-2 px-5 py-5">
          <span className="h-2.5 w-2.5 rounded-full bg-primary signal-live" />
          <span className="font-display text-lg font-semibold tracking-tight text-foreground">cebolla</span>
        </div>
        <nav className="flex flex-col gap-0.5 px-3">
          {NAV.map(({ href, label, icon: Icon }) => (
            <Link
              key={href}
              href={href}
              className={cn(
                'flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors',
                active(href)
                  ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                  : 'text-sidebar-foreground hover:bg-sidebar-accent/50 hover:text-sidebar-accent-foreground',
              )}
            >
              <Icon className={cn('size-4', active(href) ? 'text-primary' : 'text-muted-foreground')} />
              {label}
            </Link>
          ))}
        </nav>
        <div className="mt-auto px-5 py-4 text-[11px] leading-relaxed text-muted-foreground">
          <div className="mono">read-only</div>
          <div>Tor hidden-service index</div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-10 flex h-14 items-center justify-between gap-4 border-b border-border bg-background/70 px-6 backdrop-blur">
          <TopSearch />
          <Signal />
        </header>
        <main className="min-w-0 flex-1 px-6 py-6">{children}</main>
      </div>
    </div>
  );
}
