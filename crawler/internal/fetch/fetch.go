// Package fetch retrieves onion pages through the tor SOCKS pool with the
// text-only guards: GET only, no cookies, Content-Type and sniffed-bytes
// checks, a hard size cap (after decompression), bounded redirects that are
// re-checked against the blocklist and filter at every hop.
package fetch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"onioncrawler/internal/onion"
)

type Config struct {
	MaxBytes      int64
	UserAgent     string
	DialTimeout   time.Duration // SOCKS connect incl. descriptor fetch + rendezvous
	HeaderTimeout time.Duration
	IdleTimeout   time.Duration // max time without progress on read/write
	TotalTimeout  time.Duration
	MaxRedirects  int
}

func DefaultConfig() Config {
	return Config{
		MaxBytes:      2 << 20,
		UserAgent:     "Mozilla/5.0 (Windows NT 10.0; rv:140.0) Gecko/20100101 Firefox/140.0",
		DialTimeout:   60 * time.Second,
		HeaderTimeout: 30 * time.Second,
		IdleTimeout:   20 * time.Second,
		TotalTimeout:  120 * time.Second,
		MaxRedirects:  5,
	}
}

// Error carries the fetch outcome recorded in fetch_log and metrics.
type Error struct {
	Outcome string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Outcome + ": " + e.Err.Error()
	}
	return e.Outcome
}
func (e *Error) Unwrap() error { return e.Err }

// Guard vets every URL before it is requested, including redirect targets.
// It returns an *Error (e.g. outcome "blocked" or "filtered") to refuse.
type Guard func(u *url.URL, addr string) error

type Result struct {
	URL         *url.URL // final URL after redirects
	Status      int
	ContentType string
	Server      string
	Body        []byte // nil unless 2xx and text/html
	Proxy       string
	Latency     time.Duration
}

type Fetcher struct {
	cfg    Config
	pool   *Pool
	guard  Guard
	client *http.Client
}

var htmlTypes = map[string]bool{"text/html": true, "application/xhtml+xml": true}

func New(cfg Config, pool *Pool, guard Guard) *Fetcher {
	f := &Fetcher{cfg: cfg, pool: pool, guard: guard}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           f.dial,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // the onion address authenticates the service
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: cfg.HeaderTimeout,
		MaxIdleConnsPerHost:   2,
		MaxConnsPerHost:       2,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     false,
	}
	f.client = &http.Client{
		Transport: tr,
		Jar:       nil,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Del("Referer")
			if len(via) > cfg.MaxRedirects {
				return &Error{Outcome: "redirect_limit"}
			}
			cu, addr, ok := onion.Canonical(req.URL.String(), nil)
			if !ok {
				return &Error{Outcome: "redirect_offsite"}
			}
			if err := f.guard(cu, addr); err != nil {
				var ge *Error
				if errors.As(err, &ge) {
					// Distinguish "this site is blocked" from "this site points at a blocked one".
					return &Error{Outcome: "redirect_" + ge.Outcome, Err: ge.Err}
				}
				return err
			}
			return nil
		},
	}
	return f
}

func (f *Fetcher) dial(ctx context.Context, _, addr string) (net.Conn, error) {
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(ps, 10, 16)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(host)
	if u, err := url.Parse("http://" + host); err == nil {
		if a, ok := onion.Host(u); ok {
			key = a
		}
	}
	proxy := f.pool.Pick(key)
	if proxy == "" {
		return nil, errors.New("no tor proxy available")
	}
	dctx, cancel := context.WithTimeout(ctx, f.cfg.DialTimeout)
	defer cancel()
	c, err := dialSOCKS(dctx, proxy, host, uint16(port), key)
	if err != nil {
		return nil, err
	}
	return &idleConn{Conn: c, d: f.cfg.IdleTimeout}, nil
}

// Proxy reports which proxy an onion is pinned to (for metrics).
func (f *Fetcher) Proxy(addr string) string { return f.pool.Pick(addr) }

// Fetch GETs u (a canonical onion URL). Non-2xx responses return a Result
// without body and no error: the service answered, so it is alive.
func (f *Fetcher) Fetch(ctx context.Context, u *url.URL, addr string) (*Result, error) {
	if err := f.guard(u, addr); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, f.cfg.TotalTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &Error{Outcome: "bad_url", Err: err}
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	start := time.Now()
	res := &Result{URL: u, Proxy: f.pool.Pick(addr)}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, classify(err)
	}
	defer resp.Body.Close() // closing an unread body aborts the transfer
	res.URL = resp.Request.URL
	res.Status = resp.StatusCode
	res.Server = trunc(resp.Header.Get("Server"), 200)
	res.ContentType = trunc(resp.Header.Get("Content-Type"), 200)
	defer func() { res.Latency = time.Since(start) }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return res, nil
	}
	ct := res.ContentType
	if ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || !htmlTypes[strings.ToLower(mt)] {
			return res, &Error{Outcome: "content_type", Err: fmt.Errorf("%q", ct)}
		}
	}
	if resp.ContentLength > f.cfg.MaxBytes {
		return res, &Error{Outcome: "too_large", Err: fmt.Errorf("content-length %d", resp.ContentLength)}
	}

	// Servers lie about Content-Type: look at the first bytes before reading on.
	head := make([]byte, 512)
	n, err := io.ReadFull(resp.Body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return res, classify(err)
	}
	head = head[:n]
	sniff := http.DetectContentType(head)
	if !strings.HasPrefix(sniff, "text/") || (ct == "" && !strings.HasPrefix(sniff, "text/html")) {
		return res, &Error{Outcome: "sniff", Err: fmt.Errorf("sniffed %q", sniff)}
	}

	// resp.Body is already decompressed, so this caps gzip bombs too.
	rest, err := io.ReadAll(io.LimitReader(resp.Body, f.cfg.MaxBytes+1-int64(n)))
	if err != nil {
		return res, classify(err)
	}
	if int64(n+len(rest)) > f.cfg.MaxBytes {
		return res, &Error{Outcome: "too_large"}
	}
	res.Body = append(head, rest...)
	return res, nil
}

func classify(err error) error {
	var fe *Error
	if errors.As(err, &fe) {
		return fe
	}
	var se *SOCKSError
	if errors.As(err, &se) {
		return &Error{Outcome: se.Outcome(), Err: se}
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &Error{Outcome: "timeout", Err: err}
	}
	return &Error{Outcome: "conn_error", Err: err}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
