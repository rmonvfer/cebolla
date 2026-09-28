// Package store is the Postgres layer: frontier and per-site scheduling,
// pages and their versions, liveness, blocklist and purging.
package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/klauspost/compress/zstd"

	"onioncrawler/internal/entities"
	"onioncrawler/internal/onion"
	"onioncrawler/migrations"
)

type Store struct{ db *pgxpool.Pool }

var zenc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	for i := 0; ; i++ { // postgres may still be starting
		if err = db.Ping(ctx); err == nil || i == 30 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() { s.db.Close() }

// Migrate applies embedded migrations not applied yet, under an advisory
// lock so the crawler and seedsync can start together.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7331)`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(7331)`)
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, n := range names {
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, n).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, _ := migrations.FS.ReadFile(n)
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", n, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, n); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ---- Blocklist -------------------------------------------------------------

func (s *Store) BlocklistHashes(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.Query(ctx, `SELECT md5 FROM blocklist`)
	if err != nil {
		return nil, err
	}
	m := map[string]struct{}{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		m[h] = struct{}{}
	}
	return m, rows.Err()
}

// ReplaceBlocklist swaps all entries of source for hashes, in one transaction.
func (s *Store) ReplaceBlocklist(ctx context.Context, source string, hashes []string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM blocklist WHERE source=$1`, source); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE bl_in (md5 text) ON COMMIT DROP`); err != nil {
			return err
		}
		rows := make([][]any, len(hashes))
		for i, h := range hashes {
			rows[i] = []any{h}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"bl_in"}, []string{"md5"}, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO blocklist (md5, source) SELECT DISTINCT md5, $1 FROM bl_in ON CONFLICT DO NOTHING`, source)
		return err
	})
}

type SiteRef struct {
	ID    int64
	Onion string
}

// BlockedSites lists stored sites whose address is on the blocklist (e.g.
// after a blocklist update), so they can be purged.
func (s *Store) BlockedSites(ctx context.Context) ([]SiteRef, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, onion FROM sites
		WHERE md5(onion || '.onion') IN (SELECT md5 FROM blocklist)
		   OR md5(onion) IN (SELECT md5 FROM blocklist)`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SiteRef])
}

// PurgeSite deletes everything stored for a site and blocklists it locally.
// Everything cascades from the sites row; shared blobs are collected after.
func (s *Store) PurgeSite(ctx context.Context, siteID int64, addr, reason string) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for _, h := range onion.BlockHashes(addr) {
			if _, err := tx.Exec(ctx, `INSERT INTO blocklist (md5, source, reason) VALUES ($1, 'local', $2) ON CONFLICT DO NOTHING`, h, reason); err != nil {
				return err
			}
		}
		var hashes [][]byte
		rows, err := tx.Query(ctx, `SELECT DISTINCT v.html_hash FROM page_versions v JOIN pages p ON p.id = v.page_id WHERE p.site_id = $1`, siteID)
		if err != nil {
			return err
		}
		if hashes, err = pgx.CollectRows(rows, pgx.RowTo[[]byte]); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sites WHERE id = $1`, siteID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM blobs b WHERE b.hash = ANY($1) AND NOT EXISTS (SELECT 1 FROM page_versions v WHERE v.html_hash = b.hash)`, hashes)
		return err
	})
	if err != nil {
		return err
	}
	// Deleted rows stay in the heap until vacuumed; reclaim them now so the
	// space is reused (and overwritten) as soon as possible.
	for _, t := range []string{"page_versions", "blobs", "pages", "links", "entities", "frontier", "fetch_log"} {
		if _, err := s.db.Exec(ctx, "VACUUM "+t); err != nil {
			return fmt.Errorf("vacuum %s: %w", t, err)
		}
	}
	return nil
}

// ---- Discovery -------------------------------------------------------------

type Discovered struct {
	Onion    string
	URL      string // canonical
	Depth    int
	Priority int
}

// Enqueue records sites and queues URLs that were never fetched, respecting
// the per-site page cap. Callers must have applied the blocklist and filter.
func (s *Store) Enqueue(ctx context.Context, via string, items []Discovered, pageCap int) error {
	if len(items) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, it := range items {
		b.Queue(`
			WITH ins AS (
				INSERT INTO sites (onion, discovered_via) VALUES ($1, $2)
				ON CONFLICT (onion) DO NOTHING RETURNING id, 0 AS pages_fetched),
			sid AS (
				SELECT id, pages_fetched FROM ins
				UNION ALL SELECT id, pages_fetched FROM sites WHERE onion = $1
				LIMIT 1)
			INSERT INTO frontier (url_hash, url, site_id, depth, priority)
			SELECT $3, $4, sid.id, $5, $6 FROM sid
			WHERE sid.pages_fetched < $7
			  AND NOT EXISTS (SELECT 1 FROM pages WHERE url_hash = $3)
			ON CONFLICT (url_hash) DO NOTHING`,
			it.Onion, via, onion.URLHash(it.URL), it.URL, it.Depth, it.Priority, pageCap)
	}
	return s.db.SendBatch(ctx, b).Close()
}

// ---- Scheduling ------------------------------------------------------------

type Job struct {
	SiteID   int64
	Onion    string
	URLHash  []byte
	URL      string
	Depth    int
	Attempts int
}

// Claim leases up to n due sites (so no other worker touches them) and
// returns the next frontier URL of each. Politeness lives here: a site is due
// only when its lease has expired and next_allowed_at has passed. Claiming in
// batches keeps this query, which scans every site, off the per-fetch path.
func (s *Store) Claim(ctx context.Context, lease time.Duration, n int) ([]*Job, error) {
	rows, err := s.db.Query(ctx, `
		WITH c AS (
			SELECT s.id FROM sites s
			WHERE s.next_allowed_at <= now() AND s.lease_until <= now() AND s.status <> 'auth_gated'
			  AND EXISTS (SELECT 1 FROM frontier f WHERE f.site_id = s.id AND f.next_fetch_at <= now())
			ORDER BY (s.pages_fetched = 0) DESC, s.next_allowed_at
			LIMIT $2 FOR UPDATE SKIP LOCKED),
		l AS (
			UPDATE sites SET lease_until = now() + $1::interval FROM c
			WHERE sites.id = c.id RETURNING sites.id, sites.onion)
		SELECT l.id, l.onion, f.url_hash, f.url, f.depth, f.attempts
		FROM l LEFT JOIN LATERAL (
			SELECT url_hash, url, depth, attempts FROM frontier
			WHERE site_id = l.id AND next_fetch_at <= now()
			ORDER BY priority, next_fetch_at LIMIT 1) f ON true`, lease, n)
	if err != nil {
		return nil, err
	}
	var jobs []*Job
	var empty []int64
	for rows.Next() {
		var j Job
		var url *string
		var depth, attempts *int
		if err := rows.Scan(&j.SiteID, &j.Onion, &j.URLHash, &url, &depth, &attempts); err != nil {
			return nil, err
		}
		if url == nil { // raced with a purge or a finish: nothing left to fetch
			empty = append(empty, j.SiteID)
			continue
		}
		j.URL, j.Depth, j.Attempts = *url, *depth, *attempts
		jobs = append(jobs, &j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range empty {
		s.Release(ctx, id, 0)
	}
	return jobs, nil
}

// Release ends the lease; the site is due again after delay.
func (s *Store) Release(ctx context.Context, siteID int64, delay time.Duration) error {
	_, err := s.db.Exec(ctx, `UPDATE sites SET lease_until = 'epoch', next_allowed_at = now() + $2::interval WHERE id = $1`, siteID, delay)
	return err
}

// Finish removes the frontier entry, or reschedules it for a retry with
// exponential backoff (30m, 1h, 2h) when retry is set.
func (s *Store) Finish(ctx context.Context, j *Job, retry bool) error {
	if retry && j.Attempts < 3 {
		_, err := s.db.Exec(ctx, `UPDATE frontier SET attempts = attempts + 1,
			next_fetch_at = now() + interval '30 minutes' * power(2, attempts) WHERE url_hash = $1`, j.URLHash)
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM frontier WHERE url_hash = $1`, j.URLHash)
	return err
}

// ---- Liveness --------------------------------------------------------------

// Outcomes where the onion service itself answered: it is alive.
var aliveOutcomes = map[string]bool{
	"ok": true, "http_error": true, "content_type": true, "too_large": true, "sniff": true,
	"refused": true, "redirect_offsite": true, "redirect_limit": true, "redirect_blocked": true,
}

// RecordFetch logs the attempt and moves the site through the liveness state
// machine: up (recheck daily), flaky (6h), down (1h doubling to 7d), dead
// (no answer for 30 days; monthly), auth_gated (never crawled, monthly).
func (s *Store) RecordFetch(ctx context.Context, j *Job, outcome string, httpStatus, latencyMS, bytes int, proxy string) error {
	alive := aliveOutcomes[outcome]
	auth := outcome == "auth_required"
	b := &pgx.Batch{}
	b.Queue(`INSERT INTO fetch_log (site_id, url_hash, outcome, http_status, latency_ms, bytes, proxy)
		VALUES ($1, $2, $3, NULLIF($4, 0), $5, $6, $7)`, j.SiteID, j.URLHash, outcome, httpStatus, latencyMS, bytes, proxy)
	b.Queue(`
		UPDATE sites SET
			last_seen = now(),
			last_ok = CASE WHEN $2 THEN now() ELSE last_ok END,
			consecutive_failures = CASE WHEN $2 THEN 0 ELSE consecutive_failures + 1 END,
			status = CASE
				WHEN $2 THEN 'up'
				WHEN $3 THEN 'auth_gated'
				WHEN coalesce(last_ok, first_seen) < now() - interval '30 days' THEN 'dead'
				WHEN $4 = 'desc_not_found' THEN 'down'
				ELSE 'flaky' END::site_status,
			next_check_at = now() + CASE
				WHEN $2 THEN interval '24 hours'
				WHEN $3 THEN interval '30 days'
				WHEN coalesce(last_ok, first_seen) < now() - interval '30 days' THEN interval '30 days'
				WHEN $4 = 'desc_not_found' THEN least(interval '1 hour' * power(2, least(consecutive_failures, 8)), interval '7 days')
				ELSE interval '6 hours' END
		WHERE id = $1`, j.SiteID, alive, auth, outcome)
	return s.db.SendBatch(ctx, b).Close()
}

// ScheduleRechecks queues the homepage of every site whose liveness check is
// due. Returns how many were queued.
func (s *Store) ScheduleRechecks(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		WITH due AS (
			UPDATE sites SET next_check_at = now() + interval '1 hour'
			WHERE next_check_at <= now() AND status <> 'auth_gated'
			RETURNING id, 'http://' || onion || '.onion/' AS url)
		INSERT INTO frontier (url_hash, url, site_id, depth, priority)
		SELECT substring(sha256(convert_to(url, 'UTF8')) FROM 1 FOR 16), url, id, 0, 1 FROM due
		ON CONFLICT (url_hash) DO UPDATE SET
			next_fetch_at = least(frontier.next_fetch_at, now()),
			priority = least(frontier.priority, 1)`)
	return tag.RowsAffected(), err
}

// ---- Pages -----------------------------------------------------------------

type LinkOut struct {
	URL      string
	Anchor   string
	DstOnion string // "" for clearnet/invalid (recorded, never fetched)
}

type PageData struct {
	Job         *Job
	Status      int
	ContentType string
	Server      string
	Title       string
	Text        string
	HTML        []byte // sanitized
	SimHash     uint64
	Links       []LinkOut
	Entities    []entities.Entity
}

// SavePage stores a fetched page. A new version row is written only when the
// text or HTML changed; links and entities follow the current version.
func (s *Store) SavePage(ctx context.Context, d *PageData) (pageID int64, err error) {
	j := d.Job
	htmlHash := sha256.Sum256(d.HTML)
	textHash := sha256.Sum256([]byte(d.Text))
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO blobs (hash, zstd) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			htmlHash[:], zenc.EncodeAll(d.HTML, nil)); err != nil {
			return err
		}
		var curVersion *int64
		var isNew bool
		if err := tx.QueryRow(ctx, `
			INSERT INTO pages (site_id, url, url_hash, depth, last_status) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (url_hash) DO UPDATE SET last_fetched = now(), last_status = EXCLUDED.last_status
			RETURNING id, current_version_id, (xmax = 0)`, j.SiteID, j.URL, j.URLHash, j.Depth, d.Status,
		).Scan(&pageID, &curVersion, &isNew); err != nil {
			return err
		}
		if curVersion != nil {
			var same bool
			if err := tx.QueryRow(ctx, `SELECT text_hash = $2 AND html_hash = $3 FROM page_versions WHERE id = $1`,
				*curVersion, textHash[:], htmlHash[:]).Scan(&same); err != nil {
				return err
			}
			if same {
				return nil
			}
		}
		var vid int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO page_versions (page_id, http_status, content_type, text_hash, simhash, title, text, html_hash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			pageID, d.Status, d.ContentType, textHash[:], int64(d.SimHash), d.Title, d.Text, htmlHash[:],
		).Scan(&vid); err != nil {
			return err
		}
		b := &pgx.Batch{}
		b.Queue(`UPDATE pages SET current_version_id = $2 WHERE id = $1`, pageID, vid)
		b.Queue(`DELETE FROM links WHERE src_page_id = $1`, pageID)
		for _, l := range d.Links {
			b.Queue(`INSERT INTO links (src_page_id, dst_site_id, dst_url, anchor_text, is_onion)
				VALUES ($1, (SELECT id FROM sites WHERE onion = NULLIF($2, '')), $3, $4, $2 <> '')`,
				pageID, l.DstOnion, l.URL, l.Anchor)
		}
		for _, e := range d.Entities {
			b.Queue(`INSERT INTO entities (page_version_id, site_id, kind, value) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				vid, j.SiteID, e.Kind, e.Value)
		}
		if isNew {
			b.Queue(`UPDATE sites SET pages_fetched = pages_fetched + 1 WHERE id = $1`, j.SiteID)
		}
		if j.Depth == 0 && strings.HasSuffix(j.URL, ".onion/") {
			b.Queue(`UPDATE sites SET title = NULLIF($2, ''), server = NULLIF($3, '') WHERE id = $1`, j.SiteID, d.Title, d.Server)
		}
		return tx.SendBatch(ctx, b).Close()
	})
	return pageID, err
}

// ---- Indexing --------------------------------------------------------------

type IndexDoc struct {
	PageID    int64
	SiteID    int64
	Onion     string
	URL       string
	Title     string
	Text      string
	FetchedAt time.Time
	VersionID int64
	SimHash   int64
}

func (s *Store) PagesToIndex(ctx context.Context, limit int) ([]IndexDoc, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.site_id, s.onion, p.url, coalesce(v.title, ''), v.text, v.fetched_at, v.id, v.simhash
		FROM pages p
		JOIN page_versions v ON v.id = p.current_version_id
		JOIN sites s ON s.id = p.site_id
		WHERE p.current_version_id IS DISTINCT FROM p.indexed_version_id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[IndexDoc])
}

func (s *Store) MarkIndexed(ctx context.Context, docs []IndexDoc) error {
	b := &pgx.Batch{}
	for _, d := range docs {
		b.Queue(`UPDATE pages SET indexed_version_id = $2 WHERE id = $1`, d.PageID, d.VersionID)
	}
	return s.db.SendBatch(ctx, b).Close()
}

// ---- Gauges ----------------------------------------------------------------

type Gauges struct {
	SitesByStatus  map[string]int64
	FrontierTotal  int64
	FrontierDue    int64
	Pages          int64
	BlocklistLocal int64
}

func (s *Store) Gauges(ctx context.Context) (*Gauges, error) {
	g := &Gauges{SitesByStatus: map[string]int64{}}
	rows, err := s.db.Query(ctx, `SELECT status::text, count(*) FROM sites GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st string
		var n int64
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		g.SitesByStatus[st] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	err = s.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM frontier),
		(SELECT count(*) FROM frontier WHERE next_fetch_at <= now()),
		(SELECT count(*) FROM pages),
		(SELECT count(*) FROM blocklist WHERE source = 'local')`).Scan(&g.FrontierTotal, &g.FrontierDue, &g.Pages, &g.BlocklistLocal)
	return g, err
}

// BlocklistCount returns the number of entries from source.
func (s *Store) BlocklistCount(ctx context.Context, source string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM blocklist WHERE source = $1`, source).Scan(&n)
	return n, err
}
