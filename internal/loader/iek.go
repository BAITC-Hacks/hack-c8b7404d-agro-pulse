package loader

import "Agro-Pulse/internal/model"

// LoadIEK uses the inspected September 2026 workbook layout.
func LoadIEK(dir string) (model.Dataset, error) {
	return load(dir, schema{
		supplier:     "IEK",
		monthly:      table{file: "Ежемесячные продажи в количественном выражении за последние 2 года.xlsx", sheet: "Лист_1", header: 1, start: 3, key: 1, name: 0, unit: -1, article: -1, monthStart: 2, expected: map[int]string{0: "Номенклатура", 1: "Номенклатура.Код", 2: "янв. 2024"}},
		stocks:       table{file: "Ежемесячные остатки продукции за последние 2 года  ИЭК.xlsx", sheet: "Лист_1", header: 1, start: 4, key: 2, name: 0, unit: 1, article: -1, monthStart: 3, expected: map[int]string{0: "Номенклатура", 1: "Ед.", 2: "Номенклатура.Код", 3: "янв. 2024"}},
		transactions: transactionTable("Динамика продаж_2025-2026.xlsx"),
		moq:          table{file: "MOQ  ИЭК.xlsx", sheet: "Лист7", header: 1, start: 2, key: 1, name: 3, unit: -1, article: 2, expected: map[int]string{1: "Код 1с", 2: "Артикул поставщика", 4: "Мин. разр. к отгр."}}, moqColumn: 4, moqKind: "unconfirmed",
		incoming:   table{file: "Путь ИЭК 22.09.2026.xlsx", sheet: "Лист4", header: 1, start: 2, key: 0, name: 2, unit: -1, article: 1, expected: map[int]string{0: "Код 1с", 1: "Артикул ИЭК", 2: "Наименование"}},
		seasonFile: "Сезонность ИЭК.xlsx", seasonSheet: "Сезонность",
	})
}

func transactionTable(file string) table {
	return table{file: file, sheet: "Лист_1", header: 1, start: 2, key: 3, name: 4, unit: 5, article: -1, expected: map[int]string{0: "Дата", 1: "Номер", 2: "Документ", 3: "Код", 4: "Номенклатура", 5: "Ед.", 6: "Склад", 7: "Количество"}}
}
