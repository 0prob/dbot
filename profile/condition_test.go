package profile

import (
	"encoding/json"
	"testing"
)

// The four conditions from DuckDice's own auto-bet screen.
func duckdicePreset() *BettingProfile {
	return &BettingProfile{
		Name: "preset", BaseBet: 1, Chance: 50,
		OnWinAction: OnWinKeep, OnLossAction: OnLossKeep,
		Conditions: []Condition{
			{Trigger: EveryN, Count: 1, Outcome: Lose, Action: SetAmount, Value: 0.01},
			{Trigger: StreakAbove, Count: 2, Outcome: Win, Action: IncreasePct, Value: 100},
			{Trigger: EveryStreak, Count: 2, Outcome: Win, Action: ResetAmount},
			{Trigger: StreakAbove, Count: 4, Outcome: Win, Action: ResetAmount},
		},
	}
}

func TestRunnerDuckDicePreset(t *testing.T) {
	r := NewRunner(duckdicePreset(), 100, true)
	steps := []struct {
		won  bool
		want float64
	}{
		{true, 1},     // win 1: nothing fires, keep
		{true, 1},     // win 2: every streak of 2 → reset
		{true, 2},     // win 3: streak above 2 → +100%
		{true, 1},     // win 4: +100% then every-streak-of-2 reset
		{true, 1},     // win 5: +100% then streak above 4 → reset
		{false, 0.01}, // loss: set amount
		{false, 0.01},
		{true, 0.01}, // win 1 again: keep
	}
	for i, s := range steps {
		r.Record(s.won, 0, 100)
		if !near(r.Amount, s.want) {
			t.Fatalf("step %d: amount %v, want %v", i+1, r.Amount, s.want)
		}
	}
	if r.Bets != 8 || r.Wins != 6 || r.WinStreak != 1 {
		t.Errorf("counters: %+v", r)
	}
}

func TestRunnerBaseRuleWithoutConditions(t *testing.T) {
	r := NewRunner(&BettingProfile{BaseBet: 1, OnLossAction: OnLossMultiply, LossMultiplier: 2}, 0, true)
	for _, want := range []float64{2, 4, 8} {
		r.Record(false, -1, 0)
		if !near(r.Amount, want) {
			t.Fatalf("got %v, want %v", r.Amount, want)
		}
	}
	r.Record(true, 1, 0)
	if !near(r.Amount, 1) {
		t.Fatalf("win reset: got %v", r.Amount)
	}
}

func TestRunnerSideChanceAndStop(t *testing.T) {
	p := &BettingProfile{BaseBet: 1, Chance: 49.5, OnLossAction: OnLossReset, Conditions: []Condition{
		{Trigger: EveryN, Count: 2, Outcome: Any, Action: SwitchSide},
		{Trigger: FirstStreakN, Count: 3, Outcome: Lose, Action: SetChance, Value: 90},
		{Trigger: FirstStreakN, Count: 4, Outcome: Lose, Action: Stop},
	}}
	r := NewRunner(p, 0, true)
	r.Record(false, -1, 0)
	if !r.High {
		t.Fatal("switched after 1 bet")
	}
	r.Record(false, -1, 0)
	if r.High {
		t.Fatal("did not switch after 2 bets")
	}
	r.Record(false, -1, 0)
	if r.Chance != 90 {
		t.Fatalf("chance %v, want 90", r.Chance)
	}
	if stop, _ := r.Record(false, -1, 0); !stop {
		t.Fatal("did not stop on 4th loss in a row")
	}
}

func TestRunnerProfitStopAndCap(t *testing.T) {
	p := &BettingProfile{BaseBet: 1, MaxBet: 3, TakeProfit: 2, OnWinAction: OnWinKeep, Conditions: []Condition{
		{Trigger: EveryN, Count: 1, Outcome: Win, Action: IncreasePct, Value: 200},
	}}
	r := NewRunner(p, 0, true)
	if stop, _ := r.Record(true, 1, 0); stop || !near(r.Amount, 3) {
		t.Fatalf("amount %v (cap 3), stop %v", r.Amount, stop)
	}
	if stop, why := r.Record(true, 1, 0); !stop || why != "take profit reached" {
		t.Fatalf("stop %v %q", stop, why)
	}
}

func TestConditionValidate(t *testing.T) {
	bad := []Condition{
		{Trigger: EveryStreak, Count: 2, Outcome: Any, Action: Stop},
		{Trigger: EveryN, Count: 0, Outcome: Win, Action: Stop},
		{Trigger: EveryN, Count: 1, Outcome: Win, Action: DecreasePct, Value: 100},
		{Trigger: EveryN, Count: 1, Outcome: Win, Action: SetChance, Value: 99},
		{Trigger: "Sometimes", Count: 1, Outcome: Win, Action: Stop},
	}
	for _, c := range bad {
		if c.Validate() == nil {
			t.Errorf("accepted %v", c)
		}
	}
	if err := (Condition{Trigger: StreakAbove, Count: 0, Outcome: Lose, Action: ResetAmount}).Validate(); err != nil {
		t.Errorf("streak above 0: %v", err)
	}
}

func TestConditionsRoundTrip(t *testing.T) {
	pm, _ := NewProfileManager(t.TempDir())
	p := duckdicePreset()
	p.ID = "preset"
	if err := pm.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	got, err := pm.LoadProfile("preset")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(p) {
		b, _ := json.Marshal(got)
		t.Fatalf("round trip changed profile: %s", b)
	}
	c := got.Clone()
	c.Conditions[0].Value = 9
	if got.Conditions[0].Value == 9 {
		t.Fatal("Clone shares the conditions slice")
	}
}

func TestPercentLimits(t *testing.T) {
	p := &BettingProfile{BaseBet: 1, StopLossPct: 10, TakeProfitPct: 3}
	if sl, tp := p.Limits(1000); sl != 100 || tp != 30 {
		t.Fatalf("limits %v %v, want 100 30", sl, tp)
	}
	p.StopLoss = 50 // fixed and % both set: tighter wins
	if sl, _ := p.Limits(1000); sl != 50 {
		t.Fatalf("tighter stop loss: got %v", sl)
	}
	if sl, tp := (&BettingProfile{StopLossPct: 10}).Limits(0); sl != 0 || tp != 0 {
		t.Fatal("percentages must be ignored without a start balance")
	}

	r := NewRunner(&BettingProfile{BaseBet: 1, TakeProfitPct: 3}, 1000, true)
	if stop, _ := r.Record(true, 29, 1029); stop {
		t.Fatal("stopped below 3%")
	}
	if stop, why := r.Record(true, 1, 1030); !stop || why != "take profit reached" {
		t.Fatalf("stop %v %q at +30 on 1000", stop, why)
	}
	r = NewRunner(&BettingProfile{BaseBet: 1, StopLossPct: 10}, 1000, true)
	if stop, why := r.Record(false, -100, 900); !stop || why != "stop loss reached" {
		t.Fatalf("stop %v %q at -100 on 1000", stop, why)
	}
}

func TestValidatePercentLimits(t *testing.T) {
	pm, _ := NewProfileManager(t.TempDir())
	p := NewDefaultProfile()
	p.StopLossPct = 101
	if err := pm.Validate(p); err != ErrInvalidStopLossPct {
		t.Errorf("got %v", err)
	}
	p.StopLossPct, p.TakeProfitPct = 10, -1
	if err := pm.Validate(p); err != ErrInvalidTakeProfitPct {
		t.Errorf("got %v", err)
	}
}

func TestSetMin(t *testing.T) {
	p := &BettingProfile{BaseBet: 5, BankrollPercent: 2, OnWinAction: OnWinKeep, OnLossAction: OnLossKeep,
		Conditions: []Condition{{Trigger: EveryN, Count: 1, Outcome: Lose, Action: SetMin}}}
	if !p.UsesMinBet() {
		t.Fatal("UsesMinBet = false")
	}
	r := NewRunner(p, 1000, true) // 2% of 1000 = 20
	r.MinBet = 0.01
	r.Record(false, -20, 980)
	if !near(r.Amount, 0.01) {
		t.Fatalf("got %v, want live minimum 0.01", r.Amount)
	}
	r.MinBet = 0.03 // minimum changed
	r.Record(false, -0.01, 980)
	if !near(r.Amount, 0.03) {
		t.Fatalf("got %v, want updated minimum 0.03", r.Amount)
	}
	r.MinBet = 0
	r.Record(false, -0.03, 980)
	if !near(r.Amount, 5) {
		t.Fatalf("got %v, want BaseBet fallback 5", r.Amount)
	}
}
