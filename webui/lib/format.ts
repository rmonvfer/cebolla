export const nf = new Intl.NumberFormat('en-US');
export const num = (n: number | null | undefined) => nf.format(Number(n || 0));

export function compact(v: number | null | undefined): string {
  if (v == null) return '–';
  const a = Math.abs(v);
  if (a >= 1e6) return (v / 1e6).toFixed(1) + 'M';
  if (a >= 1e3) return (v / 1e3).toFixed(1) + 'k';
  if (a >= 10 || a === 0) return String(Math.round(v));
  if (a >= 1) return v.toFixed(1);
  return v.toPrecision(2);
}
export const ms = (v: number | null | undefined) =>
  v == null ? '–' : v >= 1000 ? (v / 1000).toFixed(1) + 's' : Math.round(v) + 'ms';
export function ago(t: string | number | null | undefined): string {
  if (!t) return '–';
  const s = (Date.now() - new Date(t).getTime()) / 1000;
  if (s < 90) return `${Math.round(s)}s ago`;
  if (s < 5400) return `${Math.round(s / 60)}m ago`;
  if (s < 129600) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}
export const day = (t: string | number | null | undefined) => (t ? new Date(t).toISOString().slice(0, 10) : '–');
export const datetime = (t: string | number | null | undefined) =>
  t ? new Date(t).toISOString().slice(0, 16).replace('T', ' ') : '–';
export const shortOnion = (o: string) => (o ? `${o.slice(0, 10)}…${o.slice(-6)}.onion` : '');

export const STATUS = ['up', 'flaky', 'down', 'dead', 'unknown', 'auth_gated'] as const;
export const STATUS_COLOR: Record<string, string> = {
  up: '#5fd38d', flaky: '#e5c07b', down: '#f07f5e', dead: '#6b7482', unknown: '#4f5b6b', auth_gated: '#b18cf2',
};
export const OUTCOME_COLOR: Record<string, string> = {
  ok: '#5fd38d', http_error: '#82aaff', offline: '#6b7482', desc_not_found: '#6b7482', timeout: '#f07f5e',
  other: '#c792ea', conn_error: '#e06c75', intro_failed: '#e5c07b', content_type: '#56b6c2',
  p50: '#5fd38d', p90: '#82aaff', p95: '#82aaff', p99: '#f07f5e',
  pages: '#5fd38d', links: '#82aaff', busy: '#82aaff', total: '#82aaff', due: '#5fd38d',
};
export const PALETTE = ['#82aaff', '#5fd38d', '#e5c07b', '#f07f5e', '#c792ea', '#56b6c2', '#e06c75', '#98c379'];
export const colorFor = (name: string, i = 0) => STATUS_COLOR[name] || OUTCOME_COLOR[name] || PALETTE[i % PALETTE.length];
