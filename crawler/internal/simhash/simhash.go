// Package simhash computes 64-bit SimHash fingerprints over word shingles.
// Near-duplicate pages (mirrors, phishing clones) differ in only a few bits.
package simhash

import (
	"hash/fnv"
	"math/bits"
	"strings"
	"unicode"
)

const shingle = 3

// Of returns the SimHash of text (0 for text with no words).
func Of(text string) uint64 {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) == 0 {
		return 0
	}
	var v [64]int
	add := func(s string) {
		h := fnv.New64a()
		h.Write([]byte(s))
		x := h.Sum64()
		for i := range 64 {
			if x&(1<<i) != 0 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}
	if len(words) < shingle {
		add(strings.Join(words, " "))
	} else {
		for i := 0; i+shingle <= len(words); i++ {
			add(strings.Join(words[i:i+shingle], " "))
		}
	}
	var out uint64
	for i := range 64 {
		if v[i] > 0 {
			out |= 1 << i
		}
	}
	return out
}

// Distance is the number of differing bits; <= 3 means near-duplicate.
func Distance(a, b uint64) int { return bits.OnesCount64(a ^ b) }
