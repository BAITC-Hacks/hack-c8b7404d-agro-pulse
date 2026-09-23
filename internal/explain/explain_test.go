package explain

import (
	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"strings"
	"testing"
)

func TestMissingResultNeverInventsQuantity(t *testing.T) {
	s := Build(model.Recommendation{Supplier: "IEK", SKU: "001_", Warnings: []string{"current_stock_NOT_PROVIDED"}})
	if !strings.Contains(s, "current_stock_NOT_PROVIDED") || !strings.Contains(s, "недоступен") {
		t.Fatal(s)
	}
}
