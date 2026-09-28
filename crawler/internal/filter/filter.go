// Package filter decides whether text touches child-abuse material. A match
// means the page, and its whole site, is discarded and never stored.
//
// Matching is deliberately aggressive: a false positive costs one site, a
// false negative costs far more.
package filter

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Filter holds two kinds of terms, loaded from a file:
//
//	two words     phrase: matches as consecutive whole words
//	~token        substring: matches anywhere, also with separators removed
//	              (catches "token2024", "t.o.k.e.n", URL paths)
type Filter struct {
	phrases [][]string
	subs    []string
}

// Load reads a term file (one term per line, "#" comments). It fails when
// the file yields no terms, so the crawler cannot run with the filter off.
func Load(path string) (*Filter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("filter terms: %w", err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return New(lines)
}

func New(lines []string) (*Filter, error) {
	ft := &Filter{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "~") {
			if t := squash(normalize(l[1:])); t != "" {
				ft.subs = append(ft.subs, t)
			}
			continue
		}
		if ws := strings.Fields(normalize(l)); len(ws) > 0 {
			ft.phrases = append(ft.phrases, ws)
		}
	}
	if ft.Len() == 0 {
		return nil, errors.New("filter terms: no terms loaded; refusing to run without a content filter")
	}
	return ft, nil
}

func (f *Filter) Len() int { return len(f.phrases) + len(f.subs) }

// Match reports whether any of the given strings matches a term.
func (f *Filter) Match(texts ...string) bool {
	for _, t := range texts {
		if t == "" {
			continue
		}
		n := normalize(t)
		if len(f.subs) > 0 {
			sq := squash(n)
			for _, s := range f.subs {
				if strings.Contains(n, s) || strings.Contains(sq, s) {
					return true
				}
			}
		}
		if len(f.phrases) > 0 && matchPhrases(strings.Fields(n), f.phrases) {
			return true
		}
	}
	return false
}

func matchPhrases(words []string, phrases [][]string) bool {
	for i := range words {
	next:
		for _, p := range phrases {
			if i+len(p) > len(words) {
				continue
			}
			for j, w := range p {
				if words[i+j] != w {
					continue next
				}
			}
			return true
		}
	}
	return false
}

// normalize folds compatibility characters (fullwidth, ligatures), lowercases,
// strips diacritics and turns everything that is not a letter or digit into a
// space.
func normalize(s string) string {
	s = norm.NFKD.String(s)
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
			// combining mark left over from NFKD: drop
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func squash(normalized string) string { return strings.ReplaceAll(normalized, " ", "") }
