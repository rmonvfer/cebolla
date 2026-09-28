package fetch

import (
	"context"
	"errors"
	"hash/fnv"
	"net"
	"sort"
	"strings"
	"sync"
)

// Pool is the set of tor SOCKS proxies. Each onion always maps to the same
// proxy (rendezvous hashing), so its rendezvous circuit is built once and
// reused for every page of the site; round-robin would rebuild it on every
// request. Adding a proxy moves only ~1/n of the sites.
type Pool struct {
	host, port string // DNS name resolved to all replicas, e.g. "tor" in compose
	mu         sync.RWMutex
	addrs      []string
}

// NewPool takes "name:port" (a DNS name returning one A record per replica)
// or a comma-separated list of "host:port" proxies.
func NewPool(spec string) (*Pool, error) {
	if strings.Contains(spec, ",") {
		p := &Pool{}
		for _, a := range strings.Split(spec, ",") {
			if a = strings.TrimSpace(a); a != "" {
				p.addrs = append(p.addrs, a)
			}
		}
		sort.Strings(p.addrs)
		return p, nil
	}
	h, port, err := net.SplitHostPort(spec)
	if err != nil {
		return nil, err
	}
	if net.ParseIP(h) != nil {
		return &Pool{addrs: []string{spec}}, nil
	}
	return &Pool{host: h, port: port}, nil
}

// Refresh re-resolves the replica set (no-op for a static list).
func (p *Pool) Refresh(ctx context.Context) error {
	if p.host == "" {
		return nil
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, p.host)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return errors.New("tor pool: no proxies resolved")
	}
	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, net.JoinHostPort(ip, p.port))
	}
	sort.Strings(addrs)
	p.mu.Lock()
	p.addrs = addrs
	p.mu.Unlock()
	return nil
}

func (p *Pool) Addrs() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]string(nil), p.addrs...)
}

// Pick returns the proxy for key (the onion address), or "" if none.
func (p *Pool) Pick(key string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var best string
	var bestScore uint64
	for _, a := range p.addrs {
		h := fnv.New64a()
		h.Write([]byte(key))
		h.Write([]byte{0})
		h.Write([]byte(a))
		if s := h.Sum64(); best == "" || s > bestScore {
			best, bestScore = a, s
		}
	}
	return best
}
