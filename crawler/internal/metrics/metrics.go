// Package metrics defines the Prometheus metrics. Labels never carry onion
// addresses or URLs: a metric must not become a record of a filtered site.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Fetches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crawler_fetches_total", Help: "Fetch attempts by outcome and proxy.",
	}, []string{"outcome", "proxy"})

	FetchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "crawler_fetch_duration_seconds", Help: "Fetch latency, including circuit setup.",
		Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 90, 120},
	}, []string{"outcome"})

	Bytes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crawler_bytes_total", Help: "HTML bytes accepted.",
	})

	BlocklistSkips = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crawler_blocklist_skips_total", Help: "URLs skipped because their site is blocklisted.",
	}, []string{"stage"}) // enqueue, fetch, redirect

	FilterHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crawler_filter_hits_total", Help: "Content filter matches.",
	}, []string{"stage"}) // url, page

	Purges = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crawler_site_purges_total", Help: "Sites purged from storage and index.",
	}, []string{"reason"}) // filter, blocklist

	PagesStored = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crawler_pages_stored_total", Help: "Pages saved (new or changed content).",
	})

	LinksQueued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crawler_links_queued_total", Help: "Onion URLs offered to the frontier.",
	})

	Indexed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crawler_indexed_docs_total", Help: "Documents sent to OpenSearch.",
	})

	Errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crawler_internal_errors_total", Help: "Internal errors (db, index, parse).",
	}, []string{"component"})

	Sites = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "crawler_sites", Help: "Known sites by liveness status.",
	}, []string{"status"})

	Frontier = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "crawler_frontier", Help: "Frontier entries (all / due now).",
	}, []string{"kind"})

	Pages = promauto.NewGauge(prometheus.GaugeOpts{Name: "crawler_pages", Help: "Stored pages."})

	LocalBlocklist = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "crawler_blocklist_local", Help: "Hashes on the local blocklist.",
	})

	Proxies = promauto.NewGauge(prometheus.GaugeOpts{Name: "crawler_tor_proxies", Help: "Tor proxies in the pool."})

	Workers = promauto.NewGauge(prometheus.GaugeOpts{Name: "crawler_busy_workers", Help: "Workers currently fetching."})
)
