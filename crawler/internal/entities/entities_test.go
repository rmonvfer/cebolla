package entities

import "testing"

func TestExtract(t *testing.T) {
	text := `Pay to bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq or 1BoatSLRMHzfcHYk7fpSWPoKsvj8ZSYiSE.
	ETH 0x52908400098527886E0F7030069857D2E4169EE7, contact Admin@Example.org.
	Mirror: duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion
	-----BEGIN PGP PUBLIC KEY BLOCK-----
	mQENBFexampleexampleexample
	-----END PGP PUBLIC KEY BLOCK-----`
	got := map[string]int{}
	for _, e := range Extract(text, "") {
		got[e.Kind]++
	}
	want := map[string]int{"btc": 2, "eth": 1, "email": 1, "onion": 1, "pgp": 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: got %d want %d (all %v)", k, got[k], n, got)
		}
	}
	if len(Extract(text, "duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad")) != 5 {
		t.Error("self onion not excluded")
	}
}
