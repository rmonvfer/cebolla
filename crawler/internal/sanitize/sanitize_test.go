package sanitize

import (
	"net/url"
	"strings"
	"testing"
)

const page = `<!doctype html><html><head>
<meta charset="utf-8"><title>  Hello
 World </title>
<style>body{background:url(data:image/png;base64,AAAA)}</style>
<link rel="icon" href="data:image/png;base64,AAAA">
<script>alert(1)</script>
</head><body background="x.png" style="background-image:url(data:image/gif;base64,R0lG)">
<h1 onclick="x()">Title here</h1>
<p>First <b>para</b>.</p>
<img src="data:image/png;base64,iVBORw0KGgo=" alt="pic">
<picture><source srcset="a.webp"><img src="a.jpg"></picture>
<svg><image href="data:image/png;base64,AAAA"/></svg>
<object data="movie.swf"></object><video poster="p.jpg" src="v.mp4"></video>
<iframe srcdoc="<img src=x>"></iframe>
<a href="/rel/page">Relative   link</a>
<a href="  DaTa:text/html;base64,PHNjcmlwdD4=">sneaky</a>
<a href="javascript:alert(1)">js</a>
<div style="x">Second<br>line</div>
<form action="/login"><input type="image" src="btn.png"><input name="q"></form>
<!-- a comment with data:image/png;base64,AAAA -->
</body></html>`

func TestProcess(t *testing.T) {
	base, _ := url.Parse("http://example.onion/dir/")
	p, err := Process([]byte(page), "text/html; charset=utf-8", base)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.ToLower(string(p.HTML))
	for _, bad := range []string{"data:", "<img", "<svg", "<picture", "<source", "<video", "<object",
		"<iframe", "<script", "<style", "<link", "style=", "src=", "srcset", "background=", "poster",
		"onclick", "javascript:", "action=", "<!--"} {
		if strings.Contains(out, bad) {
			t.Errorf("sanitized HTML still contains %q", bad)
		}
	}
	if p.Title != "Hello World" {
		t.Errorf("title %q", p.Title)
	}
	wantText := "Title here\nFirst para.\nRelative link sneaky js\nSecond\nline"
	if p.Text != wantText {
		t.Errorf("text:\n%q\nwant\n%q", p.Text, wantText)
	}
	if len(p.Links) != 1 || p.Links[0].URL != "/rel/page" || p.Links[0].Anchor != "Relative link" {
		t.Errorf("links %+v", p.Links)
	}
}

func TestCharsetAndBase(t *testing.T) {
	body := []byte("<html><head><base href='http://abc.onion/x/'></head><body>caf\xe9 <a href='y'>y</a></body></html>")
	base, _ := url.Parse("http://other.onion/")
	p, err := Process(body, "text/html; charset=iso-8859-1", base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Text, "café") {
		t.Errorf("charset not decoded: %q", p.Text)
	}
	if p.Base.String() != "http://abc.onion/x/" {
		t.Errorf("base %v", p.Base)
	}
	// A clearnet <base> must not redirect link resolution off the onion.
	p, _ = Process([]byte("<base href='http://evil.com/'><a href='z'>z</a>"), "text/html", base)
	if p.Base.String() != base.String() {
		t.Errorf("clearnet base accepted: %v", p.Base)
	}
}

func TestUnknownCharset(t *testing.T) {
	base, _ := url.Parse("http://example.onion/")
	p, err := Process([]byte("<p>hello</p>"), "text/html; charset=x-made-up-charset", base)
	if err != nil || p.Text != "hello" {
		t.Fatalf("got %v %+v", err, p)
	}
}
