package profile

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestNext(t *testing.T) {
	p := &BettingProfile{BaseBet: 1, OnLossAction: OnLossMultiply, LossMultiplier: 2}
	if got := p.Next(4, false, 100); !near(got, 8) {
		t.Errorf("multiply: got %v, want 8", got)
	}
	if got := p.Next(8, true, 100); !near(got, 1) {
		t.Errorf("win resets: got %v, want 1", got)
	}

	p.OnLossAction, p.LossMultiplier = OnLossAdd, 1
	if got := p.Next(3, false, 100); !near(got, 4) {
		t.Errorf("add: got %v, want 4", got)
	}

	p.OnLossAction = OnLossReset
	if got := p.Next(3, false, 100); !near(got, 1) {
		t.Errorf("reset: got %v, want 1", got)
	}
}

func TestMaxBetCaps(t *testing.T) {
	p := &BettingProfile{BaseBet: 1, OnLossAction: OnLossMultiply, LossMultiplier: 2, MaxBet: 5}
	if got := p.Next(4, false, 100); !near(got, 5) {
		t.Errorf("got %v, want cap 5", got)
	}
	p.BankrollPercent = 10
	if got := p.Base(1000); !near(got, 5) {
		t.Errorf("bankroll base capped: got %v, want 5", got)
	}
}

func TestBankrollPercent(t *testing.T) {
	p := &BettingProfile{BaseBet: 0.5, BankrollPercent: 1, OnLossAction: OnLossMultiply, LossMultiplier: 2}
	if got := p.Base(200); !near(got, 2) {
		t.Errorf("1%% of 200: got %v, want 2", got)
	}
	if got := p.Base(10); !near(got, 0.5) {
		t.Errorf("BaseBet is the floor: got %v, want 0.5", got)
	}
	// After a win the bet follows the new balance.
	if got := p.Next(2, true, 300); !near(got, 3) {
		t.Errorf("win at balance 300: got %v, want 3", got)
	}
	// Add steps by the bankroll-sized base.
	p.OnLossAction, p.LossMultiplier = OnLossAdd, 1
	if got := p.Next(2, false, 200); !near(got, 4) {
		t.Errorf("add with bankroll base: got %v, want 4", got)
	}
}

func TestShouldStop(t *testing.T) {
	p := &BettingProfile{StopLoss: 1, TakeProfit: 2}
	for _, c := range []struct {
		profit float64
		stop   bool
	}{{0, false}, {1.99, false}, {2, true}, {-0.99, false}, {-1, true}} {
		if got, _ := p.ShouldStop(c.profit); got != c.stop {
			t.Errorf("profit %v: stop=%v, want %v", c.profit, got, c.stop)
		}
	}
	off := &BettingProfile{}
	if stop, _ := off.ShouldStop(-1e9); stop {
		t.Error("zero limits must not stop")
	}
}

func TestValidateBankrollPercent(t *testing.T) {
	pm, _ := NewProfileManager(t.TempDir())
	p := NewDefaultProfile()
	p.BankrollPercent = 101
	if err := pm.Validate(p); err != ErrInvalidBankrollPct {
		t.Errorf("got %v, want ErrInvalidBankrollPct", err)
	}
	p.BankrollPercent = 0.5
	if err := pm.Validate(p); err != nil {
		t.Errorf("valid percent rejected: %v", err)
	}
}
