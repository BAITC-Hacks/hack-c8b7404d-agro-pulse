package demo

import (
	"fmt"

	"Agro-Pulse/internal/pipeline"
)

// Every exposed scalar (including dates, rules and metadata) has its own source.
// DerivedFrom distinguishes backend results from supplied synthetic inputs.
type Value struct {
	Value       any      `json:"value"`
	Source      string   `json:"source"`
	DerivedFrom []string `json:"derived_from,omitempty"`
}
type ScenarioReport struct {
	Source      string           `json:"source"`
	Inputs      map[string]Value `json:"inputs"`
	Metrics     map[string]Value `json:"metrics"`
	Assumptions []Value          `json:"mvp_assumptions"`
}
type Report struct {
	Source    string           `json:"source"`
	Mode      Value            `json:"mode"`
	Scenarios []ScenarioReport `json:"scenarios"`
}

func val(v any, from ...string) Value { return Value{Value: v, Source: Source, DerivedFrom: from} }

var Assumptions = []string{
	"All inputs are synthetic_demo, not supplied by a partner; supplier DEMO_SUPPLIER; units шт",
	"As-of 2026-01-01; one calendar month; exact same-date stock snapshot; explicit zero incoming where absent",
	"Each synthetic SKU has an explicitly flat seasonal profile (12 coefficients equal to 1)",
	"MOQ is multiple of 12, except GROWING DEMAND with minimum 400; discrete quantities round up",
	"IQR outer fence Q3+3*IQR, at least four positive observations; Theil-Sen trend; safety stock 0",
	"Urgency is a demo-only rule: uniform within-month consumption; receipt available at beginning of due date",
	"STOCKOUT has confirmed 31 days in December 2025; lost demand uses median daily rate of other observed months",
	"Empty stockout evidence explicitly means no stockout in other synthetic fixtures; never inferred for partner data",
	"LLM is not used; numerical recommendations are demonstrations, not purchase orders",
}

func Run() (Report, error) {
	d, cfg, names := Fixtures()
	r, err := pipeline.RunDemo(d, cfg)
	if err != nil {
		return Report{}, err
	}
	out := Report{Source: Source, Mode: val("DEMO ONLY — NOT PARTNER DATA")}
	for _, rec := range r.Recommendations {
		if rec.Quantity == nil || rec.Forecast == nil || rec.CurrentStock == nil || rec.Incoming == nil || rec.Analysis == nil {
			return out, fmt.Errorf("incomplete demo result %s: %v", rec.SKU, rec.NeedsData)
		}
		p := d.Products[rec.SKU]
		f := rec.Forecast
		inputs := map[string]Value{"sku": val(p.SKU), "scenario": val(names[p.SKU]), "supplier": val(d.Supplier), "units": val(p.Unit), "as_of": val(cfg.AsOf), "forecast_horizon_months": val(cfg.Months), "current_stock": val(p.Stocks[0].Quantity.Value), "stock_date": val(p.Stocks[0].Date), "stock_kind": val(p.Stocks[0].Kind), "moq_rule": val(p.MOQ.Kind), "moq_quantity": val(p.MOQ.Quantity.Value), "safety_stock": val(0), "urgency_rule": val("uniform_monthly_consumption; receipt_at_start_of_due_date")}
		var history, transactions, arrivals, evidence []map[string]Value
		var coefficients []Value
		for _, m := range p.Monthly {
			history = append(history, map[string]Value{"month": val(m.Month), "quantity": val(m.Quantity.Value)})
		}
		for _, s := range p.Sales {
			transactions = append(transactions, map[string]Value{"date": val(s.Date), "quantity": val(s.Quantity), "document": val(s.Document), "unit": val(s.Unit)})
		}
		for _, s := range p.Incoming {
			arrivals = append(arrivals, map[string]Value{"due": val(s.Due), "quantity": val(s.Quantity.Value)})
		}
		for _, s := range p.Stockouts {
			evidence = append(evidence, map[string]Value{"month": val(s.Month), "days": val(s.Days), "confirmed": val(s.Confirmed)})
		}
		for _, n := range *p.Seasonality {
			coefficients = append(coefficients, val(n.Value))
		}
		inputs["monthly_history"] = val(history)
		inputs["transactions"] = val(transactions)
		inputs["incoming"] = val(arrivals)
		inputs["stockout_evidence"] = val(evidence)
		inputs["seasonal_coefficients"] = val(coefficients)
		observed, regular, corrected := 0.0, 0.0, 0.0
		var corrections []map[string]Value
		for _, h := range f.History {
			observed += h.Observed
			regular += h.Regular
			corrected += h.Corrected
			if h.Lost > 0 {
				corrections = append(corrections, map[string]Value{"month": val(h.Month, "inputs.stockout_evidence"), "observed": val(h.Observed, "inputs.monthly_history"), "estimated_lost": val(h.Lost, "inputs.stockout_evidence", "inputs.monthly_history"), "corrected": val(h.Corrected, "inputs.stockout_evidence", "inputs.monthly_history")})
			}
		}
		metrics := map[string]Value{
			"sku": val(rec.SKU, "inputs.sku"), "scenario": val(names[p.SKU], "inputs.scenario"),
			"regular_demand":      val(regular/float64(len(f.History)), "inputs.monthly_history", "inputs.transactions"),
			"raw_observed_demand": val(observed/float64(len(f.History)), "inputs.monthly_history"),
			"corrected_demand":    val(corrected/float64(len(f.History)), "inputs.monthly_history", "inputs.stockout_evidence"),
			"outliers_detected":   val(len(f.Outliers), "inputs.transactions", "inputs.monthly_history"),
			"trend":               val(f.TrendPerMonth, "inputs.monthly_history", "inputs.stockout_evidence", "inputs.seasonal_coefficients"),
			"seasonality_status":  val(rec.SeasonalityStatus, "inputs.seasonal_coefficients"), "seasonality_impact": val(f.SeasonalityImpact, "inputs.seasonal_coefficients", "inputs.monthly_history"),
			"stockout_status": val(rec.StockoutStatus, "inputs.stockout_evidence"), "stockout_adjustment": val(f.StockoutAdjustment, "inputs.stockout_evidence", "inputs.monthly_history"), "stockout_details": val(corrections),
			"forecast":      val(f.Quantity, "inputs.monthly_history", "inputs.transactions", "inputs.stockout_evidence", "inputs.seasonal_coefficients", "inputs.forecast_horizon_months"),
			"current_stock": val(rec.CurrentStock.Quantity.Value, "inputs.current_stock", "inputs.stock_date"), "incoming": val(*rec.Incoming, "inputs.incoming", "inputs.as_of", "inputs.forecast_horizon_months"),
			"raw_recommended_quantity": val(*rec.RawOrder, "metrics.forecast", "metrics.current_stock", "metrics.incoming"), "moq_rule": val(rec.MOQ.Kind, "inputs.moq_rule"), "moq_quantity": val(rec.MOQ.Quantity.Value, "inputs.moq_quantity"),
			"final_recommended_quantity": val(*rec.Quantity, "metrics.raw_recommended_quantity", "inputs.moq_rule", "inputs.moq_quantity", "inputs.units"),
			"urgency":                    val(rec.Urgency, "metrics.forecast", "inputs.current_stock", "inputs.incoming", "inputs.urgency_rule"), "status": val(rec.Status, "inputs"),
			"explanation": val("SYNTHETIC DEMO. "+rec.Explanation, "metrics"),
		}
		var warnings []Value
		for _, w := range rec.Warnings {
			warnings = append(warnings, val(w, "inputs"))
		}
		metrics["warnings"] = val(warnings)
		item := ScenarioReport{Source: Source, Inputs: inputs, Metrics: metrics}
		for _, a := range Assumptions {
			item.Assumptions = append(item.Assumptions, val(a))
		}
		out.Scenarios = append(out.Scenarios, item)
	}
	return out, nil
}
