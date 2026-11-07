package cli

import (
	"reflect"
	"testing"
	"time"

	"github.com/idarciooliveira/namecheap-cli/internal/api"
)

func TestParseDateAndDays(t *testing.T) {
	now := time.Date(2026, 11, 7, 15, 0, 0, 0, time.Local)
	for in, want := range map[string]int{
		"11/07/2026":            0,
		"12/02/2026":            25,
		"1/15/2027":             69,
		"11/01/2026":            -6,
		"4/30/2027 11:31:13 AM": 174,
	} {
		d, ok := parseDate(in)
		if !ok {
			t.Fatalf("parseDate(%q) failed", in)
		}
		if got := daysUntil(d, now); got != want {
			t.Errorf("daysUntil(%q) = %d, want %d", in, got, want)
		}
	}
	if isoDate("02/15/2016") != "2016-02-15" {
		t.Error("isoDate")
	}
}

func TestParseDays(t *testing.T) {
	for in, want := range map[string]int{"60": 60, "60d": 60, "8w": 56, "3m": 90, "1y": 365} {
		if got, err := parseDays(in); err != nil || got != want {
			t.Errorf("parseDays(%q) = %d, %v", in, got, err)
		}
	}
	if _, err := parseDays("soon"); err == nil {
		t.Error("expected error")
	}
}

func TestExpandNames(t *testing.T) {
	got := expandNames([]string{"MyIdea", "https://www.other.dev/path", "myidea.com", "x.co.uk"}, []string{"com", "io"})
	want := []string{"myidea.com", "myidea.io", "other.dev", "x.co.uk"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if tldOf("x.co.uk") != "co.uk" {
		t.Error("tldOf")
	}
}

func TestToDomainOutMarksExpired(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.Local)
	o := toDomainOut(api.Domain{Name: "a.com", Expires: "11/01/2026", WhoisGuard: "ENABLED"}, now)
	if !o.Expired || o.DaysLeft == nil || *o.DaysLeft != -6 || o.Expires != "2026-11-01" || o.Privacy != "enabled" {
		t.Errorf("got %+v", o)
	}
}

func TestTermPrice(t *testing.T) {
	ps := []api.Price{
		{Duration: "2", DurationType: "YEAR", Price: "20"},
		{Duration: "1", DurationType: "YEAR", Price: "", YourPrice: "12.98", Currency: "USD"},
	}
	a, ok := termPrice(ps)
	if !ok || a.value != 12.98 || a.currency != "USD" || a.years != 1 {
		t.Errorf("got %+v %v", a, ok)
	}
	// .ai: 2-year minimum, ICANN fee carried separately.
	ai := []api.Price{
		{Duration: "3", DurationType: "YEAR", Price: "89.98"},
		{Duration: "2", DurationType: "YEAR", Price: "89.98", AdditionalCost: "0.20", Currency: "USD"},
	}
	a, ok = termPrice(ai)
	if !ok || a.years != 2 || a.value != 89.98 || a.fee != 0.20 {
		t.Errorf("ai got %+v %v", a, ok)
	}
}
