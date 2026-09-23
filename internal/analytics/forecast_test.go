package analytics

import (
	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"math"
	"testing"
	"time"
)

func TestSeasonalityAndTrend(t *testing.T) {
	var season [12]float64
	for i := range season {
		season[i] = 1
	}
	season[0] = 2
	for _, slope := range []float64{-2, 0, 2} {
		r := model.ForecastResult{}
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 24; i++ {
			d := start.AddDate(0, i, 0)
			n := (100 + slope*float64(i)) * season[int(d.Month())-1]
			r.History = append(r.History, model.DemandPoint{Month: d, Observed: n, Regular: n, Corrected: n})
		}
		f, err := ForecastSeries(r, season, model.Config{AsOf: start.AddDate(0, 24, 0), Months: 1})
		want := (100 + slope*24) * 2
		if err != nil || math.Abs(f.Quantity-want) > 1e-8 || math.Abs(f.TrendPerMonth-slope) > 1e-8 {
			t.Fatal(f.Quantity, want, err)
		}
	}
}

func TestForecastHugeSaleAndFutureExclusion(t *testing.T) {
	var season [12]model.Number
	for i := range season {
		season[i] = model.Number{Value: 1, Valid: true}
	}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &model.Product{}
	for i := 0; i < 13; i++ {
		n := 100.0
		if i == 7 || i == 12 {
			n = 1e8
		}
		p.Monthly = append(p.Monthly, model.MonthlySale{Month: start.AddDate(0, i, 0), Quantity: model.Number{Value: n, Valid: true}})
	}
	f, err := Forecast(p, season, model.Config{AsOf: start.AddDate(0, 12, 0), Months: 1})
	if err != nil || math.Abs(f.Quantity-100) > 1e-6 || len(f.History) != 12 || len(f.Outliers) != 1 {
		t.Fatal(f.Quantity, len(f.History), f.Outliers, err)
	}
}

func TestForecastMissingNotZero(t *testing.T) {
	var season [12]model.Number
	for i := range season {
		season[i] = model.Number{Value: 1, Valid: true}
	}
	p := &model.Product{Monthly: []model.MonthlySale{{Month: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}}}
	c := model.Config{AsOf: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Months: 1}
	if _, err := Forecast(p, season, c); err == nil {
		t.Fatal("invented demand")
	}
	c.BlankSalesZero = true
	f, err := Forecast(p, season, c)
	if err != nil || f.Quantity != 0 {
		t.Fatal(f, err)
	}
}

func TestDiagnosticDoesNotInventSeasonalityOrReturns(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &model.Product{Monthly: []model.MonthlySale{
		{Month: start, Quantity: model.Number{Value: 10, Valid: true}},
		{Month: start.AddDate(0, 1, 0), Quantity: model.Number{}},
		{Month: start.AddDate(0, 2, 0), Quantity: model.Number{Value: -5, Valid: true}},
		{Month: start.AddDate(0, 3, 0), Quantity: model.Number{Value: 40, Valid: true}},
	}}
	a, err := Analyze(p, model.Config{AsOf: start.AddDate(0, 5, 0), Months: 1})
	if err != nil || len(a.History) != 2 || a.SeasonalityImpact != nil || a.RegularDemand != 25 || a.Trend == nil || *a.Trend != 10 {
		t.Fatal(a, err)
	}
}
