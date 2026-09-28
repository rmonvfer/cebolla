//go:build integration

// End-to-end test against real Postgres and OpenSearch, with a fake tor
// (SOCKS5) and fake onion sites. Run via ../../scripts/integration-test.sh.
package crawl

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha3"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/klauspost/compress/zstd"

	"onioncrawler/internal/fetch"
	"onioncrawler/internal/filter"
	"onioncrawler/internal/index"
	"onioncrawler/internal/report"
	"onioncrawler/internal/store"
)

func mk(b byte) string {
	pub := bytes.Repeat([]byte{b}, 32)
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(pub)
	h.Write([]byte{3})
	raw := append(append(pub, h.Sum(nil)[:2]...), 3)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

// socks is a minimal SOCKS5 proxy forwarding every CONNECT to target.
func socks(t *testing.T, target string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				b := make([]byte, 300)
				io.ReadFull(c, b[:2])
				io.ReadFull(c, b[:b[1]])
				c.Write([]byte{5, 2})
				io.ReadFull(c, b[:2])
				io.ReadFull(c, b[:b[1]])
				io.ReadFull(c, b[:1])
				io.ReadFull(c, b[:b[0]])
				c.Write([]byte{1, 0})
				io.ReadFull(c, b[:5])
				io.ReadFull(c, b[:int(b[4])+2])
				up, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer up.Close()
				c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	good, bad, blocked := mk(1), mk(2), mk(3)
	const btc = "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"

	var mu sync.Mutex
	requested := map[string][]string{}
	pages := map[string]map[string]string{
		good: {
			"/": `<html><head><title>Alpha Home</title></head><body><p>alphaunique marketplace. Pay ` + btc + `</p>
				<img src="data:image/png;base64,AAAA"><a href="/about">About</a> <a href="/img.png">pic</a>
				<a href="/deep1">deep</a> <a href="http://` + bad + `.onion/">bravo</a>
				<a href="http://` + blocked + `.onion/">charlie</a> <a href="https://example.com/x">clearnet</a></body></html>`,
			"/about": `<html><title>About</title><p>about alphaunique</p><a href="/deep1">d</a></html>`,
			"/deep1": `<html><p>level one</p><a href="/deep2">d2</a></html>`,
			"/deep2": `<html><p>level two</p><a href="/deep3">d3</a></html>`,
			"/deep3": `<html><p>level three</p><a href="/deep4">d4</a></html>`,
			"/deep4": `<html><p>too deep</p></html>`,
		},
		bad: {
			"/":     `<html><title>Bravo</title><p>bravounique harmless home</p><a href="/evil">more</a></html>`,
			"/evil": `<html><p>this page is about a forbidden topic</p></html>`,
		},
		blocked: {"/": `<html><p>must never be fetched</p></html>`},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.TrimSuffix(r.Host, ".onion")
		mu.Lock()
		requested[host] = append(requested[host], r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/img.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\n...."))
			return
		}
		body, ok := pages[host][r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, body)
	}))
	defer srv.Close()

	st, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	defer db.Close()

	// Ahmia-style list: filler plus md5("<blocked>.onion").
	var hashes []string
	for i := range 1200 {
		s := md5.Sum([]byte(fmt.Sprint(i)))
		hashes = append(hashes, hex.EncodeToString(s[:]))
	}
	s := md5.Sum([]byte(blocked + ".onion"))
	hashes = append(hashes, hex.EncodeToString(s[:]))
	if err := st.ReplaceBlocklist(ctx, "ahmia", hashes); err != nil {
		t.Fatal(err)
	}
	if err := st.Enqueue(ctx, "test", []store.Discovered{
		{Onion: good, URL: "http://" + good + ".onion/", Priority: 0},
		{Onion: bad, URL: "http://" + bad + ".onion/", Priority: 0},
	}, 200); err != nil {
		t.Fatal(err)
	}

	ix := index.New(os.Getenv("OPENSEARCH_URL"), "admin", os.Getenv("OPENSEARCH_PASSWORD"))
	if err := ix.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ix.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	flt, _ := filter.New([]string{"forbidden topic"})
	id, _ := age.GenerateX25519Identity()
	repDir := t.TempDir()
	rep, err := report.New(repDir, id.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	bl, _ := st.BlocklistHashes(ctx)
	pool, _ := fetch.NewPool(socks(t, strings.TrimPrefix(srv.URL, "http://")))
	fcfg := fetch.DefaultConfig()
	fcfg.TotalTimeout = 10 * time.Second
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c := New(Config{Workers: 4, PerSiteDelay: 50 * time.Millisecond, MaxDepth: 3, PageCap: 200, MaxLinks: 100, Lease: time.Minute},
		st, ix, fcfg, pool, flt, NewBlocklist(bl), rep, log)

	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { c.Run(runCtx); close(done) }()

	count := func(q string, args ...any) int {
		var n int
		if err := db.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		goodPages := count(`SELECT count(*) FROM pages p JOIN sites s ON s.id=p.site_id WHERE s.onion=$1`, good)
		badGone := count(`SELECT count(*) FROM sites WHERE onion=$1`, bad) == 0
		queued := count(`SELECT count(*) FROM frontier WHERE next_fetch_at <= now()`)
		unindexed := count(`SELECT count(*) FROM pages WHERE current_version_id IS DISTINCT FROM indexed_version_id`)
		if goodPages >= 5 && badGone && queued == 0 && unindexed == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: goodPages=%d badGone=%v queued=%d unindexed=%d", goodPages, badGone, queued, unindexed)
		}
		time.Sleep(500 * time.Millisecond)
	}
	stop()
	<-done

	// Good site: pages up to depth 3, not the image, not depth 4.
	rows, _ := db.Query(ctx, `SELECT p.url FROM pages p JOIN sites s ON s.id=p.site_id WHERE s.onion=$1 ORDER BY 1`, good)
	var urls []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		urls = append(urls, strings.TrimPrefix(u, "http://"+good+".onion"))
	}
	if strings.Join(urls, " ") != "/ /about /deep1 /deep2 /deep3" {
		t.Errorf("stored pages: %v", urls)
	}
	if n := count(`SELECT count(*) FROM fetch_log WHERE outcome='content_type'`); n != 1 {
		t.Errorf("content_type aborts: %d", n)
	}
	// Every stored HTML blob must be free of embedded images.
	brows, _ := db.Query(ctx, `SELECT zstd FROM blobs`)
	dec, _ := zstd.NewReader(nil)
	nblobs := 0
	for brows.Next() {
		var z []byte
		brows.Scan(&z)
		h, err := dec.DecodeAll(z, nil)
		if err != nil {
			t.Fatal(err)
		}
		nblobs++
		if lh := strings.ToLower(string(h)); strings.Contains(lh, "data:") || strings.Contains(lh, "<img") || strings.Contains(lh, "<svg") {
			t.Errorf("stored HTML contains an image: %s", h)
		}
	}
	if nblobs == 0 {
		t.Error("no blobs stored")
	}
	if n := count(`SELECT count(*) FROM entities WHERE kind='btc' AND value=$1`, btc); n != 1 {
		t.Errorf("btc entity: %d", n)
	}
	if n := count(`SELECT count(*) FROM sites WHERE onion=$1 AND status='up' AND title='Alpha Home'`, good); n != 1 {
		t.Errorf("good site status/title")
	}
	if n := count(`SELECT count(*) FROM links WHERE is_onion=false AND dst_url='https://example.com/x'`); n != 1 {
		t.Errorf("clearnet link not recorded")
	}

	// Blocked site: never requested, never stored, not even as a link target.
	mu.Lock()
	if len(requested[blocked]) != 0 {
		t.Errorf("blocked site was requested: %v", requested[blocked])
	}
	mu.Unlock()
	if n := count(`SELECT count(*) FROM sites WHERE onion=$1`, blocked); n != 0 {
		t.Errorf("blocked site stored")
	}
	if n := count(`SELECT count(*) FROM links WHERE dst_url LIKE '%'||$1||'%'`, blocked); n != 0 {
		t.Errorf("link to blocked site stored")
	}

	// Filtered site: fetched, then purged everywhere, blocklisted, reported.
	mu.Lock()
	if strings.Join(requested[bad], " ") != "/ /evil" {
		t.Errorf("bad site requests: %v", requested[bad])
	}
	mu.Unlock()
	if n := count(`SELECT count(*) FROM links WHERE dst_url LIKE '%'||$1||'%'`, bad); n != 0 {
		t.Errorf("links to purged site remain: %d", n)
	}
	if n := count(`SELECT count(*) FROM page_versions WHERE text LIKE '%bravounique%' OR text LIKE '%forbidden%'`); n != 0 {
		t.Errorf("purged text remains")
	}
	if n := count(`SELECT count(*) FROM blocklist WHERE source='local' AND md5 = md5($1||'.onion')`, bad); n != 1 {
		t.Errorf("purged site not blocklisted")
	}
	if n := count(`SELECT count(*) FROM blobs b WHERE NOT EXISTS (SELECT 1 FROM page_versions v WHERE v.html_hash=b.hash)`); n != 0 {
		t.Errorf("orphan blobs: %d", n)
	}
	files, _ := filepath.Glob(filepath.Join(repDir, "*.age"))
	if len(files) != 1 {
		t.Fatalf("report files: %d", len(files))
	}
	f, _ := os.Open(files[0])
	r, err := age.Decrypt(f, id)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(r)
	if !strings.HasPrefix(string(plain), bad+".onion\t") {
		t.Errorf("report content %q", plain)
	}

	// Index: good site searchable, purged site gone.
	if _, n, err := ix.Search(ctx, "alphaunique", 10); err != nil || n != 2 {
		t.Errorf("search alphaunique: %d hits, %v", n, err)
	}
	if _, n, err := ix.Search(ctx, "bravounique", 10); err != nil || n != 0 {
		t.Errorf("search bravounique (purged): %d hits, %v", n, err)
	}

	// Liveness: a due check re-queues the homepage.
	db.Exec(ctx, `UPDATE sites SET next_check_at = now() - interval '1 minute' WHERE onion=$1`, good)
	if n, err := st.ScheduleRechecks(ctx); err != nil || n != 1 {
		t.Errorf("rechecks: %d %v", n, err)
	}
}
