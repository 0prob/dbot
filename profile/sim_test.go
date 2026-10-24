package profile

import (
	"math"
	"testing"
)

func TestSimulateHouseEdge(t *testing.T) {
	// Flat betting with no limits: the average loss should be about 1% of
	// everything wagered.
	p := &BettingProfile{BaseBet: 1, Chance: 49.5, OnLossAction: OnLossReset}
	r := Simulate(p, SimConfig{StartBalance: 1000, Sessions: 4000, MaxBets: 500, Seed: 1})
	if math.Abs(r.AvgWageredX-0.5) > 1e-9 {
		t.Fatalf("wagered %vx, want 0.5x", r.AvgWageredX)
	}
	want := -HouseEdge * r.AvgWageredX // % of start balance
	if math.Abs(r.MeanPct-want) > 0.15 {
		t.Errorf("mean %.3f%%, want about %.3f%%", r.MeanPct, want)
	}
	if r.BustShare != 0 || r.AvgBets != 500 {
		t.Errorf("unexpected stops: %+v", r)
	}
	if !(r.WorstPct <= r.P10Pct && r.P10Pct <= r.MedianPct && r.MedianPct <= r.P90Pct && r.P90Pct <= r.BestPct) {
		t.Errorf("percentiles out of order: %+v", r)
	}
}

func TestSimulateStopsAndBust(t *testing.T) {
	limited := &BettingProfile{BaseBet: 1, Chance: 49.5, OnLossAction: OnLossReset, StopLossPct: 2, TakeProfitPct: 2}
	r := Simulate(limited, SimConfig{StartBalance: 100, Sessions: 2000, MaxBets: 100000, Seed: 2})
	if got := r.TakeProfitShare + r.StopLossShare; math.Abs(got-1) > 1e-9 {
		t.Errorf("every session should end at a limit, got %v", got)
	}
	if r.WorstPct < -3 || r.BestPct > 3 {
		t.Errorf("results escaped the ±2%% limits: worst %v best %v", r.WorstPct, r.BestPct)
	}

	mart := &BettingProfile{BaseBet: 1, Chance: 49.5, OnLossAction: OnLossMultiply, LossMultiplier: 2}
	r = Simulate(mart, SimConfig{StartBalance: 100, Sessions: 500, MaxBets: 2000, Seed: 3})
	if r.BustShare < 0.5 {
		t.Errorf("martingale with a 100-unit bankroll should usually bust, got %v", r.BustShare)
	}
}

func TestSimulateDeterministic(t *testing.T) {
	p := duckdicePreset()
	cfg := SimConfig{StartBalance: 1000, Sessions: 300, MaxBets: 200, Seed: 7}
	if a, b := Simulate(p, cfg), Simulate(p, cfg); a != b {
		t.Errorf("same seed gave different results:\n%+v\n%+v", a, b)
	}
	if (Simulate(p, SimConfig{}) != SimResult{}) {
		t.Error("empty config should give an empty result")
	}
}
