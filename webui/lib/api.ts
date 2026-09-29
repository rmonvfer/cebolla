// Typed-ish client for the Go explorer JSON API. Rows come back as
// column-name maps, so most shapes are Record<string, any>.
export type Row = Record<string, any>;

export async function apiGet<T = any>(path: string): Promise<T> {
  const r = await fetch(`/api/${path}`, { headers: { accept: 'application/json' } });
  if (!r.ok) {
    let msg = `HTTP ${r.status}`;
    try {
      msg = (await r.json()).error || msg;
    } catch {}
    throw new Error(msg);
  }
  return r.json();
}

export interface PromLine { name: string; points: [number, number][]; }
export interface PromMatrix { series: PromLine[]; step: number; }
export const prom = (series: string, hours: number) =>
  apiGet<PromMatrix>(`prom?series=${series}&hours=${hours}`).catch(() => ({ series: [], step: 0 }));
