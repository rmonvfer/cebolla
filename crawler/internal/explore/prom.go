package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// promSeries are the whitelisted PromQL range queries the analytics page may
// request. Keeping them server-side means the browser cannot run arbitrary
// queries against Prometheus.
var promSeries = map[string]string{
	"fetch_rate":  `sum by (outcome) (rate(crawler_fetches_total[5m]))`,
	"pages_rate":  `rate(crawler_pages_stored_total[5m])`,
	"links_rate":  `rate(crawler_links_queued_total[5m])`,
	"workers":     `crawler_busy_workers`,
	"frontier":    `crawler_frontier`,
	"sites":       `crawler_sites`,
	"latency":     `histogram_quantile(0.50, sum by (le) (rate(crawler_fetch_duration_seconds_bucket{outcome="ok"}[5m])))`,
	"latency_p95": `histogram_quantile(0.95, sum by (le) (rate(crawler_fetch_duration_seconds_bucket{outcome="ok"}[5m])))`,
	"errors":      `sum by (component) (rate(crawler_internal_errors_total[5m]))`,
	"blocklist":   `sum by (stage) (rate(crawler_blocklist_skips_total[5m]))`,
	"purges":      `sum by (reason) (rate(crawler_site_purges_total[15m]))`,
}

type promMatrix struct {
	Series []promLine `json:"series"`
	Step   int        `json:"step"`
}
type promLine struct {
	Name   string      `json:"name"`
	Points [][2]float64 `json:"points"` // [unix_seconds, value]
}

func (s *Server) prom(ctx context.Context, r *http.Request) (any, error) {
	key := r.URL.Query().Get("series")
	q, ok := promSeries[key]
	if !ok {
		return nil, errNotFound
	}
	hours := intParam(r, "hours", 6, 168)
	if hours == 0 {
		hours = 6
	}
	step := hours * 3600 / 300 // ~300 points
	if step < 15 {
		step = 15
	}
	end := time.Now()
	start := end.Add(-time.Duration(hours) * time.Hour)
	v := url.Values{}
	v.Set("query", q)
	v.Set("start", strconv.FormatInt(start.Unix(), 10))
	v.Set("end", strconv.FormatInt(end.Unix(), 10))
	v.Set("step", strconv.Itoa(step))

	req, err := http.NewRequestWithContext(ctx, "GET", s.promURL+"/api/v1/query_range?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus: %w", err)
	}
	defer resp.Body.Close()
	var pr struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]any          `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	out := promMatrix{Step: step}
	for _, res := range pr.Data.Result {
		name := res.Metric["outcome"]
		if name == "" {
			name = res.Metric["status"]
		}
		if name == "" {
			name = res.Metric["kind"]
		}
		if name == "" {
			name = res.Metric["reason"]
		}
		if name == "" {
			name = key
		}
		line := promLine{Name: name}
		for _, pt := range res.Values {
			ts, _ := pt[0].(float64)
			var val float64
			if str, ok := pt[1].(string); ok {
				val, _ = strconv.ParseFloat(str, 64)
			}
			line.Points = append(line.Points, [2]float64{ts, val})
		}
		out.Series = append(out.Series, line)
	}
	return out, nil
}
