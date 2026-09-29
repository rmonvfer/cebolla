package analyze

import (
	"math"
	"testing"
)

func TestPageRank(t *testing.T) {
	// 0->1, 0->2, 1->2, 2->0 : node 2 should rank highest.
	e := []edge{{0, 1, 1}, {0, 2, 1}, {1, 2, 1}, {2, 0, 1}}
	pr := pagerank(3, e)
	var sum float64
	for _, v := range pr {
		sum += v
	}
	if math.Abs(sum-1) > 1e-6 {
		t.Fatalf("ranks sum to %v, want 1", sum)
	}
	if pr[2] <= pr[0] || pr[2] <= pr[1] {
		t.Errorf("node 2 should rank highest: %v", pr)
	}
}

func TestPageRankDangling(t *testing.T) {
	// Node 1 is a sink; the algorithm must still converge and sum to 1.
	pr := pagerank(2, []edge{{0, 1, 1}})
	sum := pr[0] + pr[1]
	if math.Abs(sum-1) > 1e-6 {
		t.Fatalf("sum %v", sum)
	}
	if pr[1] <= pr[0] {
		t.Errorf("sink should rank higher: %v", pr)
	}
}

func TestComponentsAndLabel(t *testing.T) {
	// Two clusters {0,1,2} and {3,4}, node 5 isolated.
	e := []edge{{0, 1, 1}, {1, 2, 1}, {3, 4, 1}}
	comp := components(6, e)
	id, n := label(6, comp, false)
	if n != 3 {
		t.Fatalf("got %d components, want 3", n)
	}
	// Largest component (size 3) is labelled 0.
	if id[0] != 0 || id[1] != 0 || id[2] != 0 {
		t.Errorf("largest component not 0: %v", id)
	}
	if id[3] != id[4] || id[3] == id[0] {
		t.Errorf("second component wrong: %v", id)
	}
}

func TestLabelDropSingletons(t *testing.T) {
	// roots: {0,1} share root 0; 2 alone.
	id, n := label(3, []int{0, 0, 2}, true)
	if n != 1 {
		t.Fatalf("kept %d groups, want 1", n)
	}
	if id[0] != 0 || id[1] != 0 || id[2] != -1 {
		t.Errorf("singleton not dropped: %v", id)
	}
}
