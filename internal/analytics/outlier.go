package analytics

import (
	"fmt"
	"math"
	"sort"

	"github.com/AlisherBaitas/agro-pulse/internal/normalize"
)

func median(a []float64) float64 {
	if len(a) == 0 {
		return 0
	}
	v := append([]float64(nil), a...)
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return v[n/2-1]/2 + v[n/2]/2
}

func quantile(sorted []float64, p float64) float64 {
	x := p * float64(len(sorted)-1)
	i := int(x)
	if i == len(sorted)-1 {
		return sorted[i]
	}
	f := x - float64(i)
	return sorted[i]*(1-f) + sorted[i+1]*f
}

// CapOutliers uses the standard Tukey outer fence Q3+3*IQR (MVP method choice).
// Zeros are retained, but the fence is fitted to positive observed sizes.
// With fewer than four positive observations there is insufficient evidence.
func CapOutliers(values []float64) ([]float64, []int, error) {
	out := append([]float64(nil), values...)
	var positive []float64
	for _, v := range values {
		if !normalize.Finite(v) || v < 0 {
			return nil, nil, fmt.Errorf("outlier input must be finite and nonnegative")
		}
		if v > 0 {
			positive = append(positive, v)
		}
	}
	if len(positive) < 4 {
		return out, nil, nil
	}
	sort.Float64s(positive)
	q1, q3 := quantile(positive, .25), quantile(positive, .75)
	cap := q3 + 3*(q3-q1)
	if !normalize.Finite(cap) {
		return nil, nil, fmt.Errorf("outlier fence overflow")
	}
	var indices []int
	for i, v := range values {
		if v > cap {
			out[i] = math.Max(0, cap)
			indices = append(indices, i)
		}
	}
	return out, indices, nil
}
