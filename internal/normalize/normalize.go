package normalize

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func SKU(s string) string { return strings.TrimSpace(s) }

func Number(s string) (float64, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false, nil
	}
	s = strings.NewReplacer("\u00a0", "", "\u202f", "", " ", "", ",", ".").Replace(s)
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false, fmt.Errorf("invalid finite number %q", s)
	}
	return n, true, nil
}

func Date(s string) (time.Time, error) {
	for _, layout := range []string{"02.01.2006 15:04:05", "02.01.2006", "2006-01-02"} {
		if d, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return d, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q", s)
}

func Month(d time.Time) time.Time { return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC) }

func MonthHeader(s string) (time.Time, error) {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(f) != 2 {
		return time.Time{}, fmt.Errorf("invalid month header %q", s)
	}
	months := map[string]time.Month{"янв": 1, "февр": 2, "март": 3, "апр": 4, "май": 5, "июнь": 6, "июль": 7, "авг": 8, "сент": 9, "окт": 10, "нояб": 11, "дек": 12}
	m, ok := months[strings.TrimSuffix(f[0], ".")]
	y, err := strconv.Atoi(f[1])
	if !ok || err != nil || y < 1900 || y > 9999 {
		return time.Time{}, fmt.Errorf("invalid month header %q", s)
	}
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC), nil
}

func Finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
