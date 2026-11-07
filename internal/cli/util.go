package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var dateLayouts = []string{
	"01/02/2006",
	"1/2/2006",
	"1/2/2006 3:04:05 PM",
	"01/02/2006 15:04:05",
	"2006-01-02",
}

// parseDate parses Namecheap's US-style dates (MM/DD/YYYY).
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// isoDate converts a Namecheap date to YYYY-MM-DD, or returns it unchanged.
func isoDate(s string) string {
	if t, ok := parseDate(s); ok {
		return t.Format("2006-01-02")
	}
	return s
}

// daysUntil counts calendar days from today (local) to t. Negative = past.
func daysUntil(t, now time.Time) int {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return int(day.Sub(today).Hours() / 24)
}

// parseDays accepts "60", "60d", "8w", "3m" (30-day months), "1y".
func parseDays(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	mult := 1
	switch s[len(s)-1] {
	case 'd':
		s = s[:len(s)-1]
	case 'w':
		mult, s = 7, s[:len(s)-1]
	case 'm':
		mult, s = 30, s[:len(s)-1]
	case 'y':
		mult, s = 365, s[:len(s)-1]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 60d, 8w, 3m)", s)
	}
	return n * mult, nil
}

func parseMoney(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func money(v *float64, currency string) string {
	if v == nil {
		return "-"
	}
	if currency == "" || currency == "USD" {
		return fmt.Sprintf("$%.2f", *v)
	}
	return fmt.Sprintf("%.2f %s", *v, currency)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// tldOf returns everything after the first label: "a.co.uk" -> "co.uk".
func tldOf(domain string) string {
	if i := strings.IndexByte(domain, '.'); i >= 0 {
		return domain[i+1:]
	}
	return ""
}

// normalizeDomain lowercases and strips schemes, paths and trailing dots.
func normalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.Trim(s, ".")
}

// expandNames turns bare names into name.tld for each TLD; full domains pass through.
func expandNames(args, tlds []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, a := range args {
		for _, part := range strings.Split(a, ",") {
			d := normalizeDomain(part)
			if d == "" {
				continue
			}
			if strings.Contains(d, ".") {
				add(d)
				continue
			}
			for _, t := range tlds {
				add(d + "." + t)
			}
		}
	}
	return out
}

func splitTLDs(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		t = strings.Trim(strings.ToLower(strings.TrimSpace(t)), ".")
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
