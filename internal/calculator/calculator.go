package calculator

import (
	"fmt"
	"math"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/normalize"
)

type Input struct {
	Forecast     float64
	CurrentStock float64
	Incoming     float64
	MOQ          model.MOQ
	Unit         string
}

type Result struct {
	Raw           float64
	Quantity      float64
	MOQAdjustment float64
	Warnings      []string
}

func Calculate(in Input) (Result, error) {
	r := Result{}
	for _, v := range []float64{in.Forecast, in.CurrentStock, in.Incoming} {
		if !normalize.Finite(v) || v < 0 || v > 1<<53 {
			return r, fmt.Errorf("quantity must be finite, nonnegative and <= 2^53")
		}
	}
	r.Raw = math.Max(0, in.Forecast-in.CurrentStock-in.Incoming)
	q := r.Raw
	if in.Unit == "шт" || in.Unit == "упак" || in.Unit == "компл" {
		q = math.Ceil(q)
	} else if in.Unit != "м" {
		return r, fmt.Errorf("unit conversion NOT PROVIDED for %q", in.Unit)
	}
	before := q
	if in.MOQ.Quantity.Valid {
		m := in.MOQ.Quantity.Value
		if !normalize.Finite(m) || m <= 0 || m > 1<<53 {
			return r, fmt.Errorf("invalid MOQ")
		}
		if in.Unit != "м" && m != math.Trunc(m) {
			return r, fmt.Errorf("fractional MOQ incompatible with discrete unit")
		}
		switch in.MOQ.Kind {
		case "minimum":
			if q > 0 {
				q = math.Max(q, m)
			}
		case "multiple":
			if q > 0 {
				q = math.Ceil(q/m) * m
			}
		default:
			return r, fmt.Errorf("MOQ rule NOT PROVIDED")
		}
	} else {
		r.Warnings = append(r.Warnings, "MOQ_NOT_PROVIDED; no MOQ adjustment")
	}
	if !normalize.Finite(q) || q > 1<<53 {
		return r, fmt.Errorf("recommended quantity overflow")
	}
	r.Quantity = q
	r.MOQAdjustment = q - before
	return r, nil
}
