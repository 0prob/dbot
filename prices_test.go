package main

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPriceFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cb"):
			w.Write([]byte(`{"data":{"currency":"USD","rates":{"BTC":"0.00001","SOL":"0.01","BAD":"0"}}}`))
		case strings.HasPrefix(r.URL.Path, "/cg"):
			if got := r.URL.Query().Get("symbols"); got != "trx,decoy" {
				t.Errorf("coingecko asked for %q", got)
			}
			w.Write([]byte(`{"trx":{"usd":0.33}}`))
		}
	}))
	defer srv.Close()
	oldCB, oldCG := coinbaseRatesURL, coingeckoURL
	coinbaseRatesURL, coingeckoURL = srv.URL+"/cb", srv.URL+"/cg"
	defer func() { coinbaseRatesURL, coingeckoURL = oldCB, oldCG }()

	f := newPriceFeed()
	if err := f.Update(context.Background(), []string{"BTC", "sol", "TRX", "DECOY"}); err != nil {
		t.Fatal(err)
	}
	for sym, want := range map[string]float64{"BTC": 100000, "SOL": 100, "TRX": 0.33, "usd": 1} {
		if got, ok := f.USD(sym); !ok || math.Abs(got-want) > want*1e-9 {
			t.Errorf("%s: got %v %v, want %v", sym, got, ok, want)
		}
	}
	if _, ok := f.USD("DECOY"); ok {
		t.Error("DECOY has no market price")
	}
	if _, ok := f.USD("BAD"); ok {
		t.Error("zero rate must be ignored")
	}
}

func TestFmtUSD(t *testing.T) {
	for in, want := range map[float64]string{
		0: "$0.00", 1.5: "$1.50", 1234567.891: "$1,234,567.89", -42.1: "-$42.10",
		0.00012345: "$0.000123", 999.999: "$1,000.00",
	} {
		if got := fmtUSD(in); got != want {
			t.Errorf("fmtUSD(%v) = %q, want %q", in, got, want)
		}
	}
}
