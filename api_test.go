package main

import (
	"errors"
	"testing"
)

func TestMinBetFromError(t *testing.T) {
	cases := []struct {
		body string
		want float64
		ok   bool
	}{
		{`{"message":"The minimum bet amount is 0.00000123 BTC"}`, 0.00000123, true},
		{`{"message":"The given data was invalid.","errors":{"amount":["The amount must be at least 0.01."]}}`, 0.01, true},
		{`{"error":"Minimal bet: 1e-05"}`, 0.00001, true},
		{`{"error":"Bet amount is too small. Min: 0.5 DECOY"}`, 0.5, true},
		{`{"error":"Insufficient funds"}`, 0, false},
	}
	for _, c := range cases {
		got, ok := MinBetFromError(apiError(422, []byte(c.body)))
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %v %v, want %v %v", c.body, got, ok, c.want, c.ok)
		}
	}
	if _, ok := MinBetFromError(errors.New("min 5")); ok {
		t.Error("non-API errors must be ignored")
	}
}

func TestParseLimit(t *testing.T) {
	for _, c := range []struct {
		in       string
		amt, pct float64
		bad      bool
	}{
		{"", 0, 0, false}, {"0", 0, 0, false}, {"250", 250, 0, false},
		{"10%", 0, 10, false}, {" 2.5 % ", 0, 2.5, false}, {"-1", 0, 0, true}, {"abc%", 0, 0, true},
	} {
		amt, pct, err := parseLimit(c.in)
		if (err != nil) != c.bad || amt != c.amt || pct != c.pct {
			t.Errorf("%q: got %v %v %v", c.in, amt, pct, err)
		}
		if err == nil {
			if a2, p2, _ := parseLimit(fmtLimit(amt, pct)); a2 != amt || p2 != pct {
				t.Errorf("%q does not round-trip", c.in)
			}
		}
	}
}
