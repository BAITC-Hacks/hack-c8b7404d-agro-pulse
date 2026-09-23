package calculator

import (
	"fmt"
	"sort"
	"time"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/normalize"
)

// CurrentStock never substitutes an older valid value for a newer missing one.
func CurrentStock(stocks []model.Stock, cfg model.Config) (*model.Stock, []string) {
	var selected *model.Stock
	for _, s := range stocks {
		if s.Date.After(cfg.AsOf) {
			continue
		}
		if selected == nil || s.Date.After(selected.Date) || (s.Date.Equal(selected.Date) && s.Kind != "opening") {
			copy := s
			selected = &copy
		}
	}
	if selected == nil {
		return nil, []string{"current_stock_NOT_PROVIDED"}
	}
	if !selected.Quantity.Valid || !normalize.Finite(selected.Quantity.Value) || selected.Quantity.Value < 0 {
		return nil, []string{"latest_stock_missing_or_invalid"}
	}
	if selected.Kind == "opening" {
		return nil, []string{"current_stock_NOT_PROVIDED; only opening stock at " + selected.Date.Format("2006-01-02")}
	}
	if !selected.Date.Equal(cfg.AsOf) {
		return nil, []string{"snapshot_date_does_not_match_as_of"}
	}
	return selected, nil
}

func IncomingStock(p *model.Product, cfg model.Config) (float64, []model.Incoming, []string, error) {
	if !p.IncomingListed {
		return 0, nil, nil, fmt.Errorf("incoming SKU NOT PROVIDED")
	}
	var included []model.Incoming
	total := 0.0
	end := cfg.AsOf.AddDate(0, cfg.Months, 0)
	for _, v := range p.Incoming {
		if !v.Quantity.Valid {
			return 0, nil, nil, fmt.Errorf("incoming quantity NOT PROVIDED or invalid at %s!%s", v.Quantity.Source.Sheet, v.Quantity.Source.Cell)
		}
		if !normalize.Finite(v.Quantity.Value) || v.Quantity.Value < 0 {
			return 0, nil, nil, fmt.Errorf("invalid incoming quantity")
		}
		if v.Due.IsZero() {
			return 0, nil, nil, fmt.Errorf("incoming date NOT PROVIDED")
		}
		if v.Quantity.Value > 0 && v.Due.Before(cfg.AsOf) {
			return 0, nil, nil, fmt.Errorf("overdue shipment: delivery status NOT PROVIDED")
		}
		if v.Due.Before(end) && !v.Due.Before(cfg.AsOf) {
			total += v.Quantity.Value
			included = append(included, v)
		}
	}
	if !normalize.Finite(total) {
		return 0, nil, nil, fmt.Errorf("incoming overflow")
	}
	sort.SliceStable(included, func(i, j int) bool { return included[i].Due.Before(included[j].Due) })
	return total, included, nil, nil
}

// Urgency checks inventory before each dated receipt, not only the horizon total.
// MVP assumption: forecast consumption is uniform within each calendar month;
// a receipt becomes available at the start of its indicated date.
func Urgency(stock float64, f model.ForecastResult, incoming []model.Incoming) string {
	if f.Quantity <= 0 {
		return "covered"
	}
	if stock <= 0 {
		return "now"
	}
	if len(f.Periods) == 0 {
		return "unknown"
	}
	start := f.Periods[0].Start
	end := f.Periods[len(f.Periods)-1].End
	dates := []time.Time{start, end}
	receipts := map[time.Time]float64{}
	for _, v := range incoming {
		if !v.Due.Before(start) && v.Due.Before(end) {
			dates = append(dates, v.Due)
			receipts[v.Due] += v.Quantity.Value
		}
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	balance := stock
	last := start
	for _, d := range dates {
		if d.Before(last) || d.Equal(last) && !d.Equal(start) {
			continue
		}
		if d.After(last) {
			for _, p := range f.Periods {
				a, b := last, d
				if a.Before(p.Start) {
					a = p.Start
				}
				if b.After(p.End) {
					b = p.End
				}
				if b.After(a) {
					balance -= p.Quantity * b.Sub(a).Hours() / p.End.Sub(p.Start).Hours()
				}
			}
			if balance < 0 {
				return "within_horizon"
			}
		}
		balance += receipts[d]
		delete(receipts, d)
		last = d
	}
	return "covered"
}
