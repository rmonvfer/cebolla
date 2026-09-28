// Package sanitize turns a fetched HTML page into text-only material: it
// removes everything that can carry an image or other binary (img, svg,
// video, object, data: URIs, inline styles...), then extracts title, text
// and links. Nothing leaves this package until it has been sanitized.
package sanitize

import (
	"bytes"
	"io"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

type Link struct {
	URL    string // as written in the page (resolve with Page.Base)
	Anchor string
}

type Page struct {
	HTML  []byte // sanitized document
	Title string
	Text  string
	Links []Link
	Base  *url.URL
}

// Elements removed with their whole subtree. Anything that renders media,
// embeds another document or runs code goes.
var dropElems = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Img: true, atom.Svg: true, atom.Math: true, atom.Picture: true, atom.Source: true,
	atom.Video: true, atom.Audio: true, atom.Track: true, atom.Canvas: true,
	atom.Object: true, atom.Embed: true, atom.Applet: true, atom.Param: true,
	atom.Iframe: true, atom.Frame: true, atom.Frameset: true, atom.Link: true,
	atom.Image: true,
}

// Attributes that load or embed resources, or carry styling that can.
var dropAttrs = map[string]bool{
	"style": true, "src": true, "srcset": true, "srcdoc": true, "background": true,
	"poster": true, "lowsrc": true, "dynsrc": true, "data": true, "codebase": true,
	"archive": true, "classid": true, "xlink:href": true, "ping": true,
	"formaction": true, "action": true, "icon": true, "manifest": true,
}

// Block-level elements get a line break in the extracted text.
var blockElems = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true, atom.Li: true, atom.Tr: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true,
	atom.Nav: true, atom.Aside: true, atom.Blockquote: true, atom.Pre: true,
	atom.Table: true, atom.Ul: true, atom.Ol: true, atom.Dl: true, atom.Dt: true,
	atom.Dd: true, atom.Hr: true, atom.Form: true, atom.Fieldset: true, atom.Main: true,
	atom.Td: true, atom.Th: true, atom.Title: true, atom.Address: true, atom.Figcaption: true,
}

// Process decodes body (using the Content-Type charset or <meta>), sanitizes
// it and extracts title, text and links. pageURL is used as the link base
// unless the page declares an onion <base href>.
func Process(body []byte, contentType string, pageURL *url.URL) (*Page, error) {
	var r io.Reader = bytes.NewReader(body)
	// An unknown or broken charset label should not cost us the page: fall
	// back to the raw bytes, which the HTML parser reads as UTF-8.
	if cr, err := charset.NewReader(bytes.NewReader(body), contentType); err == nil {
		r = cr
	}
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	p := &Page{Base: pageURL}
	clean(doc, p)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return nil, err
	}
	p.HTML = buf.Bytes()

	var tb strings.Builder
	text(doc, &tb)
	p.Text = tidy(tb.String())
	p.Title = strings.Join(strings.Fields(p.Title), " ")
	return p, nil
}

func clean(n *html.Node, p *Page) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch c.Type {
		case html.CommentNode:
			n.RemoveChild(c)
		case html.ElementNode:
			if dropElems[c.DataAtom] || c.Namespace == "svg" || c.Namespace == "math" {
				n.RemoveChild(c)
				break
			}
			cleanAttrs(c)
			switch c.DataAtom {
			case atom.Title:
				if p.Title == "" {
					p.Title = textOf(c)
				}
			case atom.Base:
				if b, err := url.Parse(attr(c, "href")); err == nil && b.IsAbs() &&
					strings.HasSuffix(strings.ToLower(b.Hostname()), ".onion") {
					p.Base = b
				}
			case atom.A, atom.Area:
				if h := attr(c, "href"); h != "" {
					p.Links = append(p.Links, Link{URL: h, Anchor: trunc(strings.Join(strings.Fields(textOf(c)), " "), 300)})
				}
			}
			clean(c, p)
		}
		c = next
	}
}

func cleanAttrs(n *html.Node) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		k := strings.ToLower(a.Key)
		if dropAttrs[k] || strings.HasPrefix(k, "on") || a.Namespace != "" {
			continue
		}
		if hasDataURI(a.Val) {
			continue
		}
		kept = append(kept, a)
	}
	n.Attr = kept
}

// hasDataURI catches "data:" hidden by case, whitespace or entities that
// the parser already decoded.
func hasDataURI(v string) bool {
	var b strings.Builder
	for _, r := range v {
		if !unicode.IsSpace(r) && r != 0 {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	s := b.String()
	return strings.Contains(s, "data:") || strings.Contains(s, "javascript:") || strings.Contains(s, "vbscript:")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func text(n *html.Node, b *strings.Builder) {
	if n.Type == html.TextNode {
		// Source newlines are layout, not structure: only block elements break lines.
		b.WriteString(strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}
			return r
		}, n.Data))
		return
	}
	if n.Type == html.ElementNode && n.DataAtom == atom.Head {
		return // title is kept separately; head has no visible text
	}
	block := n.Type == html.ElementNode && blockElems[n.DataAtom]
	if block {
		b.WriteByte('\n')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text(c, b)
	}
	if block {
		b.WriteByte('\n')
	}
}

// tidy collapses whitespace within lines and drops empty lines.
func tidy(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
