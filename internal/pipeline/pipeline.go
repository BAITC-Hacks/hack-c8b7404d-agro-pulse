// Package pipeline coordinates the same core for all suppliers.
package pipeline

import (
	"sort"

	"github.com/AlisherBaitas/agro-pulse/internal/analytics"
	"github.com/AlisherBaitas/agro-pulse/internal/calculator"
	"github.com/AlisherBaitas/agro-pulse/internal/explain"
	"github.com/AlisherBaitas/agro-pulse/internal/model"
)

var Assumptions = []string{
	"MVP ASSUMPTION: default horizon 1 calendar month; configurable; safety stock is not applied",
	"MVP ASSUMPTION: Tukey outer fence Q3+3*IQR; winsorization; >=4 positive observations; monthly fallback uses deseasonalized levels",
	"MVP ASSUMPTION: Theil-Sen trend; diagnostic trend has no seasonal adjustment",
	"MVP ASSUMPTION: baseline is mean observed demand; missing and negative months are excluded with warnings, never filled with zero",
	"MVP ASSUMPTION: recommended quantity cannot be negative",
	"NOT PROVIDED: confirmed stockout duration; no lost demand is fabricated from monthly opening snapshots",
	"All numerical recommendations are drafts for human review; no automatic supplier submission",
}

func Run(d model.Dataset, cfg model.Config) model.SupplierReport {
	return run(d, cfg, false)
}

func run(d model.Dataset, cfg model.Config, demo bool) model.SupplierReport {
	// Deprecated permissive options are not business approval; ignore them.
	cfg.BlankSalesZero = false
	cfg.AllowOpeningStock = false
	cfg.AbsentIncomingZero = false
	report := model.SupplierReport{Source: d.Source, Supplier: d.Supplier, Quality: d.Quality, Seasonality: d.Seasonality}
	keys := make([]string, 0, len(d.Products))
	for k := range d.Products {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, sku := range keys {
		p := d.Products[sku]
		r := model.Recommendation{Supplier: d.Supplier, SKU: sku, Name: p.Name, Unit: p.Unit, Category: p.Category, Status: "needs_data", MOQ: p.MOQ, Urgency: "unknown", Warnings: append([]string(nil), p.Warnings...)}
		r.StockoutStatus = "not_available"
		if len(p.Stocks) > 0 {
			r.StockoutStatus = "not_confirmed"
		}
		r.SeasonalityStatus = "sku_scope_unconfirmed"
		a, analysisErr := analytics.Analyze(p, cfg)
		if analysisErr != nil {
			r.NeedsData = append(r.NeedsData, "sales_history_unavailable")
			r.Warnings = append(r.Warnings, analysisErr.Error())
		} else {
			r.Analysis = &a
			r.Warnings = append(r.Warnings, a.Warnings...)
		}
		if p.Seasonality == nil {
			r.NeedsData = append(r.NeedsData, "seasonality_sku_scope_unconfirmed")
		} else {
			var f model.ForecastResult
			var err error
			if demo {
				f, err = analytics.ForecastWithEvidence(p, *p.Seasonality, cfg, p.Stockouts)
			} else {
				f, err = analytics.Forecast(p, *p.Seasonality, cfg)
			}
			if err != nil {
				r.NeedsData = append(r.NeedsData, "forecast_unavailable")
				r.Warnings = append(r.Warnings, err.Error())
			} else {
				r.Forecast = &f
				r.SeasonalityStatus = "confirmed_sku_profile"
			}
		}
		for _, s := range p.Stocks {
			if !s.Date.After(cfg.AsOf) && (r.LatestStockObservation == nil || s.Date.After(r.LatestStockObservation.Date)) {
				v := s
				r.LatestStockObservation = &v
			}
		}
		s, warnings := calculator.CurrentStock(p.Stocks, cfg)
		r.CurrentStock = s
		r.Warnings = append(r.Warnings, warnings...)
		if s == nil {
			r.NeedsData = append(r.NeedsData, "current_stock_unavailable")
		}
		inc, _, warnings, incErr := calculator.IncomingStock(p, cfg)
		r.IncomingDetails = append([]model.Incoming(nil), p.Incoming...)
		r.Warnings = append(r.Warnings, warnings...)
		if incErr != nil {
			r.NeedsData = append(r.NeedsData, "incoming_unavailable_or_incomplete")
			r.Warnings = append(r.Warnings, incErr.Error())
		} else {
			r.Incoming = &inc
		}
		if p.Blocked {
			r.NeedsData = append(r.NeedsData, "ambiguous_source_data")
			r.Warnings = append(r.Warnings, "ambiguous_source_data; see data quality issues")
		}
		if !p.MOQ.Quantity.Valid {
			r.NeedsData = append(r.NeedsData, "moq_missing_or_invalid")
		} else if p.MOQ.Kind != "minimum" && p.MOQ.Kind != "multiple" {
			r.NeedsData = append(r.NeedsData, "moq_rule_unconfirmed")
		}
		if p.Unit != "шт" {
			r.NeedsData = append(r.NeedsData, "order_unit_conversion_unconfirmed")
		}
		r.Warnings = append(r.Warnings, "urgency_rule_unconfirmed")
		if d.Source == "synthetic_demo" && !demo {
			r.NeedsData = append(r.NeedsData, "synthetic_inputs_require_demo_mode")
		}
		if len(r.NeedsData) == 0 && r.Forecast != nil && s != nil && incErr == nil {
			v, calcErr := calculator.Calculate(calculator.Input{Forecast: r.Forecast.Quantity, CurrentStock: s.Quantity.Value, Incoming: inc, MOQ: p.MOQ, Unit: p.Unit})
			if calcErr != nil {
				r.NeedsData = append(r.NeedsData, "calculator_input_invalid")
				r.Warnings = append(r.Warnings, calcErr.Error())
			} else {
				r.RawOrder = &v.Raw
				r.Quantity = &v.Quantity
				r.MOQAdjustment = v.MOQAdjustment
				r.Warnings = append(r.Warnings, v.Warnings...)
				r.Status = "draft"
				// Urgency policy has not been approved; do not silently classify.
				if demo {
					r.Status = "demo_recommendation"
					r.Urgency = calculator.Urgency(s.Quantity.Value, *r.Forecast, r.IncomingDetails)
				}
			}
		}
		if demo {
			r.StockoutStatus = "explicitly_absent_in_demo"
			if len(p.Stockouts) > 0 {
				r.StockoutStatus = "confirmed_synthetic"
			}
			r.SeasonalityStatus = "explicit_synthetic_sku_profile"
			var warnings []string
			for _, w := range r.Warnings {
				if w != "urgency_rule_unconfirmed" && w != "stockout_duration_NOT_PROVIDED" && w != "diagnostic_history_only; seasonality_not_applied; trend_not_seasonally_adjusted" {
					warnings = append(warnings, w)
				}
			}
			r.Warnings = append(warnings, "synthetic_demo: not partner data or a supplier order")
		}
		r.Explanation = explain.Build(r)
		report.Recommendations = append(report.Recommendations, r)
	}
	return report
}
