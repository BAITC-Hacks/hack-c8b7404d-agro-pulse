package analytics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/normalize"
)

func monthIndex(d time.Time) float64 { return float64(d.Year()*12 + int(d.Month())) }

func seasonalProfile(input [12]model.Number) ([12]float64, error) {
	var profile [12]float64
	mean := 0.0
	for i, n := range input {
		if !n.Valid || !normalize.Finite(n.Value) || n.Value <= 0 {
			return profile, fmt.Errorf("seasonality month %d NOT PROVIDED", i+1)
		}
		mean += n.Value / 12
	}
	for i, n := range input {
		profile[i] = n.Value / mean
	}
	return profile, nil
}

// Forecast uses only complete months before AsOf, a normalized supplied seasonal
// profile and a robust Theil-Sen trend on deseasonalized demand. Coefficients in
// the supplied workbook are an as-of input, not suitable for historical backtests.
func Forecast(p *model.Product, season [12]model.Number, cfg model.Config) (model.ForecastResult, error) {
	return ForecastWithEvidence(p, season, cfg, nil)
}

// ForecastWithEvidence shares the normal engine; confirmed evidence is opt-in.
// The strict real-data pipeline continues to call Forecast without evidence.
func ForecastWithEvidence(p *model.Product, season [12]model.Number, cfg model.Config, evidence []model.StockoutEvidence) (model.ForecastResult, error) {
	profile, err := seasonalProfile(season)
	if err != nil {
		return model.ForecastResult{}, err
	}
	result, err := prepareDemand(p, profile, cfg)
	if err != nil {
		return result, err
	}
	if len(evidence) > 0 {
		months := map[time.Time]bool{}
		for _, h := range result.History {
			months[normalize.Month(h.Month)] = true
		}
		for _, e := range evidence {
			if !e.Confirmed || !months[normalize.Month(e.Month)] {
				return result, fmt.Errorf("stockout evidence must be confirmed and match an observed month")
			}
		}
		result.History, err = CorrectStockouts(result.History, evidence)
		if err != nil {
			return result, err
		}
		var warnings []string
		for _, w := range result.Warnings {
			if w != "stockout_duration_NOT_PROVIDED" {
				warnings = append(warnings, w)
			}
		}
		result.Warnings = warnings
	}
	return ForecastSeries(result, profile, cfg)
}

// Analyze reports nonseasonal history statistics only. A neutral scale here is
// not a substitute seasonal coefficient and is never exposed as a forecast.
func Analyze(p *model.Product, cfg model.Config) (model.DemandAnalysis, error) {
	var scale [12]float64
	for i := range scale {
		scale[i] = 1
	}
	result, err := prepareDemand(p, scale, cfg)
	a := model.DemandAnalysis{History: result.History, Outliers: result.Outliers, Warnings: result.Warnings}
	if err != nil {
		return a, err
	}
	a.Warnings = append(a.Warnings, "diagnostic_history_only; seasonality_not_applied; trend_not_seasonally_adjusted")
	if len(a.History) < 4 {
		a.Warnings = append(a.Warnings, "short_history; trend_is_not_evidence_of_sustained_growth")
	}
	for i := range a.Outliers {
		if a.Outliers[i].Method == "monthly_deseasonalized_tukey_outer_fence" {
			a.Outliers[i].Method = "monthly_unadjusted_tukey_outer_fence"
		}
	}
	var slopes []float64
	for i, h := range a.History {
		a.RegularDemand += h.Regular / float64(len(a.History))
		a.Baseline += h.Observed / float64(len(a.History))
		for j := i + 1; j < len(a.History); j++ {
			slopes = append(slopes, (a.History[j].Regular-h.Regular)/(monthIndex(a.History[j].Month)-monthIndex(h.Month)))
		}
	}
	if len(slopes) > 0 {
		slope := median(slopes)
		if !normalize.Finite(slope) {
			return a, fmt.Errorf("trend overflow")
		}
		a.Trend = &slope
	}
	if !normalize.Finite(a.RegularDemand) || !normalize.Finite(a.Baseline) {
		return a, fmt.Errorf("demand overflow")
	}
	return a, nil
}

func prepareDemand(p *model.Product, profile [12]float64, cfg model.Config) (model.ForecastResult, error) {
	result := model.ForecastResult{Warnings: []string{"stockout_duration_NOT_PROVIDED"}}
	if cfg.AsOf.IsZero() || cfg.Months < 1 || cfg.Months > 24 {
		return result, fmt.Errorf("as-of and horizon 1..24 months required")
	}
	cutoff := normalize.Month(cfg.AsOf)
	txSum := map[time.Time]float64{}
	txExcess := map[time.Time]float64{}
	positive := []float64{}
	sales := []model.Sale{}
	for _, s := range p.Sales {
		if !s.Date.Before(cutoff) || s.Date.Year() < 2025 {
			continue
		}
		if !strings.HasPrefix(s.Document, "Расходная накладная") {
			continue
		}
		if !normalize.Finite(s.Quantity) {
			return result, fmt.Errorf("invalid sale")
		}
		m := normalize.Month(s.Date)
		txSum[m] += s.Quantity
		if s.Quantity > 0 {
			positive = append(positive, s.Quantity)
			sales = append(sales, s)
		}
	}
	clean, indices, err := CapOutliers(positive)
	if err != nil {
		return result, err
	}
	for _, i := range indices {
		m := normalize.Month(sales[i].Date)
		txExcess[m] += positive[i] - clean[i]
	}
	applied := map[time.Time]bool{}
	missing, negative, mismatch := 0, 0, 0
	seen := map[time.Time]bool{}
	for _, m := range p.Monthly {
		if !m.Month.Before(cutoff) {
			continue
		}
		if seen[m.Month] {
			return result, fmt.Errorf("duplicate monthly observation")
		}
		seen[m.Month] = true
		if !m.Quantity.Valid {
			missing++
			if !cfg.BlankSalesZero {
				continue
			}
		}
		v := m.Quantity.Value
		if !normalize.Finite(v) {
			return result, fmt.Errorf("invalid monthly demand")
		}
		regular := math.Max(0, v)
		if v < 0 {
			negative++
			continue // Return policy is unresolved; never silently turn signed net sales into zero demand.
		}
		if total, exists := txSum[m.Month]; exists {
			if math.Abs(total-v) <= 1e-6*math.Max(1, math.Abs(v)) {
				regular = math.Max(0, regular-txExcess[m.Month])
				applied[m.Month] = true
			} else {
				mismatch++
			}
		}
		result.History = append(result.History, model.DemandPoint{Month: m.Month, Observed: v, Regular: regular, Corrected: regular, Source: m.Quantity.Source})
	}
	if len(result.History) == 0 {
		return result, fmt.Errorf("sales history NOT PROVIDED")
	}
	sort.Slice(result.History, func(i, j int) bool { return result.History[i].Month.Before(result.History[j].Month) })
	for _, i := range indices {
		result.Outliers = append(result.Outliers, model.Outlier{Source: sales[i].Source, Original: positive[i], Adjusted: clean[i], Method: "transaction_tukey_outer_fence", Applied: applied[normalize.Month(sales[i].Date)]})
	}
	if missing > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("missing_sales_months=%d; zero_assumption=%t", missing, cfg.BlankSalesZero))
	}
	if negative > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("negative_net_sales_months_excluded_pending_return_policy=%d", negative))
	}
	if mismatch > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("transaction_monthly_mismatches=%d; monthly_source_retained", mismatch))
	}
	if len(positive) < 4 {
		result.Warnings = append(result.Warnings, "insufficient_transactions_for_outlier_fence")
	}
	// Monthly fallback also protects months without reconcilable transaction detail.
	levels := make([]float64, len(result.History))
	for i, h := range result.History {
		levels[i] = h.Regular / profile[int(h.Month.Month())-1]
	}
	bounded, monthlyOutliers, err := CapOutliers(levels)
	if err != nil {
		return result, err
	}
	for _, i := range monthlyOutliers {
		h := &result.History[i]
		adjusted := bounded[i] * profile[int(h.Month.Month())-1]
		result.Outliers = append(result.Outliers, model.Outlier{Source: h.Source, Original: h.Regular, Adjusted: adjusted, Method: "monthly_deseasonalized_tukey_outer_fence", Applied: true})
		h.Regular = adjusted
	}
	result.History, err = CorrectStockouts(result.History, nil)
	if err != nil {
		return result, err
	}
	return result, nil
}

// ForecastSeries accepts already corrected history so confirmed stockout
// evidence can be integrated without adding lost demand to the final order twice.
func ForecastSeries(result model.ForecastResult, profile [12]float64, cfg model.Config) (model.ForecastResult, error) {
	if len(result.History) == 0 {
		return result, fmt.Errorf("empty demand history")
	}
	if cfg.AsOf.IsZero() || cfg.Months < 1 || cfg.Months > 24 {
		return result, fmt.Errorf("invalid forecast horizon")
	}
	for _, v := range profile {
		if !normalize.Finite(v) || v <= 0 {
			return result, fmt.Errorf("invalid seasonal profile")
		}
	}
	base := monthIndex(result.History[0].Month)
	xs := []float64{}
	ys := []float64{}
	average := 0.0
	for _, p := range result.History {
		if !p.Month.Before(normalize.Month(cfg.AsOf)) || !normalize.Finite(p.Corrected) || p.Corrected < 0 {
			return result, fmt.Errorf("invalid or future history")
		}
		xs = append(xs, monthIndex(p.Month)-base)
		ys = append(ys, p.Corrected/profile[int(p.Month.Month())-1])
		average += p.Regular / float64(len(result.History))
		result.StockoutAdjustment += p.Lost
	}
	slopes := []float64{}
	for i := range xs {
		for j := i + 1; j < len(xs); j++ {
			if xs[j] <= xs[i] {
				return result, fmt.Errorf("history not strictly chronological")
			}
			slopes = append(slopes, (ys[j]-ys[i])/(xs[j]-xs[i]))
		}
	}
	slope := median(slopes)
	intercepts := make([]float64, len(ys))
	for i, y := range ys {
		intercepts[i] = y - slope*xs[i]
	}
	intercept := median(intercepts)
	result.TrendPerMonth = slope
	if len(ys) < 4 {
		result.Warnings = append(result.Warnings, "short_history_forecast_requires_review")
	}
	end := cfg.AsOf.AddDate(0, cfg.Months, 0)
	noSeason := 0.0
	for start := cfg.AsOf; start.Before(end); {
		m := normalize.Month(start)
		next := m.AddDate(0, 1, 0)
		if next.After(end) {
			next = end
		}
		fraction := next.Sub(start).Hours() / 24 / float64(daysInMonth(m))
		level := math.Max(0, intercept+slope*(monthIndex(m)-base))
		quantity := level * profile[int(m.Month())-1] * fraction
		result.Periods = append(result.Periods, model.ForecastPeriod{Start: start, End: next, Quantity: quantity, Seasonality: profile[int(m.Month())-1]})
		result.Quantity += quantity
		noSeason += level * fraction
		result.Baseline += average * fraction
		start = next
	}
	result.SeasonalityImpact = result.Quantity - noSeason
	if !normalize.Finite(result.Quantity) || !normalize.Finite(result.Baseline) || !normalize.Finite(slope) || !normalize.Finite(result.SeasonalityImpact) {
		return result, fmt.Errorf("forecast overflow")
	}
	return result, nil
}
