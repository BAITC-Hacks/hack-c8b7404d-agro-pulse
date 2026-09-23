package normalize

import "testing"

func TestNumber(t *testing.T) {
	for _, bad := range []string{"NaN", "+Inf", "#N/A", "1e999", "1,2,3"} {
		if _, _, err := Number(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, ok, err := Number(""); ok || err != nil {
		t.Fatal("blank must remain missing")
	}
	if n, ok, err := Number("1\u00a0234,5"); n != 1234.5 || !ok || err != nil {
		t.Fatal(n, ok, err)
	}
	if n, ok, err := Number("0"); n != 0 || !ok || err != nil {
		t.Fatal("zero lost")
	}
}

func TestKeysAndDates(t *testing.T) {
	if SKU(" 001_ ") != "001_" || SKU("щт-0001014") != "щт-0001014" {
		t.Fatal("key corrupted")
	}
	if _, err := Date("31.02.2026"); err == nil {
		t.Fatal("invalid date accepted")
	}
	d, err := MonthHeader("сент. 2026")
	if err != nil || d.Format("2006-01-02") != "2026-09-01" {
		t.Fatal(d, err)
	}
}
