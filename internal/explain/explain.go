package explain

import (
	"fmt"
	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"strings"
)

// Build is the unconditional deterministic fallback. A future LLM may rephrase
// this text, but may not write into Recommendation or calculate quantities.
func Build(r model.Recommendation) string {
	if r.Quantity == nil {
		text := fmt.Sprintf("%s / %s: расчёт заказа недоступен. Причины: %s. Stockout: %s.", r.Supplier, r.SKU, strings.Join(r.NeedsData, ", "), r.StockoutStatus)
		if r.Analysis != nil {
			text += fmt.Sprintf(" Диагностика: регулярный спрос %.3f за наблюдаемый месяц; baseline %.3f.", r.Analysis.RegularDemand, r.Analysis.Baseline)
			if r.Analysis.Trend != nil {
				text += fmt.Sprintf(" Тренд без сезонной корректировки %.3f ед./месяц.", *r.Analysis.Trend)
			}
		}
		return text + " Предупреждения: " + strings.Join(r.Warnings, "; ")
	}
	text := fmt.Sprintf("%s / %s: прогноз %.3f, остаток %.3f на %s, учтено в пути %.3f. Исходная потребность %.3f; заказ %.3f %s. Срочность: %s.", r.Supplier, r.SKU, r.Forecast.Quantity, r.CurrentStock.Quantity.Value, r.CurrentStock.Date.Format("2006-01-02"), *r.Incoming, *r.RawOrder, *r.Quantity, r.Unit, r.Urgency)
	text += fmt.Sprintf(" Сезонное влияние %.3f; тренд %.3f ед./месяц; историческая оценка lost demand %.3f; отмечено выбросов %d.", r.Forecast.SeasonalityImpact, r.Forecast.TrendPerMonth, r.Forecast.StockoutAdjustment, len(r.Forecast.Outliers))
	if r.MOQ.Quantity.Valid {
		text += fmt.Sprintf(" MOQ %.3f (%s), изменение после MOQ %.3f.", r.MOQ.Quantity.Value, r.MOQ.Kind, r.MOQAdjustment)
	}
	if len(r.Warnings) > 0 {
		text += " Ограничения: " + strings.Join(r.Warnings, "; ")
	}
	return text
}
