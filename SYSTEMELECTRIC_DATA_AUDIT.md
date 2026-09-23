# SystemElectric data audit

Audit performed on the six source workbooks named in the task. Workbook contents and source files were read only. Row counts below include physically present blank rows and, where noted, total rows.

## Workbook inventory

| Actual file | Sheet(s) | Rows × max columns | Headers and relevant fields |
|---|---|---:|---|
| `MOQ SystemElectric.xlsx` | `Лист_1` | 557 × 5 | `№`, `Номенклатура`, `Номенклатура.Код`, `Артикул`, `Кратность`. 554 product rows, one blank row, one `Итого` row. `Кратность` is supplied; whether it means minimum order quantity or pack multiple is not established by the label. |
| `Динамика продаж_Syseme Electric_2025-2026.xlsx` | `Лист_1` | 77,314 × 8 | `Дата`, `Номер`, `Документ`, `Код`, `Номенклатура`, `Ед.`, `Склад`, `Количество`. 77,312 transaction lines plus header and `Итого`; dates observed from 18.01.2023 through 22.09.2026; unit `шт`. |
| `Ежемесячные остатки SystemElectric 2024-2026.xlsx` | `Лист_1` | 704 × 37 | `№`, `Номенклатура`, `Номенклатура.Код`, `Ед.изм`, month columns Jan 2024–Sep 2026. 701 product rows, two blank rows, one row with no monthly values. Unit `шт`. |
| `Ежемесячные продажи в кол-м выражении SystemElectric 2024-2026.xlsx` | `Лист_1`, `Лист1` | `Лист_1`: 557 × 38; `Лист1`: 23 × 15 | `Лист_1`: product name, `Номенклатура.Код`, `Артикул`, `Кратность`, monthly quantity Jan 2024–Sep 2026 and `Итого`; second header row says `Количество`. 554 product rows plus header/total rows. `Лист1` is a company-wide monthly monetary sales summary, not SKU-level seasonality. |
| `Сезонность SystemElectric 2024-2026.xlsx` | `Лист1` | 23 × 15 | Blank preamble; table headers on row 10: month, sales, seasonal coefficients and annual shares for 2024/2025/2026, `СЕЗОННОСТЬ`, and annual totals/averages. No SKU or 1C code. |
| `Товар в пути_SystemElectric на 22.09.2026.xlsx` | `TDSheet` | 499 × 70 | Two header rows; row 2 contains `Артикул поставщика`, `Код 1с`, `Наименование`, `Категория 2026`, monthly sales, growth/seasonality coefficients, warehouse balances, `Остаток`, `Зарезервировано`, `Свободный остаток`, `Запас`, `Заказ`, `СЭ в пути 24.09`, `Вес`. 497 product rows; title/as-of date is 22.09.2026. Incoming quantity is in the column headed `СЭ в пути 24.09`; that header embeds 24.09 but there are no per-order records. |

`Динамика продаж_Syseme Electric_2025-2026.xlsx` contains transaction dates beginning in 2023 despite the filename. The stock and monthly sales sheets have month columns through September 2026. The monetary monthly summary has full months for 2024 and 2025 and Jan–Sep 2026.

## Requirement mapping

| Requirement | Excel file / sheet / actual column | Status |
|---|---|---|
| SKU / 1C code | MOQ: `Лист_1`.`Номенклатура.Код`; transactions: `Лист_1`.`Код`; stock: `Лист_1`.`Номенклатура.Код`; monthly sales: `Лист_1`.`Номенклатура.Код`; in-transit: `TDSheet`.`Код 1с` | PROVIDED; actual common key is the 1C code when populated. Preserve trailing `_` and leading zeroes. Some stock/in-transit rows instead contain non-1C identifiers, so SKU normalization must not coerce codes to numbers. |
| Product name | All product-level workbooks: `Номенклатура` or `Наименование` | PROVIDED |
| Transaction sales / dates | Transaction workbook: `Дата`, `Количество`, `Ед.` | PROVIDED as signed quantity movements; negative values must not be silently converted to positive sales. |
| Monthly sales | Monthly quantity workbook: month columns; `Количество` | PROVIDED |
| Historical stock | Monthly stock workbook: month columns Jan 2024–Sep 2026 | PROVIDED; historical values are not current stock. |
| Current stock | In-transit snapshot: `Свободный остаток` and separately `Остаток`, as of workbook date 22.09.2026 | PROVIDED for the snapshot date; reserved and total balances are separate fields. |
| Incoming goods | In-transit snapshot: `СЭ в пути 24.09` | PROVIDED as one aggregate quantity column; per-order details are NOT PROVIDED. |
| Expected incoming dates | Header text `СЭ в пути 24.09` | PARTIALLY PROVIDED: 24.09 is embedded in the column name; year and per-shipment dates are NOT PROVIDED. Do not treat it as supplier lead time. |
| Seasonality | Seasonality workbook: `Лист1`, row 10 `СЕЗОННОСТЬ` and monthly coefficient columns | PROVIDED as aggregate monthly coefficients, not SKU-level seasonality. |
| MOQ / order multiple | MOQ workbook: `Лист_1`.`Кратность`; monthly sales also has `Кратность` | PARTIALLY PROVIDED: numeric field exists, but business meaning differs/needs confirmation. Do not assume this is minimum order quantity without a definition. |
| Category | In-transit snapshot: `Категория 2026` | PROVIDED in snapshot only. |
| Units | Transactions: `Ед.`; monthly stock: `Ед.изм`; monthly sales has `Количество` second header row | PROVIDED; observed product units are `шт`. |
| Supplier | No supplier column in the workbooks | NOT PROVIDED as a row-level field; “SystemElectric” is source context only. |
| Supplier lead time | No lead-time field | NOT PROVIDED |
| Stockout periods | Monthly historical stock may support a zero-stock heuristic; transaction sales are not stockout markers | PARTIALLY PROVIDED / DERIVED only if the agreed rule defines which monthly balances represent stockout. |

## Key and overlap

Unique non-empty product keys extracted using the source-specific code columns:

| Source | Unique keys |
|---|---:|
| Transactions | 565 |
| Monthly stock | 701 |
| Monthly quantity sales | 554 |
| MOQ | 554 |
| In-transit snapshot | 497 |
| Seasonality | No SKU key |

Exact 1C-code intersections: transactions ↔ monthly sales: 545 (20 transaction-only, 9 monthly-only); monthly sales ↔ MOQ: 554 (complete match); monthly stock ↔ monthly sales: 554 (147 stock-only, none monthly-only); transactions ↔ in-transit: 469 (96 transaction-only, 28 in-transit-only); monthly sales ↔ in-transit: 468 (86 monthly-only, 29 in-transit-only); MOQ ↔ in-transit: 468 (86 MOQ-only, 29 in-transit-only). Transaction repeats are expected because the sheet is at transaction-line grain; they are not duplicate SKU master records.

The common join key is the literal 1C code where it is present. Preserve values such as `030200128_` exactly. The stock and in-transit sheets contain some non-1C identifiers in the nominal code column, so those rows will not join on the 1C key; keep them as source-level warnings rather than guessing a mapping from product name/article.

## Data quality findings

- Blank rows: MOQ 1; monthly stock 2; seasonality 3; no entirely blank rows in the transaction, monthly sales product table, or in-transit data range. Monthly stock has one additional product record with no monthly values.
- Missing SKU among identified product rows: none found in the counted product tables. Summary/total rows are not product records.
- Duplicate product keys: none in MOQ, monthly stock, monthly quantity sales, or in-transit product rows. Transaction SKU repetition is expected (77,312 lines, 565 unique SKUs).
- Negative quantities: 302 transaction lines and 323 monthly quantity cells are negative. These are material sign/return/adjustment cases; adapter must preserve values and surface warnings until business semantics are defined.
- Negative historical stock values: none found in parsed monthly stock cells.
- Invalid numeric text in scanned transaction quantities, monthly stock values, and monthly quantity values: none found. Empty cells occur and are missing observations, not numeric zero by default.
- MOQ values: source field is named `Кратность`; semantic definition is unresolved. Invalid/zero/missing item values require row-level validation in the adapter; no MOQ interpretation should be applied silently.
- Unit consistency: observed product unit is `шт` where explicit; monthly sales labels quantities but has no unit field. Cross-file unit equivalence is therefore PARTIALLY PROVIDED.
- Aggregated sales vs transactions: not reconciled in this pass. The source uses signed transaction quantities, while monthly aggregates include negative values; reconciliation needs an agreed treatment of returns/corrections and matching periods.
- Extreme values: no domain threshold was supplied. Negative and unusually large observations should be preserved and reported for review, not automatically clipped.

## Status buckets

- **PROVIDED:** dated transaction lines and signed quantities; monthly quantity sales; dated monthly historical balances; snapshot balances; aggregate incoming quantity; monthly seasonality coefficients; product/category/unit values where explicit.
- **DERIVED:** exact key intersections; monthly transaction aggregates (once sign/return semantics are agreed); stockout flags from historical stock only under an agreed rule.
- **PARTIALLY PROVIDED:** MOQ meaning; expected incoming date (one date encoded in a column name only); current stock only at the 22.09.2026 snapshot; seasonality at portfolio/month level rather than per SKU; unit equivalence across files; stockout periods.
- **NOT PROVIDED:** supplier lead time; per-shipment incoming records/dates; supplier field per product; explicit stockout annotations; confirmed transaction sign semantics.

## Adapter integration note

This checkout currently has no common input model or active loader pipeline: `main.go`, `internal/analytics/*`, and `internal/calculator/*` are package-only stubs. The SystemElectric source mappings are now known, but implementing `internal/loader/systemelectric.go` against the “same architecture” requires the shared model/interface from the teammate implementing the IEK calculation path. Inventing a parallel model here would make integration less reliable. The source workbooks were not modified.
