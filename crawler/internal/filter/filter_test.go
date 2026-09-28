package filter

import (
	"os"
	"path/filepath"
	"testing"
)

// Neutral stand-in terms: the behaviour under test is the matching, not the list.
func testFilter(t *testing.T) *Filter {
	f, err := New([]string{"# comment", "", "forbidden topic", "~badtoken", "  Crème Brûlée  "})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestMatch(t *testing.T) {
	f := testFilter(t)
	hits := []string{
		"a page about the FORBIDDEN   topic here",
		"forbidden-topic",
		"/path/badtoken2024/index.html",
		"b.a.d.t.o.k.e.n",
		"ｂａｄｔｏｋｅｎ", // fullwidth
		"creme brulee recipe",
	}
	for _, s := range hits {
		if !f.Match(s) {
			t.Errorf("missed %q", s)
		}
	}
	misses := []string{"forbidden", "a topic", "forbiddentopic x", "good token only", ""}
	for _, s := range misses {
		if f.Match(s) {
			t.Errorf("false hit %q", s)
		}
	}
	if !f.Match("clean", "", "has forbidden topic") {
		t.Error("variadic match missed")
	}
}

func TestFailsClosed(t *testing.T) {
	if _, err := New([]string{"# only comments", "  "}); err == nil {
		t.Fatal("empty term list accepted")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Fatal("missing file accepted")
	}
	p := filepath.Join(t.TempDir(), "terms.txt")
	os.WriteFile(p, []byte("~abc\n"), 0o600)
	if f, err := Load(p); err != nil || f.Len() != 1 {
		t.Fatal(err)
	}
}
