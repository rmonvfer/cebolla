// Package crawl runs the crawl: workers claim a due site, fetch one page,
// filter it, store it and queue its links. Background loops keep the index,
// liveness rechecks, blocklist and gauges up to date.
package crawl

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"sync"
	"time"

	"onioncrawler/internal/entities"
	"onioncrawler/internal/fetch"
	"onioncrawler/internal/filter"
	"onioncrawler/internal/index"
	"onioncrawler/internal/metrics"
	"onioncrawler/internal/onion"
	"onioncrawler/internal/report"
	"onioncrawler/internal/sanitize"
	"onioncrawler/internal/simhash"
	"onioncrawler/internal/store"
)

type Config struct {
	Workers      int
	PerSiteDelay time.Duration // pause between two requests to the same site
	MaxDepth     int
	PageCap      int // max pages stored per site
	MaxLinks     int // max links taken from one page
	Lease        time.Duration
}

type Crawler struct {
	cfg  Config
	st   *store.Store
	ix   *index.Client
	f    *fetch.Fetcher
	pool *fetch.Pool
	flt  *filter.Filter
	bl   *Blocklist
	rep  *report.Queue
	log  *slog.Logger
}

// Outcomes worth retrying later: the network or the service hiccupped.
var transient = map[string]bool{
	"timeout": true, "conn_error": true, "intro_failed": true, "rend_failed": true,
	"intro_timeout": true, "socks_error": true, "desc_invalid": true,
}

func New(cfg Config, st *store.Store, ix *index.Client, fcfg fetch.Config, pool *fetch.Pool,
	flt *filter.Filter, bl *Blocklist, rep *report.Queue, log *slog.Logger) *Crawler {
	c := &Crawler{cfg: cfg, st: st, ix: ix, pool: pool, flt: flt, bl: bl, rep: rep, log: log}
	c.f = fetch.New(fcfg, pool, c.guard)
	return c
}

// guard runs before every request, including each redirect hop.
func (c *Crawler) guard(u *url.URL, addr string) error {
	if c.bl.Blocked(addr) {
		metrics.BlocklistSkips.WithLabelValues("fetch").Inc()
		return &fetch.Error{Outcome: "blocked"}
	}
	if c.flt.Match(u.String()) {
		metrics.FilterHits.WithLabelValues("url").Inc()
		return &fetch.Error{Outcome: "filtered"}
	}
	return nil
}

// Run blocks until ctx is cancelled.
func (c *Crawler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	loops := []struct {
		every time.Duration
		fn    func(context.Context) error
		name  string
	}{
		{5 * time.Second, c.indexOnce, "index"},
		{time.Minute, c.recheckOnce, "liveness"},
		{5 * time.Minute, c.blocklistOnce, "blocklist"},
		{30 * time.Second, c.gaugesOnce, "gauges"},
		{time.Minute, c.poolOnce, "pool"},
	}
	for _, l := range loops {
		wg.Go(func() {
			t := time.NewTicker(l.every)
			defer t.Stop()
			for {
				if err := l.fn(ctx); err != nil && ctx.Err() == nil {
					metrics.Errors.WithLabelValues(l.name).Inc()
					c.log.Error("loop failed", "loop", l.name, "err", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		})
	}
	jobs := make(chan *store.Job, claimBatch)
	wg.Go(func() { c.dispatch(ctx, jobs) })
	for range c.cfg.Workers {
		wg.Go(func() { c.worker(ctx, jobs) })
	}
	wg.Wait()
}

// claimBatch is how many sites the dispatcher leases per query.
const claimBatch = 64

// dispatch leases due sites in batches and hands them to the workers. It only
// claims when the queue is running low, so jobs never wait long enough in the
// channel for their lease to matter.
func (c *Crawler) dispatch(ctx context.Context, jobs chan<- *store.Job) {
	defer close(jobs)
	for ctx.Err() == nil {
		if len(jobs) > claimBatch/4 {
			sleep(ctx, 200*time.Millisecond)
			continue
		}
		batch, err := c.st.Claim(ctx, c.cfg.Lease, claimBatch-len(jobs))
		if err != nil {
			if ctx.Err() == nil {
				metrics.Errors.WithLabelValues("claim").Inc()
				c.log.Error("claim", "err", err)
			}
			sleep(ctx, 5*time.Second)
			continue
		}
		if len(batch) == 0 {
			sleep(ctx, 2*time.Second+rand.N(2*time.Second))
			continue
		}
		for _, j := range batch {
			select {
			case jobs <- j:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (c *Crawler) worker(ctx context.Context, jobs <-chan *store.Job) {
	for job := range jobs {
		metrics.Workers.Inc()
		delay := c.process(ctx, job)
		metrics.Workers.Dec()
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := c.st.Release(rctx, job.SiteID, delay); err != nil {
			c.log.Error("release", "err", err)
		}
		cancel()
	}
}

// process fetches one job and returns how long the site should rest.
func (c *Crawler) process(ctx context.Context, job *store.Job) time.Duration {
	u, addr, ok := onion.Canonical(job.URL, nil)
	if !ok || addr != job.Onion {
		c.st.Finish(ctx, job, false)
		return 0
	}
	start := time.Now()
	res, err := c.f.Fetch(ctx, u, addr)
	if ctx.Err() != nil {
		return 0 // shutting down: leave the job for next time
	}
	outcome, status, nbytes := "ok", 0, 0
	var fe *fetch.Error
	switch {
	case errors.As(err, &fe):
		outcome = fe.Outcome
	case err != nil:
		outcome = "conn_error"
	}
	if res != nil {
		status, nbytes = res.Status, len(res.Body)
		if err == nil && (status < 200 || status > 299) {
			outcome = "http_error"
		}
	}
	metrics.Fetches.WithLabelValues(outcome, c.f.Proxy(addr)).Inc()
	metrics.FetchDuration.WithLabelValues(outcome).Observe(time.Since(start).Seconds())

	switch outcome {
	case "blocked": // the site itself is on a blocklist now
		c.purge(ctx, job.SiteID, addr, "blocklist")
		return 0
	case "filtered", "redirect_filtered": // its URL, or where it redirects, matches the filter
		c.purge(ctx, job.SiteID, addr, "filter")
		return 0
	}

	if err := c.st.RecordFetch(ctx, job, outcome, status, int(time.Since(start).Milliseconds()), nbytes, c.f.Proxy(addr)); err != nil {
		metrics.Errors.WithLabelValues("db").Inc()
		c.log.Error("record fetch", "err", err)
	}
	if outcome == "ok" && res.Body != nil {
		metrics.Bytes.Add(float64(nbytes))
		if purged := c.handlePage(ctx, job, res); purged {
			return 0
		}
	}
	if err := c.st.Finish(ctx, job, transient[outcome]); err != nil {
		c.log.Error("finish", "err", err)
	}
	if outcome == "ok" || outcome == "http_error" {
		return c.cfg.PerSiteDelay
	}
	return 4 * c.cfg.PerSiteDelay // back off a struggling service
}

// handlePage sanitizes, filters and stores a page. It reports whether the
// site was purged instead.
func (c *Crawler) handlePage(ctx context.Context, job *store.Job, res *fetch.Result) bool {
	page, err := sanitize.Process(res.Body, res.ContentType, res.URL)
	if err != nil {
		metrics.Errors.WithLabelValues("parse").Inc()
		c.log.Error("parse", "site_id", job.SiteID, "content_type", res.ContentType, "err", err)
		return false
	}
	// The filter sees everything that could be stored: title, text, URL and
	// every link with its anchor. A hit anywhere discards the whole site.
	texts := make([]string, 0, 3+2*len(page.Links))
	texts = append(texts, page.Title, page.Text, res.URL.String())
	for _, l := range page.Links {
		texts = append(texts, l.URL, l.Anchor)
	}
	if c.flt.Match(texts...) {
		metrics.FilterHits.WithLabelValues("page").Inc()
		c.purge(ctx, job.SiteID, job.Onion, "filter")
		return true
	}

	var outs []store.LinkOut
	var disc []store.Discovered
	seen := map[string]bool{}
	queue := func(d store.Discovered) {
		if !seen[d.URL] {
			seen[d.URL] = true
			disc = append(disc, d)
		}
	}
	for i, l := range page.Links {
		if i >= c.cfg.MaxLinks {
			break
		}
		cu, a, ok := onion.Canonical(l.URL, page.Base)
		if !ok {
			// Clearnet or malformed: recorded for the link graph, never fetched.
			if abs, err := page.Base.Parse(l.URL); err == nil && (abs.Scheme == "http" || abs.Scheme == "https") {
				abs.Fragment = ""
				outs = append(outs, store.LinkOut{URL: trunc(abs.String(), 2000), Anchor: l.Anchor})
			}
			continue
		}
		if c.bl.Blocked(a) {
			metrics.BlocklistSkips.WithLabelValues("enqueue").Inc()
			continue // not even stored as a link
		}
		cs := cu.String()
		outs = append(outs, store.LinkOut{URL: trunc(cs, 2000), Anchor: l.Anchor, DstOnion: a})
		if a == job.Onion {
			if job.Depth+1 <= c.cfg.MaxDepth {
				queue(store.Discovered{Onion: a, URL: cs, Depth: job.Depth + 1, Priority: 2 + job.Depth + 1})
			}
			continue
		}
		home := "http://" + a + ".onion/"
		queue(store.Discovered{Onion: a, URL: home, Depth: 0, Priority: 0})
		if cs != home && c.cfg.MaxDepth >= 1 {
			queue(store.Discovered{Onion: a, URL: cs, Depth: 1, Priority: 3})
		}
	}
	// Queue first so links to newly discovered sites resolve to their site id.
	if err := c.st.Enqueue(ctx, "link", disc, c.cfg.PageCap); err != nil {
		metrics.Errors.WithLabelValues("db").Inc()
		c.log.Error("enqueue", "err", err)
	}
	metrics.LinksQueued.Add(float64(len(disc)))

	_, err = c.st.SavePage(ctx, &store.PageData{
		Job: job, Status: res.Status, ContentType: res.ContentType, Server: res.Server,
		Title: trunc(page.Title, 1000), Text: page.Text, HTML: page.HTML,
		SimHash: simhash.Of(page.Text), Links: outs,
		Entities: entities.Extract(page.Text, job.Onion),
	})
	if err != nil {
		metrics.Errors.WithLabelValues("db").Inc()
		c.log.Error("save page", "err", err)
		return false
	}
	metrics.PagesStored.Inc()
	return false
}

// purge removes a site everywhere and blocklists it. The address is never
// logged; for filter hits it goes, encrypted, to the report queue only.
func (c *Crawler) purge(ctx context.Context, siteID int64, addr, reason string) {
	c.bl.Add(addr)
	ctx = context.WithoutCancel(ctx)
	if err := c.st.PurgeSite(ctx, siteID, addr, reason); err != nil {
		metrics.Errors.WithLabelValues("purge").Inc()
		c.log.Error("purge: database", "site_id", siteID, "err", err)
	}
	if err := c.ix.DeleteSite(ctx, siteID); err != nil {
		metrics.Errors.WithLabelValues("purge").Inc()
		c.log.Error("purge: index", "site_id", siteID, "err", err)
	}
	if reason == "filter" {
		if err := c.rep.Add(addr, time.Now()); err != nil {
			c.log.Error("report queue", "err", err)
		}
	}
	metrics.Purges.WithLabelValues(reason).Inc()
	c.log.Warn("site purged", "reason", reason, "site_id", siteID)
}

func (c *Crawler) indexOnce(ctx context.Context) error {
	for ctx.Err() == nil {
		docs, err := c.st.PagesToIndex(ctx, 200)
		if err != nil || len(docs) == 0 {
			return err
		}
		if err := c.ix.Bulk(ctx, docs); err != nil {
			return err
		}
		if err := c.st.MarkIndexed(ctx, docs); err != nil {
			return err
		}
		metrics.Indexed.Add(float64(len(docs)))
	}
	return nil
}

func (c *Crawler) recheckOnce(ctx context.Context) error {
	n, err := c.st.ScheduleRechecks(ctx)
	if n > 0 {
		c.log.Info("liveness rechecks queued", "sites", n)
	}
	return err
}

// blocklistOnce reloads the blocklist (seedsync refreshes Ahmia's copy) and
// purges any stored site that is now on it.
func (c *Crawler) blocklistOnce(ctx context.Context) error {
	h, err := c.st.BlocklistHashes(ctx)
	if err != nil {
		return err
	}
	c.bl.Replace(h)
	sites, err := c.st.BlockedSites(ctx)
	if err != nil {
		return err
	}
	for _, s := range sites {
		c.purge(ctx, s.ID, s.Onion, "blocklist")
	}
	return nil
}

func (c *Crawler) gaugesOnce(ctx context.Context) error {
	g, err := c.st.Gauges(ctx)
	if err != nil {
		return err
	}
	for _, st := range []string{"unknown", "up", "flaky", "down", "dead", "auth_gated"} {
		metrics.Sites.WithLabelValues(st).Set(float64(g.SitesByStatus[st]))
	}
	metrics.Frontier.WithLabelValues("total").Set(float64(g.FrontierTotal))
	metrics.Frontier.WithLabelValues("due").Set(float64(g.FrontierDue))
	metrics.Pages.Set(float64(g.Pages))
	metrics.LocalBlocklist.Set(float64(g.BlocklistLocal))
	return nil
}

func (c *Crawler) poolOnce(ctx context.Context) error {
	err := c.pool.Refresh(ctx)
	metrics.Proxies.Set(float64(len(c.pool.Addrs())))
	return err
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Blocklist is the in-memory set of blocked MD5s (Ahmia + local).
type Blocklist struct {
	mu sync.RWMutex
	m  map[string]struct{}
}

func NewBlocklist(m map[string]struct{}) *Blocklist { return &Blocklist{m: m} }

func (b *Blocklist) Blocked(addr string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, h := range onion.BlockHashes(addr) {
		if _, ok := b.m[h]; ok {
			return true
		}
	}
	return false
}

func (b *Blocklist) Add(addr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, h := range onion.BlockHashes(addr) {
		b.m[h] = struct{}{}
	}
}

// Replace swaps in a fresh set from the database (which includes local entries).
func (b *Blocklist) Replace(m map[string]struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m = m
}
