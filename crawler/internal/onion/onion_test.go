package onion

import (
	"crypto/sha3"
	"net/url"
	"strings"
	"testing"
)

// A public, well-known v3 address (DuckDuckGo).
const ddg = "duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad"

// mk builds a valid v3 address from a key filled with b.
func mk(b byte) string {
	pub := make([]byte, 32)
	for i := range pub {
		pub[i] = b
	}
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(pub)
	h.Write([]byte{3})
	raw := append(append(pub, h.Sum(nil)[:2]...), 3)
	return strings.ToLower(b32.EncodeToString(raw))
}

func TestValidV3(t *testing.T) {
	if !ValidV3(ddg) {
		t.Fatal("known address rejected")
	}
	if !ValidV3(mk(7)) {
		t.Fatal("generated address rejected")
	}
	// Flip one character: same shape, bad checksum (typical phishing typo).
	bad := []byte(ddg)
	bad[10] = 'a'
	if ValidV3(string(bad)) {
		t.Fatal("corrupted address accepted")
	}
	if ValidV3(ddg[:55]) || ValidV3("") {
		t.Fatal("short address accepted")
	}
}

func TestExtract(t *testing.T) {
	a, b := mk(1), mk(2)
	s := "visit http://" + strings.ToUpper(a) + ".onion/x or " + b + ".onion, again " + a + ".onion" +
		" junk " + strings.Repeat("a", 56) + ".onion and prefix" + "zz" + b + ".onion"
	got := Extract(s)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("got %v", got)
	}
}

func TestCanonical(t *testing.T) {
	a := mk(9)
	base, _ := url.Parse("http://" + a + ".onion/dir/page.html")
	cases := map[string]string{
		"../x/?b=2&a=1#frag":                 "http://" + a + ".onion/x/?a=1&b=2",
		"/p?utm_source=x&PHPSESSID=1&q=ok":   "http://" + a + ".onion/p?q=ok",
		"HTTP://WWW." + strings.ToUpper(a) + ".ONION:80": "http://www." + a + ".onion/",
		"https://" + a + ".onion:8443/a//b/../c": "https://" + a + ".onion:8443/a/c",
	}
	for in, want := range cases {
		u, addr, ok := Canonical(in, base)
		if !ok || u.String() != want || addr != a {
			t.Errorf("%q: got %v %q %v, want %q", in, u, addr, ok, want)
		}
	}
	for _, in := range []string{"mailto:x@y", "http://example.com/", "ftp://" + a + ".onion/", "data:text/html,hi"} {
		if _, _, ok := Canonical(in, base); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestBlockHashes(t *testing.T) {
	h := BlockHashes("abc")
	// md5("abc.onion") and md5("abc")
	if h[1] != "900150983cd24fb0d6963f7d28e17f72" || len(h[0]) != 32 || h[0] == h[1] {
		t.Fatal(h)
	}
}
