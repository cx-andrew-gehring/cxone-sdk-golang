package retry

import (
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"", 0, false},
		{"  ", 0, false},
		{"0", 0, true},
		{"7", 7 * time.Second, true},
		{" 12 ", 12 * time.Second, true},
		{"-3", 0, false},
		{"1.5", 0, false},
		{"soon", 0, false},
		{"Fri, 23 Oct 2026 12:00:30 GMT", 30 * time.Second, true},
		{"Fri, 23 Oct 2026 11:59:00 GMT", 0, true}, // in the past: retry now
		{"Friday, 23-Oct-26 12:01:00 GMT", time.Minute, true},
		{"99999999999999999999", 0, false},
	} {
		got, ok := ParseRetryAfter(tc.in, now)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseRetryAfter(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
