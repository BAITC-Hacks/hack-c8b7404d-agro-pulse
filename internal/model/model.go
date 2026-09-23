// Package model contains supplier-independent inputs and auditable results.
package model

import "time"

type Source struct {
	Kind  string `json:"source,omitempty"`
	File  string `json:"file"`
	Sheet string `json:"sheet"`
	Cell  string `json:"cell"`
}

// Number distinguishes an absent/invalid value from a real zero.
type Number struct {
	Value  float64 `json:"value"`
	Valid  bool    `json:"valid"`
	Source Source  `json:"source"`
}

type Issue struct {
	Code    string `json:"code"`
	SKU     string `json:"sku,omitempty"`
	Message string `json:"message"`
	Source  Source `json:"source"`
}

type SourceStats struct {
	Rows          int      `json:"rows"`
	UniqueSKU     int      `json:"unique_sku"`
	MissingSKU    int      `json:"missing_sku"`
	DuplicateRows int      `json:"duplicate_rows"`
	MissingValues int      `json:"missing_values"`
	InvalidValues int      `json:"invalid_values"`
	MatchedSKU    int      `json:"matched_monthly_sku"`
	UnmatchedSKU  []string `json:"unmatched_sku"`
}

type Quality struct {
	Sources           map[string]*SourceStats `json:"sources"`
	Issues            []Issue                 `json:"issues"`
	MatchedAllSources []string                `json:"matched_all_sources"`
}

type Sale struct {
	Date     time.Time
	Document string
	Quantity float64 // Signed source quantity; never blindly converted with abs.
	Unit     string
	Source   Source
}

type MonthlySale struct {
	Month    time.Time `json:"month"`
	Quantity Number    `json:"quantity"`
}

type Stock struct {
	Date     time.Time `json:"date"`
	Quantity Number    `json:"quantity"`
	Kind     string    `json:"kind"` // opening, snapshot_free, or snapshot_total
}

type Incoming struct {
	Due      time.Time `json:"due"`
	Quantity Number    `json:"quantity"`
}

type MOQ struct {
	Quantity Number `json:"quantity"`
	Kind     string `json:"kind"` // minimum or multiple
}

type Product struct {
	SKU            string
	Name           string
	Article        string
	Unit           string
	Category       string
	Sales          []Sale
	Monthly        []MonthlySale
	Stocks         []Stock
	Incoming       []Incoming
	IncomingListed bool
	MOQ            MOQ
	Blocked        bool
	Warnings       []string
	// Only an explicitly verified SKU profile may enter the order forecast.
	Seasonality *[12]Number
	Stockouts   []StockoutEvidence
}

type Dataset struct {
	Source      string
	Supplier    string
	Products    map[string]*Product
	Seasonality [12]Number
	Quality     Quality
}

type Config struct {
	AsOf               time.Time `json:"as_of"`
	Months             int       `json:"months"`
	BlankSalesZero     bool      `json:"blank_sales_zero"`
	AllowOpeningStock  bool      `json:"allow_opening_stock"`
	AbsentIncomingZero bool      `json:"absent_incoming_zero"`
}

type Outlier struct {
	Source   Source  `json:"source"`
	Original float64 `json:"original"`
	Adjusted float64 `json:"adjusted"`
	Method   string  `json:"method"`
	Applied  bool    `json:"applied"`
}

type DemandPoint struct {
	Month     time.Time `json:"month"`
	Observed  float64   `json:"observed"`
	Regular   float64   `json:"regular"`
	Lost      float64   `json:"estimated_lost"`
	Corrected float64   `json:"corrected"`
	Source    Source    `json:"source"`
}

// StockoutEvidence is only usable when the duration is independently confirmed.
// Monthly opening snapshots alone do not populate this model.
type StockoutEvidence struct {
	Month     time.Time
	Days      int
	Confirmed bool
	Source    Source
}

type ForecastPeriod struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"` // exclusive
	Quantity    float64   `json:"quantity"`
	Seasonality float64   `json:"seasonality"`
}

type ForecastResult struct {
	Quantity           float64          `json:"quantity"`
	Baseline           float64          `json:"baseline"`
	TrendPerMonth      float64          `json:"trend_per_month"`
	SeasonalityImpact  float64          `json:"seasonality_impact"`
	StockoutAdjustment float64          `json:"historical_stockout_adjustment"`
	Periods            []ForecastPeriod `json:"periods"`
	History            []DemandPoint    `json:"history"`
	Outliers           []Outlier        `json:"outliers"`
	Warnings           []string         `json:"warnings"`
}

type Recommendation struct {
	Supplier               string          `json:"supplier"`
	SKU                    string          `json:"sku"`
	Name                   string          `json:"name"`
	Unit                   string          `json:"unit"`
	Category               string          `json:"category,omitempty"`
	Status                 string          `json:"status"`
	NeedsData              []string        `json:"needs_data"`
	StockoutStatus         string          `json:"stockout_status"`
	SeasonalityStatus      string          `json:"seasonality_status"`
	Analysis               *DemandAnalysis `json:"demand_analysis"`
	LatestStockObservation *Stock          `json:"latest_stock_observation"`
	Forecast               *ForecastResult `json:"forecast"`
	CurrentStock           *Stock          `json:"current_stock"`
	Incoming               *float64        `json:"incoming"`
	IncomingDetails        []Incoming      `json:"incoming_details"`
	MOQ                    MOQ             `json:"moq"`
	RawOrder               *float64        `json:"raw_order,omitempty"`
	Quantity               *float64        `json:"recommended_quantity"`
	MOQAdjustment          float64         `json:"moq_adjustment"`
	Urgency                string          `json:"urgency"`
	Warnings               []string        `json:"warnings"`
	Explanation            string          `json:"explanation"`
}

// Diagnostic history statistics, not an order forecast or a seasonal adjustment.
type DemandAnalysis struct {
	RegularDemand     float64       `json:"regular_demand_per_observed_month"`
	Baseline          float64       `json:"baseline_per_observed_month"`
	Trend             *float64      `json:"trend_per_month_unadjusted"`
	SeasonalityImpact *float64      `json:"seasonality_impact"`
	History           []DemandPoint `json:"history"`
	Outliers          []Outlier     `json:"outliers"`
	Warnings          []string      `json:"warnings"`
}

type SupplierReport struct {
	Source          string           `json:"source,omitempty"`
	Supplier        string           `json:"supplier"`
	Quality         Quality          `json:"quality"`
	Seasonality     [12]Number       `json:"seasonality"`
	Recommendations []Recommendation `json:"recommendations"`
}

type Report struct {
	MethodVersion string           `json:"method_version"`
	Config        Config           `json:"config"`
	Assumptions   []string         `json:"mvp_assumptions"`
	Suppliers     []SupplierReport `json:"suppliers"`
}
