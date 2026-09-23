package analytics

import (
	"fmt"
	"time"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/normalize"
)

func daysInMonth(d time.Time) int { return normalize.Month(d).AddDate(0, 1, -1).Day() }

// CorrectStockouts estimates lost units from the median daily demand of other
// observed, non-stockout months. Caller must supply confirmed durations.
// Empty stock cells and monthly opening snapshots are not duration evidence.
func CorrectStockouts(points []model.DemandPoint, evidence []model.StockoutEvidence) ([]model.DemandPoint, error) {
	out := append([]model.DemandPoint(nil), points...)
	confirmed := map[time.Time]int{}
	for _, e := range evidence {
		if !e.Confirmed {
			continue
		}
		m := normalize.Month(e.Month)
		if e.Days < 1 || e.Days > daysInMonth(m) {
			return nil, fmt.Errorf("invalid stockout duration")
		}
		if _, ok := confirmed[m]; ok {
			return nil, fmt.Errorf("duplicate stockout evidence")
		}
		confirmed[m] = e.Days
	}
	var rates []float64
	for _, p := range points {
		if !normalize.Finite(p.Regular) || p.Regular < 0 {
			return nil, fmt.Errorf("invalid regular demand")
		}
		if confirmed[normalize.Month(p.Month)] == 0 {
			rates = append(rates, p.Regular/float64(daysInMonth(p.Month)))
		}
	}
	if len(confirmed) > 0 && len(rates) == 0 {
		return nil, fmt.Errorf("no non-stockout observations for lost demand estimate")
	}
	for i := range out {
		out[i].Lost = 0
		if days := confirmed[normalize.Month(out[i].Month)]; days > 0 {
			out[i].Lost = median(rates) * float64(days)
		}
		out[i].Corrected = out[i].Regular + out[i].Lost
		if !normalize.Finite(out[i].Corrected) {
			return nil, fmt.Errorf("stockout correction overflow")
		}
	}
	return out, nil
}
