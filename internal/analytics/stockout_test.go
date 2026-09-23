package analytics

import (
	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"testing"
	"time"
)

func TestStockoutRequiresEvidence(t *testing.T) {
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mar := jan.AddDate(0, 2, 0)
	points := []model.DemandPoint{{Month: jan, Regular: 310}, {Month: mar, Regular: 0}}
	unchanged, err := CorrectStockouts(points, nil)
	if err != nil || unchanged[1].Lost != 0 {
		t.Fatal("invented stockout")
	}
	corrected, err := CorrectStockouts(points, []model.StockoutEvidence{{Month: mar, Days: 31, Confirmed: true}})
	if err != nil || corrected[1].Lost != 310 || corrected[1].Corrected != 310 || points[1].Regular != 0 {
		t.Fatal(corrected, err)
	}
	if _, err = CorrectStockouts(points, []model.StockoutEvidence{{Month: mar, Days: 32, Confirmed: true}}); err == nil {
		t.Fatal("invalid duration")
	}
}
