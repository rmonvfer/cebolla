// Package explore serves a read-only web UI over the crawl data: overview,
// search, sites, pages, entities, link graph and duplicate clusters.
//
// Safety: stored content is only ever returned as JSON and rendered as
// escaped text. No stored URL becomes a clickable link, and the page runs
// under a strict Content-Security-Policy.
package explore

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"onioncrawler/internal/index"
	"onioncrawler/internal/simhash"
)

//go:embed static
var static embed.FS

type Server struct {
	db  *pgxpool.Pool
	ix  *index.Client
	log *slog.Logger

	mu          sync.Mutex
	clusters    []cluster
	clustersAt  time.Time
	clusterBusy bool
}

// Open connects with every session forced read-only.
func Open(ctx context.Context, dsn string, ix *index.Client, log *slog.Logger) (*Server, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, `SET default_transaction_read_only = on; SET statement_timeout = '30s'`)
		return err
	}
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Server{db: db, ix: ix, log: log}, nil
}

func (s *Server) Close() { s.db.Close() }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	mux.Handle("GET /", http.FileServerFS(sub))
	mux.HandleFunc("GET /api/overview", s.api(s.overview))
	mux.HandleFunc("GET /api/search", s.api(s.search))
	mux.HandleFunc("GET /api/sites", s.api(s.sites))
	mux.HandleFunc("GET /api/site/{id}", s.api(s.site))
	mux.HandleFunc("GET /api/page/{id}", s.api(s.page))
	mux.HandleFunc("GET /api/entities", s.api(s.entities))
	mux.HandleFunc("GET /api/entity", s.api(s.entity))
	mux.HandleFunc("GET /api/graph", s.api(s.graph))
	mux.HandleFunc("GET /api/clusters", s.api(s.clustersAPI))
	return secure(mux)
}

func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'none'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

type apiFunc func(ctx context.Context, r *http.Request) (any, error)

var errNotFound = errors.New("not found")

func (s *Server) api(f apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := f(r.Context(), r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case errors.Is(err, errNotFound), errors.Is(err, pgx.ErrNoRows):
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		case err != nil:
			s.log.Error("explore api", "path", r.URL.Path, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		default:
			json.NewEncoder(w).Encode(v)
		}
	}
}

// rows runs a query and returns each row as a column-name map.
func (s *Server) rows(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	r, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(r, pgx.RowToMap)
	if out == nil {
		out = []map[string]any{}
	}
	return out, err
}

func (s *Server) row(ctx context.Context, q string, args ...any) (map[string]any, error) {
	rs, err := s.rows(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, errNotFound
	}
	return rs[0], nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errNotFound
	}
	return id, nil
}

func intParam(r *http.Request, k string, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(k))
	if err != nil || n < 0 {
		return def
	}
	return min(n, max)
}

// Outcomes where the service answered (same set the store uses for "alive").
const aliveSQL = `('ok','http_error','content_type','too_large','sniff','refused','redirect_offsite','redirect_limit','redirect_blocked')`

// Degree per site from the site_edges view (refreshed by the crawler every 10 minutes).
const degreeSQL = `
	LEFT JOIN (SELECT dst_site_id AS id, count(*) AS indeg FROM site_edges GROUP BY 1) di ON di.id = s.id
	LEFT JOIN (SELECT src_site_id AS id, count(*) AS outdeg FROM site_edges GROUP BY 1) do_ ON do_.id = s.id`

const siteCols = `s.id, s.onion, coalesce(s.title, '') AS title, s.status::text AS status`

// ---- Overview -------------------------------------------------------------

func (s *Server) overview(ctx context.Context, r *http.Request) (any, error) {
	out := map[string]any{}
	var err error
	if out["status"], err = s.rows(ctx, `SELECT status::text AS status, count(*) AS n FROM sites GROUP BY 1 ORDER BY 2 DESC`); err != nil {
		return nil, err
	}
	if out["totals"], err = s.row(ctx, `SELECT
		(SELECT count(*) FROM sites) AS sites,
		(SELECT count(*) FROM pages) AS pages,
		(SELECT count(*) FROM page_versions) AS versions,
		(SELECT count(*) FROM links) AS links,
		(SELECT count(*) FROM entities) AS entities,
		(SELECT count(*) FROM frontier) AS frontier,
		(SELECT count(*) FROM site_edges) AS edges`); err != nil {
		return nil, err
	}
	if out["outcomes"], err = s.rows(ctx, `SELECT outcome, count(*) AS n FROM fetch_log WHERE ts > now() - interval '24 hours' GROUP BY 1 ORDER BY 2 DESC`); err != nil {
		return nil, err
	}
	if out["hubs"], err = s.rows(ctx, `SELECT `+siteCols+`, e.n AS degree FROM
		(SELECT dst_site_id AS id, count(*) AS n FROM site_edges GROUP BY 1 ORDER BY 2 DESC LIMIT 15) e
		JOIN sites s ON s.id = e.id ORDER BY e.n DESC`); err != nil {
		return nil, err
	}
	if out["directories"], err = s.rows(ctx, `SELECT `+siteCols+`, e.n AS degree FROM
		(SELECT src_site_id AS id, count(*) AS n FROM site_edges GROUP BY 1 ORDER BY 2 DESC LIMIT 15) e
		JOIN sites s ON s.id = e.id ORDER BY e.n DESC`); err != nil {
		return nil, err
	}
	if out["recent"], err = s.rows(ctx, `SELECT `+siteCols+`, s.first_seen FROM sites s
		WHERE s.status = 'up' ORDER BY s.first_seen DESC LIMIT 15`); err != nil {
		return nil, err
	}
	out["activity"], err = s.rows(ctx, `SELECT date_trunc('hour', ts) AS t, count(*) AS fetches,
		count(*) FILTER (WHERE outcome = 'ok') AS ok
		FROM fetch_log WHERE ts > now() - interval '48 hours' GROUP BY 1 ORDER BY 1`)
	return out, err
}

// ---- Search ---------------------------------------------------------------

func (s *Server) search(ctx context.Context, r *http.Request) (any, error) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		return map[string]any{"total": 0, "hits": []any{}}, nil
	}
	from := intParam(r, "from", 0, 9900)
	hits, total, err := s.ix.SearchFrom(ctx, q, from, 25)
	if err != nil {
		return nil, err
	}
	return map[string]any{"total": total, "from": from, "hits": hits}, nil
}

// ---- Sites ----------------------------------------------------------------

var siteSorts = map[string]string{
	"indeg":  "coalesce(di.indeg, 0) DESC, s.id",
	"outdeg": "coalesce(do_.outdeg, 0) DESC, s.id",
	"pages":  "s.pages_fetched DESC, s.id",
	"recent": "s.first_seen DESC, s.id",
	"seen":   "s.last_ok DESC NULLS LAST, s.id",
}

func (s *Server) sites(ctx context.Context, r *http.Request) (any, error) {
	qs := r.URL.Query()
	order, ok := siteSorts[qs.Get("sort")]
	if !ok {
		order = siteSorts["indeg"]
	}
	return s.rows(ctx, `SELECT `+siteCols+`, s.first_seen, s.last_ok, s.pages_fetched,
		coalesce(di.indeg, 0) AS indeg, coalesce(do_.outdeg, 0) AS outdeg
		FROM sites s `+degreeSQL+`
		WHERE ($1 = '' OR s.status::text = $1)
		  AND ($2 = '' OR s.title ILIKE '%' || $2 || '%' OR s.onion LIKE lower($2) || '%')
		ORDER BY `+order+` LIMIT 100 OFFSET $3`,
		qs.Get("status"), strings.TrimSpace(qs.Get("q")), intParam(r, "offset", 0, 1_000_000))
}

func (s *Server) site(ctx context.Context, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if out["site"], err = s.row(ctx, `SELECT `+siteCols+`, s.discovered_via, s.first_seen, s.last_seen, s.last_ok,
		s.consecutive_failures, s.next_check_at, coalesce(s.server, '') AS server, s.pages_fetched,
		coalesce(di.indeg, 0) AS indeg, coalesce(do_.outdeg, 0) AS outdeg
		FROM sites s `+degreeSQL+` WHERE s.id = $1`, id); err != nil {
		return nil, err
	}
	if out["uptime"], err = s.rows(ctx, `SELECT ts::date AS day,
		count(*) FILTER (WHERE outcome IN `+aliveSQL+`) AS up, count(*) AS total,
		(array_agg(outcome ORDER BY ts DESC))[1] AS last
		FROM fetch_log WHERE site_id = $1 AND ts > now() - interval '90 days' GROUP BY 1 ORDER BY 1`, id); err != nil {
		return nil, err
	}
	if out["fetches"], err = s.rows(ctx, `SELECT ts, outcome, http_status, latency_ms FROM fetch_log
		WHERE site_id = $1 ORDER BY ts DESC LIMIT 20`, id); err != nil {
		return nil, err
	}
	if out["pages"], err = s.rows(ctx, `SELECT p.id, p.url, p.depth, p.last_fetched, p.last_status,
		coalesce(v.title, '') AS title, length(v.text) AS chars,
		(SELECT count(*) FROM page_versions pv WHERE pv.page_id = p.id) AS versions
		FROM pages p LEFT JOIN page_versions v ON v.id = p.current_version_id
		WHERE p.site_id = $1 ORDER BY p.depth, p.url LIMIT 300`, id); err != nil {
		return nil, err
	}
	// Live (not the view) so a freshly crawled site shows its links at once.
	if out["out"], err = s.rows(ctx, `SELECT `+siteCols+`, x.n FROM
		(SELECT l.dst_site_id AS id, count(*) AS n FROM links l JOIN pages p ON p.id = l.src_page_id
		 WHERE p.site_id = $1 AND l.dst_site_id IS NOT NULL AND l.dst_site_id <> $1 GROUP BY 1) x
		JOIN sites s ON s.id = x.id ORDER BY x.n DESC, s.id LIMIT 300`, id); err != nil {
		return nil, err
	}
	if out["in"], err = s.rows(ctx, `SELECT `+siteCols+`, x.n FROM
		(SELECT p.site_id AS id, count(*) AS n FROM links l JOIN pages p ON p.id = l.src_page_id
		 WHERE l.dst_site_id = $1 AND p.site_id <> $1 GROUP BY 1) x
		JOIN sites s ON s.id = x.id ORDER BY x.n DESC, s.id LIMIT 300`, id); err != nil {
		return nil, err
	}
	if out["clearnet"], err = s.rows(ctx, `SELECT l.dst_url AS url, count(*) AS n FROM links l
		JOIN pages p ON p.id = l.src_page_id WHERE p.site_id = $1 AND NOT l.is_onion
		GROUP BY 1 ORDER BY 2 DESC LIMIT 100`, id); err != nil {
		return nil, err
	}
	if out["entities"], err = s.rows(ctx, `SELECT e.kind, e.value,
		(SELECT count(DISTINCT x.site_id) FROM entities x WHERE x.kind = e.kind AND x.value = e.value) AS sites
		FROM (SELECT DISTINCT kind, value FROM entities WHERE site_id = $1) e
		ORDER BY sites DESC, e.kind, e.value LIMIT 300`, id); err != nil {
		return nil, err
	}
	if out["shared"], err = s.rows(ctx, `SELECT `+siteCols+`, count(DISTINCT (x.kind, x.value)) AS n,
		string_agg(DISTINCT x.kind, ',') AS kinds
		FROM entities x
		JOIN (SELECT DISTINCT kind, value FROM entities WHERE site_id = $1 AND kind <> 'onion') m USING (kind, value)
		JOIN sites s ON s.id = x.site_id
		WHERE x.site_id <> $1 GROUP BY s.id ORDER BY n DESC LIMIT 50`, id); err != nil {
		return nil, err
	}
	out["similar"], err = s.rows(ctx, `
		WITH me AS (
			SELECT v.simhash FROM pages p JOIN page_versions v ON v.id = p.current_version_id
			WHERE p.site_id = $1 AND p.depth = 0 ORDER BY p.id LIMIT 1)
		SELECT `+siteCols+`, bit_count((v.simhash # me.simhash)::bit(64)) AS distance
		FROM me, pages p
		JOIN page_versions v ON v.id = p.current_version_id
		JOIN sites s ON s.id = p.site_id
		WHERE p.depth = 0 AND p.site_id <> $1 AND length(v.text) >= 200
		  AND bit_count((v.simhash # me.simhash)::bit(64)) <= 6
		ORDER BY distance, s.id LIMIT 50`, id)
	return out, err
}

// ---- Pages ----------------------------------------------------------------

const maxText = 300_000

func (s *Server) page(ctx context.Context, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	p, err := s.row(ctx, `SELECT p.id, p.site_id, s.onion, coalesce(s.title, '') AS site_title, p.url, p.depth,
		p.first_fetched, p.last_fetched, p.last_status, p.current_version_id
		FROM pages p JOIN sites s ON s.id = p.site_id WHERE p.id = $1`, id)
	if err != nil {
		return nil, err
	}
	out["page"] = p
	if out["versions"], err = s.rows(ctx, `SELECT id, fetched_at, http_status, coalesce(title, '') AS title,
		length(text) AS chars, lpad(to_hex(simhash), 16, '0') AS simhash
		FROM page_versions WHERE page_id = $1 ORDER BY fetched_at DESC`, id); err != nil {
		return nil, err
	}
	vid := int64(intParam(r, "v", 0, 1<<62))
	if vid == 0 {
		if cv, ok := p["current_version_id"].(int64); ok {
			vid = cv
		}
	}
	if out["version"], err = s.row(ctx, `SELECT id, fetched_at, coalesce(title, '') AS title,
		left(text, $3) AS text, length(text) > $3 AS truncated
		FROM page_versions WHERE id = $1 AND page_id = $2`, vid, id, maxText); err != nil && !errors.Is(err, errNotFound) {
		return nil, err
	}
	if out["links"], err = s.rows(ctx, `SELECT l.dst_url AS url, coalesce(l.anchor_text, '') AS anchor, l.is_onion,
		l.dst_site_id AS site_id, coalesce(s.title, '') AS site_title
		FROM links l LEFT JOIN sites s ON s.id = l.dst_site_id WHERE l.src_page_id = $1 LIMIT 2000`, id); err != nil {
		return nil, err
	}
	out["entities"], err = s.rows(ctx, `SELECT kind, value FROM entities WHERE page_version_id = $1 ORDER BY kind, value`, vid)
	return out, err
}

// ---- Entities -------------------------------------------------------------

func (s *Server) entities(ctx context.Context, r *http.Request) (any, error) {
	return s.rows(ctx, `SELECT kind, value, count(DISTINCT site_id) AS sites, count(*) AS mentions
		FROM entities WHERE ($1 = '' OR kind = $1) AND ($2 = '' OR value ILIKE '%' || $2 || '%')
		GROUP BY 1, 2 ORDER BY sites DESC, mentions DESC, value LIMIT 200 OFFSET $3`,
		r.URL.Query().Get("kind"), strings.TrimSpace(r.URL.Query().Get("q")), intParam(r, "offset", 0, 1_000_000))
}

func (s *Server) entity(ctx context.Context, r *http.Request) (any, error) {
	kind, value := r.URL.Query().Get("kind"), r.URL.Query().Get("value")
	return s.rows(ctx, `SELECT `+siteCols+`, count(*) AS pages, min(v.fetched_at) AS first_seen,
		max(v.fetched_at) AS last_seen, min(p.id) AS page_id
		FROM entities e
		JOIN sites s ON s.id = e.site_id
		JOIN page_versions v ON v.id = e.page_version_id
		JOIN pages p ON p.id = v.page_id
		WHERE e.kind = $1 AND e.value = $2
		GROUP BY s.id ORDER BY pages DESC, s.id LIMIT 500`, kind, value)
}

// ---- Graph ----------------------------------------------------------------

type gnode struct {
	ID     int64  `json:"id"`
	Onion  string `json:"onion"`
	Title  string `json:"title"`
	Status string `json:"status"`
	In     int64  `json:"indeg"`
	Out    int64  `json:"outdeg"`
}

type gedge struct {
	S int64 `json:"s"`
	T int64 `json:"t"`
	W int64 `json:"w"`
}

// graph returns either the neighbourhood of ?site= (up to ?hops=1|2) or the
// ?top= best-connected sites, with the edges among the chosen nodes.
func (s *Server) graph(ctx context.Context, r *http.Request) (any, error) {
	maxNodes := intParam(r, "max", 250, 1500)
	var ids []int64
	if center := int64(intParam(r, "site", 0, 1<<62)); center > 0 {
		ids = []int64{center}
		seen := map[int64]bool{center: true}
		ring := []int64{center}
		for range intParam(r, "hops", 1, 2) {
			rows, err := s.db.Query(ctx, `SELECT src_site_id, dst_site_id, n_links FROM site_edges
				WHERE src_site_id = ANY($1) OR dst_site_id = ANY($1)`, ring)
			if err != nil {
				return nil, err
			}
			weight := map[int64]int64{}
			for rows.Next() {
				var a, b, w int64
				if err := rows.Scan(&a, &b, &w); err != nil {
					return nil, err
				}
				for _, x := range [2]int64{a, b} {
					if !seen[x] {
						weight[x] += w
					}
				}
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			next := make([]int64, 0, len(weight))
			for x := range weight {
				next = append(next, x)
			}
			sort.Slice(next, func(i, j int) bool { return weight[next[i]] > weight[next[j]] })
			ring = ring[:0]
			for _, x := range next {
				if len(ids) >= maxNodes {
					break
				}
				seen[x] = true
				ids = append(ids, x)
				ring = append(ring, x)
			}
			if len(ring) == 0 {
				break
			}
		}
	} else {
		rows, err := s.db.Query(ctx, `SELECT id FROM (
			SELECT src_site_id AS id FROM site_edges UNION ALL SELECT dst_site_id FROM site_edges) x
			GROUP BY id ORDER BY count(*) DESC LIMIT $1`, maxNodes)
		if err != nil {
			return nil, err
		}
		if ids, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
			return nil, err
		}
	}

	nrows, err := s.db.Query(ctx, `SELECT s.id, s.onion, coalesce(s.title, ''), s.status::text,
		coalesce(di.indeg, 0), coalesce(do_.outdeg, 0)
		FROM sites s `+degreeSQL+` WHERE s.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	nodes, err := pgx.CollectRows(nrows, pgx.RowToStructByPos[gnode])
	if err != nil {
		return nil, err
	}
	erows, err := s.db.Query(ctx, `SELECT src_site_id, dst_site_id, n_links FROM site_edges
		WHERE src_site_id = ANY($1) AND dst_site_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	edges, err := pgx.CollectRows(erows, pgx.RowToStructByPos[gedge])
	if err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = []gnode{}
	}
	if edges == nil {
		edges = []gedge{}
	}
	return map[string]any{"nodes": nodes, "edges": edges}, nil
}

// ---- Clusters -------------------------------------------------------------

type cluster struct {
	Size  int              `json:"size"`
	Exact bool             `json:"exact"` // every homepage has identical text
	Sites []map[string]any `json:"sites"`
}

// clustersAPI groups sites whose homepages are near-duplicates (SimHash
// distance <= 3). Computing it touches every homepage, so the result is
// cached for 10 minutes and rebuilt in the background.
func (s *Server) clustersAPI(ctx context.Context, r *http.Request) (any, error) {
	s.mu.Lock()
	stale := time.Since(s.clustersAt) > 10*time.Minute
	if stale && !s.clusterBusy {
		s.clusterBusy = true
		go s.buildClusters()
	}
	cl, at, busy := s.clusters, s.clustersAt, s.clusterBusy
	s.mu.Unlock()
	if cl == nil {
		cl = []cluster{}
	}
	return map[string]any{"clusters": cl, "computed_at": at, "building": busy}, nil
}

func (s *Server) buildClusters() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cl, err := s.computeClusters(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterBusy = false
	if err != nil {
		s.log.Error("clusters", "err", err)
		return
	}
	s.clusters, s.clustersAt = cl, time.Now()
}

func (s *Server) computeClusters(ctx context.Context) ([]cluster, error) {
	rows, err := s.db.Query(ctx, `SELECT s.id, s.onion, coalesce(s.title, ''), s.status::text, v.simhash, encode(v.text_hash, 'hex')
		FROM pages p JOIN page_versions v ON v.id = p.current_version_id JOIN sites s ON s.id = p.site_id
		WHERE p.depth = 0 AND length(v.text) >= 200`)
	if err != nil {
		return nil, err
	}
	type home struct {
		id                         int64
		onion, title, status, hash string
		sh                         uint64
	}
	var hs []home
	for rows.Next() {
		var h home
		var sh int64
		if err := rows.Scan(&h.id, &h.onion, &h.title, &h.status, &sh, &h.hash); err != nil {
			return nil, err
		}
		h.sh = uint64(sh)
		hs = append(hs, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Union-find over candidate pairs. Distance <= 3 over 64 bits means at
	// least one of four 16-bit bands is identical, so only pages sharing a
	// band value are compared.
	parent := make([]int, len(hs))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for band := range 4 {
		buckets := map[uint64][]int{}
		for i, h := range hs {
			k := (h.sh >> (16 * band)) & 0xffff
			buckets[k] = append(buckets[k], i)
		}
		for _, b := range buckets {
			if len(b) < 2 || len(b) > 2000 { // huge buckets are degenerate (e.g. empty-ish pages)
				continue
			}
			for i := 0; i < len(b); i++ {
				for j := i + 1; j < len(b); j++ {
					if simhash.Distance(hs[b[i]].sh, hs[b[j]].sh) <= 3 {
						if a, c := find(b[i]), find(b[j]); a != c {
							parent[a] = c
						}
					}
				}
			}
		}
	}
	groups := map[int][]int{}
	for i := range hs {
		groups[find(i)] = append(groups[find(i)], i)
	}
	var out []cluster
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		c := cluster{Size: len(g), Exact: true}
		for _, i := range g {
			if hs[i].hash != hs[g[0]].hash {
				c.Exact = false
			}
			if len(c.Sites) < 100 {
				c.Sites = append(c.Sites, map[string]any{"id": hs[i].id, "onion": hs[i].onion, "title": hs[i].title, "status": hs[i].status})
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out, nil
}

// Serve runs the HTTP server until ctx ends.
func (s *Server) Serve(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	s.log.Info("explorer listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("explorer: %w", err)
	}
	return nil
}
