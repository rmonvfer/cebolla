// Package onion validates, extracts and normalizes v3 onion addresses and URLs.
package onion

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// ValidV3 reports whether addr (56 base32 chars, without ".onion") is a
// well-formed v3 address: 32-byte key, 2-byte checksum, version 3.
// The regex alone lets through junk and deliberately mistyped phishing
// addresses; the checksum rejects them.
func ValidV3(addr string) bool {
	if len(addr) != 56 {
		return false
	}
	raw, err := b32.DecodeString(strings.ToUpper(addr))
	if err != nil || len(raw) != 35 {
		return false
	}
	pub, sum, ver := raw[:32], raw[32:34], raw[34]
	if ver != 3 {
		return false
	}
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(pub)
	h.Write([]byte{ver})
	d := h.Sum(nil)
	return d[0] == sum[0] && d[1] == sum[1]
}

// A run of base32 chars ending in ".onion". Longer runs are matched whole and
// then rejected by the length check, so a 56-char tail of a longer string
// never counts as an address.
var candidate = regexp.MustCompile(`(?i)[a-z2-7]+\.onion`)

// Extract returns the distinct valid v3 addresses (without ".onion") in s.
func Extract(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range candidate.FindAllString(s, -1) {
		a := strings.TrimSuffix(strings.ToLower(m), ".onion")
		if seen[a] || !ValidV3(a) {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// Host returns the v3 address (without ".onion") that u points to.
// Subdomains (www.xxx.onion) are folded onto the service address.
func Host(u *url.URL) (string, bool) {
	h := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(h, ".onion") {
		return "", false
	}
	labels := strings.Split(strings.TrimSuffix(h, ".onion"), ".")
	a := labels[len(labels)-1]
	if !ValidV3(a) {
		return "", false
	}
	return a, true
}

// BlockHashes returns the MD5 forms a blocklist may use for addr: Ahmia hashes
// the index "domain" field, which we match both with and without ".onion".
func BlockHashes(addr string) [2]string {
	a := md5.Sum([]byte(addr + ".onion"))
	b := md5.Sum([]byte(addr))
	return [2]string{hex.EncodeToString(a[:]), hex.EncodeToString(b[:])}
}

// Query parameters that only identify a session or a campaign. Keeping them
// turns one page into thousands of frontier entries (a classic spider trap).
var dropParams = map[string]bool{
	"sid": true, "sessionid": true, "session_id": true, "phpsessid": true,
	"jsessionid": true, "aspsessionid": true, "fbclid": true, "gclid": true,
	"ref": true,
}

// Canonical parses raw (resolved against base, if given) into a normalized
// onion URL: http(s) only, lowercase host, no fragment, default port dropped,
// session/tracking params removed, remaining params sorted, path cleaned.
// The second value is the onion address.
func Canonical(raw string, base *url.URL) (*url.URL, string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, "", false
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, "", false
	}
	addr, ok := Host(u)
	if !ok {
		return nil, "", false
	}
	host := addr + ".onion"
	if strings.Count(strings.ToLower(u.Hostname()), ".") > 1 {
		host = strings.ToLower(u.Hostname()) // keep an explicit subdomain in the URL itself
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		host += ":" + p
	}
	u.Host = host
	u.User = nil
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	} else {
		trailing := strings.HasSuffix(u.Path, "/")
		u.Path = path.Clean(u.Path)
		if trailing && u.Path != "/" {
			u.Path += "/"
		}
	}
	u.RawPath = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if dropParams[lk] || strings.HasPrefix(lk, "utm_") {
			q.Del(k)
		}
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		vs := q[k]
		sort.Strings(vs)
		for _, v := range vs {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(v))
		}
	}
	u.RawQuery = b.String()
	u.ForceQuery = false
	return u, addr, true
}

// URLHash is the frontier/pages key for a canonical URL.
func URLHash(canonical string) []byte {
	h := sha256.Sum256([]byte(canonical))
	return h[:16]
}
