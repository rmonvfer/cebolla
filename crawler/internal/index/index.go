// Package index keeps the OpenSearch full-text index in sync with Postgres,
// using the plain REST API (bulk, delete_by_query, search).
package index

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"onioncrawler/internal/store"
)

const Name = "pages"

type Client struct {
	base, user, pass string
	hc               *http.Client
}

func New(base, user, pass string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"), user: user, pass: pass,
		hc: &http.Client{
			Timeout: 60 * time.Second,
			// Self-signed demo certificate on an internal-only Docker network.
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
	}
}

func (c *Client) do(ctx context.Context, method, path, ctype string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(c.user, c.pass)
	if body != nil {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return out, resp.StatusCode, err
}

// Wait blocks until the cluster answers (it takes a while to start).
func (c *Client) Wait(ctx context.Context) error {
	var last error
	for range 90 {
		_, code, err := c.do(ctx, "GET", "/_cluster/health?wait_for_status=yellow&timeout=5s", "", nil)
		if err == nil && code == 200 {
			return nil
		}
		last = fmt.Errorf("status %d: %v", code, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("opensearch not ready: %w", last)
}

const mapping = `{
  "settings": {"number_of_shards": 1, "number_of_replicas": 0, "refresh_interval": "10s"},
  "mappings": {
    "dynamic": "strict",
    "properties": {
      "site_id":    {"type": "long"},
      "onion":      {"type": "keyword"},
      "url":        {"type": "keyword"},
      "title":      {"type": "text"},
      "text":       {"type": "text"},
      "fetched_at": {"type": "date"},
      "simhash":    {"type": "long"}
    }
  }
}`

func (c *Client) EnsureIndex(ctx context.Context) error {
	_, code, err := c.do(ctx, "HEAD", "/"+Name, "", nil)
	if err != nil {
		return err
	}
	if code == 200 {
		return nil
	}
	out, code, err := c.do(ctx, "PUT", "/"+Name, "application/json", []byte(mapping))
	if err != nil {
		return err
	}
	if code != 200 && !bytes.Contains(out, []byte("resource_already_exists")) {
		return fmt.Errorf("create index: %d %s", code, out)
	}
	return nil
}

// Bulk indexes docs (document id = page id).
func (c *Client) Bulk(ctx context.Context, docs []store.IndexDoc) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, d := range docs {
		enc.Encode(map[string]any{"index": map[string]any{"_index": Name, "_id": strconv.FormatInt(d.PageID, 10)}})
		enc.Encode(map[string]any{
			"site_id": d.SiteID, "onion": d.Onion, "url": d.URL, "title": d.Title,
			"text": d.Text, "fetched_at": d.FetchedAt, "simhash": d.SimHash,
		})
	}
	out, code, err := c.do(ctx, "POST", "/_bulk", "application/x-ndjson", buf.Bytes())
	if err != nil {
		return err
	}
	var r struct {
		Errors bool `json:"errors"`
	}
	if code != 200 || json.Unmarshal(out, &r) != nil || r.Errors {
		return fmt.Errorf("bulk: status %d: %.500s", code, out)
	}
	return nil
}

// DeleteSite removes all documents of a site and expunges them from the
// segments: a plain delete only marks documents, which keeps their text on
// disk until the next merge.
func (c *Client) DeleteSite(ctx context.Context, siteID int64) error {
	q := fmt.Sprintf(`{"query":{"term":{"site_id":%d}}}`, siteID)
	out, code, err := c.do(ctx, "POST", "/"+Name+"/_delete_by_query?refresh=true&conflicts=proceed", "application/json", []byte(q))
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("delete_by_query: %d %.300s", code, out)
	}
	out, code, err = c.do(ctx, "POST", "/"+Name+"/_forcemerge?only_expunge_deletes=true", "", nil)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("forcemerge: %d %.300s", code, out)
	}
	return nil
}

type Hit struct {
	Onion   string   `json:"onion"`
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Snippet []string `json:"snippet"`
}

func (c *Client) Search(ctx context.Context, query string, size int) ([]Hit, int, error) {
	q, _ := json.Marshal(map[string]any{
		"size":    size,
		"_source": []string{"onion", "url", "title"},
		"query": map[string]any{"multi_match": map[string]any{
			"query": query, "fields": []string{"title^3", "text"},
		}},
		"highlight": map[string]any{"fields": map[string]any{"text": map[string]any{"fragment_size": 160, "number_of_fragments": 2}}},
	})
	out, code, err := c.do(ctx, "POST", "/"+Name+"/_search", "application/json", q)
	if err != nil {
		return nil, 0, err
	}
	if code != 200 {
		return nil, 0, fmt.Errorf("search: %d %.300s", code, out)
	}
	var r struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source    Hit                 `json:"_source"`
				Highlight map[string][]string `json:"highlight"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, 0, err
	}
	hits := make([]Hit, 0, len(r.Hits.Hits))
	for _, h := range r.Hits.Hits {
		h.Source.Snippet = h.Highlight["text"]
		hits = append(hits, h.Source)
	}
	return hits, r.Hits.Total.Value, nil
}
