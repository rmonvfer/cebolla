// Command crawler is the onion crawler. Subcommands:
//
//	run        crawl (default)
//	seedsync   refresh Ahmia's blocklist and seed lists, daily
//	search Q   full-text search the index
//	migrate    apply database migrations and exit
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"onioncrawler/internal/crawl"
	"onioncrawler/internal/fetch"
	"onioncrawler/internal/filter"
	"onioncrawler/internal/index"
	"onioncrawler/internal/report"
	"onioncrawler/internal/seed"
	"onioncrawler/internal/store"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return n
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return d
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "run":
		err = run(ctx, log)
	case "seedsync":
		err = seedsync(ctx, log)
	case "search":
		err = search(ctx, strings.Join(os.Args[2:], " "))
	case "migrate":
		var st *store.Store
		if st, err = store.Open(ctx, env("DATABASE_URL", "")); err == nil {
			err = st.Migrate(ctx)
			st.Close()
		}
	default:
		err = fmt.Errorf("unknown command %q (run, seedsync, search, migrate)", cmd)
	}
	if err != nil && ctx.Err() == nil {
		log.Error("fatal", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

// common opens the database (migrated) and loads the filter. Both commands
// refuse to run without a content filter.
func common(ctx context.Context) (*store.Store, *filter.Filter, error) {
	flt, err := filter.Load(env("FILTER_TERMS", "/etc/crawler/filter/terms.txt"))
	if err != nil {
		return nil, nil, err
	}
	st, err := store.Open(ctx, env("DATABASE_URL", ""))
	if err != nil {
		return nil, nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		st.Close()
		return nil, nil, err
	}
	return st, flt, nil
}

func run(ctx context.Context, log *slog.Logger) error {
	st, flt, err := common(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	log.Info("content filter loaded", "terms", flt.Len())

	// No crawling before Ahmia's blocklist is in place (seedsync loads it).
	for {
		n, err := st.BlocklistCount(ctx, "ahmia")
		if err != nil {
			return err
		}
		if n >= 1000 {
			log.Info("blocklist ready", "ahmia_hashes", n)
			break
		}
		log.Warn("waiting for seedsync to load the Ahmia blocklist", "ahmia_hashes", n)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(15 * time.Second):
		}
	}
	hashes, err := st.BlocklistHashes(ctx)
	if err != nil {
		return err
	}

	ix := index.New(env("OPENSEARCH_URL", "https://opensearch:9200"), env("OPENSEARCH_USER", "admin"), os.Getenv("OPENSEARCH_PASSWORD"))
	if err := ix.Wait(ctx); err != nil {
		return err
	}
	if err := ix.EnsureIndex(ctx); err != nil {
		return err
	}

	pool, err := fetch.NewPool(env("TOR_PROXIES", "tor:9050"))
	if err != nil {
		return err
	}
	for pool.Refresh(ctx) != nil || len(pool.Addrs()) == 0 {
		log.Warn("waiting for tor proxies")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
	log.Info("tor pool", "proxies", len(pool.Addrs()))

	rep, err := report.New(env("REPORT_DIR", "/report"), os.Getenv("REPORT_AGE_RECIPIENT"))
	if err != nil {
		return err
	}
	if rep == nil {
		log.Warn("REPORT_AGE_RECIPIENT not set: filter hits are purged but not queued for reporting")
	}

	fcfg := fetch.DefaultConfig()
	fcfg.MaxBytes = int64(envInt("MAX_BYTES", int(fcfg.MaxBytes)))
	fcfg.UserAgent = env("USER_AGENT", fcfg.UserAgent)
	fcfg.DialTimeout = envDur("DIAL_TIMEOUT", fcfg.DialTimeout)

	cfg := crawl.Config{
		Workers:      envInt("WORKERS", 64),
		PerSiteDelay: envDur("PER_SITE_DELAY", 8*time.Second),
		MaxDepth:     envInt("MAX_DEPTH", 3),
		PageCap:      envInt("PAGE_CAP", 200),
		MaxLinks:     envInt("MAX_LINKS_PER_PAGE", 5000),
		Lease:        fcfg.TotalTimeout + time.Minute,
	}
	c := crawl.New(cfg, st, ix, fcfg, pool, flt, crawl.NewBlocklist(hashes), rep, log)

	go serveMetrics(env("METRICS_ADDR", ":9100"), log)
	log.Info("crawling", "workers", cfg.Workers, "per_site_delay", cfg.PerSiteDelay, "max_depth", cfg.MaxDepth, "page_cap", cfg.PageCap)
	c.Run(ctx)
	return nil
}

func seedsync(ctx context.Context, log *slog.Logger) error {
	st, flt, err := common(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	var sources []string
	for _, s := range strings.Split(env("SEED_URLS", "https://ahmia.fi/onions/"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			sources = append(sources, s)
		}
	}
	s := &seed.Syncer{
		St: st, Filter: flt, Sources: sources, Log: log,
		BlocklistURL: env("BLOCKLIST_URL", "https://ahmia.fi/blacklist/banned/"),
		PageCap:      envInt("PAGE_CAP", 200),
		HTTP:         &http.Client{Timeout: 5 * time.Minute},
	}
	s.Loop(ctx, envDur("SEED_INTERVAL", 24*time.Hour))
	return nil
}

func search(ctx context.Context, q string) error {
	if q == "" {
		return fmt.Errorf("usage: crawler search <query>")
	}
	ix := index.New(env("OPENSEARCH_URL", "https://opensearch:9200"), env("OPENSEARCH_USER", "admin"), os.Getenv("OPENSEARCH_PASSWORD"))
	hits, total, err := ix.Search(ctx, q, 20)
	if err != nil {
		return err
	}
	fmt.Printf("%d results\n\n", total)
	for _, h := range hits {
		fmt.Printf("%s\n  %s\n", h.Title, h.URL)
		for _, s := range h.Snippet {
			fmt.Printf("  … %s …\n", strings.Join(strings.Fields(s), " "))
		}
		fmt.Println()
	}
	return nil
}

func serveMetrics(addr string, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("metrics server", "err", err)
	}
}
