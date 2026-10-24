package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Public price sources (no API key). Coinbase prices ~650 assets in one
// call; CoinGecko fills in coins Coinbase lacks (e.g. TRX, XMR). Only coin
// symbols are sent. DuckDice's own DECOY token has no market price.
var (
	coinbaseRatesURL = "https://api.coinbase.com/v2/exchange-rates?currency=USD"
	coingeckoURL     = "https://api.coingecko.com/api/v3/simple/price"
)

// priceFeed caches USD prices by coin symbol. Safe for concurrent use.
type priceFeed struct {
	http    *http.Client
	mu      sync.RWMutex
	prices  map[string]float64
	updated time.Time
}

func newPriceFeed() *priceFeed {
	return &priceFeed{http: &http.Client{Timeout: 15 * time.Second}, prices: map[string]float64{}}
}

// USD returns the USD price of one unit of sym, if known.
func (f *priceFeed) USD(sym string) (float64, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.prices[strings.ToUpper(sym)]
	return p, ok
}

// Update fetches prices for syms. Coins neither source knows stay unknown.
func (f *priceFeed) Update(ctx context.Context, syms []string) error {
	got, err := f.coinbase(ctx)
	var missing []string
	for _, s := range syms {
		s = strings.ToUpper(s)
		if _, ok := got[s]; !ok && s != "USD" {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		if cg, cgErr := f.coingecko(ctx, missing); cgErr == nil {
			for k, v := range cg {
				got[k] = v
			}
		} else if err == nil {
			err = cgErr
		}
	}
	got["USD"] = 1
	f.mu.Lock()
	for k, v := range got {
		f.prices[k] = v
	}
	f.updated = time.Now()
	f.mu.Unlock()
	return err
}

func (f *priceFeed) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// coinbase returns USD prices for every asset Coinbase lists. Its rates are
// units per 1 USD, so the price is the reciprocal.
func (f *priceFeed) coinbase(ctx context.Context) (map[string]float64, error) {
	var body struct {
		Data struct {
			Rates map[string]string `json:"rates"`
		} `json:"data"`
	}
	out := map[string]float64{}
	if err := f.getJSON(ctx, coinbaseRatesURL, &body); err != nil {
		return out, err
	}
	for sym, r := range body.Data.Rates {
		if v, err := strconv.ParseFloat(r, 64); err == nil && v > 0 && !math.IsInf(1/v, 0) {
			out[strings.ToUpper(sym)] = 1 / v
		}
	}
	return out, nil
}

func (f *priceFeed) coingecko(ctx context.Context, syms []string) (map[string]float64, error) {
	q := url.Values{"symbols": {strings.ToLower(strings.Join(syms, ","))}, "vs_currencies": {"usd"}}
	var body map[string]struct {
		USD float64 `json:"usd"`
	}
	out := map[string]float64{}
	if err := f.getJSON(ctx, coingeckoURL+"?"+q.Encode(), &body); err != nil {
		return out, err
	}
	for sym, p := range body {
		if p.USD > 0 {
			out[strings.ToUpper(sym)] = p.USD
		}
	}
	return out, nil
}

// fmtUSD formats a dollar amount: cents for normal amounts, a few
// significant digits for tiny ones, thousands separators for large ones.
func fmtUSD(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	if v != 0 && v < 0.01 {
		return sign + "$" + strconv.FormatFloat(v, 'g', 3, 64)
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return sign + "$" + b.String() + "." + frac
}
