package analytics

import (
	"math"
	"testing"
)

func TestGiantTransaction(t *testing.T) {
	v := []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 1e9}
	out, idx, err := CapOutliers(v)
	if err != nil || len(idx) != 1 || idx[0] != 9 {
		t.Fatal(out, idx, err)
	}
	if out[9] != 10 || v[9] != 1e9 {
		t.Fatal("giant order leaked or source mutated")
	}
}
func TestOutlierEdges(t *testing.T) {
	for _, v := range [][]float64{{}, {0}, {1}, {1, 2, 3}} {
		if _, _, err := CapOutliers(v); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), -1} {
		if _, _, err := CapOutliers([]float64{v}); err == nil {
			t.Fatal("accepted invalid value")
		}
	}
}
