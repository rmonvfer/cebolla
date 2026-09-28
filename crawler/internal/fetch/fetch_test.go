package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha3"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"onioncrawler/internal/onion"
)

func mkOnion(b byte) string {
	pub := bytes.Repeat([]byte{b}, 32)
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(pub)
	h.Write([]byte{3})
	raw := append(append(pub, h.Sum(nil)[:2]...), 3)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

// fakeTor is a SOCKS5 proxy that routes every CONNECT to target, or answers
// with reply code fail for hosts listed in failures. It records the user
// names (isolation keys) it saw.
type fakeTor struct {
	ln       net.Listener
	target   string
	failures map[string]byte
	mu       sync.Mutex
	users    map[string]string // host -> user
}

func newFakeTor(t *testing.T, target string) *fakeTor {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeTor{ln: ln, target: target, failures: map[string]byte{}, users: map[string]string{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go ft.serve(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ft
}

func (ft *fakeTor) serve(c net.Conn) {
	defer c.Close()
	b := make([]byte, 512)
	io.ReadFull(c, b[:2])
	io.ReadFull(c, b[:b[1]])
	c.Write([]byte{5, 2})
	io.ReadFull(c, b[:2])
	user := make([]byte, b[1])
	io.ReadFull(c, user)
	io.ReadFull(c, b[:1])
	io.ReadFull(c, b[:b[0]])
	c.Write([]byte{1, 0})
	io.ReadFull(c, b[:5])
	host := make([]byte, b[4])
	io.ReadFull(c, host)
	io.ReadFull(c, b[:2])
	_ = binary.BigEndian.Uint16(b[:2])
	ft.mu.Lock()
	ft.users[string(host)] = string(user)
	code := ft.failures[string(host)]
	ft.mu.Unlock()
	if code != 0 {
		c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	up, err := net.Dial("tcp", ft.target)
	if err != nil {
		c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
	go io.Copy(up, c)
	io.Copy(c, up)
}

func setup(t *testing.T, h http.Handler, blocked map[string]bool) (*Fetcher, *fakeTor) {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ft := newFakeTor(t, strings.TrimPrefix(srv.URL, "http://"))
	pool, _ := NewPool(ft.ln.Addr().String())
	cfg := DefaultConfig()
	cfg.MaxBytes = 64 << 10
	cfg.TotalTimeout = 5 * time.Second
	cfg.IdleTimeout = 1 * time.Second
	guard := func(u *url.URL, addr string) error {
		if blocked[addr] {
			return &Error{Outcome: "blocked"}
		}
		return nil
	}
	return New(cfg, pool, guard), ft
}

func get(t *testing.T, f *Fetcher, raw string) (*Result, error) {
	u, addr, ok := onion.Canonical(raw, nil)
	if !ok {
		t.Fatalf("bad test url %s", raw)
	}
	return f.Fetch(context.Background(), u, addr)
}

func outcome(err error) string {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Outcome
	}
	if err != nil {
		return "other:" + err.Error()
	}
	return "ok"
}

func TestFetch(t *testing.T) {
	a, blockedAddr := mkOnion(1), mkOnion(2)
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Method != http.MethodGet {
			t.Error("cookie or non-GET sent")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "1"})
		io.WriteString(w, "<html><body>hello</body></html>")
	})
	mux.HandleFunc("/png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 1000))
	})
	mux.HandleFunc("/liar", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...))
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html>"+strings.Repeat("a", 200<<10))
	})
	mux.HandleFunc("/bigcl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "99999999")
		io.WriteString(w, "<html>")
	})
	mux.HandleFunc("/gzbomb", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write([]byte("<html>" + strings.Repeat("a", 10<<20)))
		zw.Close()
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(buf.Bytes())
	})
	mux.HandleFunc("/to-blocked", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+blockedAddr+".onion/", http.StatusFound)
	})
	mux.HandleFunc("/to-clearnet", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/", http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	mux.HandleFunc("/to-ok", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html>"+strings.Repeat(" ", 600))
		w.(http.Flusher).Flush()
		time.Sleep(3 * time.Second)
	})
	f, ft := setup(t, mux, map[string]bool{blockedAddr: true})
	base := "http://" + a + ".onion"

	res, err := get(t, f, base+"/ok")
	if err != nil || res.Status != 200 || !strings.Contains(string(res.Body), "hello") {
		t.Fatalf("ok: %v %+v", err, res)
	}
	if ft.users[a+".onion"] != a {
		t.Errorf("isolation user = %q, want onion address", ft.users[a+".onion"])
	}

	cases := map[string]string{
		"/png":         "content_type",
		"/liar":        "sniff",
		"/big":         "too_large",
		"/bigcl":       "too_large",
		"/gzbomb":      "too_large",
		"/to-blocked":  "redirect_blocked",
		"/to-clearnet": "redirect_offsite",
		"/loop":        "redirect_limit",
		"/slow":        "timeout",
	}
	for path, want := range cases {
		res, err := get(t, f, base+path)
		if got := outcome(err); got != want {
			t.Errorf("%s: outcome %q, want %q", path, got, want)
		}
		if res != nil && res.Body != nil {
			t.Errorf("%s: body kept on abort", path)
		}
	}

	res, err = get(t, f, base+"/to-ok")
	if err != nil || res.URL.Path != "/ok" || res.Body == nil {
		t.Errorf("redirect: %v %+v", err, res)
	}
	res, err = get(t, f, base+"/404")
	if err != nil || res.Status != 404 || res.Body != nil {
		t.Errorf("404: %v %+v", err, res)
	}
	if _, err := get(t, f, "http://"+blockedAddr+".onion/"); outcome(err) != "blocked" {
		t.Errorf("blocked start url: %v", err)
	}
}

func TestSOCKSOutcomes(t *testing.T) {
	f, ft := setup(t, http.NotFoundHandler(), nil)
	for code, want := range map[byte]string{0xF0: "desc_not_found", 0xF2: "intro_failed", 0xF3: "rend_failed",
		0xF4: "auth_required", 0xF7: "intro_timeout", 0x05: "refused"} {
		a := mkOnion(code)
		ft.mu.Lock()
		ft.failures[a+".onion"] = code
		ft.mu.Unlock()
		if _, err := get(t, f, "http://"+a+".onion/"); outcome(err) != want {
			t.Errorf("0x%02x: got %q want %q", code, outcome(err), want)
		}
	}
}

func TestPoolSticky(t *testing.T) {
	p, _ := NewPool("a:1,b:1,c:1,d:1,e:1")
	moved := 0
	q, _ := NewPool("a:1,b:1,c:1,d:1,e:1,f:1")
	for i := range 1000 {
		k := mkOnion(byte(i)) + string(rune(i))
		if p.Pick(k) != p.Pick(k) {
			t.Fatal("not deterministic")
		}
		if p.Pick(k) != q.Pick(k) {
			moved++
		}
	}
	// Adding a sixth proxy should move about 1/6 of the keys, not most of them.
	if moved > 300 {
		t.Errorf("%d/1000 keys moved after adding one proxy", moved)
	}
}
