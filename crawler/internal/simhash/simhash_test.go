package simhash

import (
	"strings"
	"testing"
)

func TestNearDuplicates(t *testing.T) {
	base := strings.Repeat("the quick brown fox jumps over the lazy dog while the market lists many items for sale ", 20)
	clone := base + " send payment to bc1qexampleaddress"
	other := strings.Repeat("completely different content about gardening tomatoes and soil and watering schedules ", 20)

	if d := Distance(Of(base), Of(clone)); d > 3 {
		t.Errorf("clone distance %d, want <= 3", d)
	}
	if d := Distance(Of(base), Of(other)); d < 10 {
		t.Errorf("unrelated distance %d, want >= 10", d)
	}
	if Of("") != 0 || Of("one two") == 0 {
		t.Error("edge cases")
	}
}
