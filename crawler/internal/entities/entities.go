// Package entities extracts identifiers worth correlating across sites:
// payment addresses, emails, PGP key blocks and onion addresses mentioned in
// text. Phishing clones usually copy a site verbatim and swap exactly these.
package entities

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"onioncrawler/internal/onion"
)

type Entity struct {
	Kind  string // btc, xmr, eth, email, pgp, onion
	Value string
}

var (
	btcBech32 = regexp.MustCompile(`\bbc1[ac-hj-np-z02-9]{11,71}\b`)
	btcLegacy = regexp.MustCompile(`\b[13][a-km-zA-HJ-NP-Z1-9]{25,34}\b`)
	xmr       = regexp.MustCompile(`\b[48][0-9AB][1-9A-HJ-NP-Za-km-z]{93}\b`)
	eth       = regexp.MustCompile(`\b0x[a-fA-F0-9]{40}\b`)
	email     = regexp.MustCompile(`\b[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9.-]+\.[A-Za-z]{2,24}\b`)
	pgpBlock  = regexp.MustCompile(`-----BEGIN PGP PUBLIC KEY BLOCK-----[\s\S]+?-----END PGP PUBLIC KEY BLOCK-----`)
)

// Extract returns distinct entities found in text. self is the page's own
// onion address, excluded from mentions.
func Extract(text, self string) []Entity {
	seen := map[Entity]bool{}
	var out []Entity
	add := func(kind, v string) {
		e := Entity{kind, v}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	for _, m := range pgpBlock.FindAllString(text, -1) {
		// Key blocks are long; the normalized body hash identifies the key.
		body := strings.Join(strings.Fields(m), "")
		h := sha256.Sum256([]byte(body))
		add("pgp", hex.EncodeToString(h[:]))
	}
	text = pgpBlock.ReplaceAllString(text, " ")
	for _, m := range btcBech32.FindAllString(strings.ToLower(text), -1) {
		add("btc", m)
	}
	for _, m := range btcLegacy.FindAllString(text, -1) {
		add("btc", m)
	}
	for _, m := range xmr.FindAllString(text, -1) {
		add("xmr", m)
	}
	for _, m := range eth.FindAllString(text, -1) {
		add("eth", strings.ToLower(m))
	}
	for _, m := range email.FindAllString(text, -1) {
		add("email", strings.ToLower(m))
	}
	for _, a := range onion.Extract(text) {
		if a != self {
			add("onion", a)
		}
	}
	return out
}
