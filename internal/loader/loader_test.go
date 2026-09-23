package loader

import (
	"path/filepath"
	"testing"

	"Agro-Pulse/internal/model"
	"github.com/xuri/excelize/v2"
)

func TestMOQCachedValuesMissingAndDuplicate(t *testing.T) {
	dir := t.TempDir()
	f := excelize.NewFile()
	rows := [][]interface{}{{"Код", "MOQ"}, {"001_", 5}, {"001_", 10}, {"002_", "#N/A"}, {"003_", nil}, {nil, 4}}
	for i, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SaveAs(filepath.Join(dir, "test.xlsx")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	r := reader{dir: dir, d: model.Dataset{Products: map[string]*model.Product{}, Quality: model.Quality{Sources: map[string]*model.SourceStats{}}}, keys: map[string]map[string]bool{}}
	err := r.minimum(table{file: "test.xlsx", sheet: "Sheet1", header: 1, start: 2, key: 0, name: -1, unit: -1, article: -1, expected: map[int]string{0: "Код", 1: "MOQ"}}, 1, "multiple")
	if err != nil {
		t.Fatal(err)
	}
	if !r.d.Products["001_"].Blocked {
		t.Fatal("conflicting duplicate must block")
	}
	if r.d.Products["002_"].MOQ.Quantity.Valid || r.d.Products["003_"].MOQ.Quantity.Valid {
		t.Fatal("invented MOQ")
	}
	if r.d.Quality.Sources["moq"].MissingSKU != 1 {
		t.Fatal("missing SKU not reported")
	}
}
