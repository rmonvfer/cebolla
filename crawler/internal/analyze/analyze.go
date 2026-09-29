// Package analyze computes structural analytics over the crawl data and
// stores them in site_analysis: PageRank and weakly-connected components over
// the site link graph, and operator clusters over sites that share a payment
// address or PGP key. It is recomputed in full on a schedule.
package analyze

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxShare caps how many sites an entity value may appear on before it is
// treated as shared infrastructure rather than an operator fingerprint. A
// value on many sites (a shared support email) would otherwise merge
// unrelated sites into one giant blob.
const maxShare = 18

type edge struct{ s, t, w int } // dense node indices, weight

type Analyzer struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Analyzer { return &Analyzer{db: db} }

type Result struct {
	Sites      int
	Edges      int
	Components int
	Operators  int
	Took       time.Duration
}

// Run recomputes everything and writes site_analysis.
func (a *Analyzer) Run(ctx context.Context) (*Result, error) {
	start := time.Now()

	nrows, err := a.db.Query(ctx, `SELECT id FROM sites ORDER BY id`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(nrows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	n := len(ids)
	idx := make(map[int64]int, n) // site id -> dense index
	for i, id := range ids {
		idx[id] = i
	}

	erows, err := a.db.Query(ctx, `SELECT src_site_id, dst_site_id, n_links FROM site_edges`)
	if err != nil {
		return nil, err
	}
	var edges []edge
	for erows.Next() {
		var s, t int64
		var w int
		if err := erows.Scan(&s, &t, &w); err != nil {
			return nil, err
		}
		if si, ok := idx[s]; ok {
			if ti, ok := idx[t]; ok && si != ti {
				edges = append(edges, edge{si, ti, w})
			}
		}
	}
	if err := erows.Err(); err != nil {
		return nil, err
	}

	pr := pagerank(n, edges)
	comp := components(n, edges)

	// Operator clustering: union sites that share a non-onion entity value,
	// unless the value is too widespread (maxShare).
	uf := newUF(n)
	// Cluster on PGP keys and contact emails only. Payment addresses are
	// shared by processors and escrows, which chains unrelated shops into one
	// blob; keys and emails are far more identity-bound.
	orows, err := a.db.Query(ctx, `
		SELECT array_agg(DISTINCT site_id) FROM entities
		WHERE kind IN ('pgp', 'email')
		GROUP BY kind, value
		HAVING count(DISTINCT site_id) BETWEEN 2 AND $1`, maxShare)
	if err != nil {
		return nil, err
	}
	for orows.Next() {
		var sites []int64
		if err := orows.Scan(&sites); err != nil {
			return nil, err
		}
		first := -1
		for _, sid := range sites {
			i, ok := idx[sid]
			if !ok {
				continue
			}
			if first < 0 {
				first = i
			} else {
				uf.union(first, i)
			}
		}
	}
	if err := orows.Err(); err != nil {
		return nil, err
	}

	compID, nComp := label(n, comp, false)
	opRoot := make([]int, n)
	for i := range opRoot {
		opRoot[i] = uf.find(i)
	}
	opID, nOp := label(n, opRoot, true) // drop singleton operators (id -1)

	rows := make([][]any, n)
	now := time.Now()
	for i, id := range ids {
		var op any
		if opID[i] >= 0 {
			op = opID[i]
		}
		rows[i] = []any{id, pr[i], compID[i], op, now}
	}
	if err := a.upsert(ctx, rows); err != nil {
		return nil, err
	}
	return &Result{Sites: n, Edges: len(edges), Components: nComp, Operators: nOp, Took: time.Since(start)}, nil
}

func (a *Analyzer) upsert(ctx context.Context, rows [][]any) error {
	return pgx.BeginFunc(ctx, a.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE sa_in (site_id bigint, pagerank double precision, component int, operator int, updated_at timestamptz) ON COMMIT DROP`); err != nil {
			return err
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"sa_in"}, []string{"site_id", "pagerank", "component", "operator", "updated_at"}, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
		// Join sites so a site purged mid-run is not re-inserted.
		if _, err := tx.Exec(ctx, `
			INSERT INTO site_analysis (site_id, pagerank, component, operator, updated_at)
			SELECT i.site_id, i.pagerank, i.component, i.operator, i.updated_at
			FROM sa_in i JOIN sites s ON s.id = i.site_id
			ON CONFLICT (site_id) DO UPDATE SET
				pagerank = EXCLUDED.pagerank, component = EXCLUDED.component,
				operator = EXCLUDED.operator, updated_at = EXCLUDED.updated_at`); err != nil {
			return err
		}
		// Drop rows for sites that vanished (older than this run's stamp).
		_, err := tx.Exec(ctx, `DELETE FROM site_analysis WHERE updated_at < (SELECT max(updated_at) FROM site_analysis)`)
		return err
	})
}

// pagerank returns the PageRank of each node (damping 0.85), redistributing
// the mass of dangling nodes uniformly.
func pagerank(n int, edges []edge) []float64 {
	if n == 0 {
		return nil
	}
	const d = 0.85
	outW := make([]float64, n)
	type we struct {
		t int
		w float64
	}
	adj := make([][]we, n)
	for _, e := range edges {
		outW[e.s] += float64(e.w)
		adj[e.s] = append(adj[e.s], we{e.t, float64(e.w)})
	}
	rank := make([]float64, n)
	for i := range rank {
		rank[i] = 1.0 / float64(n)
	}
	next := make([]float64, n)
	for iter := 0; iter < 40; iter++ {
		var dangling float64
		for i := 0; i < n; i++ {
			if outW[i] == 0 {
				dangling += rank[i]
			}
		}
		base := (1-d)/float64(n) + d*dangling/float64(n)
		for i := range next {
			next[i] = base
		}
		for i := 0; i < n; i++ {
			if outW[i] == 0 {
				continue
			}
			share := d * rank[i] / outW[i]
			for _, e := range adj[i] {
				next[e.t] += share * e.w
			}
		}
		rank, next = next, rank
	}
	return rank
}

// components returns a weakly-connected component root per node.
func components(n int, edges []edge) []int {
	uf := newUF(n)
	for _, e := range edges {
		uf.union(e.s, e.t)
	}
	out := make([]int, n)
	for i := range out {
		out[i] = uf.find(i)
	}
	return out
}

// label renumbers roots to dense ids ordered by descending group size. With
// dropSingletons, groups of size 1 get id -1. Returns ids and kept-group count.
func label(n int, root []int, dropSingletons bool) ([]int, int) {
	size := map[int]int{}
	for i := 0; i < n; i++ {
		size[root[i]]++
	}
	type g struct{ r, n int }
	gs := make([]g, 0, len(size))
	for r, c := range size {
		gs = append(gs, g{r, c})
	}
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].n != gs[j].n {
			return gs[i].n > gs[j].n
		}
		return gs[i].r < gs[j].r
	})
	id := map[int]int{}
	next := 0
	for _, x := range gs {
		if dropSingletons && x.n < 2 {
			id[x.r] = -1
			continue
		}
		id[x.r] = next
		next++
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = id[root[i]]
	}
	return out, next
}

type uf struct{ p, r []int }

func newUF(n int) *uf {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &uf{p, make([]int, n)}
}
func (u *uf) find(i int) int {
	for u.p[i] != i {
		u.p[i] = u.p[u.p[i]]
		i = u.p[i]
	}
	return i
}
func (u *uf) union(a, b int) {
	a, b = u.find(a), u.find(b)
	if a == b {
		return
	}
	if u.r[a] < u.r[b] {
		a, b = b, a
	}
	u.p[b] = a
	if u.r[a] == u.r[b] {
		u.r[a]++
	}
}
