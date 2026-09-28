// Package seed refreshes Ahmia's blocklist and queues onion addresses from
// public seed lists. It runs in its own container, the only crawler component
// with clearnet access; it never talks to onion services.
package seed

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"onioncrawler/internal/filter"
	"onioncrawler/internal/onion"
	"onioncrawler/internal/store"
)

type Syncer struct {
	St           *store.Store
	Filter       *filter.Filter
	BlocklistURL string
	Sources      []string
	PageCap      int
	Log          *slog.Logger
	HTTP         *http.Client
}

var md5Line = regexp.MustCompile(`^[0-9a-f]{32}$`)

// minBlocklist guards against replacing the list with an error page.
const minBlocklist = 1000

func (s *Syncer) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// Once runs one full sync: blocklist first, so seeds are checked against it.
func (s *Syncer) Once(ctx context.Context) error {
	if err := s.syncBlocklist(ctx); err != nil {
		return fmt.Errorf("blocklist: %w", err)
	}
	blocked, err := s.St.BlocklistHashes(ctx)
	if err != nil {
		return err
	}
	isBlocked := func(a string) bool {
		for _, h := range onion.BlockHashes(a) {
			if _, ok := blocked[h]; ok {
				return true
			}
		}
		return false
	}
	for _, src := range s.Sources {
		body, err := s.get(ctx, src, 64<<20)
		if err != nil {
			s.Log.Error("seed source failed", "source", src, "err", err)
			continue
		}
		via := "seed"
		if u, err := url.Parse(src); err == nil {
			via = "seed:" + u.Host
		}
		var items []store.Discovered
		skipped := 0
		for _, a := range onion.Extract(string(body)) {
			home := "http://" + a + ".onion/"
			if isBlocked(a) || s.Filter.Match(home) {
				skipped++
				continue
			}
			items = append(items, store.Discovered{Onion: a, URL: home, Depth: 0, Priority: 0})
		}
		for i := 0; i < len(items); i += 1000 {
			if err := s.St.Enqueue(ctx, via, items[i:min(i+1000, len(items))], s.PageCap); err != nil {
				return err
			}
		}
		s.Log.Info("seed source synced", "source", src, "onions", len(items), "skipped", skipped)
	}
	return nil
}

func (s *Syncer) syncBlocklist(ctx context.Context) error {
	body, err := s.get(ctx, s.BlocklistURL, 32<<20)
	if err != nil {
		return err
	}
	var hashes []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		if l := strings.ToLower(strings.TrimSpace(sc.Text())); md5Line.MatchString(l) {
			hashes = append(hashes, l)
		}
	}
	if len(hashes) < minBlocklist {
		return fmt.Errorf("only %d hashes in response; keeping the previous list", len(hashes))
	}
	if err := s.St.ReplaceBlocklist(ctx, "ahmia", hashes); err != nil {
		return err
	}
	s.Log.Info("blocklist synced", "source", "ahmia", "hashes", len(hashes))
	return nil
}

// Loop syncs now and then every interval until ctx ends.
func (s *Syncer) Loop(ctx context.Context, every time.Duration) {
	for {
		if err := s.Once(ctx); err != nil && ctx.Err() == nil {
			s.Log.Error("sync failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
